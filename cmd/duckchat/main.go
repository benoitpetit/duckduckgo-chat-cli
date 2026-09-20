package main

import (
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"duckduckgo-chat-cli/internal/api"
	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/command"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/ui"
	"duckduckgo-chat-cli/internal/update"

	"github.com/AlecAivazis/survey/v2"
	"github.com/c-bata/go-prompt"
	"golang.org/x/term"
)

// Version will be set at build time via ldflags
var Version = "dev"

var chatSession *chat.Chat
var cfg *config.Config

// Terminal state management
var originalState *term.State

// saveTerminalState saves the current terminal state for later restoration
func saveTerminalState() error {
	fd := int(os.Stdin.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		return err
	}
	originalState = state
	return nil
}

// restoreTerminalState restores the terminal to its original state
func restoreTerminalState() error {
	if originalState != nil {
		fd := int(os.Stdin.Fd())
		return term.Restore(fd, originalState)
	}
	return nil
}

// getCommands returns the command suggestions for autocompletion
func getCommands() []prompt.Suggest {
	registry := command.GetCommandRegistry()
	commands := make([]prompt.Suggest, 0, len(registry.Commands))

	for _, name := range command.GetSupportedCommands() {
		cmd := registry.Commands[name]
		commands = append(commands, prompt.Suggest{
			Text:        cmd.Name,
			Description: cmd.Description,
		})
	}

	return commands
}

var commands = getCommands()

func completer(d prompt.Document) []prompt.Suggest {
	text := d.TextBeforeCursor()
	segment := text
	if i := strings.LastIndex(text, "&&"); i >= 0 {
		segment = strings.TrimLeft(text[i+2:], " ")
	}

	// We only want to complete the command name, not its arguments.
	if strings.Contains(segment, " ") {
		return nil
	}

	// We only want to complete if the segment starts with a slash
	if strings.HasPrefix(segment, "/") {
		return prompt.FilterHasPrefix(commands, segment, true)
	}

	return nil
}

func main() {
	// Save the terminal state at startup
	if err := saveTerminalState(); err != nil {
		ui.Warningln("Warning: Could not save terminal state: %v", err)
		// Continue execution even if we can't save state
	}

	// Ensure terminal state is restored when main function exits
	defer func() {
		if err := restoreTerminalState(); err != nil {
			ui.Warningln("Warning: Could not restore terminal state: %v", err)
		}
	}()

	// create a channel to listen for interrupts
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		for range sigChan {
			if chatSession != nil && chatSession.CancelCurrentRequest() {
				ui.Warningln("\nRequest canceled.")
				continue
			}
			ui.Warningln("\nReceived interrupt. Exiting gracefully.")

			// Show session statistics before exiting
			if chatSession != nil {
				chatSession.ShowSessionStats()
			}

			// Restore terminal state before exiting
			if err := restoreTerminalState(); err != nil {
				ui.Warningln("Warning: Could not restore terminal state: %v", err)
			}
			os.Exit(0)
		}
	}()

	ui.Systemln("Welcome to DuckDuckGo AI Chat CLI!")

	cfg = config.Initialize()
	models.CheckChromeVersion()

	if !config.AcceptTermsOfService(cfg) {
		ui.Warningln("You must accept the terms to use this app. Exiting.")
		return
	}

	chatSession = chat.InitializeSession(cfg)

	if cfg.API.Enabled && cfg.API.Autostart {
		api.StartServer(chatSession, cfg, cfg.API.Port)
	}

	// Do not block the first prompt on the optional GitHub update check.
	go update.CheckForUpdatesAtStartup(Version)

	if cfg.ShowMenu {
		chat.PrintWelcomeMessage()
	} else {
		chat.PrintCommands()
	}

	p := prompt.New(
		executor,
		completer,
		prompt.OptionTitle("duckduckgo-chat-cli"),
		prompt.OptionPrefix("You: "),
		prompt.OptionPrefixTextColor(prompt.Blue),
	)
	p.Run()

}

