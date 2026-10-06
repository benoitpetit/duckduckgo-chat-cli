package dashboard

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/command"
	"duckduckgo-chat-cli/internal/config"
)

func testServer(t *testing.T, port int) *Server {
	t.Helper()
	if port == 0 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		_, portText, _ := net.SplitHostPort(listener.Addr().String())
		port, _ = strconv.Atoi(portText)
		_ = listener.Close()
	}
	return NewServer(Dependencies{
		Analytics: analytics.NewChatAnalytics(),
		History:   NewHistoryStore(t.TempDir()+"/stats.jsonl", 90),
		Commands:  command.GetCommandRegistry,
		Config:    config.DashboardConfig{Port: port, RefreshIntervalSeconds: 3, RetentionDays: 90},
	})
}

func TestServerBindsLoopbackAndStopsCleanly(t *testing.T) {
	server := testServer(t, 0)
	url, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Fatalf("Start() URL = %q, want loopback URL", url)
	}
	_, address, err := net.SplitHostPort(strings.TrimPrefix(url, "http://"))
	if err != nil || address == "" {
		t.Fatalf("invalid server URL %q: %v", url, err)
	}
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(url, "http://"), time.Second)
	if err != nil {
		t.Fatalf("server is not listening: %v", err)
	}
	_ = conn.Close()
	if running, gotURL := server.Status(); !running || gotURL != url {
		t.Fatalf("Status() = %t, %q while running", running, gotURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if running, _ := server.Status(); running {
		t.Fatal("server remains active after Stop")
	}
}

func TestServerRejectsOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	if _, err := testServer(t, port).Start(); err == nil {
		t.Fatal("Start succeeded on an occupied port")
	}
}

func TestDashboardHandlersValidateHostOriginAndJSON(t *testing.T) {
	server := testServer(t, 0)
	url, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop(context.Background())
	client := &http.Client{Timeout: time.Second}

	request, _ := http.NewRequest(http.MethodGet, url+"/api/stats", nil)
	request.Host = "malicious.example"
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("unexpected Host status = %d, want 403", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPost, url+"/api/stats", strings.NewReader("{}"))
	request.Header.Set("Origin", "http://evil.example")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST status = %d, want 403", response.StatusCode)
	}

	for _, endpoint := range []string{"/api/stats", "/api/settings", "/api/commands"} {
		response, err = client.Get(url + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || !strings.Contains(response.Header.Get("Content-Type"), "application/json") {
			response.Body.Close()
			t.Fatalf("GET %s status=%d content-type=%q", endpoint, response.StatusCode, response.Header.Get("Content-Type"))
		}
		var value map[string]any
		if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
			response.Body.Close()
			t.Fatalf("GET %s JSON: %v", endpoint, err)
		}
		response.Body.Close()
	}
}

func TestHostValidationHandlerDirect(t *testing.T) {
	server := testServer(t, 8765)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/api/settings", nil)
	req.Host = "localhost:8765"
	server.handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("host status = %d, want 403", rr.Code)
	}
}
