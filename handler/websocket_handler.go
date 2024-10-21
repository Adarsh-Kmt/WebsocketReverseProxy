package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"sync"
	"time"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/rwmutex"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/server"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/types"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/util"
	"github.com/gookit/ini/v2"
	"github.com/gorilla/websocket"
)

type WebsocketHandler struct {
	WebsocketServerPool        []server.WebsocketServer
	HealthyWebsocketServerPool []server.WebsocketServer //contains healthy end server structs.

	Algorithm    string
	HWSPMutex    *sync.Mutex     // mutex used to write to healthy end server pool.
	TWSWaitGroup *sync.WaitGroup // wait group for TestServer go routines.

	GlobalConnectionId *int
	GCIDMutex          *sync.Mutex // mutex for updating the global connection ID.

	healthCheckClient http.Client

	HealthyServerIdChannel   chan int
	UnhealthyServerIdChannel chan int

	/*
		reader-writer mutex used to provide synchronization between HealthCheck go routine (writer) and ConnectUser go routines (readers)
	*/
	RWMutex *rwmutex.ReadWriteMutex

	logger *log.Logger
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func ConfigureWebsocketHandler() (http.Handler, error) {

	cfgFilePath := "/prod/reverse-proxy-config.ini"

	if _, err := os.Stat(cfgFilePath); os.IsNotExist(err) {
		log.Fatalf("Config file does not exist")
	}
	err := ini.LoadExists(cfgFilePath)

	if err != nil {

		return nil, fmt.Errorf("no .ini file found at file path %s", cfgFilePath)
	}

	cfg := ini.Default()

	ws := cfg.Section("websocket")

	hcEnabledString := strings.ToLower(cfg.String("websocket.enable_health_check"))

	healthCheckEnabled := false

	if hcEnabledString == "" || hcEnabledString == "false" {
		healthCheckEnabled = false
	} else if hcEnabledString == "true" {
		healthCheckEnabled = true
	} else {
		return nil, fmt.Errorf("invalid config, websocket.enable_health_check should be true/false")
	}

	hcIntervalString := cfg.String("websocket.health_check_interval")

	healthCheckInterval := 10

	if hcIntervalString != "" {
		val, err := strconv.Atoi(hcIntervalString)
		if err != nil {
			return nil, fmt.Errorf("invalid config, websocket.health_check_interval should be a valid integer")
		}
		healthCheckInterval = val
	}

	wsServerPool, err := server.ConfigureWebsocketServers(ws)

	if err != nil {
		return nil, err
	}

	algorithm := "random"

	if algo := cfg.String("websocket.algorithm"); algo != "" {

		if algo != "round-robin" && algo != "random" {
			return nil, fmt.Errorf("format for websocket section:\n\n[websocket]\nalgorithm={round-robin/random}")
		}
		algorithm = algo

	}

	gcid := 0
	lg := log.New(os.Stdout, "WEBSOCKET_HANDLER : ", 0)
	wh := &WebsocketHandler{
		WebsocketServerPool:        wsServerPool,
		HealthyWebsocketServerPool: []server.WebsocketServer{},
		HWSPMutex:                  &sync.Mutex{},
		TWSWaitGroup:               &sync.WaitGroup{},
		RWMutex:                    rwmutex.InitializeReadWriteMutex(),
		GCIDMutex:                  &sync.Mutex{},
		GlobalConnectionId:         &gcid,
		logger:                     lg,
		healthCheckClient:          util.InitializeHandlerHTTPClient(lg),
		HealthyServerIdChannel:     make(chan int),
		UnhealthyServerIdChannel:   make(chan int),
		Algorithm:                  algorithm,
	}

	periodicFunc := func(healthCheckInterval int) {

		for {
			wg := &sync.WaitGroup{}
			wg.Add(1)
			wh.HealthCheck(wg)
			wg.Wait()
			time.Sleep(5 * time.Second)
		}
	}

	if healthCheckEnabled {
		go periodicFunc(int(healthCheckInterval))
	}

	return wh, nil

}

func (wh *WebsocketHandler) HealthCheck(wg *sync.WaitGroup) {

	defer wg.Done()
	hwsPool := make([]server.WebsocketServer, 0, len(wh.WebsocketServerPool))
	hwsIdPool := make([]int, 0)
	for _, es := range wh.WebsocketServerPool {

		go wh.TestWebsocketServer(es)
	}

	serversResponded := 0

	for serversResponded != len(wh.WebsocketServerPool) {

		select {

		case serverId := <-wh.HealthyServerIdChannel:
			//hesPool = append(hesPool, wh.WebsocketServerPool[serverId-1])
			hwsIdPool = append(hwsIdPool, serverId)
			serversResponded++

		case <-wh.UnhealthyServerIdChannel:
			serversResponded++

		}

	}

	if wh.Algorithm == "round-robin" {
		sort.Ints(hwsIdPool)
	}
	wh.logger.Printf("length of healthy server id list : %d", len(hwsIdPool))
	for serverId := range hwsIdPool {
		hwsPool = append(hwsPool, wh.WebsocketServerPool[serverId])
	}
	//wh.logger.Println("finished health check.")
	//wh.logger.Printf("length of the healthy http server list after health check: %d", len(hesPool))

	wh.RWMutex.WriteLock()
	wh.HealthyWebsocketServerPool = hwsPool

	wh.RWMutex.WriteUnlock()

}

/*
go routine used to check wether a server is online, then writes to HealthyServerId channel or UnhealthyServerId channel.
*/
func (wh *WebsocketHandler) TestWebsocketServer(s server.WebsocketServer) {

	response, err := wh.healthCheckClient.Get(fmt.Sprintf("http://" + s.Addr + "/healthCheck"))

	if err != nil {
		wh.logger.Println(s.Addr + " health check error: " + err.Error())
		wh.UnhealthyServerIdChannel <- s.ServerId
		return
	}

	var hcr types.HealthCheckResponse
	respBody, err := io.ReadAll(response.Body)
	response.Body.Close()
	json.Unmarshal(respBody, &hcr)

	if err != nil {
		wh.logger.Println("error while json decoding health check response: " + err.Error())
		wh.UnhealthyServerIdChannel <- s.ServerId
		return
	}
	//wh.logger.Printf("response status received from %s : %d\n", s.Addr, hcr.Status)

	if hcr.Status == 200 {
		wh.HealthyServerIdChannel <- s.ServerId
	} else {
		wh.UnhealthyServerIdChannel <- s.ServerId
	}

}

func (wh *WebsocketHandler) ApplyLoadBalancingAlgorithm() server.WebsocketServer {

	wh.GCIDMutex.Lock()
	*wh.GlobalConnectionId++
	userWebsocketConnId := *wh.GlobalConnectionId
	*wh.GlobalConnectionId++
	serverWebsocketConnId := *wh.GlobalConnectionId
	wh.GCIDMutex.Unlock()

	var server server.WebsocketServer
	if wh.Algorithm == "round-robin" || wh.Algorithm == "random" {

		wh.logger.Printf("websocket server websocket connection id %d\n", serverWebsocketConnId)
		wh.logger.Printf("user websocket connection id %d\n", userWebsocketConnId)

		wh.RWMutex.ReadLock()

		serverId := serverWebsocketConnId % len(wh.HealthyWebsocketServerPool)
		server = wh.HealthyWebsocketServerPool[serverId]

		wh.logger.Printf("user connected to server %s", server.Addr)

		wh.RWMutex.ReadUnlock()

	}
	return server
	// else {

	// 	wh.RWMutex.ReadLock()

	// 	server1Id := 0
	// 	server2Id := 0

	// 	for server1Id == server2Id {
	// 		server1Id = rand.IntN(len(wh.HealthyWebsocketServerPool))
	// 		server2Id = rand.IntN(len(wh.HealthyWebsocketServerPool))
	// 	}

	// 	server1 := wh.HealthyWebsocketServerPool[server1Id]
	// 	server2 := wh.HealthyWebsocketServerPool[server2Id]

	// 	wh.RWMutex.ReadUnlock()

	// 	server1.NumConnMutex.Lock()
	// 	server2.NumConnMutex.Lock()

	// 	var server server.WebsocketServer

	// 	if *server1.NumConns <= *server2.NumConns {
	// 		server = server1
	// 	} else {
	// 		server = server2
	// 	}

	// 	server1.NumConnMutex.Unlock()
	// 	server2.NumConnMutex.Unlock()

	// 	//wh.logger.Printf("received request %d, forwarded to http server %d", httpRequestId, server.ServerId)

	// 	return server
	// }

}

/*
ServeHTTP func used to handle user connection attempts.
spawns 2 go routines:

1) StartListeningToServer : listens to ws server websocket connection, writes to user websocket connection.
2) StartListeningToUser		 : listens to user websocket connection, writes to ws server websocket connection.
*/

func (wh *WebsocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	wh.logger.Printf("received %s request, path %s", r.Method, r.URL.Path)

	websocketServer := wh.ApplyLoadBalancingAlgorithm()

	url := url.URL{Scheme: "ws", Host: websocketServer.Addr, Path: r.URL.Path}

	header := util.InitializeHeaders(r)
	WSServerWebsocketConn, _, err := websocket.DefaultDialer.Dial(url.String(), header)

	if err != nil {

		wh.logger.Printf("error while establishing server websocket connection with address %s : %s ", websocketServer.Addr, err.Error())
		util.WriteJSON(w, 500, map[string]string{"error": "internal server error"})
		return
	}
	userWebsocketConn, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		wh.logger.Printf("error while upgrading user websocket connection: %s ", err.Error())
		util.WriteJSON(w, 500, map[string]string{"error": "internal server error"})
		return

	}

	go util.StartListeningToServer(userWebsocketConn, WSServerWebsocketConn, websocketServer.Logger)
	go util.StartListeningToUser(userWebsocketConn, WSServerWebsocketConn, websocketServer.Logger)

	wh.logger.Printf("responded to request")

}
