package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/rwmutex"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/server"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/types"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/util"
	"github.com/gookit/ini/v2"
)

type HTTPHandler struct {
	HTTPServerPool        []server.HTTPServer
	HealthyHTTPServerPool []server.HTTPServer //contains healthy end server structs.

	HealthyServerIdChannel   chan int
	UnhealthyServerIdChannel chan int

	GlobalRequestId *int
	GRIDMutex       *sync.Mutex // mutex for updating the global connection ID.

	healthCheckClient http.Client
	/*
		reader-writer mutex used to provide synchronization between HealthCheck go routine (writer) and ServeHTTP go routines (readers)
	*/
	RWMutex *rwmutex.ReadWriteMutex

	logger *log.Logger
}

func (httph *HTTPHandler) HealthCheck() {

	hesPool := make([]server.HTTPServer, 0, len(httph.HTTPServerPool))

	for _, es := range httph.HTTPServerPool {

		go httph.TestHTTPServer(es)
	}

	serversResponded := 0

	for serversResponded != len(httph.HTTPServerPool) {

		select {

		case serverId := <-httph.HealthyServerIdChannel:
			hesPool = append(hesPool, httph.HTTPServerPool[serverId-1])
			serversResponded++

		case <-httph.UnhealthyServerIdChannel:
			serversResponded++

		}

	}
	httph.logger.Println("finished health check.")
	httph.logger.Printf("length of the healthy http server list after health check: %d", len(hesPool))

	httph.RWMutex.WriteLock()
	httph.HealthyHTTPServerPool = hesPool

	httph.RWMutex.WriteUnlock()

}

/*
go routine used to check wether an end server is online, then writes to HealthyEndServerPool in a thread-safe manner.
*/
func (httph *HTTPHandler) TestHTTPServer(s server.HTTPServer) {

	response, err := httph.healthCheckClient.Get(fmt.Sprintf("http://" + s.Addr + "/healthCheck"))

	if err != nil {
		httph.logger.Println(s.Addr + " health check error: " + err.Error())
		httph.UnhealthyServerIdChannel <- s.ServerId
		return
	}

	var hcr types.HealthCheckResponse
	respBody, err := io.ReadAll(response.Body)
	response.Body.Close()
	json.Unmarshal(respBody, &hcr)

	if err != nil {
		httph.logger.Println("error while json decoding health check response: " + err.Error())
		httph.UnhealthyServerIdChannel <- s.ServerId
		return
	}
	httph.logger.Printf("response status received from %s : %d\n", s.Addr, hcr.Status)

	if hcr.Status == 200 {
		httph.HealthyServerIdChannel <- s.ServerId
	} else {
		httph.UnhealthyServerIdChannel <- s.ServerId
	}

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

	httpServerPool, err := server.ConfigureHTTPServers(hs)

	if err != nil {
		return nil, err
	}

	grid := 0

	hh := &HTTPHandler{
		HTTPServerPool:           httpServerPool,
		HealthyHTTPServerPool:    []server.HTTPServer{},
		HealthyServerIdChannel:   make(chan int),
		UnhealthyServerIdChannel: make(chan int),
		RWMutex:                  rwmutex.InitializeReadWriteMutex(),
		GRIDMutex:                &sync.Mutex{},
		GlobalRequestId:          &grid,
		logger:                   log.New(os.Stdout, "HTTP_HANDLER :      ", 0),
		healthCheckClient:        http.Client{Timeout: 20 * time.Millisecond},
	}

	periodicFunc := func() {

		for {
			hh.HealthCheck()
			time.Sleep(50 * time.Millisecond)
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

	/*
		done channel is used to wait for worker to send request to server, and write response to ResponseWriter.
		ServeHTTP func needs to wait, as ResponseWriter can only be accessed by Worker go routine without giving superflous error while ServeHTTP func is still in execution.
	*/

	doneChannel := make(chan struct{})

	/*
		the following for loop is used to spawn more workers after a timeout.
		If the maximum number of workers already spawned, then retry deducted.
		if number of retries reaches 0, then request is not serviced.

	*/

	retries := 5

	for {

		select {

		case serverJobChannel <- server.Job{ResponseWriter: w, Request: r, Done: doneChannel}:
			<-doneChannel
			return

		case <-time.After(time.Duration(100) * time.Millisecond):

			httpServer.Logger.Printf("attempting to spawn more workers for HTTP server %d...", httpServer.ServerId)

			httpServer.WorkerCountMutex.Lock()

			if *httpServer.WorkerCount == httpServer.MaxWorkerCount {

				retries--
				httpServer.Logger.Printf("maximum worker count reached.")
				if retries == 0 {

					util.WriteJSON(w, 500, map[string]string{"error": "internal server error."})
					httpServer.WorkerCountMutex.Unlock()
					return
				}

				httpServer.WorkerCountMutex.Unlock()

			} else {
				*httpServer.WorkerCount++
				workerId := *httpServer.WorkerCount
				httpServer.WorkerCountMutex.Unlock()

				newWorker := httpServer.SpawnHTTPWorker(workerId, httpServer.WorkerTimeout, httpServer.Logger, httpServer.WorkerCount, httpServer.WorkerCountMutex)

				go newWorker.ProcessHTTPRequest()
			}

		}
	}

}
