package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/rwmutex"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/server"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/types"
	"github.com/gookit/ini/v2"
)

type HTTPHandler struct {
	HTTPServerPool        []server.HTTPServer
	HealthyHTTPServerPool []server.HTTPServer //contains healthy end server structs.

	HHSPMutex    *sync.Mutex     // mutex used to write to healthy end server pool.
	THSWaitGroup *sync.WaitGroup // wait group for TestHTTPServer go routines.

	GlobalRequestId *int
	GRIDMutex       *sync.Mutex // mutex for updating the global connection ID.

	/*
		reader-writer mutex used to provide synchronization between HealthCheck go routine (writer) and ServeHTTP go routines (readers)
	*/
	RWMutex *rwmutex.ReadWriteMutex

	logger *log.Logger
}

func (httph *HTTPHandler) HealthCheck() {

	httph.RWMutex.WriteLock()

	httph.THSWaitGroup = &sync.WaitGroup{}

	hesPool := make([]server.HTTPServer, 0, len(httph.HTTPServerPool))

	httph.HealthyHTTPServerPool = hesPool
	for _, es := range httph.HTTPServerPool {

		httph.THSWaitGroup.Add(1)
		go httph.TestHTTPServer(es)
	}

	httph.THSWaitGroup.Wait()
	httph.logger.Println("finished health check.")

	httph.HHSPMutex.Lock()
	httph.logger.Printf("length of the healthy websocket server list after health check: %d", len(httph.HealthyHTTPServerPool))
	httph.HHSPMutex.Unlock()

	httph.RWMutex.WriteUnlock()

}

/*
go routine used to check wether an end server is online, then writes to HealthyEndServerPool in a thread-safe manner.
*/
func (httph *HTTPHandler) TestHTTPServer(s server.HTTPServer) error {

	defer httph.THSWaitGroup.Done()

	response, err := http.Get(fmt.Sprintf("http://" + s.Addr + "/healthCheck"))

	if err != nil {
		httph.logger.Println(s.Addr + " health check error: " + err.Error())
		return err
	}

	var hcr types.HealthCheckResponse
	respBody, err := io.ReadAll(response.Body)
	json.Unmarshal(respBody, &hcr)

	if err != nil {
		httph.logger.Println("error while json decoding health check response: " + err.Error())
		return err
	}
	httph.logger.Printf("response status received from %s : %d\n", s.Addr, hcr.Status)

	httph.HHSPMutex.Lock()
	httph.HealthyHTTPServerPool = append(httph.HealthyHTTPServerPool, s)
	httph.HHSPMutex.Unlock()

	return nil
}

func ConfigureHTTPHandler(cfgFilePath string) (http.Handler, error) {

	if _, err := os.Stat(cfgFilePath); os.IsNotExist(err) {
		log.Fatalf("Config file does not exist")
	}
	err := ini.LoadExists(cfgFilePath)

	if err != nil {

		return nil, fmt.Errorf("no .ini file found at file path %s", cfgFilePath)
	}

	cfg := ini.Default()

	hs := cfg.Section("http")

	httpServerPool := make([]server.HTTPServer, 0)

	serverId := 1
	for key, srvAddr := range hs {

		if !strings.HasPrefix(key, "server") {
			return nil, fmt.Errorf("format for http section:\n\n[http]\nserver{number}={Host:Port}")
		}

		httpServerPool = append(httpServerPool, server.InitializeHTTPServer(srvAddr, serverId))
		serverId++
	}

	grid := 0
	hh := &HTTPHandler{
		HTTPServerPool:        httpServerPool,
		HealthyHTTPServerPool: []server.HTTPServer{},
		HHSPMutex:             &sync.Mutex{},
		THSWaitGroup:          &sync.WaitGroup{},
		RWMutex:               rwmutex.InitializeReadWriteMutex(),
		GRIDMutex:             &sync.Mutex{},
		GlobalRequestId:       &grid,
		logger:                log.New(os.Stdout, "HTTP_HANDLER :      ", 0),
	}

	periodicFunc := func() {

		for {
			hh.HealthCheck()
			time.Sleep(10 * time.Second)
		}
	}

	go periodicFunc()

	return hh, nil
}

func (httph *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if r.Body == nil {
		httph.logger.Println("request body is empty.")
	}
	httph.GRIDMutex.Lock()
	*httph.GlobalRequestId++
	httpRequestId := *httph.GlobalRequestId
	httph.GRIDMutex.Unlock()

	httph.RWMutex.ReadLock()

	serverId := httpRequestId % len(httph.HealthyHTTPServerPool)

	httpServer := httph.HealthyHTTPServerPool[serverId]

	httph.RWMutex.ReadUnlock()

	httph.logger.Printf("received request %d, Method %s Path %s, forwarded to http server %d", httpRequestId, r.Method, r.URL.Path, httpServer.ServerId)
	serverJobChannel := httpServer.JobChannel

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("error while reading the body at handler %s", err.Error())
	}

	doneChannel := make(chan struct{})
	serverJobChannel <- server.Job{ResponseWriter: w, RequestBody: body, Request: r, Done: doneChannel}

	/*
		done channel is used to wait for worker to send request to server, and write response to ResponseWriter.
		ServeHTTP func needs to wait, as ResponseWriter can only be accessed without giving superflous error while ServeHTTP func is still in execution.
	*/
	<-doneChannel

}
