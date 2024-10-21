package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
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
)

type HTTPHandler struct {
	HTTPServerPool        []server.HTTPServer
	HealthyHTTPServerPool []server.HTTPServer //contains healthy end server structs.

	Algorithm                string
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

func (httph *HTTPHandler) HealthCheck(wg *sync.WaitGroup) {

	defer wg.Done()

	hsPool := make([]server.HTTPServer, 0, len(httph.HTTPServerPool))
	hsIdPool := make([]int, 0)

	for _, es := range httph.HTTPServerPool {

		go httph.TestHTTPServer(es)
	}

	serversResponded := 0

	for serversResponded != len(httph.HTTPServerPool) {

		select {

		case serverId := <-httph.HealthyServerIdChannel:
			//httph.logger.Printf("server %d is healthy...", serverId)
			//hesPool = append(hesPool, httph.HTTPServerPool[serverId-1])
			hsIdPool = append(hsIdPool, serverId)
			serversResponded++

		case <-httph.UnhealthyServerIdChannel:
			serversResponded++

		}

	}

	if httph.Algorithm == "round-robin" {
		sort.Ints(hsIdPool)
	}
	httph.logger.Printf("length of healthy server id list : %d", len(hsIdPool))
	for index := range hsIdPool {
		serverId := hsIdPool[index]
		hsPool = append(hsPool, httph.HTTPServerPool[serverId-1])
	}
	//httph.logger.Println("finished health check.")
	//httph.logger.Printf("length of the healthy http server list after health check: %d", len(hesPool))

	httph.RWMutex.WriteLock()
	httph.HealthyHTTPServerPool = hsPool

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
	//httph.logger.Printf("response status received from %s : %d\n", s.Addr, hcr.Status)

	if hcr.Status == 200 {
		httph.HealthyServerIdChannel <- s.ServerId
	} else {
		httph.UnhealthyServerIdChannel <- s.ServerId
	}

}

func ConfigureHTTPHandler() (http.Handler, error) {

	cfgFilePath := "/prod/reverse-proxy-config.ini"

	if _, err := os.Stat(cfgFilePath); os.IsNotExist(err) {
		log.Fatalf("Config file does not exist")
	}
	err := ini.LoadExists(cfgFilePath)

	if err != nil {

		return nil, fmt.Errorf("no .ini file found at file path %s", cfgFilePath)
	}

	cfg := ini.Default()

	hs := cfg.Section("http")

	hcEnabledString := strings.ToLower(cfg.String("http.enable_health_check"))

	var healthCheckEnabled bool

	if hcEnabledString == "" || hcEnabledString == "false" {
		healthCheckEnabled = false
	} else if hcEnabledString == "true" {
		healthCheckEnabled = true
	} else {
		return nil, fmt.Errorf("invalid config, websocket.enable_health_check should be true/false")
	}

	healthCheckInterval := 10
	hcIntervalString := cfg.String("websocket.health_check_interval")

	if hcIntervalString != "" {
		val, err := strconv.Atoi(hcIntervalString)
		if err != nil {
			return nil, fmt.Errorf("invalid config, websocket.health_check_interval should be a valid integer")
		}
		healthCheckInterval = val
	}

	algorithm := "random"

	if algo := cfg.String("http.algorithm"); algo != "" {

		if algo != "round-robin" && algo != "random" {
			return nil, fmt.Errorf("format for http section:\n\n[http]\nalgorithm={round-robin/random}")
		}
		algorithm = algo

	}
	httpServerPool, err := server.ConfigureHTTPServers(hs)

	if err != nil {
		return nil, err
	}

	grid := 0
	lg := log.New(os.Stdout, "HTTP_HANDLER :      ", 0)
	hh := &HTTPHandler{
		HTTPServerPool:           httpServerPool,
		HealthyHTTPServerPool:    []server.HTTPServer{},
		HealthyServerIdChannel:   make(chan int),
		UnhealthyServerIdChannel: make(chan int),
		RWMutex:                  rwmutex.InitializeReadWriteMutex(),
		GRIDMutex:                &sync.Mutex{},
		GlobalRequestId:          &grid,
		logger:                   lg,
		healthCheckClient:        util.InitializeHandlerHTTPClient(lg),
		Algorithm:                algorithm,
	}

	periodicFunc := func(healthCheckInterval int) {

		for {
			wg := &sync.WaitGroup{}
			wg.Add(1)
			hh.HealthCheck(wg)
			wg.Wait()
			time.Sleep(5 * time.Second)
		}
	}

	if healthCheckEnabled {
		go periodicFunc(healthCheckInterval)
	}

	return hh, nil
}

func (httph *HTTPHandler) ApplyLoadBalancingAlgorithm() server.HTTPServer {

	httph.GRIDMutex.Lock()
	*httph.GlobalRequestId++
	httpRequestId := *httph.GlobalRequestId
	httph.GRIDMutex.Unlock()

	var server server.HTTPServer
	if httph.Algorithm == "round-robin" || httph.Algorithm == "random" {

		httph.RWMutex.ReadLock()

		serverId := httpRequestId % len(httph.HealthyHTTPServerPool)

		server = httph.HealthyHTTPServerPool[serverId]
		httph.logger.Printf("received request %d, forwarded to http server %d", httpRequestId, server.ServerId)

		httph.RWMutex.ReadUnlock()

	}

	return server
	// else {
	// 	httph.RWMutex.ReadLock()

	// 	server1Id := 0
	// 	server2Id := 0

	// 	for server1Id == server2Id {
	// 		server1Id = rand.IntN(len(httph.HealthyHTTPServerPool))
	// 		server2Id = rand.IntN(len(httph.HealthyHTTPServerPool))
	// 	}

	// 	server1 := httph.HealthyHTTPServerPool[server1Id]
	// 	server2 := httph.HealthyHTTPServerPool[server2Id]

	// 	httph.RWMutex.ReadUnlock()

	// 	server1.WorkerCountMutex.Lock()
	// 	server2.WorkerCountMutex.Lock()

	// 	var server server.HTTPServer

	// 	if *server1.WorkerCount <= *server2.WorkerCount {
	// 		server = server1
	// 	} else {
	// 		server = server2
	// 	}

	// 	server1.WorkerCountMutex.Unlock()
	// 	server2.WorkerCountMutex.Unlock()

	// 	httph.logger.Printf("received request %d, forwarded to http server %d", httpRequestId, server.ServerId)

	// 	return server

	// }

}

func (httph *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if r.Body == nil {
		httph.logger.Println("request body is empty.")
	}

	httpServer := httph.ApplyLoadBalancingAlgorithm()

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

	maxRetries := 5.0
	retriesLeft := 5.0

	for {

		select {

		case serverJobChannel <- server.Job{ResponseWriter: w, Request: r, Done: doneChannel}:
			<-doneChannel
			return

		case <-time.After(time.Duration(100*math.Pow(2, maxRetries-retriesLeft)) * time.Millisecond):

			httpServer.Logger.Printf("attempting to spawn more workers for HTTP server %d...", httpServer.ServerId)

			httpServer.WorkerCountMutex.Lock()

			if *httpServer.WorkerCount == httpServer.MaxWorkerCount {

				retriesLeft--
				httpServer.Logger.Printf("maximum worker count reached.")
				if retriesLeft == 0 {

					util.WriteJSON(w, 429, map[string]string{"error": "Too Many Requests."})
					httpServer.WorkerCountMutex.Unlock()
					return
				}

				httpServer.WorkerCountMutex.Unlock()

			} else {
				*httpServer.WorkerCount++
				workerId := *httpServer.WorkerCount
				httpServer.WorkerCountMutex.Unlock()

				newWorker := httpServer.SpawnHTTPWorker(workerId, httpServer.MinWorkerCount, httpServer.WorkerTimeout, httpServer.Logger, httpServer.WorkerCount, httpServer.WorkerCountMutex)

				go newWorker.ProcessHTTPRequest()
			}

		}
	}

}
