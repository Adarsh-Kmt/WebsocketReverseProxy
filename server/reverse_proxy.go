package server

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
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/types"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/util"
	"github.com/gookit/ini/v2"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type ReverseProxy struct {
	Addr                       string
	WebsocketServerPool        []Server
	HealthyWebsocketServerPool []Server //contains healthy end server structs.

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

func ConfigureReverseProxy(cfgFilePath string) (*ReverseProxy, error) {

	if _, err := os.Stat(cfgFilePath); os.IsNotExist(err) {
		log.Fatalf("Config file does not exist")
	}
	err := ini.LoadExists(cfgFilePath)

	if err != nil {

		return nil, fmt.Errorf("no .ini file found at file path %s", cfgFilePath)
	}

	cfg := ini.Default()

	host := cfg.String("frontend.host")

	if host == "" {

		return nil, fmt.Errorf("frontend.host cannot be empty")
	}

	port := cfg.String("frontend.port")

	if port == "" {

		return nil, fmt.Errorf("frontend.port cannot be empty")

	}

	addr := cfg.String("frontend.host") + ":" + cfg.String("frontend.port")

	ws := cfg.Section("websocket")

	wsServerPool := make([]Server, 0)

	for key, srvAddr := range ws {

		if !strings.HasPrefix(key, "server") {
			return nil, fmt.Errorf("format for websocket section:\nserver{number}={Address}")
		}

		wsServerPool = append(wsServerPool, InitializeServer(srvAddr))
	}

	gcid := 0
	rp := &ReverseProxy{
		Addr:                       addr,
		WebsocketServerPool:        wsServerPool,
		HealthyWebsocketServerPool: []Server{},
		HWSPMutex:                  &sync.Mutex{},
		TWSWaitGroup:               &sync.WaitGroup{},
		RWMutex:                    rwmutex.InitializeReadWriteMutex(),
		GCIDMutex:                  &sync.Mutex{},
		GlobalConnectionId:         &gcid,
		logger:                     log.New(os.Stdout, "LOAD_BALANCER : ", 0),
	}

	return rp, nil

}
func NewReverseProxy() (handler http.Handler, addr string, err error) {

	rp, err := ConfigureReverseProxy("/app/reverse-proxy-config.ini")

	if err != nil {
		return nil, "", err
	}

	periodicFunc := func() {

		for {
			rp.HealthCheck()
			time.Sleep(10 * time.Second)
		}

	}

	go periodicFunc()
	mux := mux.NewRouter()
	mux.Use(rp.LoggingMiddleware)
	mux.HandleFunc("/sendMessage", util.MakeHttpHandlerFunc(rp.ConnectUser))

	return mux, rp.Addr, nil

}

func (rp *ReverseProxy) LoggingMiddleware(handler http.Handler) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		rp.logger.Printf("received %s request, path %s", r.Method, r.URL.Path)
		handler.ServeHTTP(w, r)
		rp.logger.Printf("responded to request")
	})
}
func (rp *ReverseProxy) HealthCheck() {

	rp.RWMutex.WriteLock()

	rp.TWSWaitGroup = &sync.WaitGroup{}

	hesPool := make([]Server, 0, len(rp.WebsocketServerPool))
	//log.Println("healthy end server pool is empty again.")

	rp.HealthyWebsocketServerPool = hesPool
	for _, es := range rp.WebsocketServerPool {

		rp.TWSWaitGroup.Add(1)
		go rp.TestEndServer(es)
	}

	rp.TWSWaitGroup.Wait()
	rp.logger.Println("finished health check.")

	rp.HWSPMutex.Lock()
	rp.logger.Printf("length of the healthy end server list after health check: %d", len(rp.HealthyWebsocketServerPool))
	rp.HWSPMutex.Unlock()

	rp.RWMutex.WriteUnlock()

}

/*
go routine used to check wether an end server is online, then writes to HealthyEndServerPool in a thread-safe manner.
*/
func (rp *ReverseProxy) TestEndServer(s Server) error {

	defer rp.TWSWaitGroup.Done()

	response, err := http.Get(fmt.Sprintf("http://" + s.ServerAddress + "/healthCheck"))

	if err != nil {
		rp.logger.Println(s.ServerAddress + " health check error: " + err.Error())
		return err
	}

	var hcr types.HealthCheckResponse
	respBody, err := io.ReadAll(response.Body)
	json.Unmarshal(respBody, &hcr)

	if err != nil {
		rp.logger.Println("error while json decoding health check response: " + err.Error())
		return err
	}
	rp.logger.Printf("response status received from %s : %d\n", s.ServerAddress, hcr.Status)

	rp.HWSPMutex.Lock()
	rp.HealthyWebsocketServerPool = append(rp.HealthyWebsocketServerPool, s)
	//log.Printf("healthy end server length %d\n", len(rp.HealthyEndServerPool))
	rp.HWSPMutex.Unlock()

	return nil
}

/*
ConnectUser func used to handle user connection attempts.
spawns 2 go routines:

1) StartListeningToServer : listens to end server websocket connection, writes to user websocket connection.
2) StartListeningToUser		 : listens to user websocket connection, writes to end server websocket connection.
*/

func (rp *ReverseProxy) ConnectUser(w http.ResponseWriter, r *http.Request) *util.HTTPError {

	rp.RWMutex.ReadLock()

	rp.GCIDMutex.Lock()
	*rp.GlobalConnectionId++
	userWebsocketConnId := *rp.GlobalConnectionId
	*rp.GlobalConnectionId++
	endServerWebsocketConnId := *rp.GlobalConnectionId
	rp.GCIDMutex.Unlock()

	rp.logger.Printf("end server websocket connection id %d\n", endServerWebsocketConnId)
	rp.logger.Printf("user websocket connection id %d\n", userWebsocketConnId)

	rp.HWSPMutex.Lock()
	//log.Printf("healthy end server len %d\n", len(rp.HealthyEndServerPool))
	endServerId := endServerWebsocketConnId % len(rp.HealthyWebsocketServerPool)
	s := rp.HealthyWebsocketServerPool[endServerId]
	rp.HWSPMutex.Unlock()

	rp.logger.Printf("user connected to end server %s", s.ServerAddress)

	rp.RWMutex.ReadUnlock()

	url := url.URL{Scheme: "ws", Host: s.ServerAddress, Path: "/sendMessage"}

	header := util.InitializeHeaders(r)
	endServerWebsocketConn, _, err := websocket.DefaultDialer.Dial(url.String(), header)

	if err != nil {

		rp.logger.Printf("error while establishing end server websocket connection with address %s : %s ", s.ServerAddress, err.Error())
		return &util.HTTPError{Status: 500, Error: "internal server error"}
	}
	userWebsocketConn, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		rp.logger.Printf("error while upgrading user websocket connection: %s ", err.Error())
		return &util.HTTPError{Status: 500, Error: "internal server error"}

	}

	go util.StartListeningToServer(userWebsocketConn, endServerWebsocketConn)
	go util.StartListeningToUser(userWebsocketConn, endServerWebsocketConn)

	return nil

}
