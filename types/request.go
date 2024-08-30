package types

type MessageRequest struct {
	ReceiverUsername string `json:"ReceiverUsername"`
	Body             string `json:"Body"`
}
