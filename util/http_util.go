package util

import (
	"encoding/json"
	"net/http"
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

func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func InitializeHeaders(r *http.Request) http.Header {

	forwardHeader := make(http.Header, 1)

	forwardHeader.Set("Auth", r.Header.Get("Auth"))
	return forwardHeader

}