func executor(input string) {
	if input == "" {
		return
	}
	if input == "/exit" {
		ui.Warningln("\nExiting chat. Goodbye!")

		// Show session statistics before exiting
		if chatSession != nil {
			chatSession.ShowSessionStats()
		}

		// Restore terminal state before exiting
		if err := restoreTerminalState(); err != nil {
			ui.Warningln("Warning: Could not restore terminal state: %v", err)
		}
		os.Exit(0)
	}
	if !strings.HasPrefix(strings.TrimSpace(input), "/") {
		if cfg.ConfirmLongInput && shouldConfirmLongInput(input) && !confirmSendMessage(input) {
			ui.Warningln("Message not sent.")
			return
		}
		chat.ProcessInput(chatSession, input, cfg)
		return
	}

	// Track command usage
	if strings.HasPrefix(input, "/") {
		commandName := strings.Fields(input)[0]
		if chatSession != nil && chatSession.Analytics != nil {
			chatSession.Analytics.RecordCommand(commandName)
		}
	}

	chainedCmd, err := command.Parse(input)
	if err != nil {
		ui.Errorln("Error parsing command: %v", err)
		return
	}
	for _, parsedCommand := range chainedCmd.Commands {
		if err := command.ValidateCommand(parsedCommand); err != nil {
			ui.Errorln("Invalid command: %v", err)
			return
		}
	}

	if len(chainedCmd.Commands) == 1 && !command.IsChainableCommand(chainedCmd.Commands[0].Type) {
		// Pour les commandes non chainables (ex: /prompt), passer le prompt à handleCommand
		if chainedCmd.Prompt != "" {
			chainedCmd.Commands[0].Raw = strings.TrimSpace(chainedCmd.Commands[0].Raw + " -- " + chainedCmd.Prompt)
		}
		handleCommand(chatSession, cfg, chainedCmd.Commands[0])
	} else if len(chainedCmd.Commands) > 1 || chainedCmd.Prompt != "" {
		handleCommandChain(chatSession, cfg, chainedCmd)
	} else if len(chainedCmd.Commands) == 1 {
		handleCommand(chatSession, cfg, chainedCmd.Commands[0])
	}
}

func handleCommandChain(chatSession *chat.Chat, cfg *config.Config, chainedCmd *command.ChainedCommand) {
	chainCtx := chatcontext.New()

	for _, cmd := range chainedCmd.Commands {
		switch cmd.Type {
		case "/file":
			chat.HandleFileCommand(chatSession, cmd.Raw, cfg, chainCtx)
		case "/url":
			chat.HandleURLCommand(chatSession, cmd.Raw, cfg, chainCtx)
		case "/search":
			chat.HandleSearchCommand(chatSession, cmd.Raw, cfg, chainCtx)
		default:
			ui.Errorln("Command '%s' is not supported in a command chain.", cmd.Type)
			return
		}
	}

	if chainCtx.IsEmpty() {
		if chainedCmd.Prompt != "" {
			// This case is for when the user just types "-- some prompt"
			chat.ProcessInput(chatSession, chainedCmd.Prompt, cfg)
		}
		return
	}

	// We have context. Now check for a prompt.
	finalInput := chainCtx.String()
	if chainedCmd.Prompt != "" {
		finalInput += "\n\n" + chainedCmd.Prompt
		chat.ProcessInput(chatSession, finalInput, cfg)
	} else {
		// Context loaded, but no prompt. Add to session and notify user.
		chatSession.AddContextMessage(finalInput)
		ui.AIln("Context from the command chain has been added. You can now ask questions about it.")
	}
}

