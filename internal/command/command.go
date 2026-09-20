package command

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// CommandRegistry holds all CLI commands with their metadata
type CommandRegistry struct {
	Commands map[string]CommandInfo
}

// CommandInfo holds metadata about a command
type CommandInfo struct {
	Name         string
	Description  string
	Usage        string
	IsChainable  bool
	RequiresArgs bool
	Category     string
}

// GetCommandRegistry returns the centralized command registry
func GetCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		Commands: map[string]CommandInfo{
			"/help": {
				Name:        "/help",
				Description: "Show the welcome message and command list",
				Usage:       "/help",
				Category:    "core",
			},
			"/exit": {
				Name:        "/exit",
				Description: "Exit the chat",
				Usage:       "/exit",
				Category:    "core",
			},
			"/clear": {
				Name:        "/clear",
				Description: "Clear the chat history",
				Usage:       "/clear",
				Category:    "core",
			},
			"/history": {
				Name:        "/history",
				Description: "Show the chat history",
				Usage:       "/history",
				Category:    "core",
			},
			"/search": {
				Name:         "/search",
				Description:  "Search with a query",
				Usage:        "/search <query> [-- prompt]",
				IsChainable:  true,
				RequiresArgs: true,
				Category:     "context",
			},
			"/file": {
				Name:         "/file",
				Description:  "Chat with a file",
				Usage:        "/file <path> [-- prompt]",
				IsChainable:  true,
				RequiresArgs: false, // Can be used without args for file browser
				Category:     "context",
			},
			"/library": {
				Name:         "/library",
				Description:  "Chat with your library",
				Usage:        "/library [command] [args] [-- prompt]",
				IsChainable:  true,
				RequiresArgs: false,
				Category:     "context",
			},
			"/url": {
				Name:         "/url",
				Description:  "Chat with a URL",
				Usage:        "/url <url> [-- prompt]",
				IsChainable:  true,
				RequiresArgs: true,
				Category:     "context",
			},
			"/export": {
				Name:        "/export",
				Description: "Export the chat history",
				Usage:       "/export",
				Category:    "productivity",
			},
			"/copy": {
				Name:        "/copy",
				Description: "Copy the last response to the clipboard",
				Usage:       "/copy",
				Category:    "productivity",
			},
			"/config": {
				Name:        "/config",
				Description: "Open the configuration menu",
				Usage:       "/config",
				Category:    "core",
			},
			"/model": {
				Name:        "/model",
				Description: "Change the chat model",
				Usage:       "/model [model_name]",
				Category:    "core",
			},
			"/version": {
				Name:        "/version",
				Description: "Show version information",
				Usage:       "/version",
				Category:    "core",
			},
			"/api": {
				Name:        "/api",
				Description: "Start or stop the API server interactively",
				Usage:       "/api [port]",
				Category:    "core",
			},
			"/stats": {
				Name:        "/stats",
				Description: "Show real-time session analytics",
				Usage:       "/stats",
				Category:    "core",
			},
			"/update": {
				Name:        "/update",
				Description: "Update the CLI to the latest version",
				Usage:       "/update [--force]",
				Category:    "core",
			},
			"/load": {
				Name:        "/load",
				Description: "Load a previous chat session",
				Usage:       "/load [session_id]",
				Category:    "core",
			},
			"/prompt": {
				Name:        "/prompt",
				Description: "Manage and load custom prompts",
				Usage:       "/prompt <load|add|edit|remove|list> [name] [-- prompt] OR /prompt",
				IsChainable: false,
				Category:    "context",
			},
		},
	}
}

// GetSupportedCommands returns a list of all supported commands
func GetSupportedCommands() []string {
	registry := GetCommandRegistry()
	commands := make([]string, 0, len(registry.Commands))
	for cmdName := range registry.Commands {
		commands = append(commands, cmdName)
	}
	sort.Strings(commands)
	return commands
}

