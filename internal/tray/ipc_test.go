package tray

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
)

func TestIPCRejectsUnauthenticatedRequests(t *testing.T) {
	dispatched := false
	response := processRequest(context.Background(), ipcRequest{Token: "wrong", Action: ActionOpenTextChat}, "secret", func(context.Context, Action) (Response, error) {
		dispatched = true
		return Response{OK: true}, nil
	})
	if response.OK || response.Error != "unauthorized" {
		t.Fatalf("unauthorized response = %+v, want unauthorized", response)
	}
	if dispatched {
		t.Fatal("unauthenticated action was dispatched")
	}
}

func TestIPCRejectsUnknownActions(t *testing.T) {
	dispatched := false
	response := processRequest(context.Background(), ipcRequest{Token: "secret", Action: Action("run-shell-command")}, "secret", func(context.Context, Action) (Response, error) {
		dispatched = true
		return Response{OK: true}, nil
	})
	if response.OK || response.Error != "unknown action" {
		t.Fatalf("unknown action response = %+v, want unknown action", response)
	}
	if dispatched {
		t.Fatal("unknown action was dispatched")
	}
}

func TestIPCDispatchesAuthenticatedAction(t *testing.T) {
	response := processRequest(context.Background(), ipcRequest{Token: "secret", Action: ActionOpenTextChat}, "secret", func(_ context.Context, action Action) (Response, error) {
		if action != ActionOpenTextChat {
			t.Fatalf("dispatched action = %q, want %q", action, ActionOpenTextChat)
		}
		return Response{Message: "opened"}, nil
	})
	if !response.OK || response.Message != "opened" {
		t.Fatalf("dispatch response = %+v, want successful open response", response)
	}
}

func TestIPCRejectsOversizedRequestLine(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	responseReady := make(chan Response, 1)
	go func() {
		responseReady <- handleIPCConnection(context.Background(), loopbackConn{Conn: server}, "secret", nil)
	}()
	go func() {
		_, _ = client.Write([]byte("{\"token\":\"secret\",\"action\":\"open-text-chat\",\"extra\":\"" + strings.Repeat("x", maxIPCMessageBytes) + "\"}\n"))
	}()
	var response Response
	if err := json.NewDecoder(client).Decode(&response); err != nil {
		t.Fatal(err)
	}
	<-responseReady
	if response.OK || response.Error != "request too large" {
		t.Fatalf("oversized request response = %+v, want request too large", response)
	}
}

type loopbackConn struct{ net.Conn }

func (c loopbackConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

func TestIPCRequestLineRoundTripsJSON(t *testing.T) {
	request := ipcRequest{Token: "secret", Action: ActionVoiceStatus}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeIPCRequest(bufio.NewReader(strings.NewReader(string(data) + "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if got != request {
		t.Fatalf("decoded request = %+v, want %+v", got, request)
	}
}
