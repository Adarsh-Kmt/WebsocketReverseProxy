package handler

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/util"
	"github.com/gookit/ini/v2"
)

/*
ReverseProxy now used as a global http.Handler.
It checks the scheme of the request, then calls ServeHTTP method of HTTPHandler or WebsocketHandler.
*/
type ReverseProxy struct {
	Addr             string
	WebsocketHandler http.Handler
	HTTPHandler      http.Handler
	logger           *log.Logger
}

func ConfigureReverseProxy(cfgFilePath string) (*ReverseProxy, error) {

	logger := log.New(os.Stdout, "REVERSE PROXY : ", 0)
	if _, err := os.Stat(cfgFilePath); os.IsNotExist(err) {
		logger.Fatalf("Config file does not exist")
	}
	err := ini.LoadExists(cfgFilePath)

	if err != nil {

		return nil, fmt.Errorf("no .ini file found at file path %s", cfgFilePath)
	}

	cfg := ini.Default()

	host := cfg.String("frontend.host")

	if host == "" {

		return nil, fmt.Errorf("frontend.host cannot be empty")
	}

	port := cfg.String("frontend.port")

	if port == "" {

		return nil, fmt.Errorf("frontend.port cannot be empty")

	}

	addr := cfg.String("frontend.host") + ":" + cfg.String("frontend.port")

	var wsHandler http.Handler

	// check wether config file has [websocket] section before configuring Websocket Handler.
	if cfg.HasSection("websocket") {
		wsHandler, err = ConfigureWebsocketHandler(cfgFilePath)
		if err != nil {
			return nil, err
		}
	}

	httpHandler, err := ConfigureHTTPHandler(cfgFilePath)

	if err != nil {
		return nil, err
	}
	rp := &ReverseProxy{
		Addr:             addr,
		WebsocketHandler: wsHandler,
		HTTPHandler:      httpHandler,
		logger:           logger,
	}

	return rp, nil

}
func NewReverseProxy() (rp *ReverseProxy, addr string, err error) {

	rp, err = ConfigureReverseProxy("/app/reverse-proxy-config.ini")

	if err != nil {
		return nil, "", err
	}

	return rp, rp.Addr, nil

}

func (rp *ReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if r.Header.Get("Connection") == "Upgrade" && r.Header.Get("Upgrade") == "websocket" {

		// if config has missing [websocket] section, websocketHandler should not be created.
		if rp.WebsocketHandler == nil {
			util.WriteJSON(w, 400, map[string]string{"error": "proxy not configured to handle websocket connections"})
			return
		}
		rp.WebsocketHandler.ServeHTTP(w, r)

	} else {
		if r.Body == nil {
			rp.logger.Println("empty request body..")
		}
		rp.HTTPHandler.ServeHTTP(w, r)
	}
}