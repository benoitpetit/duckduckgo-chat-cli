package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"duckduckgo-chat-cli/internal/activity"
	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/api"
	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/command"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/dashboard"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/ui"
	"duckduckgo-chat-cli/internal/update"
	"duckduckgo-chat-cli/internal/voice"

	"github.com/AlecAivazis/survey/v2"
	"github.com/c-bata/go-prompt"
	"github.com/fatih/color"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// Version will be set at build time via ldflags
var Version = "dev"

// cliUsageLogoInset is how many rows of logo sit above "Usage:" in --help.
const cliUsageLogoInset = 4

var chatSession *chat.Chat
var cfg *config.Config
var dashboardServer *dashboard.Server
var dashboardWindow *dashboard.ChromiumWindow
var dashboardWindowMu sync.Mutex
var dashboardHistory *dashboard.HistoryStore
var dashboardActivity *activity.Hub
var cliShutdown func()
var executorMu sync.Mutex

// themeConsoleWriter adapts go-prompt's fixed ANSI colors to the active CLI
// palette. The theme is resolved for every redraw, so /config applies at once.
type themeConsoleWriter struct {
	prompt.ConsoleWriter
	colorEnabled bool
}

func newThemeConsoleWriter() prompt.ConsoleWriter {
	return &themeConsoleWriter{
		ConsoleWriter: prompt.NewStdoutWriter(),
		colorEnabled:  term.IsTerminal(int(os.Stdout.Fd())) && !termenv.EnvNoColor(),
	}
}

func (w *themeConsoleWriter) SetColor(fg, bg prompt.Color, bold bool) {
	if !w.colorEnabled {
		return
	}

	// Reset the previous style before applying this prompt segment's colors.
	w.WriteRawStr("\x1b[0m")
	theme := ui.CurrentTheme()
	if foreground := promptColorHex(theme, fg, false); foreground != "" {
		writePromptRGB(w, foreground, 38)
	}
	if background := promptColorHex(theme, bg, true); background != "" {
		writePromptRGB(w, background, 48)
	}
	if bold {
		w.WriteRawStr("\x1b[1m")
	}
}

func promptColorHex(theme ui.Theme, color prompt.Color, background bool) string {
	if background && color == prompt.DefaultColor {
		return ""
	}
	switch color {
	case prompt.DefaultColor, prompt.White, prompt.LightGray:
		return theme.Colors.Foreground
	case prompt.Black:
		return "#171717"
	case prompt.DarkGray:
		return theme.Colors.Muted
	case prompt.DarkRed, prompt.Red:
		return theme.Colors.Error
	case prompt.DarkGreen, prompt.Green:
		return theme.Colors.Success
	case prompt.Brown, prompt.Yellow:
		return theme.Colors.Warning
	case prompt.DarkBlue, prompt.Blue, prompt.Purple, prompt.Fuchsia:
		return theme.Colors.Accent
	case prompt.Cyan, prompt.Turquoise:
		return theme.Colors.Info
	default:
		return theme.Colors.Foreground
	}
}

func writePromptRGB(writer prompt.ConsoleWriter, value string, channel int) {
	value = strings.TrimPrefix(value, "#")
	if len(value) != 6 {
		return
	}
	rgb, err := strconv.ParseUint(value, 16, 24)
	if err != nil {
		return
	}
	writer.WriteRawStr(fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", channel, rgb>>16, (rgb>>8)&0xff, rgb&0xff))
}

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

func newShutdownFinalizer(stopSnapshots func(), saveConversation func() error, saveAnalytics func() error, stopAPI func() error, stopDashboard func() error, stopVoice func() error, shutdownBrowser func() error, restore func() error) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if stopSnapshots != nil {
				stopSnapshots()
			}
			// Stop request-serving handlers before archiving state and closing the
			// shared browser, so an in-flight handler cannot start a new capture
			// after the browser has been shut down or write to the session after
			// it has been archived.
			for _, step := range []struct {
				name string
				run  func() error
			}{
				{"api", stopAPI},
				{"dashboard", stopDashboard},
				{"voice", stopVoice},
				{"conversation", saveConversation},
				{"analytics", saveAnalytics},
				{"browser", shutdownBrowser},
				{"terminal", restore},
			} {
				if step.run != nil {
					if err := step.run(); err != nil {
						ui.Warningln("Could not finalize %s: %v", step.name, err)
					}
				}
			}
		})
	}
}

