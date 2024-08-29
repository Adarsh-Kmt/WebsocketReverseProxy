package server

import (
	"sync"
)

type EndServer struct {
	EndServerAddress string
	MapMutex         *sync.Mutex

	UserWebsocketConnIdList []int
}

func InitializeEndServer(endServerAddress string) EndServer {

	return EndServer{
		EndServerAddress:        endServerAddress,
		UserWebsocketConnIdList: make([]int, 1000),
		MapMutex:                &sync.Mutex{},
	}

}
