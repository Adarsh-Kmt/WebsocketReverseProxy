package util

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
)

type HTTPFunc func(http.ResponseWriter, *http.Request) *HTTPError

type HTTPError struct {
	Status int
	Error  string
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
	log.Printf("new request for %s", newURL.String())
	r2, err := http.NewRequest(r.Method, newURL.String(), bodyReader)

	for key, values := range r.Header {
		// Copy each header value from the source to the destination
		r2.Header[key] = append(r2.Header[key], values...)
	}

	return r2, err
}

func WriteJSON(w http.ResponseWriter, status int, body any) {
	log.Println("called this function.")
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