func startSnapshotRecorder(store *dashboard.HistoryStore, tracker *analytics.ChatAnalytics) func() {
	if store == nil || tracker == nil {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := store.Save(tracker.Snapshot()); err != nil {
					ui.Warningln("Could not save dashboard snapshot: %v", err)
				}
			case <-stop:
				return
			}
		}
	}()
	return func() {
		once.Do(func() { close(stop) })
		<-done
	}
}

func refreshDashboardSettings(configured *config.Config) {
	if configured == nil {
		return
	}
	if dashboardServer != nil {
		dashboardServer.UpdateSettings(configured.Dashboard)
	}
	if dashboardHistory != nil {
		if err := dashboardHistory.SetRetentionDays(configured.Dashboard.RetentionDays); err != nil {
			ui.Warningln("Could not update dashboard history retention: %v", err)
		}
	}
	if chatSession != nil && chatSession.HistoryManager != nil {
		if err := chatSession.HistoryManager.SetRetentionDays(configured.Dashboard.RetentionDays); err != nil {
			ui.Warningln("Could not update conversation history retention: %v", err)
		}
	}
}

func handleDashboardCommand(action string) {
	if dashboardServer == nil {
		ui.Errorln("Local dashboard is not initialized.")
		return
	}
	switch action {
	case "open":
		dashboardWindowMu.Lock()
		if dashboardWindow != nil && dashboardWindow.Open() {
			_, address := dashboardServer.Status()
			dashboardWindowMu.Unlock()
			ui.AIln("Local dashboard app is already open: %s", address)
			return
		}
		dashboardWindow = nil
		dashboardWindowMu.Unlock()
		_, address := dashboardServer.Status()
		if address == "" {
			var err error
			address, err = dashboardServer.Start()
			if err != nil {
				ui.Errorln("Could not start local dashboard: %v", err)
				return
			}
			if dashboardActivity != nil {
				dashboardActivity.Publish(activity.Event{Category: "dashboard", Status: "info", Summary: "Local dashboard started"})
			}
		}
		window, err := dashboard.OpenChromiumApp(context.Background(), address)
		if err != nil {
			ui.Errorln("Could not open local dashboard app: %v", err)
			return
		}
		dashboardWindowMu.Lock()
		dashboardWindow = window
		dashboardWindowMu.Unlock()
		go func() {
			<-window.Done()
			dashboardWindowMu.Lock()
			if dashboardWindow == window {
				dashboardWindow = nil
			}
			dashboardWindowMu.Unlock()
		}()
		ui.AIln("Local dashboard opened in an app window: %s", address)
	case "on":
		if running, address := dashboardServer.Status(); running {
			ui.AIln("Local dashboard is already running: %s", address)
			return
		}
		address, err := dashboardServer.Start()
		if err != nil {
			ui.Errorln("Could not start local dashboard: %v", err)
			return
		}
		if dashboardActivity != nil {
			dashboardActivity.Publish(activity.Event{Category: "dashboard", Status: "info", Summary: "Local dashboard started"})
		}
		ui.AIln("Local dashboard: %s", address)
	case "off":
		if err := closeDashboardWindow(); err != nil {
			ui.Warningln("Could not close the dashboard app window: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := dashboardServer.Stop(ctx); err != nil {
			ui.Errorln("Could not stop local dashboard: %v", err)
			return
		}
		if dashboardActivity != nil {
			dashboardActivity.Publish(activity.Event{Category: "dashboard", Status: "info", Summary: "Local dashboard stopped"})
		}
		ui.AIln("Local dashboard stopped.")
	case "status":
		if running, address := dashboardServer.Status(); running {
			ui.AIln("Local dashboard is running: %s", address)
		} else {
			ui.Mutedln("Local dashboard is stopped.")
		}
	case "":
		ui.Mutedln("Usage: /dashboard on|off|open|status")
	default:
		ui.Errorln("Invalid dashboard action %q. Usage: /dashboard on|off|open|status", action)
	}
}

func closeDashboardWindow() error {
	dashboardWindowMu.Lock()
	window := dashboardWindow
	dashboardWindow = nil
	dashboardWindowMu.Unlock()
	if window != nil {
		return window.Close()
	}
	return nil
}

// getCommands returns the command suggestions for autocompletion
func getCommands() []prompt.Suggest {
	registry := command.GetCommandRegistry()
	commands := make([]prompt.Suggest, 0, len(registry.Commands))

	for _, name := range command.GetSupportedCommands() {
		cmd := registry.Commands[name]
		usage := strings.TrimSpace(cmd.Usage)
		if usage == "" {
			usage = cmd.Name
		}
		commands = append(commands, prompt.Suggest{
			Text:        cmd.Name,
			Description: fmt.Sprintf("%s  %s", usage, strings.TrimSpace(cmd.Description)),
		})
	}

	return commands
}

var commands = getCommands()

func completer(d prompt.Document) []prompt.Suggest {
	text := d.TextBeforeCursor()
	segment := text
	if i := strings.LastIndex(text, "&&"); i >= 0 {
		segment = strings.TrimSpace(text[i+2:])
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

type cliOptions struct {
	help       bool
	version    bool
	jsonOutput bool
	prompt     string
	promptSet  bool
	model      string
}

func parseCLIOptions(args []string) (cliOptions, error) {
	var options cliOptions
	flags := flag.NewFlagSet("duckchat", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&options.help, "help", false, "Show this help message")
	flags.BoolVar(&options.help, "h", false, "Show this help message")
	flags.BoolVar(&options.version, "version", false, "Show version information")
	flags.BoolVar(&options.jsonOutput, "json", false, "Output one-shot response as JSON")
	flags.StringVar(&options.prompt, "prompt", "", "Send one prompt and exit; use - to read from stdin")
	flags.StringVar(&options.model, "model", "", "Select a model for --prompt")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, fmt.Errorf("unexpected positional argument %q", flags.Arg(0))
	}
	options.promptSet = false
	flags.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "prompt" {
			options.promptSet = true
		}
	})
	if options.model != "" && !options.promptSet {
		return options, fmt.Errorf("--model can only be used with --prompt")
	}
	if options.jsonOutput && !options.promptSet {
		return options, fmt.Errorf("--json can only be used with --prompt")
	}
	if options.promptSet && options.prompt != "-" && strings.TrimSpace(options.prompt) == "" {
		return options, fmt.Errorf("--prompt cannot be empty")
	}
	if options.model != "" {
		if _, ok := models.ResolveModel(options.model); !ok {
			return options, fmt.Errorf("unknown model %q", options.model)
		}
	}
	return options, nil
}

