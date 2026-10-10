package chat

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/fatih/color"

	"duckduckgo-chat-cli/internal/ptytest"
)

// sgrSequence matches the colour escapes fatih/color wraps around help text.
var sgrSequence = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestRenderCommandsTableProducesWrappedColouredLines(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	previous := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = previous }()

	lines := renderCommandsTable([]CommandHelp{
		{Command: "/clear", Description: "Clear the chat history"},
		{Command: "/dashboard <on|off|open|status>", Description: "Open, start, stop, or check the local usage dashboard"},
	}, 30)

	if len(lines) == 0 {
		t.Fatal("expected the table to render some lines")
	}

	for i, line := range lines {
		if !strings.Contains(line, "\x1b[") {
			t.Errorf("line %d carries no colour sequence: %q", i, line)
		}
		if visible := len([]rune(sgrSequence.ReplaceAllString(line, ""))); visible > 30 {
			t.Errorf("line %d is %d visible cells wide, budget is 30: %q", i, visible, line)
		}
	}
}

func TestPrintWelcomeMessageDrawsResponsiveHelpPanel(t *testing.T) {
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("cannot allocate a pseudo-terminal: %v", err)
	}
	defer master.Close()

	if err := ptytest.SetSize(slave, 110, 40); err != nil {
		t.Fatalf("set pty size: %v", err)
	}

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	previousOutput := color.Output
	previousNoColor := color.NoColor
	color.Output = slave
	color.NoColor = false
	defer func() {
		color.Output = previousOutput
		color.NoColor = previousNoColor
	}()

	got, err := ptytest.Capture(master, slave, func() error {
		PrintWelcomeMessage()
		return nil
	})
	if err != nil {
		t.Fatalf("capturing /help output failed: %v", err)
	}

	plain := strings.ReplaceAll(sgrSequence.ReplaceAllString(got, ""), "\r\n", "\n")
	lines := strings.Split(plain, "\n")

	titleRow := -1
	for i, line := range lines {
		if strings.Contains(line, "DuckDuckGo AI Chat CLI - Help") {
			titleRow = i
			break
		}
	}
	if titleRow < 0 {
		t.Fatal("expected the help title to be printed")
	}

	if !strings.HasPrefix(lines[titleRow], "╭─ DuckDuckGo AI Chat CLI - Help") {
		t.Errorf("expected the title in the help panel border, got %q", lines[titleRow])
	}

	// Help content should be inside the same panel and remain within the terminal.
	commandsRow := -1
	for i, line := range lines {
		if strings.Contains(line, "Core Commands:") {
			commandsRow = i
			break
		}
	}
	if commandsRow < 0 {
		t.Fatal("expected the command tables to be printed")
	}
	if !strings.Contains(lines[commandsRow], "│ Core Commands:") {
		t.Errorf("Core Commands should be inside the panel, got %q", lines[commandsRow])
	}
	for i, line := range lines {
		if width := utf8.RuneCountInString(line); width > 110 {
			t.Errorf("line %d is %d cells wide, terminal is 110: %q", i, width, line)
		}
	}
}

func TestPrintWelcomeMessageFramesHelpOnTerminal(t *testing.T) {
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("cannot allocate a pseudo-terminal: %v", err)
	}
	defer master.Close()

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	previousOutput := color.Output
	color.Output = slave
	defer func() { color.Output = previousOutput }()
	color.NoColor = false

	got, err := ptytest.Capture(master, slave, func() error {
		PrintWelcomeMessage()
		return nil
	})
	if err != nil {
		t.Fatalf("capturing /help output failed: %v", err)
	}

	if !strings.Contains(got, "DuckDuckGo AI Chat CLI - Help") {
		t.Error("expected /help to still print its title")
	}
	plain := strings.ReplaceAll(sgrSequence.ReplaceAllString(got, ""), "\r\n", "\n")
	if !strings.Contains(plain, "╭─ DuckDuckGo AI Chat CLI - Help") {
		t.Error("expected /help title to be framed on a terminal")
	}
}

func TestPrintWelcomeMessageStaysPlainWhenRedirected(t *testing.T) {
	var out bytes.Buffer

	previousOutput := color.Output
	previousNoColor := color.NoColor
	color.Output = &out
	color.NoColor = true
	defer func() {
		color.Output = previousOutput
		color.NoColor = previousNoColor
	}()

	PrintWelcomeMessage()

	got := out.String()
	if strings.Contains(got, "\x1b[38;2;") {
		t.Error("redirected /help output must not contain truecolor sequences")
	}
	if !strings.Contains(got, "DuckDuckGo AI Chat CLI - Help") {
		t.Error("expected /help to still print its title")
	}
}

