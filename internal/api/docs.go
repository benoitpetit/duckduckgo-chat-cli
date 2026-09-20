// Package api Duck.ai Chat CLI API
//
// This is the REST API for DuckDuckGo Chat CLI, providing programmatic access to AI chat functionality.
//
// The API allows you to:
// - Send messages to various AI models
// - Retrieve chat history
// - Manage AI models
// - Monitor system health
//
// All responses follow a consistent format with success/error indicators and standardized error codes.
//
//	@title						duckduckgo-chat-cli API
//	@version					1.5.1
//	@description				REST API for Duck.ai Chat CLI - programmatic access to AI chat functionality
//	@termsOfService				https://duck.ai/
//
//	@contact.name				devbyben
//	@contact.url				https://github.com/benoitpetit/duckduckgo-chat-cli
//	@contact.email				contact@devbyben.fr
//
//	@license.name				MIT
//	@license.url				https://opensource.org/licenses/MIT
//
//	@host						127.0.0.1:8080
//	@BasePath					/api/v1
//
//	@schemes					http https
//
//	@securityDefinitions.apikey	ApiKeyAuth
//	@in							header
//	@name						Authorization
//	@description				API key authorization. Example: "Bearer {api_key}"
//
//	@tag.name					Chat
//	@tag.description			Chat operations for sending messages and managing conversation
//
//	@tag.name					Models
//	@tag.description			AI model management and information
//
//	@tag.name					Health
//	@tag.description			System health and status monitoring
//
//	@tag.name					Session
//	@tag.description			Session management and information
//
//	@externalDocs.description	duckduckgo-chat-cli Documentation
//	@externalDocs.url			https://github.com/benoitpetit/duckduckgo-chat-cli/blob/main/README.md
package api
