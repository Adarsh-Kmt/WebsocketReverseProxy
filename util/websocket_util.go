package util

import (
	"log"

	"github.com/gorilla/websocket"
)

func HandleWebsocketConnClosure(conn *websocket.Conn, message string) error {

	err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, message))

	return err
}

// go routine listens to end server websocket connection, writes to user websocket connection.
func StartListeningToServer(userWebsocketConn *websocket.Conn, serverWebsocketConn *websocket.Conn, logger *log.Logger) {

	logger.Println("listening to server for messages.....")
	for {

		_, b, err := serverWebsocketConn.ReadMessage()

		//log.Printf("load balancer received byte message of length %d from end server.", len(b))

		if err != nil {

			if closeError, ok := err.(*websocket.CloseError); ok {
				log.Printf("received conn closure from end server with code: %d message : %s", closeError.Code, closeError.Text)
				HandleWebsocketConnClosure(userWebsocketConn, "internal server error")
				break
			}
			logger.Fatalf("error while reading message from websocket connection.")

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

				logger.Printf("received conn closure from user with code: %d message : %s", closeError.Code, closeError.Text)
				HandleWebsocketConnClosure(serverWebsocketConn, "user closed websocket connection")
				break
			}
			logger.Fatalf("error while writing message to websocket connection.")

		}

	}
}

// go routine listens to user websocket connection, writes to end server websocket connection.
func StartListeningToUser(userWebsocketConn *websocket.Conn, serverWebsocketConn *websocket.Conn, logger *log.Logger) {

	logger.Println("listening to user for messages.....")
	for {

		_, b, err := userWebsocketConn.ReadMessage()

		if err != nil {

			if closeError, ok := err.(*websocket.CloseError); ok {

				logger.Printf("received conn closure from user with code: %d message : %s", closeError.Code, closeError.Text)
				HandleWebsocketConnClosure(serverWebsocketConn, "user closed websocket connection")
				break
			}
			logger.Fatalf("error while reading message from websocket connection : %s", err.Error())

		}
		err = serverWebsocketConn.WriteMessage(websocket.BinaryMessage, b)

		if err != nil {

			if closeError, ok := err.(*websocket.CloseError); ok {

				logger.Printf("received conn closure from server with code: %d message : %s", closeError.Code, closeError.Text)
				HandleWebsocketConnClosure(userWebsocketConn, "user closed websocket connection")
				break
			}
			logger.Fatalf("error while writing message to websocket connection : %s", err.Error())

		}

	}
}
