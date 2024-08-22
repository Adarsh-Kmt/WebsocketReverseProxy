package main

import (
	"sync"
	"time"
)

func main() {

	rp := InitializeReverseProxy()

	rwm := sync.RWMutex{}

	periodicFunc := func(rwm *sync.RWMutex) {

		for {
			time.Sleep(2 * time.Second)
			rp.HealthCheck(rwm)
		}

	}

	go periodicFunc(&rwm)

	time.Sleep(10000 * time.Second)
}
