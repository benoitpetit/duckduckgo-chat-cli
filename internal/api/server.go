package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/ui"

	"github.com/gin-gonic/gin"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "duckduckgo-chat-cli/docs" // Import generated swagger docs
)

var server *http.Server
var router *gin.Engine

// IsRunning checks if the API server is currently active.
func IsRunning() bool {
	return server != nil
}

// StartServer starts the API server in a new goroutine.
func StartServer(chatSession *chat.Chat, cfg *config.Config, port int) {
	if server != nil {
		ui.Warningln("API server is already running.")
		return
	}

	// Set Gin mode based on environment
	if os.Getenv("DEBUG") != "true" {
		gin.SetMode(gin.ReleaseMode)
	}

	router = setupRouter(chatSession, cfg)

	server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		ui.Systemln("Starting enhanced API server on port %d", port)
		ui.Systemln("API Documentation available at: http://localhost:%d/doc/index.html", port)
		ui.Systemln("API Base URL: http://localhost:%d/api/v1", port)

		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			ui.Errorln("API server error: %v", err)
			server = nil
		}
	}()
}

// StopServer gracefully shuts down the API server.
func StopServer() {
	if server == nil {
		ui.Warningln("API server is not running.")
		return
	}

	ui.Systemln("Stopping API server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		ui.Errorln("API server shutdown error: %v", err)
	} else {
		ui.Systemln("API server stopped.")
	}
	server = nil
	router = nil
}

// setupRouter configures the Gin router with all routes and middleware
func setupRouter(chatSession *chat.Chat, cfg *config.Config) *gin.Engine {
	router := gin.New()

	// Add middleware conditionally
	if cfg.API.ShowGinLogs {
		router.Use(gin.Logger())
	}
	router.Use(gin.Recovery())
	router.Use(corsMiddleware())

	// API root with basic info
	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Duck.ai Chat CLI API",
			"version": "1.3.0",
			"docs":    "/doc/index.html",
			"api":     "/api/v1",
		})
	})

	// Swagger documentation route
	router.GET("/doc/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// Chat endpoints
		v1.POST("/chat", ChatHandler(chatSession, cfg))
		v1.GET("/history", HistoryHandler(chatSession))
		v1.DELETE("/history", ClearHistoryHandler(chatSession, cfg))

		// Model endpoints
		v1.GET("/models", ModelsHandler(chatSession))
		v1.POST("/models", ModelChangeHandler(chatSession))

		// Session endpoints
		v1.GET("/session", SessionInfoHandler(chatSession))

		// Health endpoint
		v1.GET("/health", HealthHandler())
	}

	return router
}

// corsMiddleware adds CORS headers to allow cross-origin requests
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