func printCLIUsage(out io.Writer) {
	// PrintLogoBesideInset prints these lines next to the logo, or above it on a
	// narrow terminal, and writes them verbatim when there is no logo to draw.
	// The inset drops the usage block a little below the top of the mascot so
	// the two read as a header rather than two stacked columns.
	_ = ui.PrintLogoBesideInset(out, []string{
		"Usage: duckchat [options]",
		"",
		"Options:",
		"  -h, --help          Show this help message",
		"      --version       Show version information",
		"      --prompt TEXT   Send one prompt and exit; use - to read from stdin",
		"      --model ID      Select a model for --prompt",
		"      --json          Output one-shot response as JSON",
	}, cliUsageLogoInset)
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	return encoder.Encode(value)
}

func printOneShotError(options cliOptions, message string) {
	if options.jsonOutput {
		_ = writeJSON(map[string]string{"error": message})
		return
	}
	fmt.Fprintln(os.Stderr, "duckchat:", message)
}

func main() {
	options, err := parseCLIOptions(os.Args[1:])
	if err != nil {
		if slices.Contains(os.Args[1:], "--json") {
			_ = writeJSON(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintln(os.Stderr, "duckchat:", err)
			printCLIUsage(os.Stderr)
		}
		os.Exit(2)
	}
	if options.help {
		printCLIUsage(os.Stdout)
		return
	}
	if options.version {
		fmt.Printf("DuckDuckGo AI Chat CLI version %s\n", Version)
		return
	}
	if options.promptSet {
		os.Exit(runOneShot(options))
	}
	runInteractive()
}

func runOneShot(options cliOptions) int {
	// Keep machine-readable response text on stdout and initialization/errors on stderr.
	color.Output = os.Stderr
	if options.jsonOutput {
		color.Output = io.Discard
		ui.SetSpinnerDisabled(true)
	}
	color.NoColor = true
	if options.prompt != "-" && strings.TrimSpace(options.prompt) == "" {
		printOneShotError(options, "prompt is empty")
		return 2
	}

	cfg = config.Initialize()
	if !cfg.TOSAccepted && !term.IsTerminal(int(os.Stdin.Fd())) {
		printOneShotError(options, "accept the terms in interactive mode before using --prompt")
		return 1
	}
	if !config.AcceptTermsOfService(cfg, survey.WithStdio(os.Stdin, os.Stderr, os.Stderr)) {
		printOneShotError(options, "terms of service were not accepted")
		return 1
	}
	promptText := options.prompt
	if promptText == "-" {
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			printOneShotError(options, "could not read prompt from stdin: "+err.Error())
			return 1
		}
		promptText = strings.TrimSpace(string(content))
	}
	if strings.TrimSpace(promptText) == "" {
		printOneShotError(options, "prompt is empty")
		return 2
	}

	model := models.GetModel(cfg.DefaultModel)
	if options.model != "" {
		model, _ = models.ResolveModel(options.model)
	}
	chatSession = chat.NewChat("", "", "", "", model, cfg)
	defer func() {
		if err := chat.ShutdownBrowser(); err != nil {
			ui.Warningln("Could not shut down browser: %v", err)
		}
	}()
	response, err := chat.ProcessInputContext(context.Background(), chatSession, promptText, cfg)
	if err != nil {
		printOneShotError(options, err.Error())
		return 1
	}
	if response == "" {
		printOneShotError(options, "received an empty response")
		return 1
	}
	if err := chatSession.SaveCurrentSession(); err != nil {
		printOneShotError(options, "could not save conversation: "+err.Error())
		return 1
	}
	if options.jsonOutput {
		if err := writeJSON(map[string]string{"response": response, "model": string(model)}); err != nil {
			fmt.Fprintln(os.Stderr, "duckchat: could not write JSON response:", err)
			return 1
		}
	} else {
		fmt.Println(response)
	}
	return 0
}

