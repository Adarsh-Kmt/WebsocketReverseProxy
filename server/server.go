package server

import (
	"sync"
)

type Server struct {
	ServerAddress string
	MapMutex      *sync.Mutex

	UserWebsocketConnIdList []int
}

func InitializeServer(endServerAddress string) Server {

	return Server{
		ServerAddress:           endServerAddress,
		UserWebsocketConnIdList: make([]int, 1000),
		MapMutex:                &sync.Mutex{},
	}

}
