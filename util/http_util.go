package util

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"
)

type HTTPFunc func(http.ResponseWriter, *http.Request) *HTTPError

type HTTPError struct {
	Status int
	Error  string
}

type WorkerDialer struct {
	WorkerId int
	Logger   *log.Logger
	Dialer   net.Dialer
}

type HandlerDialer struct {
	Logger *log.Logger
	Dialer net.Dialer
}

func (wd *WorkerDialer) Dial(network, address string) (net.Conn, error) {

	conn, err := wd.Dialer.Dial(network, address)

	if err != nil {

		return nil, err
	}

	wd.Logger.Printf("Worker %d created %s connection from source addr : %s to destination addr : %s", wd.WorkerId, network, conn.LocalAddr().String(), address)

	return conn, nil
}

func InitializeWorkerHTTPClient(logger *log.Logger, workerId int) http.Client {

	dialer := &WorkerDialer{
		Logger:   logger,
		Dialer:   net.Dialer{},
		WorkerId: workerId,
	}

	transport := &http.Transport{
		Dial: dialer.Dial,
	}

	return http.Client{

		Transport: transport,
	}
}

func (hd *HandlerDialer) Dial(network, address string) (net.Conn, error) {

	conn, err := hd.Dialer.Dial(network, address)

	if err != nil {

		return nil, err
	}

	hd.Logger.Printf("created %s connection from source addr : %s to destination addr : %s", network, conn.LocalAddr().String(), address)

	return conn, nil
}
func InitializeHandlerHTTPClient(logger *log.Logger) http.Client {

	dialer := &HandlerDialer{
		Logger: logger,
		Dialer: net.Dialer{},
	}

	return http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{

			Dial: dialer.Dial,
		},
	}
}
func MakeHttpHandlerFunc(f HTTPFunc) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		if httpError := f(w, r); httpError != nil {
			WriteJSON(w, httpError.Status, map[string]string{"Error": httpError.Error})
		}
	}
}

func CopyRequest(r *http.Request, destinationAddr string) (*http.Request, error) {

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("error while reading the body at handler %s", err.Error())
	}
	r.Body.Close()

	bodyReader := io.NopCloser(bytes.NewReader(body))

	newURL := url.URL{Scheme: "http", Host: destinationAddr, Path: r.URL.Path}

	r2, err := http.NewRequest(r.Method, newURL.String(), bodyReader)

	for key, values := range r.Header {
		// Copy each header value from the source to the destination
		r2.Header[key] = append(r2.Header[key], values...)
	}

	return r2, err
}

func WriteJSON(w http.ResponseWriter, status int, body any) {
	//log.Println("called this function.")
	w.WriteHeader(status)
	w.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(w).Encode(body)
	if err != nil {
		log.Printf("error occured while writing to responseWriter %s", err.Error())
	}
}

// used to write already json encoded messages to ResponseWriter
func WriteResponse(w http.ResponseWriter, status int, body []byte) {

	w.WriteHeader(status)
	w.Header().Set("Content-Type", "application/json")

	_, err := w.Write(body)
	if err != nil {
		log.Printf("error occured while writing to responseWriter %s", err.Error())
	}
}
func InitializeHeaders(r *http.Request) http.Header {

	forwardHeader := make(http.Header, 1)

	forwardHeader.Set("Auth", r.Header.Get("Auth"))
	return forwardHeader

}
