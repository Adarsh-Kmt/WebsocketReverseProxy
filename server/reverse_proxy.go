package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/types"
	"github.com/Adarsh-Kmt/WebsocketReverseProxy/util"
	"github.com/gorilla/websocket"
)

type ReverseProxy struct {
	EndServerPool        []EndServer
	HealthyEndServerPool []EndServer //contains healthy end server structs.

	HESPMutex    *sync.Mutex     // mutex used to write to healthy end server pool.
	TESWaitGroup *sync.WaitGroup // wait group for TestEndServer go routines.

	GlobalConnectionId *int
	GCIDMutex          *sync.Mutex // mutex for updating the global connection ID.

	/*
		reader-writer mutex used to provide synchronization between HealthCheck go routine (writer) and ConnectUser go routines (readers)
	*/
	RWMutex *sync.RWMutex
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func InitializeReverseProxy() *ReverseProxy {

	es1 := InitializeEndServer("es1_hc:8080")
	es2 := InitializeEndServer("es2_hc:8080")
	es3 := InitializeEndServer("es3_hc:8080")

	gcid := 0
	esPool := []EndServer{es1, es2, es3}

	return &ReverseProxy{
		EndServerPool:        esPool,
		HealthyEndServerPool: []EndServer{},
		HESPMutex:            &sync.Mutex{},
		TESWaitGroup:         &sync.WaitGroup{},
		RWMutex:              &sync.RWMutex{},
		GCIDMutex:            &sync.Mutex{},
		GlobalConnectionId:   &gcid,
	}
}

func (rp *ReverseProxy) HealthCheck() {

	rp.RWMutex.Lock()

	rp.TESWaitGroup = &sync.WaitGroup{}

	hesPool := make([]EndServer, 0, len(rp.EndServerPool))
	//log.Println("healthy end server pool is empty again.")

	rp.HealthyEndServerPool = hesPool
	for _, es := range rp.EndServerPool {

		rp.TESWaitGroup.Add(1)
		go rp.TestEndServer(es)
	}

	rp.TESWaitGroup.Wait()
	log.Println("finished health check.")

	rp.HESPMutex.Lock()
	log.Printf("length of the healthy end server list after health check: %d", len(rp.HealthyEndServerPool))
	rp.HESPMutex.Unlock()

	rp.RWMutex.Unlock()

}

/*
go routine used to check wether an end server is online, then writes to HealthyEndServerPool in a thread-safe manner.
*/
func (rp *ReverseProxy) TestEndServer(es EndServer) error {

	defer rp.TESWaitGroup.Done()

	response, err := http.Get(fmt.Sprintf("http://" + es.EndServerAddress + "/healthCheck"))

	if err != nil {
		log.Println(es.EndServerAddress + " health check error: " + err.Error())
		return err
	}

	var hcr types.HealthCheckResponse
	respBody, err := io.ReadAll(response.Body)
	json.Unmarshal(respBody, &hcr)

	if err != nil {
		log.Println("error while json decoding health check response: " + err.Error())
		return err
	}
	log.Printf("response status received from %s : %d\n", es.EndServerAddress, hcr.Status)

	rp.HESPMutex.Lock()
	rp.HealthyEndServerPool = append(rp.HealthyEndServerPool, es)
	//log.Printf("healthy end server length %d\n", len(rp.HealthyEndServerPool))
	rp.HESPMutex.Unlock()

	return nil
}

/*
ConnectUser func used to handle user connection attempts.
spawns 2 go routines:

1) StartListeningToEndServer : listens to end server websocket connection, writes to user websocket connection.
2) StartListeningToUser		 : listens to user websocket connection, writes to end server websocket connection.
*/

func HandleWebsocketConnClosure(conn *websocket.Conn, message string) error {

	err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, message))

	return err
}

