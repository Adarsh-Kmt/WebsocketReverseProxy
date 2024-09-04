package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/rwmutex"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/types"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/util"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type ReverseProxy struct {
	EndServerPool        []EndServer
	HealthyEndServerPool []EndServer //contains healthy end server structs.

	HESPMutex    *sync.Mutex     // mutex used to write to healthy end server pool.
	TESWaitGroup *sync.WaitGroup // wait group for TestEndServer go routines.

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

func InitializeReverseProxy() http.Handler {

	es1 := InitializeEndServer("es1_hc:8080")
	es2 := InitializeEndServer("es2_hc:8080")
	es3 := InitializeEndServer("es3_hc:8080")

	gcid := 0
	esPool := []EndServer{es1, es2, es3}

	rp := &ReverseProxy{
		EndServerPool:        esPool,
		HealthyEndServerPool: []EndServer{},
		HESPMutex:            &sync.Mutex{},
		TESWaitGroup:         &sync.WaitGroup{},
		RWMutex:              rwmutex.InitializeReadWriteMutex(),
		GCIDMutex:            &sync.Mutex{},
		GlobalConnectionId:   &gcid,
		logger:               log.New(os.Stdout, "LOAD_BALANCER : ", 0),
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

	return mux

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

	rp.TESWaitGroup = &sync.WaitGroup{}

	hesPool := make([]EndServer, 0, len(rp.EndServerPool))
	//log.Println("healthy end server pool is empty again.")

	rp.HealthyEndServerPool = hesPool
	for _, es := range rp.EndServerPool {

		rp.TESWaitGroup.Add(1)
		go rp.TestEndServer(es)
	}

	rp.TESWaitGroup.Wait()
	rp.logger.Println("finished health check.")

	rp.HESPMutex.Lock()
	rp.logger.Printf("length of the healthy end server list after health check: %d", len(rp.HealthyEndServerPool))
	rp.HESPMutex.Unlock()

	rp.RWMutex.WriteUnlock()

}

/*
go routine used to check wether an end server is online, then writes to HealthyEndServerPool in a thread-safe manner.
*/
func (rp *ReverseProxy) TestEndServer(es EndServer) error {

	defer rp.TESWaitGroup.Done()

	response, err := http.Get(fmt.Sprintf("http://" + es.EndServerAddress + "/healthCheck"))

	if err != nil {
		rp.logger.Println(es.EndServerAddress + " health check error: " + err.Error())
		return err
	}

	var hcr types.HealthCheckResponse
	respBody, err := io.ReadAll(response.Body)
	json.Unmarshal(respBody, &hcr)

	if err != nil {
		rp.logger.Println("error while json decoding health check response: " + err.Error())
		return err
	}
	rp.logger.Printf("response status received from %s : %d\n", es.EndServerAddress, hcr.Status)

	rp.HESPMutex.Lock()
	rp.HealthyEndServerPool = append(rp.HealthyEndServerPool, es)
	//log.Printf("healthy end server length %d\n", len(rp.HealthyEndServerPool))
	rp.HESPMutex.Unlock()

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

	rp.HESPMutex.Lock()
	//log.Printf("healthy end server len %d\n", len(rp.HealthyEndServerPool))
	endServerId := endServerWebsocketConnId % len(rp.HealthyEndServerPool)
	es := rp.HealthyEndServerPool[endServerId]
	rp.HESPMutex.Unlock()

	rp.logger.Printf("user connected to end server %s", es.EndServerAddress)

	rp.RWMutex.ReadUnlock()

	url := url.URL{Scheme: "ws", Host: es.EndServerAddress, Path: "/sendMessage"}

	header := util.InitializeHeaders(r)
	endServerWebsocketConn, _, err := websocket.DefaultDialer.Dial(url.String(), header)

	if err != nil {

		rp.logger.Printf("error while establishing end server websocket connection with address %s : %s ", es.EndServerAddress, err.Error())
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