// GetCommandsByCategory returns commands grouped by category
func GetCommandsByCategory() map[string][]CommandInfo {
	registry := GetCommandRegistry()
	categories := make(map[string][]CommandInfo)

	for _, cmd := range registry.Commands {
		categories[cmd.Category] = append(categories[cmd.Category], cmd)
	}
	for category := range categories {
		sort.Slice(categories[category], func(i, j int) bool {
			return categories[category][i].Name < categories[category][j].Name
		})
	}

	return categories
}

// IsChainableCommand checks if a command can be used in a chain
func IsChainableCommand(cmdType string) bool {
	registry := GetCommandRegistry()
	if cmd, exists := registry.Commands[cmdType]; exists {
		return cmd.IsChainable
	}
	return false
}

// ExtractArguments extracts arguments from a command using regex patterns
func ExtractArguments(cmd *Command) map[string]string {
	args := make(map[string]string)

	switch cmd.Type {
	case "/search":
		// Extract search query
		if cmd.Args != "" {
			args["query"] = cmd.Args
		}

	case "/file":
		// Extract file path
		if cmd.Args != "" {
			args["path"] = cmd.Args
		}

	case "/url":
		// Extract URL
		if cmd.Args != "" {
			args["url"] = cmd.Args
		}

	case "/library":
		// Extract library subcommand and arguments
		parts := strings.Fields(cmd.Args)
		if len(parts) > 0 {
			args["subcommand"] = parts[0]
			if len(parts) > 1 {
				args["argument"] = strings.Join(parts[1:], " ")
			}
		}

	case "/model":
		// Extract model name/number
		if cmd.Args != "" {
			args["model"] = cmd.Args
		}

	case "/api":
		// Extract port number
		if cmd.Args != "" {
			args["port"] = cmd.Args
		}

	case "/export":
		// Extract export type
		if cmd.Args != "" {
			args["type"] = cmd.Args
		}

	case "/stats":
		// Stats command doesn't need arguments
		break
	}

	return args
}

// ValidateCommand performs additional validation on a parsed command
func ValidateCommand(cmd *Command) error {
	if _, exists := GetCommandRegistry().Commands[cmd.Type]; !exists {
		return fmt.Errorf("unknown command: %s", cmd.Type)
	}
	switch cmd.Type {
	case "/file":
		// An empty argument opens the interactive file picker.

	case "/url":
		if cmd.Args == "" {
			return fmt.Errorf("/url command requires a URL")
		}
		// Basic URL validation
		urlPattern := regexp.MustCompile(`^https?://|^[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
		if !urlPattern.MatchString(cmd.Args) {
			return fmt.Errorf("invalid URL format: %s", cmd.Args)
		}

	case "/search":
		if cmd.Args == "" {
			return fmt.Errorf("/search command requires a search query")
		}

	case "/model":
		// Model validation can be added here if needed

	case "/api":
		// Port validation can be added here if needed

	case "/stats":
		// Stats command doesn't need validation
		break

	case "/update":
		// Update command doesn't need validation
		break
	}

	return nil
}

// FormatCommand formats a command for display
func FormatCommand(cmd *Command) string {
	if cmd.Args != "" {
		return fmt.Sprintf("%s %s", cmd.Type, cmd.Args)
	}
	return cmd.Type
}

// FormatChainedCommand formats a chained command for display
func FormatChainedCommand(chainedCmd *ChainedCommand) string {
	var parts []string

	for _, cmd := range chainedCmd.Commands {
		parts = append(parts, FormatCommand(cmd))
	}

	result := strings.Join(parts, " && ")

	if chainedCmd.Prompt != "" {
		if result != "" {
			result += " -- " + chainedCmd.Prompt
		} else {
			result = chainedCmd.Prompt
		}
	}

	return result
}

// IsValidCommand checks if a command type is valid
func IsValidCommand(cmdType string) bool {
	registry := GetCommandRegistry()
	_, exists := registry.Commands[cmdType]
	return exists
}
