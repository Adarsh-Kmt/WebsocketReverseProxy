package server

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/util"
)

type HTTPServer struct {
	Addr       string
	ServerId   int
	JobChannel chan Job
	Logger     *log.Logger
}

func InitializeHTTPServer(serverAddr string, serverId int) HTTPServer {

	hs := HTTPServer{
		Addr:       serverAddr,
		ServerId:   serverId,
		JobChannel: make(chan Job, 3),
		Logger:     log.New(os.Stdout, fmt.Sprintf("HTTP SERVER %d :     ", serverId), 0),
	}

	for i := 1; i <= 3; i++ {

		worker := hs.SpawnHTTPWorker(i, hs.Logger)
		hs.Logger.Printf("Server %d spawning Worker %d...", serverId, i)
		go worker.ProcessHTTPRequest()
	}

	return hs

}

type HTTPWorker struct {
	Addr        string
	WorkerId    int
	TaskChannel <-chan Job
	HTTPClient  http.Client
	logger      *log.Logger
}

type Job struct {
	ResponseWriter http.ResponseWriter
	RequestBody    []byte
	Request        *http.Request
	Done           chan struct{}
}

func (hs *HTTPServer) SpawnHTTPWorker(workerId int, lgr *log.Logger) *HTTPWorker {

	client := http.Client{}

	return &HTTPWorker{
		Addr:        hs.Addr,
		WorkerId:    workerId,
		TaskChannel: hs.JobChannel,
		HTTPClient:  client,
		logger:      lgr,
	}
}
func (hw *HTTPWorker) ProcessHTTPRequest() {
	for {
		req := <-hw.TaskChannel

		hw.logger.Printf("worker %d received a task... ", hw.WorkerId)

		newReq, err := util.CopyRequest(req.Request, req.RequestBody, hw.Addr)

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

		if err != nil {
			hw.logger.Printf("Worker %d -> error : %s", hw.WorkerId, err.Error())
			util.WriteJSON(req.ResponseWriter, 500, map[string]string{"error": "internal server error"})
			resp.Body.Close()
			continue
		}

		util.WriteResponse(req.ResponseWriter, resp.StatusCode, respBody)

		resp.Body.Close()

		close(req.Done)

	}

}