func (rp *ReverseProxy) ConnectUser(w http.ResponseWriter, r *http.Request) {

	rp.RWMutex.RLock()

	rp.GCIDMutex.Lock()
	*rp.GlobalConnectionId++
	userWebsocketConnId := *rp.GlobalConnectionId
	*rp.GlobalConnectionId++
	endServerWebsocketConnId := *rp.GlobalConnectionId
	rp.GCIDMutex.Unlock()

	log.Printf("end server websocket connection id %d\n", endServerWebsocketConnId)
	log.Printf("user websocket connection id %d\n", userWebsocketConnId)

	rp.HESPMutex.Lock()
	//log.Printf("healthy end server len %d\n", len(rp.HealthyEndServerPool))
	endServerId := endServerWebsocketConnId % len(rp.HealthyEndServerPool)
	es := rp.HealthyEndServerPool[endServerId]
	rp.HESPMutex.Unlock()

	log.Printf("user connected to end server %s", es.EndServerAddress)

	rp.RWMutex.RUnlock()

	url := url.URL{Scheme: "ws", Host: es.EndServerAddress, Path: "/sendMessage"}

	header := util.InitializeHeaders(r)
	endServerWebsocketConn, _, err := websocket.DefaultDialer.Dial(url.String(), header)

	if err != nil {

		log.Fatalf("error while establishing end server websocket connection with address %s : %s ", es.EndServerAddress, err.Error())
	}
	userWebsocketConn, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		log.Fatalf("error while upgrading user websocket connection: %s ", err.Error())

	}

	go StartListeningToEndServer(userWebsocketConn, endServerWebsocketConn)
	go StartListeningToUser(userWebsocketConn, endServerWebsocketConn)

}

// go routine listens to end server websocket connection, writes to user websocket connection.
func StartListeningToEndServer(userWebsocketConn *websocket.Conn, endServerWebsocketConn *websocket.Conn) {

	log.Println("listening to end server for messages.....")
	for {

		_, b, err := endServerWebsocketConn.ReadMessage()

		//log.Printf("load balancer received byte message of length %d from end server.", len(b))

		if err != nil {

			if closeError, ok := err.(*websocket.CloseError); ok {
				log.Printf("received conn closure from end server with code: %d message : %s", closeError.Code, closeError.Text)
				HandleWebsocketConnClosure(userWebsocketConn, "internal server error")
				break
			}
			log.Fatalf("error while reading message from websocket connection.")

		}
		message := string(b)

		if message == "" {
			log.Println("received empty message from end server.")
			continue
		} else {
			log.Println(message + " received from the end server.")
		}

		err = userWebsocketConn.WriteMessage(websocket.TextMessage, b)

		if err != nil {

			if closeError, ok := err.(*websocket.CloseError); ok {

				log.Printf("received conn closure from user with code: %d message : %s", closeError.Code, closeError.Text)
				HandleWebsocketConnClosure(endServerWebsocketConn, "user closed websocket connection")
				break
			}
			log.Fatalf("error while writing message to websocket connection.")

		}

	}
}

// go routine listens to user websocket connection, writes to end server websocket connection.
func StartListeningToUser(userWebsocketConn *websocket.Conn, endServerWebsocketConn *websocket.Conn) {

	log.Println("listening to user for messages.....")
	for {

		_, b, err := userWebsocketConn.ReadMessage()

		if err != nil {

			if closeError, ok := err.(*websocket.CloseError); ok {

				log.Printf("received conn closure from user with code: %d message : %s", closeError.Code, closeError.Text)
				HandleWebsocketConnClosure(endServerWebsocketConn, "user closed websocket connection")
				break
			}
			log.Fatalf("error while reading message from websocket connection : %s", err.Error())

		}
		var mr types.MessageRequest

		err = json.Unmarshal(b, &mr)

		if err != nil {
			log.Printf("error while unmarshalling json: %s", err.Error())
		}
		if mr.Body == "" {
			log.Println("received empty message from user.")
			continue
		}
		log.Printf("load balancer received message %s for %s", mr.Body, mr.ReceiverUsername)

		err = endServerWebsocketConn.WriteMessage(websocket.BinaryMessage, b)

		if err != nil {

			if closeError, ok := err.(*websocket.CloseError); ok {

				log.Printf("received conn closure from end server with code: %d message : %s", closeError.Code, closeError.Text)
				HandleWebsocketConnClosure(userWebsocketConn, "user closed websocket connection")
				break
			}
			log.Fatalf("error while writing message to websocket connection : %s", err.Error())

		}

	}
}