// On a terminal too narrow for the logo and readable text together, the help
// must fall back to plain full-width text with no logo and no escapes.
func TestPrintWelcomeMessageStacksOnANarrowTerminal(t *testing.T) {
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("pty unsupported: %v", err)
	}
	defer master.Close()

	if err := ptytest.SetSize(slave, 30, 40); err != nil {
		t.Fatalf("set pty size: %v", err)
	}

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	previousOutput, previousNoColor := color.Output, color.NoColor
	color.Output = slave
	color.NoColor = false
	defer func() {
		color.Output, color.NoColor = previousOutput, previousNoColor
	}()

	got, err := ptytest.Capture(master, slave, func() error {
		PrintWelcomeMessage()
		return nil
	})
	if err != nil {
		t.Fatalf("capturing /help output failed: %v", err)
	}

	plain := strings.ReplaceAll(sgrSequence.ReplaceAllString(got, ""), "\r\n", "\n")
	for _, want := range []string{"DuckDuckGo AI Chat CLI", "Core Commands:", "/clear", "Context Commands:", "Productivity Commands:", "API Documentation:"} {
		if !strings.Contains(plain, want) {
			t.Errorf("stacked help is missing %q", want)
		}
	}

	// The narrow panel must wrap content without exceeding the terminal width.
	for i, line := range strings.Split(plain, "\n") {
		if width := utf8.RuneCountInString(line); width > 30 {
			t.Errorf("line %d is %d cells wide, want at most 30: %q", i, width, line)
		}
	}
}

// One long command must not drag the whole section into the stacked layout:
// the commands that still fit keep their description in the description
// column, and only the offending row gives up its column.
func TestRenderCommandsTableStacksOnlyTheRowsThatDoNotFit(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	previous := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = previous }()

	const width = 72
	lines := renderCommandsTable([]CommandHelp{
		{Command: "/url <url> [-- prompt]", Description: "Chat with a URL"},
		{Command: "/prompt <load|add|edit|remove|list> [name] [-- prompt] OR /prompt", Description: "Manage and load custom prompts"},
		{Command: "/file <path> [-- prompt]", Description: "Chat with a file"},
	}, width)

	plain := make([]string, 0, len(lines))
	for _, line := range lines {
		plain = append(plain, sgrSequence.ReplaceAllString(line, ""))
	}
	for i, line := range plain {
		if got := utf8.RuneCountInString(line); got > width {
			t.Errorf("line %d is %d cells wide, want at most %d: %q", i, got, width, line)
		}
	}

	rowOf := func(needle string) int {
		for i, line := range plain {
			if strings.Contains(line, needle) {
				return i
			}
		}
		return -1
	}

	// /url fits, so its description shares its row.
	urlRow := rowOf("/url <url>")
	if urlRow < 0 {
		t.Fatal("expected the /url row to be printed")
	}
	if !strings.Contains(plain[urlRow], "Chat with a URL") {
		t.Errorf("/url should keep its description on the same row, got %q", plain[urlRow])
	}

	// /file fits too, and must land on the same description column as /url.
	fileRow := rowOf("/file <path>")
	if fileRow < 0 {
		t.Fatal("expected the /file row to be printed")
	}
	if !strings.Contains(plain[fileRow], "Chat with a file") {
		t.Errorf("/file should keep its description on the same row, got %q", plain[fileRow])
	}
	if a, b := strings.Index(plain[urlRow], "Chat"), strings.Index(plain[fileRow], "Chat"); a != b {
		t.Errorf("/file description starts at %d but /url at %d; they should share a column", b, a)
	}

	// The long /prompt row is the only one that gives up its column.
	promptRow := rowOf("/prompt <load")
	if promptRow < 0 {
		t.Fatal("expected the /prompt row to be printed")
	}
	if strings.Contains(plain[promptRow], "Manage and load") {
		t.Errorf("/prompt should wrap its command instead of claiming a description column, got %q", plain[promptRow])
	}
	descriptionRow := promptRow
	for descriptionRow < len(plain) && !strings.Contains(plain[descriptionRow], "Manage and load") {
		descriptionRow++
	}
	if descriptionRow >= len(plain) {
		t.Fatalf("the /prompt description was dropped: %q", plain)
	}
	if indent := len(plain[descriptionRow]) - len(strings.TrimLeft(plain[descriptionRow], " ")); indent != 4 {
		t.Errorf("the stacked /prompt description should be indented 4, got %d: %q", indent, plain[descriptionRow])
	}
}

// With no room for any description column, the whole table stacks as before.
func TestRenderCommandsTableStacksEverythingWhenNothingFits(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	previous := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = previous }()

	lines := renderCommandsTable([]CommandHelp{
		{Command: "/a-very-long-command-name-that-cannot-fit", Description: "First"},
		{Command: "/another-equally-long-command-name-here", Description: "Second"},
	}, 40)

	plain := make([]string, 0, len(lines))
	for _, line := range lines {
		plain = append(plain, sgrSequence.ReplaceAllString(line, ""))
	}

	for _, want := range []string{"First", "Second"} {
		found := false
		for _, line := range plain {
			if strings.Contains(line, want) {
				found = true
				if indent := len(line) - len(strings.TrimLeft(line, " ")); indent != 4 {
					t.Errorf("description %q should be indented 4, got %d: %q", want, indent, line)
				}
			}
		}
		if !found {
			t.Errorf("expected the description %q to be printed", want)
		}
	}
}
