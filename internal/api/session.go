package api

import (
	"sync"

	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/config"
)

// Session serializes access to one conversation. The CLI and HTTP server may
// use the same chat instance, so the lock protects concurrent handler access.
type Session struct {
	mu   sync.RWMutex
	chat *chat.Chat
	cfg  *config.Config
}

func NewSession(chatSession *chat.Chat, cfg *config.Config) *Session {
	return &Session{chat: chatSession, cfg: cfg}
}
