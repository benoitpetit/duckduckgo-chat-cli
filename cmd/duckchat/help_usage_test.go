package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"

	"duckduckgo-chat-cli/internal/ptytest"
)

func TestPrintCLIUsageGuidesInteractiveUseWithoutLogoWhenRedirected(t *testing.T) {
	var output bytes.Buffer
	printCLIUsage(&output)

	got := output.String()
	for _, want := range []string{
		"Usage: duckchat [options]",
		"--file PATH",
		"--url URL",
		"--search QUERY",
		"/coordinate [cancel|runs|show <run_id>]",
		"Run with no arguments to start an interactive chat.",
		"Type /help at the You: prompt for the full command list.",
		"/image [idea]",
		"/exit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("--help output is missing %q", want)
		}
	}
	for _, command := range []string{
		"/coordinate [cancel|runs|show <run_id>]",
		"/retry [instruction]",
	} {
		var line string
		for _, candidate := range strings.Split(got, "\n") {
			if strings.Contains(candidate, command) {
				line = candidate
				break
			}
		}
		if line == "" {
			t.Fatalf("--help output is missing command %q", command)
		}
		end := strings.Index(line, command) + len(command)
		if end == len(line) || line[end] != ' ' {
			t.Errorf("--help command %q is not separated from its description: %q", command, line)
		}
	}
	if strings.ContainsRune(got, '▀') || strings.Contains(got, "\x1b[") {
		t.Error("redirected --help output should be plain text without the logo")
	}
}

func TestPrintCLIUsageUsesThemeColorsOnATerminal(t *testing.T) {
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("cannot allocate a pseudo-terminal: %v", err)
	}
	defer master.Close()
	if err := ptytest.SetSize(slave, 120, 40); err != nil {
		t.Fatalf("set terminal size: %v", err)
	}

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	previous := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = previous }()

	output, err := ptytest.Capture(master, slave, func() error {
		printCLIUsage(slave)
		return nil
	})
	if err != nil {
		t.Fatalf("capturing --help output failed: %v", err)
	}
	if !strings.Contains(output, "\x1b[") {
		t.Error("interactive terminal --help should use the active theme colors")
	}
	if strings.ContainsRune(output, '▀') {
		t.Error("interactive terminal --help should not display the logo")
	}
	if !strings.Contains(output, "/search <query>") || !strings.Contains(output, "/image [idea]") {
		t.Error("interactive terminal --help should show common chat commands")
	}
}