func runInteractive() {
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

	ui.Systemln("Welcome to DuckDuckGo AI Chat CLI!")

	cfg = config.Initialize()
	models.CheckChromeVersion()

	if !config.AcceptTermsOfService(cfg) {
		ui.Warningln("You must accept the terms to use this app. Exiting.")
		return
	}

	dashboardActivity = activity.NewHub()
	dashboardActivity.SetConversationContentEnabled(cfg.Dashboard.ShowConversationContent)
	chatSession = chat.InitializeSession(cfg)
	chatSession.Activity = dashboardActivity
	dashboardActivity.Publish(activity.Event{Category: "session", Status: "info", Summary: "CLI session started", Model: string(chatSession.Model)})
	dashboardHistory = dashboard.NewHistoryStore(config.DashboardHistoryPath(), cfg.Dashboard.RetentionDays)
	if err := dashboardHistory.SetRetentionDays(cfg.Dashboard.RetentionDays); err != nil {
		ui.Warningln("Could not apply dashboard history retention: %v", err)
	}
	if chatSession.HistoryManager != nil {
		if err := chatSession.HistoryManager.SetRetentionDays(cfg.Dashboard.RetentionDays); err != nil {
			ui.Warningln("Could not apply conversation history retention: %v", err)
		}
	}
	if err := dashboardHistory.Save(chatSession.Analytics.Snapshot()); err != nil {
		ui.Warningln("Could not save initial dashboard snapshot: %v", err)
	}
	stopSnapshots := startSnapshotRecorder(dashboardHistory, chatSession.Analytics)
	dashboardServer = dashboard.NewServer(dashboard.Dependencies{
		Analytics: chatSession.Analytics,
		History:   dashboardHistory,
		Sessions:  chatSession.HistoryManager,
		Commands:  command.GetCommandRegistry,
		Config:    cfg.Dashboard,
		Analyze:   chat.NewDashboardAnalyzer(cfg).Analyze,
		Activity:  dashboardActivity,
	})
	cliShutdown = newShutdownFinalizer(
		stopSnapshots,
		chatSession.SaveCurrentSession,
		func() error { return dashboardHistory.Save(chatSession.Analytics.Snapshot()) },
		func() error {
			if api.IsRunning() {
				api.StopServer()
			}
			return nil
		},
		func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			windowErr := closeDashboardWindow()
			serverErr := dashboardServer.Stop(ctx)
			if windowErr != nil {
				return windowErr
			}
			return serverErr
		},
		func() error {
			voice.StopActive()
			return nil
		},
		chat.ShutdownBrowser,
		restoreTerminalState,
	)
	if cfg.Dashboard.Autostart {
		if address, err := dashboardServer.Start(); err != nil {
			ui.Warningln("Local dashboard could not start: %v", err)
		} else {
			dashboardActivity.Publish(activity.Event{Category: "dashboard", Status: "info", Summary: "Local dashboard started"})
			ui.AIln("Local dashboard: %s", address)
		}
	}

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

	// Handle interrupts only after the shared finalizer exists.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	go func() {
		for range sigChan {
			if chatSession != nil && chatSession.CancelCurrentRequest() {
				ui.Warningln("\nRequest canceled.")
				continue
			}
			voice.StopActive()
			executorMu.Lock()
			ui.Warningln("\nReceived interrupt. Exiting gracefully.")
			if chatSession != nil {
				chatSession.ShowSessionStats()
			}
			if dashboardActivity != nil {
				dashboardActivity.Publish(activity.Event{Category: "session", Status: "info", Summary: "CLI session stopping"})
			}
			if cliShutdown != nil {
				cliShutdown()
			}
			os.Exit(0)
		}
	}()

	p := prompt.New(
		executor,
		completer,
		prompt.OptionWriter(newThemeConsoleWriter()),
		prompt.OptionMaxSuggestion(8),
		prompt.OptionTitle("duckduckgo-chat-cli"),
		prompt.OptionPrefix("You: "),
		prompt.OptionPrefixTextColor(prompt.Blue),
		prompt.OptionSuggestionTextColor(prompt.LightGray),
		prompt.OptionSuggestionBGColor(prompt.DarkGray),
		prompt.OptionSelectedSuggestionTextColor(prompt.Black),
		prompt.OptionSelectedSuggestionBGColor(prompt.Green),
		prompt.OptionDescriptionTextColor(prompt.LightGray),
		prompt.OptionDescriptionBGColor(prompt.DarkGray),
		prompt.OptionSelectedDescriptionTextColor(prompt.Black),
		prompt.OptionSelectedDescriptionBGColor(prompt.Green),
		prompt.OptionPreviewSuggestionTextColor(prompt.Turquoise),
		prompt.OptionPreviewSuggestionBGColor(prompt.DarkGray),
	)
	p.Run()
	if dashboardActivity != nil {
		dashboardActivity.Publish(activity.Event{Category: "session", Status: "info", Summary: "CLI session stopping"})
	}
	if cliShutdown != nil {
		cliShutdown()
	}

}

