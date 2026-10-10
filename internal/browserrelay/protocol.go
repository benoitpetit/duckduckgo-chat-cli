package browserrelay

import (
	"net/http"
)

type relayJob struct {
	ID     string      `json:"id"`
	URL    string      `json:"url"`
	Method string      `json:"method"`
	Header http.Header `json:"headers"`
	Body   string      `json:"body"`
}

type relayResponseStart struct {
	ID     string      `json:"id"`
	Status int         `json:"status"`
	Header http.Header `json:"headers"`
}

type relayResponseChunk struct {
	ID   string `json:"id"`
	Data string `json:"data"`
}

type relayResponseFinish struct {
	ID string `json:"id"`
}

type relayResponseFailure struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}
