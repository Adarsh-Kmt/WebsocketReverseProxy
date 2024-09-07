package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
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

	HWSPMutex    *sync.Mutex     // mutex used to write to healthy end server pool.
	TWSWaitGroup *sync.WaitGroup // wait group for TestServer go routines.

	GlobalConnectionId *int
	GCIDMutex          *sync.Mutex // mutex for updating the global connection ID.

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

func ConfigureWebsocketHandler(cfgFilePath string) (http.Handler, error) {

	if _, err := os.Stat(cfgFilePath); os.IsNotExist(err) {
		log.Fatalf("Config file does not exist")
	}
	err := ini.LoadExists(cfgFilePath)

	if err != nil {

		return nil, fmt.Errorf("no .ini file found at file path %s", cfgFilePath)
	}

	cfg := ini.Default()

	ws := cfg.Section("websocket")

	wsServerPool := make([]server.WebsocketServer, 0)

	serverId := 1
	for key, srvAddr := range ws {

		if !strings.HasPrefix(key, "server") {
			return nil, fmt.Errorf("format for websocket section:\n\n[websocket]\nserver{number}={Host:Port}")
		}

		wsServerPool = append(wsServerPool, server.InitializeWebsocketServer(srvAddr, 1))
		serverId++
	}

	gcid := 0
	wh := &WebsocketHandler{
		WebsocketServerPool:        wsServerPool,
		HealthyWebsocketServerPool: []server.WebsocketServer{},
		HWSPMutex:                  &sync.Mutex{},
		TWSWaitGroup:               &sync.WaitGroup{},
		RWMutex:                    rwmutex.InitializeReadWriteMutex(),
		GCIDMutex:                  &sync.Mutex{},
		GlobalConnectionId:         &gcid,
		logger:                     log.New(os.Stdout, "WEBSOCKET_HANDLER : ", 0),
	}

	periodicFunc := func() {

		for {
			wh.HealthCheck()
			time.Sleep(10 * time.Second)
		}
	}

	go periodicFunc()

	return wh, nil

}

func (wh *WebsocketHandler) HealthCheck() {

	wh.RWMutex.WriteLock()

	wh.TWSWaitGroup = &sync.WaitGroup{}

	hesPool := make([]server.WebsocketServer, 0, len(wh.WebsocketServerPool))

	wh.HealthyWebsocketServerPool = hesPool
	for _, es := range wh.WebsocketServerPool {

		wh.TWSWaitGroup.Add(1)
		go wh.TestWebsocketServer(es)
	}

	wh.TWSWaitGroup.Wait()
	wh.logger.Println("finished health check.")

	wh.HWSPMutex.Lock()
	wh.logger.Printf("length of the healthy websocket server list after health check: %d", len(wh.HealthyWebsocketServerPool))
	wh.HWSPMutex.Unlock()

	wh.RWMutex.WriteUnlock()

}

/*
go routine used to check wether an end server is online, then writes to HealthyEndServerPool in a thread-safe manner.
*/
func (wh *WebsocketHandler) TestWebsocketServer(s server.WebsocketServer) error {

	defer wh.TWSWaitGroup.Done()

	response, err := http.Get(fmt.Sprintf("http://" + s.Addr + "/healthCheck"))

	if err != nil {
		wh.logger.Println(s.Addr + " health check error: " + err.Error())
		return err
	}

	var hcr types.HealthCheckResponse
	respBody, err := io.ReadAll(response.Body)
	json.Unmarshal(respBody, &hcr)

	if err != nil {
		wh.logger.Println("error while json decoding health check response: " + err.Error())
		return err
	}
	wh.logger.Printf("response status received from %s : %d\n", s.Addr, hcr.Status)

	wh.HWSPMutex.Lock()
	wh.HealthyWebsocketServerPool = append(wh.HealthyWebsocketServerPool, s)
	wh.HWSPMutex.Unlock()

	return nil
}

/*
ServeHTTP func used to handle user connection attempts.
spawns 2 go routines:

1) StartListeningToServer : listens to ws server websocket connection, writes to user websocket connection.
2) StartListeningToUser		 : listens to user websocket connection, writes to ws server websocket connection.
*/

func (wh *WebsocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	wh.logger.Printf("received %s request, path %s", r.Method, r.URL.Path)

	wh.GCIDMutex.Lock()
	*wh.GlobalConnectionId++
	userWebsocketConnId := *wh.GlobalConnectionId
	*wh.GlobalConnectionId++
	serverWebsocketConnId := *wh.GlobalConnectionId
	wh.GCIDMutex.Unlock()

	wh.logger.Printf("websocket server websocket connection id %d\n", serverWebsocketConnId)
	wh.logger.Printf("user websocket connection id %d\n", userWebsocketConnId)

	wh.RWMutex.ReadLock()

	serverId := serverWebsocketConnId % len(wh.HealthyWebsocketServerPool)
	s := wh.HealthyWebsocketServerPool[serverId]

	wh.logger.Printf("user connected to server %s", s.Addr)

	wh.RWMutex.ReadUnlock()

	url := url.URL{Scheme: "ws", Host: s.Addr, Path: r.URL.Path}

	header := util.InitializeHeaders(r)
	WSServerWebsocketConn, _, err := websocket.DefaultDialer.Dial(url.String(), header)

	if err != nil {

		wh.logger.Printf("error while establishing server websocket connection with address %s : %s ", s.Addr, err.Error())
		util.WriteJSON(w, 500, map[string]string{"error": "internal server error"})
		return
	}
	userWebsocketConn, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		wh.logger.Printf("error while upgrading user websocket connection: %s ", err.Error())
		util.WriteJSON(w, 500, map[string]string{"error": "internal server error"})
		return

	}

	go util.StartListeningToServer(userWebsocketConn, WSServerWebsocketConn, s.Logger)
	go util.StartListeningToUser(userWebsocketConn, WSServerWebsocketConn, s.Logger)

	wh.logger.Printf("responded to request")

}
