package interfaces

import "duckduckgo-chat-cli/internal/models"

type ChatSession interface {
	ChangeModel(model models.Model)
	SetNativeTools(enabled, webSearch, imageGeneration bool)
}
