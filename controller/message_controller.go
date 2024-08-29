package controller

import (
	"net/http"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/server"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/util"
	"github.com/gorilla/mux"
)

type MessageController struct {
	RP *server.ReverseProxy
}

func (mc *MessageController) InitializeEndPoints(mux *mux.Router) *mux.Router {

	mux.HandleFunc("/sendMessage", util.MakeHttpHandlerFunc(mc.SendMessage))

	return mux
}

func (mc *MessageController) SendMessage(w http.ResponseWriter, r *http.Request) *util.HTTPError {

	mc.RP.ConnectUser(w, r)
	return nil
}
