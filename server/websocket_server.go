package server

import (
	"fmt"
	"log"
	"os"
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
