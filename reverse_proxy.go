package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/types"
)

type ReverseProxy struct {
	EndServerPool          []string
	HealthyEndServerPool   []string
	UnhealthyEndServerPool []string
	HESPMutex              *sync.Mutex     // mutex used to write to healthy end server pool.
	UESPMutex              *sync.Mutex     // mutex used to write to unhealthy end server pool.
	TESWaitGroup           *sync.WaitGroup // wait group for TestEndServer go routines.
}

func InitializeReverseProxy() *ReverseProxy {

	return &ReverseProxy{EndServerPool: []string{"es1_hc:8080", "es2_hc:8080", "es3_hc:8080"},
		HealthyEndServerPool:   make([]string, 3),
		UnhealthyEndServerPool: make([]string, 0),
		HESPMutex:              &sync.Mutex{},
		UESPMutex:              &sync.Mutex{}}
}

func (rp *ReverseProxy) HealthCheck(RWMutex *sync.RWMutex) {

	RWMutex.Lock()
	defer RWMutex.Unlock()

	rp.TESWaitGroup = &sync.WaitGroup{}

	hesPool := make([]string, 0, len(rp.EndServerPool))
	uesPool := make([]string, 0, len(rp.EndServerPool))

	rp.HealthyEndServerPool = hesPool
	rp.UnhealthyEndServerPool = uesPool

	for _, es := range rp.EndServerPool {

		rp.TESWaitGroup.Add(1)
		go rp.TestEndServer(es)
	}

	rp.TESWaitGroup.Wait()

	log.Printf("healthy end server pool : %s ", rp.HealthyEndServerPool)
	log.Printf("unhealthy end server pool : %s", rp.UnhealthyEndServerPool)

}
func (rp *ReverseProxy) TestEndServer(esAddress string) error {

	defer rp.TESWaitGroup.Done()

	response, err := http.Get(fmt.Sprintf("http://" + esAddress + "/healthCheck"))

	if err != nil {
		log.Println(esAddress + " health check error: " + err.Error())

		rp.UESPMutex.Lock()
		rp.UnhealthyEndServerPool = append(rp.UnhealthyEndServerPool, esAddress)
		rp.UESPMutex.Unlock()

		return err
	}

	var hcr types.HealthCheckResponse
	respBody, err := io.ReadAll(response.Body)
	json.Unmarshal(respBody, &hcr)

	if err != nil {
		log.Println("error while json decoding health check response: " + err.Error())

		rp.UESPMutex.Lock()
		rp.UnhealthyEndServerPool = append(rp.UnhealthyEndServerPool, esAddress)
		rp.UESPMutex.Unlock()

		return err
	}
	fmt.Printf("response status received from %s : %d\n", esAddress, hcr.Status)

	rp.HESPMutex.Lock()
	rp.HealthyEndServerPool = append(rp.HealthyEndServerPool, esAddress)
	rp.HESPMutex.Unlock()
	return nil
}
