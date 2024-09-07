package types

import (
	"net/http"
)

// type MessageRequest struct {
// 	ReceiverUsername string `json:"ReceiverUsername"`
// 	Body             string `json:"Body"`
// }

type HTTPRequest struct {
	ResponseWriter http.ResponseWriter
	RequestBody    []byte
	Request        *http.Request
	Done           chan struct{}
}
