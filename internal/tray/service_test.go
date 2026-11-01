package tray

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/config"
)

func TestRunServiceDispatchesTrayActionsAndRemovesEndpoint(t *testing.T) {
	endpointFile := filepath.Join(t.TempDir(), "tray-service.json")
	ctx := context.Background()
	dispatched := make(chan Action, 1)
	trayReady := make(chan struct{})
	continueTray := make(chan struct{})
	serviceDone := make(chan error, 1)
	go func() {
		serviceDone <- runService(ctx, ServiceOptions{
			EndpointFile: endpointFile,
			HandleAction: func(_ context.Context, action Action) (Response, error) {
				if action != ActionOpenTextChat && action != ActionQuitService {
					t.Errorf("unexpected dispatched action %q", action)
				}
				dispatched <- action
				return Response{OK: true}, nil
			},
			RunTray: func(_ context.Context, dispatch TrayActionDispatcher, _ <-chan config.TrayConfig) error {
				close(trayReady)
				<-continueTray
				dispatch(ActionOpenTextChat)
				dispatch(ActionQuitService)
				return nil
			},
		}, func(string, string) (net.Listener, error) {
			return newFakeListener(), nil
		})
	}()
	<-trayReady

	var endpoint serviceEndpoint
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		endpoint, _ = readEndpoint(endpointFile)
		if endpoint.Address != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if endpoint.Address == "" {
		t.Fatal("service did not publish its endpoint")
	}
	close(continueTray)

	if action := <-dispatched; action != ActionOpenTextChat {
		t.Fatalf("dispatched action = %q, want %q", action, ActionOpenTextChat)
	}
	if action := <-dispatched; action != ActionQuitService {
		t.Fatalf("dispatched action = %q, want %q", action, ActionQuitService)
	}
	select {
	case err := <-serviceDone:
		if err != nil {
			t.Fatalf("RunService() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("service did not stop after Quit DuckChat")
	}
	if _, err := os.Stat(endpointFile); !os.IsNotExist(err) {
		t.Fatalf("endpoint was not removed when service stopped: %v", err)
	}
}

func TestRunServicePreservesIPCVoiceContextForActionHandler(t *testing.T) {
	endpointFile := filepath.Join(t.TempDir(), "tray-service.json")
	client, server := net.Pipe()
	defer client.Close()
	want := []ConversationMessage{{Role: "user", Content: "carry this into voice"}}
	wantOwner := "cli-owner-from-ipc"
	type voiceRequestContext struct {
		messages []ConversationMessage
		ownerID  string
	}
	gotContext := make(chan voiceRequestContext, 1)
	serviceCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serviceDone := make(chan error, 1)
	listener := &suppliedListener{conn: loopbackConn{Conn: server}, closed: make(chan struct{})}
	go func() {
		serviceDone <- runService(serviceCtx, ServiceOptions{
			EndpointFile: endpointFile,
			HandleAction: func(ctx context.Context, action Action) (Response, error) {
				if action != ActionOpenOrShowVoice {
					t.Errorf("action = %q, want voice launch", action)
				}
				gotContext <- voiceRequestContext{messages: VoiceContextFromContext(ctx), ownerID: VoiceOwnerFromContext(ctx)}
				return Response{VoiceActive: true}, nil
			},
		}, func(string, string) (net.Listener, error) { return listener, nil })
	}()

	var endpoint serviceEndpoint
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		endpoint, _ = readEndpoint(endpointFile)
		if endpoint.Token != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if endpoint.Token == "" {
		t.Fatal("service did not publish its authenticated endpoint")
	}
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- json.NewEncoder(client).Encode(ipcRequest{Token: endpoint.Token, Action: ActionOpenOrShowVoice, VoiceContext: want, VoiceOwnerID: wantOwner})
	}()
	var response Response
	if err := json.NewDecoder(client).Decode(&response); err != nil {
		t.Fatalf("read IPC response: %v", err)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write IPC request: %v", err)
	}
	if !response.OK || !response.VoiceActive {
		t.Fatalf("IPC response = %+v, want active voice session", response)
	}
	if got := <-gotContext; !reflect.DeepEqual(got.messages, want) || got.ownerID != wantOwner {
		t.Fatalf("voice request context reaching action handler = %+v, want messages %+v and owner %q", got, want, wantOwner)
	}
	cancel()
	select {
	case err := <-serviceDone:
		if err != nil {
			t.Fatalf("RunService() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("service did not stop after cancellation")
	}
}

func TestEnsureServiceStartsOnlyWhenNoLiveServiceExists(t *testing.T) {
	endpointFile := filepath.Join(t.TempDir(), "tray-service.json")
	starts := 0
	start := func(executable string, args ...string) error {
		starts++
		if executable != "/tmp/duckchat" || len(args) != 1 || args[0] != "--tray-service" {
			t.Errorf("start args = %q %q, want executable and --tray-service", executable, args)
		}
		return createEndpoint(endpointFile, serviceEndpoint{Address: "127.0.0.1:23456", Token: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", PID: os.Getpid()})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	probe := func(_ context.Context, endpoint serviceEndpoint) bool {
		return endpoint.Address == "127.0.0.1:23456"
	}
	if err := ensureServiceWithProbe(ctx, endpointFile, "/tmp/duckchat", start, probe); err != nil {
		t.Fatalf("first ensureServiceAt() error = %v", err)
	}
	if err := ensureServiceWithProbe(ctx, endpointFile, "/tmp/duckchat", start, probe); err != nil {
		t.Fatalf("second ensureServiceAt() error = %v", err)
	}
	if starts != 1 {
		t.Fatalf("service start count = %d, want 1", starts)
	}
}

type fakeListener struct {
	closed chan struct{}
}

type suppliedListener struct {
	conn   net.Conn
	closed chan struct{}
}

func (l *suppliedListener) Accept() (net.Conn, error) {
	if l.conn != nil {
		connection := l.conn
		l.conn = nil
		return connection, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}

func (l *suppliedListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (l *suppliedListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12346}
}

func newFakeListener() *fakeListener {
	return &fakeListener{closed: make(chan struct{})}
}

func (l *fakeListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *fakeListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (l *fakeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}
