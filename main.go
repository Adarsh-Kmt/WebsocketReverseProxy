package main

import (
	"net/http"
	"time"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/controller"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/server"
	"github.com/gorilla/mux"
)

func main() {

	rp := server.InitializeReverseProxy()

	mux := mux.NewRouter()

	mc := controller.MessageController{RP: rp}

	mux = mc.InitializeEndPoints(mux)

	periodicFunc := func() {

		for {
			rp.HealthCheck()
			time.Sleep(10 * time.Second)
		}

	}

	go periodicFunc()

	http.ListenAndServe(":8084", mux)
}
