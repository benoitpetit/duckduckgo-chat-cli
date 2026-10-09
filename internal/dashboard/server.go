package dashboard

import (
	"context"
	"embed"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"duckduckgo-chat-cli/internal/activity"
	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/command"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/persistence"
)

//go:embed web/index.html web/style.css web/app.js web/logo.png web/login.html web/login.css web/login.js web/login-duck-base.png web/login-duck-hands.png
var webFiles embed.FS

type AnalyzeFunc func(context.Context, models.Model, string) (string, error)

type Dependencies struct {
	Analytics *analytics.ChatAnalytics
	History   *HistoryStore
	Sessions  *persistence.HistoryManager
	Commands  func() *command.CommandRegistry
	Config    config.DashboardConfig
	Analyze   AnalyzeFunc
	Activity  *activity.Hub
}

type Server struct {
	mu            sync.RWMutex
	analysis      sync.Mutex
	deps          Dependencies
	settings      config.DashboardConfig
	sessions      map[string]struct{}
	loginFailures map[string]loginFailure
	listener      net.Listener
	http          *http.Server
	url           string
}

func NewServer(deps Dependencies) *Server {
	if deps.Activity != nil {
		deps.Activity.SetConversationContentEnabled(deps.Config.ShowConversationContent)
	}
	return &Server{deps: deps, settings: deps.Config, sessions: make(map[string]struct{}), loginFailures: make(map[string]loginFailure)}
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
	s.sessions = make(map[string]struct{})
	s.loginFailures = make(map[string]loginFailure)
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
	wasEnabled := s.settings.ShowConversationContent
	passwordChanged := s.settings.PasswordSalt != settings.PasswordSalt || s.settings.PasswordHash != settings.PasswordHash
	if s.deps.Activity != nil {
		s.deps.Activity.SetConversationContentEnabled(settings.ShowConversationContent)
	}
	s.settings = settings
	if passwordChanged {
		s.sessions = make(map[string]struct{})
		s.loginFailures = make(map[string]loginFailure)
	}
	s.mu.Unlock()
	if s.deps.Activity != nil {
		if wasEnabled && !settings.ShowConversationContent {
			s.deps.Activity.Publish(activity.Event{Category: "privacy", Status: "redacted", Summary: "Conversation content hidden and cleared"})
		}
	}
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
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /auth/login", s.handleLogin)
	mux.HandleFunc("POST /auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/settings", s.handleSettings)
	mux.HandleFunc("GET /api/activity/stream", s.handleActivityStream)
	mux.HandleFunc("GET /api/commands", s.handleCommands)
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleSessionDetail)
	mux.HandleFunc("POST /api/analysis/metrics", s.handleMetricsAnalysis)
	mux.HandleFunc("GET /api/analysis/conversations/preview", s.handleConversationPreview)
	mux.HandleFunc("POST /api/analysis/conversations", s.handleConversationAnalysis)
	mux.HandleFunc("GET /assets/style.css", s.handleStyle)
	mux.HandleFunc("GET /assets/app.js", s.handleAppScript)
	mux.HandleFunc("GET /assets/login.css", s.handleLoginStyle)
	mux.HandleFunc("GET /assets/login.js", s.handleLoginScript)
	mux.HandleFunc("GET /assets/login-duck-base.png", s.handleLoginDuck)
	mux.HandleFunc("GET /assets/login-duck-hands.png", s.handleLoginDuck)
	mux.HandleFunc("GET /assets/logo.png", s.handleLogo)
	mux.HandleFunc("GET /favicon.ico", s.handleLogo)
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
		if r.URL.Path == "/login" && s.currentSettings().PasswordHash == "" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if !s.isPublicLoginRoute(r) && s.currentSettings().PasswordHash != "" && !s.hasValidSession(r) {
			if isDashboardAPIRequest(r) {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) isPublicLoginRoute(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/login":
		return true
	case r.Method == http.MethodGet && r.URL.Path == "/favicon.ico":
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/auth/login":
		return true
	case r.Method == http.MethodGet && (r.URL.Path == "/assets/login.css" || r.URL.Path == "/assets/login.js"):
		return true
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/assets/login-duck-") && strings.HasSuffix(r.URL.Path, ".png"):
		return r.URL.Path == "/assets/login-duck-base.png" || r.URL.Path == "/assets/login-duck-hands.png"
	default:
		return false
	}
}

func (s *Server) hasValidSession(r *http.Request) bool {
	cookie, err := r.Cookie(dashboardSessionCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.sessions[cookie.Value]
	return ok
}

func (s *Server) activityStreamAuthorized(r *http.Request) bool {
	if s.currentSettings().PasswordHash == "" {
		return true
	}
	return s.hasValidSession(r)
}

func isDashboardAPIRequest(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/favicon.ico" || r.URL.Path == "/auth/logout"
}
