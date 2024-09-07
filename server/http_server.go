package server

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/types"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/util"
)

type HTTPServer struct {
	Addr        string
	ServerId    int
	TaskChannel chan types.HTTPRequest
	logger      *log.Logger
}

func InitializeHTTPServer(serverAddr string, serverId int) HTTPServer {

	hs := HTTPServer{
		Addr:        serverAddr,
		ServerId:    serverId,
		TaskChannel: make(chan types.HTTPRequest, 3),
		logger:      log.New(os.Stdout, fmt.Sprintf("HTTP SERVER %d :     ", serverId), 0),
	}

	for i := 1; i <= 3; i++ {

		worker := hs.InitializeHTTPWorker(i, hs.logger)
		hs.logger.Printf("Server %d spawning Worker %d...", serverId, i)
		go worker.ProcessHTTPRequest()
	}

	return hs

}

type HTTPWorker struct {
	Addr        string
	WorkerId    int
	TaskChannel <-chan types.HTTPRequest
	HTTPClient  http.Client
	logger      *log.Logger
}

func (hs *HTTPServer) InitializeHTTPWorker(workerId int, lgr *log.Logger) *HTTPWorker {

	client := http.Client{}

	return &HTTPWorker{
		Addr:        hs.Addr,
		WorkerId:    workerId,
		TaskChannel: hs.TaskChannel,
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
