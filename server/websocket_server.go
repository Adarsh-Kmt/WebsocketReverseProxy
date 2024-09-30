package server

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gookit/ini/v2"
)

type WebsocketServer struct {
	ServerId int
	Addr     string
	Logger   *log.Logger
}

func InitializeWebsocketServer(serverAddr string, serverId int) WebsocketServer {

	return WebsocketServer{
		Addr:     serverAddr,
		ServerId: serverId,
		Logger:   log.New(os.Stdout, fmt.Sprintf("WEBSOCKET SERVER %d :     ", serverId), 0),
	}
}

func ConfigureWebsocketServers(websocketSection ini.Section) ([]WebsocketServer, error) {

	wsServerPool := make([]WebsocketServer, 0)
	serverId := 1

	for key, srvAddr := range websocketSection {

		if key == "algorithm" {
			continue
		}
		if !strings.HasPrefix(key, "server") {
			return nil, fmt.Errorf("format for websocket section:\n\n[websocket]\nserver{number}={Host:Port}")
		}

		wsServerPool = append(wsServerPool, InitializeWebsocketServer(srvAddr, serverId))
		serverId++
	}

	return wsServerPool, nil
}
