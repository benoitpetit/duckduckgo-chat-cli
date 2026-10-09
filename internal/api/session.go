package api

import (
	"sync"

	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/config"
)

// Session serializes access to the conversation owned by the HTTP API server.
type Session struct {
	mu   sync.RWMutex
	chat *chat.Chat
	cfg  *config.Config
}

func NewSession(chatSession *chat.Chat, cfg *config.Config) *Session {
	return &Session{chat: chatSession, cfg: cfg}
}