func executor(input string) {
	executorMu.Lock()
	defer executorMu.Unlock()
	if input == "" {
		return
	}
	if strings.HasPrefix(strings.TrimSpace(input), "/") {
		input = strings.TrimSpace(input)
	}
	if input == "" {
		return
	}
	var commandActivity activity.Event
	commandName := ""
	if dashboardActivity != nil && strings.HasPrefix(strings.TrimSpace(input), "/") {
		commandName = strings.Fields(strings.TrimSpace(input))[0]
		commandActivity = dashboardActivity.Publish(activity.Event{Category: "command", Status: "started", Summary: "Command " + commandName + " started"})
		defer func() {
			dashboardActivity.Publish(activity.Event{Category: "command", Status: "completed", Summary: "Command " + commandName + " completed", OperationID: commandActivity.OperationID})
		}()
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
	if err := command.ValidateChainedCommand(chainedCmd); err != nil {
		ui.Errorln("Invalid command chain: %v", err)
		return
	}
	if len(chainedCmd.Commands) == 1 && chainedCmd.Commands[0].Type == "/exit" {
		if chainedCmd.Prompt != "" {
			ui.Errorln("Invalid command: /exit does not accept a prompt")
			return
		}
		exitCLI(commandActivity.OperationID)
		return
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

func exitCLI(operationID string) {
	ui.Warningln("\nExiting chat. Goodbye!")
	if dashboardActivity != nil {
		dashboardActivity.Publish(activity.Event{Category: "command", Status: "completed", Summary: "Command /exit completed", OperationID: operationID})
		dashboardActivity.Publish(activity.Event{Category: "session", Status: "info", Summary: "CLI session stopping"})
	}
	if chatSession != nil {
		chatSession.ShowSessionStats()
	}
	if cliShutdown != nil {
		cliShutdown()
	}
	os.Exit(0)
}

func handleCommandChain(chatSession *chat.Chat, cfg *config.Config, chainedCmd *command.ChainedCommand) {
	handleCommandChainWithRoleProcessor(chatSession, cfg, chainedCmd, chat.ProcessInputWithContext)
}

func handleCommandChainWithProcessor(chatSession *chat.Chat, cfg *config.Config, chainedCmd *command.ChainedCommand, process func(*chat.Chat, string, *config.Config)) {
	handleCommandChainWithRoleProcessor(chatSession, cfg, chainedCmd, func(session *chat.Chat, contextContent, prompt string, configured *config.Config) {
		input := contextContent
		if prompt != "" {
			if input != "" {
				input += "\n\n"
			}
			input += prompt
		}
		process(session, input, configured)
	})
}

func handleCommandChainWithRoleProcessor(chatSession *chat.Chat, cfg *config.Config, chainedCmd *command.ChainedCommand, process func(*chat.Chat, string, string, *config.Config)) {
	chainCtx := chatcontext.New()

	for _, cmd := range chainedCmd.Commands {
		switch cmd.Type {
		case "/file":
			var fileErr error
			trackActivity("file", "File operation", func() { fileErr = chat.HandleFileCommand(chatSession, cmd.Raw, cfg, chainCtx) })
			if fileErr != nil {
				return
			}
		case "/url":
			trackActivity("url", "URL operation", func() { chat.HandleURLCommand(chatSession, cmd.Raw, cfg, chainCtx) })
		case "/search":
			trackActivity("search", "Search operation", func() { chat.HandleSearchCommand(chatSession, cmd.Raw, cfg, chainCtx) })
		default:
			ui.Errorln("Command '%s' is not supported in a command chain.", cmd.Type)
			return
		}
	}

	if chainCtx.IsEmpty() {
		if chainedCmd.Prompt != "" {
			// This case is for when the user just types "-- some prompt"
			process(chatSession, "", chainedCmd.Prompt, cfg)
		}
		return
	}

	// We have context. Now check for a prompt.
	contextContent := chainCtx.String()
	if chainedCmd.Prompt != "" {
		chatSession.QueueImageAttachments(chainCtx.ImageAttachments())
		process(chatSession, contextContent, chainedCmd.Prompt, cfg)
	} else {
		// Context loaded, but no prompt. Add to session and notify user.
		chatSession.AddContextMessageWithImages(contextContent, chainCtx.ImageAttachments())
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
		if err := chatSession.Clear(cfg); err != nil {
			ui.Warningln("Could not clear the conversation: %v", err)
		}
	case cmd.Type == "/history":
		chat.PrintHistory(chatSession)
	case cmd.Type == "/search":
		trackActivity("search", "Search operation", func() { chat.HandleSearchCommand(chatSession, cmd.Raw, cfg, nil) })
	case cmd.Type == "/file":
		trackActivity("file", "File operation", func() { chat.HandleFileCommand(chatSession, cmd.Raw, cfg, nil) })
	case cmd.Type == "/library":
		trackActivity("library", "Library operation", func() { chat.HandleLibraryCommand(chatSession, cmd.Raw, cfg) })
	case cmd.Type == "/url":
		trackActivity("url", "URL operation", func() { chat.HandleURLCommand(chatSession, cmd.Raw, cfg, nil) })
	case cmd.Type == "/export":
		chat.HandleExportCommand(chatSession, cfg)
	case cmd.Type == "/copy":
		chat.HandleCopyCommand(chatSession)
	case cmd.Type == "/config":
		config.HandleConfiguration(cfg, chatSession)
		refreshDashboardSettings(cfg)
	case cmd.Type == "/model":
		newModel := models.HandleModelChange(chatSession, cmd.Args)
		if newModel != "" {
			chatSession.ChangeModel(models.GetModel(string(newModel)))
			if dashboardActivity != nil {
				dashboardActivity.Publish(activity.Event{Category: "model", Status: "changed", Summary: "Active model changed", Model: string(chatSession.Model)})
			}
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
	case cmd.Type == "/speak":
		deps := voice.Dependencies{
			Proof:     voice.CurrentProofProvider{},
			Signaling: &voice.DuckAIClient{HTTPClient: http.DefaultClient, BaseURL: "https://duck.ai"},
		}
		if err := voice.Run(context.Background(), deps, voice.ChromiumOpener{Settings: cfg.Speak}); err != nil {
			ui.Errorln("Voice session failed: %v", err)
		}
	case cmd.Type == "/stats":
		// Show current session analytics
		if chatSession != nil {
			chatSession.ShowSessionStats()
		} else {
			ui.Errorln("No active chat session found.")
		}
	case cmd.Type == "/dashboard":
		handleDashboardCommand(cmd.Args)
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

func trackActivity(category, summary string, run func()) {
	if dashboardActivity == nil {
		run()
		return
	}
	started := dashboardActivity.Publish(activity.Event{Category: category, Status: "started", Summary: summary + " started"})
	defer dashboardActivity.Publish(activity.Event{Category: category, Status: "completed", Summary: summary + " completed", OperationID: started.OperationID})
	run()
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
