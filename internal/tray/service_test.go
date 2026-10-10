package tray

import (
	"context"
	"net"
	"os"
	"path/filepath"
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
