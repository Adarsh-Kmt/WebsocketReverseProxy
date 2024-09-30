package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/Adarsh-Kmt/WebsocketReverseProxy/handler"
)

func printBanner() {
	banner := `
 █████                              █████    ███████████            ████                                                  
░░███                              ░░███    ░░███░░░░░███          ░░███                                                  
 ░███         ██████   ██████    ███████     ░███    ░███  ██████   ░███   ██████   ████████    ██████   ██████  ████████ 
 ░███        ███░░███ ░░░░░███  ███░░███     ░██████████  ░░░░░███  ░███  ░░░░░███ ░░███░░███  ███░░███ ███░░███░░███░░███
 ░███       ░███ ░███  ███████ ░███ ░███     ░███░░░░░███  ███████  ░███   ███████  ░███ ░███ ░███ ░░░ ░███████  ░███ ░░░ 
 ░███      █░███ ░███ ███░░███ ░███ ░███     ░███    ░███ ███░░███  ░███  ███░░███  ░███ ░███ ░███  ███░███░░░   ░███     
 ███████████░░██████ ░░████████░░████████    ███████████ ░░████████ █████░░████████ ████ █████░░██████ ░░██████  █████    
░░░░░░░░░░░  ░░░░░░   ░░░░░░░░  ░░░░░░░░    ░░░░░░░░░░░   ░░░░░░░░ ░░░░░  ░░░░░░░░ ░░░░ ░░░░░  ░░░░░░   ░░░░░░  ░░░░░     
                                                                                                                          
                                                                                                                          
                                                                                                                          
`
	fmt.Println("\033[32m" + banner + "\033[0m")

}
func main() {

	printBanner()
	if err := run(); err != nil {

		fmt.Printf("error while running server: %s", err.Error())
	}

}
func run() error {

	// interruptContext used to notify gracefulShutdown go routine, when user enters Ctrl + C.
	interruptContext, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	rp, err := handler.ConfigureReverseProxy()

	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:    rp.Addr,
		Handler: rp,
	}

	go startListening(srv)

	wg := &sync.WaitGroup{}
	wg.Add(1)

	go gracefulShutdown(srv, interruptContext, wg)
	wg.Wait()
	return nil

}

func startListening(rp *http.Server) error {

	if err := rp.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Printf("error occured with server %s : %s", rp.Addr, err.Error())
		return err
	}
	return nil
}

// Handles graceful shutdown of server.
// Server waits for all connections to become idle and then stops, or stops after 10 seconds. whichever comes first.
func gracefulShutdown(rp *http.Server, interruptContext context.Context, wg *sync.WaitGroup) error {

	defer wg.Done()

	<-interruptContext.Done()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := rp.Shutdown(shutdownContext); err != nil {

		fmt.Println("error during graceful shutdown of http server: ", err.Error())
		return err
	}

	return nil
}