func handleCommand(chatSession *chat.Chat, cfg *config.Config, cmd *command.Command) {
	// if the input is empty, return
	if cmd.Raw == "" {
		return
	}

	switch {
	case cmd.Type == "/clear":
		chatSession.Clear(cfg)
	case cmd.Type == "/history":
		chat.PrintHistory(chatSession)
	case cmd.Type == "/search":
		chat.HandleSearchCommand(chatSession, cmd.Raw, cfg, nil)
	case cmd.Type == "/file":
		chat.HandleFileCommand(chatSession, cmd.Raw, cfg, nil)
	case cmd.Type == "/library":
		chat.HandleLibraryCommand(chatSession, cmd.Raw, cfg)
	case cmd.Type == "/url":
		chat.HandleURLCommand(chatSession, cmd.Raw, cfg, nil)
	case cmd.Type == "/export":
		chat.HandleExportCommand(chatSession, cfg)
	case cmd.Type == "/copy":
		chat.HandleCopyCommand(chatSession)
	case cmd.Type == "/config":
		config.HandleConfiguration(cfg, chatSession)
	case cmd.Type == "/model":
		newModel := models.HandleModelChange(chatSession, cmd.Args)
		if newModel != "" {
			chatSession.ChangeModel(models.GetModel(string(newModel)))
			cfg.DefaultModel = string(newModel)
			if err := config.SaveConfig(cfg); err != nil {
				ui.Errorln("Failed to save config: %v", err)
			}
		}
	case cmd.Type == "/help":
		chat.PrintWelcomeMessage()
	case cmd.Type == "/api":
		if api.IsRunning() {
			confirm := false
			prompt := &survey.Confirm{
				Message: "The API server is currently running. Do you want to stop it?",
				Default: true,
			}
			if err := survey.AskOne(prompt, &confirm); err != nil {
				ui.Warningln("API stop canceled.")
				return
			}
			if confirm {
				api.StopServer()
			}
		} else {
			if !cfg.API.Enabled {
				ui.Warningln("API is disabled in the configuration. Use /config to enable it.")
				return
			}
			port := cfg.API.Port
			if cmd.Args != "" {
				if p, err := strconv.Atoi(cmd.Args); err == nil {
					port = p
				} else {
					ui.Errorln("Invalid port number.")
					return
				}
			}
			api.StartServer(chatSession, cfg, port)
		}
	case cmd.Type == "/version":
		ui.AIln("DuckDuckGo AI Chat CLI version %s", Version)
		ui.Mutedln("Go version: %s", runtime.Version())
		ui.Mutedln("OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
	case cmd.Type == "/stats":
		// Show current session analytics
		if chatSession != nil {
			chatSession.ShowSessionStats()
		} else {
			ui.Errorln("No active chat session found.")
		}
	case cmd.Type == "/update":
		// Handle update command
		force := strings.Contains(cmd.Args, "--force")
		if err := update.HandleUpdateCommand(Version, force); err != nil {
			ui.Errorln("Update failed: %v", err)
		}
	case cmd.Type == "/load":
		chat.HandleLoadCommand(chatSession, cmd.Args)
	case cmd.Type == "/prompt":
		chat.HandlePromptCommand(chatSession, cmd.Raw, cfg)
	default:
		// Check if the input is potentially pasted content (long text, URLs, etc.)
		if cfg.ConfirmLongInput && shouldConfirmLongInput(cmd.Raw) {
			confirmed := confirmSendMessage(cmd.Raw)
			if !confirmed {
				ui.Warningln("Message not sent.")
				return
			}
		}
		chat.ProcessInput(chatSession, cmd.Raw, cfg)
	}
}

// shouldConfirmLongInput determines if input should be confirmed before sending
func shouldConfirmLongInput(input string) bool {
	// Trim whitespace for accurate length calculation
	trimmedInput := strings.TrimSpace(input)

	// Check if input is longer than 500 characters
	if len(trimmedInput) > 500 {
		return true
	}

	// Check if input looks like a URL (starts with http/https or contains common URL patterns)
	if strings.HasPrefix(trimmedInput, "http://") || strings.HasPrefix(trimmedInput, "https://") {
		return true
	}

	// Check for other URL-like patterns (www. or contains multiple dots suggesting a domain)
	if strings.HasPrefix(trimmedInput, "www.") || (strings.Count(trimmedInput, ".") >= 2 && !strings.Contains(trimmedInput, " ")) {
		return true
	}

	// Check if input contains newlines (multiline paste)
	if strings.Count(trimmedInput, "\n") > 3 {
		return true
	}

	return false
}

// confirmSendMessage asks user to confirm sending the message
func confirmSendMessage(input string) bool {
	// Show preview of the input
	preview := input
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}

	ui.Warningln("\nDetected potentially long or pasted content:")
	ui.Mutedln("Preview: %s", strings.ReplaceAll(preview, "\n", "\\n"))

	confirm := false
	prompt := &survey.Confirm{
		Message: "Do you want to send this as a message to the AI?",
		Default: false,
	}

	err := survey.AskOne(prompt, &confirm, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr))
	if err != nil {
		// If there's an error (like Ctrl+C), assume no
		return false
	}

	return confirm
}
