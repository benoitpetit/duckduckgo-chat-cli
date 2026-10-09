package api

import (
	"context"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/ui"
	"duckduckgo-chat-cli/internal/version"

	"github.com/gin-gonic/gin"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "duckduckgo-chat-cli/docs" // Import generated swagger docs
)

var server *http.Server
var router *gin.Engine
var serverMu sync.Mutex

// IsRunning checks if the API server is currently active.
func IsRunning() bool {
	serverMu.Lock()
	defer serverMu.Unlock()
	return server != nil
}

// StartServer starts the API server in a new goroutine.
func StartServer(chatSession *chat.Chat, cfg *config.Config, port int) {
	serverMu.Lock()
	defer serverMu.Unlock()
	if server != nil {
		ui.Warningln("API server is already running.")
		return
	}

	// Set Gin mode based on environment
	if os.Getenv("DEBUG") != "true" {
		gin.SetMode(gin.ReleaseMode)
	}

	host := strings.TrimSpace(cfg.API.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	if !isLoopbackHost(host) && strings.TrimSpace(cfg.API.APIKey) == "" {
		ui.Errorln("Refusing to expose the API on %s without an API key", host)
		return
	}

	// The API owns its conversation session. Sharing the interactive Chat
	// instance would mix histories and allow terminal/API requests to race.
	apiCfg := *cfg
	apiCfg.Library.Directories = append([]string(nil), cfg.Library.Directories...)
	apiCfg.API.AllowedOrigins = append([]string(nil), cfg.API.AllowedOrigins...)
	apiCfg.Prompts = make(map[string]string, len(cfg.Prompts))
	for name, prompt := range cfg.Prompts {
		apiCfg.Prompts[name] = prompt
	}
	apiChat := chat.NewChat("", "", "", "", chatSession.Model, &apiCfg)
	router = setupRouter(NewSession(apiChat, &apiCfg), &apiCfg)

	srv := &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:           router,
		ReadHeaderTimeout: 15 * time.Second,
		// Streaming responses must not be terminated by a fixed write timeout.
		// Individual requests carry their own context and cancellation policy.
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}
	server = srv

	go func() {
		ui.Systemln("Starting API server on %s", srv.Addr)
		ui.Systemln("API Documentation available at: http://%s/doc/index.html", srv.Addr)
		ui.Systemln("API Base URL: http://%s/api/v1", srv.Addr)

		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			ui.Errorln("API server error: %v", err)
			serverMu.Lock()
			server = nil
			serverMu.Unlock()
		}
	}()
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// StopServer gracefully shuts down the API server.
func StopServer() {
	serverMu.Lock()
	srv := server
	serverMu.Unlock()
	if srv == nil {
		ui.Warningln("API server is not running.")
		return
	}

	ui.Systemln("Stopping API server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		ui.Errorln("API server shutdown error: %v", err)
	} else {
		ui.Systemln("API server stopped.")
	}
	serverMu.Lock()
	if server == srv {
		server = nil
		router = nil
	}
	serverMu.Unlock()
}

// setupRouter configures the Gin router with all routes and middleware
func setupRouter(session *Session, cfg *config.Config) *gin.Engine {
	router := gin.New()

	// Add middleware conditionally
	if cfg.API.ShowGinLogs {
		router.Use(gin.Logger())
	}
	router.Use(gin.Recovery())
	router.Use(corsMiddleware(cfg.API.AllowedOrigins))

	// API root with basic info
	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Duck.ai Chat CLI API",
			"version": version.Current,
			"docs":    "/doc/index.html",
			"api":     "/api/v1",
		})
	})

	// Swagger documentation route
	router.GET("/doc/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))

	// API v1 routes
	v1 := router.Group("/api/v1")
	v1.Use(apiKeyMiddleware(cfg.API.APIKey))
	{
		// Chat endpoints
		v1.POST("/chat", ChatHandler(session))
		v1.GET("/history", HistoryHandler(session))
		v1.DELETE("/history", ClearHistoryHandler(session))

		// Model endpoints
		v1.GET("/models", ModelsHandler(session))
		v1.POST("/models", ModelChangeHandler(session))

		// Session endpoints
		v1.GET("/session", SessionInfoHandler(session))

		// Health endpoint
		v1.GET("/health", HealthHandler())
	}

	return router
}

// corsMiddleware adds CORS headers to allow cross-origin requests
func corsMiddleware(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if _, ok := allowed[origin]; ok {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Vary", "Origin")
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-API-Key, accept, origin, Cache-Control, X-Requested-With")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

func apiKeyMiddleware(expected string) gin.HandlerFunc {
	expected = strings.TrimSpace(expected)
	return func(c *gin.Context) {
		if expected == "" {
			c.Next()
			return
		}
		provided := strings.TrimSpace(c.GetHeader("X-API-Key"))
		if provided == "" {
			provided = strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
		}
		if provided != expected {
			c.AbortWithStatusJSON(http.StatusUnauthorized, NewErrorResponse(ErrorCodeUnauthorized, "Unauthorized", "A valid API key is required"))
			return
		}
		c.Next()
	}
}
