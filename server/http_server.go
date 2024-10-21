package server

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/util"
	"github.com/gookit/ini/v2"
)

type HTTPServer struct {
	Addr     string
	ServerId int

	MaxWorkerCount int
	MinWorkerCount int

	WorkerTimeout    int
	WorkerCount      *int
	WorkerCountMutex *sync.Mutex

	JobChannel chan Job

	Logger *log.Logger
}

type HTTPWorker struct {
	Addr     string
	WorkerId int
	Timeout  int

	JobChannel <-chan Job

	WorkerCount      *int
	WorkerCountMutex *sync.Mutex
	MinWorkerCount   int
	HTTPClient       http.Client

	logger *log.Logger
}

type Job struct {
	ResponseWriter http.ResponseWriter
	Request        *http.Request
	Done           chan struct{}
}

func InitializeHTTPServer(serverAddr string, serverId int, workerTimeout int, minWorkerCount int, maxWorkerCount int) HTTPServer {

	wc := 1
	hs := HTTPServer{
		Addr:             serverAddr,
		ServerId:         serverId,
		JobChannel:       make(chan Job, 10),
		Logger:           log.New(os.Stdout, fmt.Sprintf("HTTP SERVER %d :     ", serverId), 0),
		WorkerTimeout:    workerTimeout,
		MaxWorkerCount:   maxWorkerCount,
		MinWorkerCount:   minWorkerCount,
		WorkerCount:      &wc,
		WorkerCountMutex: &sync.Mutex{},
	}

	for workerId := 1; workerId <= minWorkerCount; workerId++ {

		worker := hs.SpawnHTTPWorker(workerId, hs.MinWorkerCount, hs.WorkerTimeout, hs.Logger, hs.WorkerCount, hs.WorkerCountMutex)
		hs.Logger.Printf("Server %d spawning Worker %d...", serverId, workerId)
		go worker.ProcessHTTPRequest()
	}

	return hs

}

func ConfigureHTTPServers(httpSection ini.Section) ([]HTTPServer, error) {

	httpServerPool := make([]HTTPServer, 0)

	serverId := 1

	var srvAddr string
	maxWorkers := 3 // default number of max workers
	minWorkers := 1
	workerTimeout := 10 // default value for worker timeout
	addrConfigured := false

	keysList := make([]string, 0)

	for key := range httpSection {
		keysList = append(keysList, key)
	}

	sort.Strings(keysList)

	for _, key := range keysList {

		val := httpSection[key]
		// Only process keys with the prefix "server"

		if key == "algorithm" || key == "enable_health_check" || key == "health_check_interval" {
			continue
		}
		if !strings.HasPrefix(key, "server") {
			return nil, fmt.Errorf("format for http section:\n\n[http]\nserver{number}_{config_name}={config}")
		}

		log.Printf("server being configured %c", key[6])
		currServerId := int(key[6] - '0')

		if serverId != currServerId {
			log.Printf("current server id %d", currServerId)
			if !addrConfigured {
				return nil, fmt.Errorf("invalid config, value of server%d_addr cannot be empty", serverId)
			}

			log.Printf("HTTP server %d configured with addr : %s worker timeout : %d max workers : %d", serverId, srvAddr, workerTimeout, maxWorkers)
			httpServerPool = append(httpServerPool, InitializeHTTPServer(srvAddr, serverId, workerTimeout, minWorkers, maxWorkers))
			serverId = currServerId
			addrConfigured = false // Reset for the next server
		}

		// Process the key to set the appropriate variables
		if strings.HasSuffix(key, "addr") {
			srvAddr = val
			addrConfigured = true

		} else if strings.HasSuffix(key, "max_workers") {
			var err error
			maxWorkers, err = strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("invalid config, server%d_max_workers value must be a valid integer", serverId)
			}

		} else if strings.HasSuffix(key, "min_workers") {

			var err error
			minWorkers, err = strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("invalid config, server%d_max_workers value must be a valid integer", serverId)
			}

		} else if strings.HasSuffix(key, "worker_timeout") {
			var err error
			workerTimeout, err = strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("invalid config, server%d_worker_timeout value must be a valid integer", serverId)
			}
		}
	}

	//handling last server to be configured
	if addrConfigured {
		httpServerPool = append(httpServerPool, InitializeHTTPServer(srvAddr, serverId, workerTimeout, minWorkers, maxWorkers))
	} else {
		return nil, fmt.Errorf("invalid config, value of server%d_addr cannot be empty", serverId)
	}

	return httpServerPool, nil
}

func (hs *HTTPServer) SpawnHTTPWorker(workerId int, minWorkerCount int, timeout int, lgr *log.Logger, workerCount *int, workerCountMutex *sync.Mutex) *HTTPWorker {

	client := util.InitializeWorkerHTTPClient(lgr, workerId)

	return &HTTPWorker{
		Addr:             hs.Addr,
		WorkerId:         workerId,
		MinWorkerCount:   minWorkerCount,
		JobChannel:       hs.JobChannel,
		HTTPClient:       client,
		logger:           lgr,
		Timeout:          timeout,
		WorkerCount:      workerCount,
		WorkerCountMutex: workerCountMutex,
	}
}

/*
Worker waits to be assigned a Job, by listening to the Job channel.
Then creates a copy of the request, sends it to the server, and writes response back to the ResponseWriter.
*/
func (hw *HTTPWorker) ProcessHTTPRequest() {
	for {

		select {

		case req := <-hw.JobChannel:
			hw.logger.Printf("worker %d received a task... ", hw.WorkerId)

			newReq, err := util.CopyRequest(req.Request, hw.Addr)

			if err != nil {
				hw.logger.Printf("Worker %d -> error : %s", hw.WorkerId, err.Error())
				util.WriteJSON(req.ResponseWriter, 500, map[string]string{"error": "internal server error"})
				continue
			}

			resp, err := hw.HTTPClient.Do(newReq)

			if resp == nil {
				hw.logger.Printf("Worker %d -> error : response is nil", hw.WorkerId)
				util.WriteJSON(req.ResponseWriter, 500, map[string]string{"error": "internal server error"})
				continue
			}

			if err != nil {
				hw.logger.Printf("Worker %d -> error : %s", hw.WorkerId, err.Error())
				util.WriteJSON(req.ResponseWriter, 500, map[string]string{"error": "internal server error"})
				resp.Body.Close()
				continue
			}

			respBody, err := io.ReadAll(resp.Body)
			resp.Body.Close()

			if err != nil {
				hw.logger.Printf("Worker %d -> error : %s", hw.WorkerId, err.Error())
				util.WriteJSON(req.ResponseWriter, 500, map[string]string{"error": "internal server error"})
				resp.Body.Close()
				continue
			}

			util.WriteResponse(req.ResponseWriter, resp.StatusCode, respBody)

			close(req.Done)

		case <-time.After(time.Duration(hw.Timeout) * time.Second):

			hw.WorkerCountMutex.Lock()

			if *hw.WorkerCount != hw.MinWorkerCount {
				*hw.WorkerCount--
				hw.logger.Printf(" Worker %d has been idle for %d seconds, exiting.....", hw.WorkerId, hw.Timeout)
				hw.WorkerCountMutex.Unlock()
				hw.HTTPClient.CloseIdleConnections()
				return
			}
			hw.WorkerCountMutex.Unlock()

		}

	}

}
