package dashboard

import (
	"context"
	"embed"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/command"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/persistence"
)

//go:embed web/index.html web/style.css web/app.js
var webFiles embed.FS

type AnalyzeFunc func(context.Context, models.Model, string) (string, error)

type Dependencies struct {
	Analytics *analytics.ChatAnalytics
	History   *HistoryStore
	Sessions  *persistence.HistoryManager
	Commands  func() *command.CommandRegistry
	Config    config.DashboardConfig
	Analyze   AnalyzeFunc
}

type Server struct {
	mu       sync.RWMutex
	analysis sync.Mutex
	deps     Dependencies
	settings config.DashboardConfig
	listener net.Listener
	http     *http.Server
	url      string
}

func NewServer(deps Dependencies) *Server {
	return &Server{deps: deps, settings: deps.Config}
}

func (s *Server) Start() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.url, nil
	}
	if s.settings.Port < 1 || s.settings.Port > 65535 {
		s.settings.Port = 8765
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.settings.Port)))
	if err != nil {
		return "", fmt.Errorf("start local dashboard on 127.0.0.1:%d: %w", s.settings.Port, err)
	}
	address := listener.Addr().(*net.TCPAddr)
	s.url = fmt.Sprintf("http://127.0.0.1:%d", address.Port)
	s.listener = listener
	s.http = &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second}
	go func(server *http.Server, ln net.Listener) { _ = server.Serve(ln) }(s.http, listener)
	return s.url, nil
}

func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	server := s.http
	if server == nil {
		s.mu.Unlock()
		return nil
	}
	s.http = nil
	s.listener = nil
	s.url = ""
	s.mu.Unlock()
	if err := server.Shutdown(ctx); err != nil {
		// Force-close active handlers (including in-flight analyses) after the
		// graceful deadline; server.Close cancels their request contexts.
		closeErr := server.Close()
		if closeErr != nil {
			return fmt.Errorf("graceful dashboard shutdown failed: %v; force close failed: %w", err, closeErr)
		}
		return err
	}
	return nil
}

func (s *Server) Status() (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listener != nil, s.url
}

func (s *Server) UpdateSettings(settings config.DashboardConfig) {
	s.mu.Lock()
	s.settings = settings
	s.mu.Unlock()
}

func (s *Server) currentSettings() config.DashboardConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

func (s *Server) expectedHost() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(s.settings.Port))
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/settings", s.handleSettings)
	mux.HandleFunc("GET /api/commands", s.handleCommands)
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleSessionDetail)
	mux.HandleFunc("POST /api/analysis/metrics", s.handleMetricsAnalysis)
	mux.HandleFunc("GET /api/analysis/conversations/preview", s.handleConversationPreview)
	mux.HandleFunc("POST /api/analysis/conversations", s.handleConversationAnalysis)
	mux.HandleFunc("GET /assets/style.css", s.handleStyle)
	mux.HandleFunc("GET /assets/app.js", s.handleAppScript)
	mux.HandleFunc("GET /", s.handleIndex)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.expectedHost() {
			http.Error(w, "invalid Host", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost {
			origin := r.Header.Get("Origin")
			if origin == "" || origin != "http://"+s.expectedHost() {
				http.Error(w, "cross-origin request denied", http.StatusForbidden)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
