//go:build windows

package tray

import (
	"fmt"
	"os/exec"
	"strings"
)

// LaunchTerminal opens an interactive child in a new Windows Terminal window
// or, when Windows Terminal is absent, a detached console window.
func LaunchTerminal(executable string, args ...string) error {
	if strings.TrimSpace(executable) == "" {
		return fmt.Errorf("chat executable is empty")
	}
	if terminal, err := exec.LookPath("wt.exe"); err == nil {
		commandArgs := append([]string{"-w", "0", "new-tab", "--title", "DuckChat", executable}, args...)
		if err := exec.Command(terminal, commandArgs...).Start(); err != nil {
			return fmt.Errorf("open Windows Terminal: %w", err)
		}
		return nil
	}
	command := "start \"\" " + windowsShellQuote(executable)
	for _, arg := range args {
		command += " " + windowsShellQuote(arg)
	}
	if err := exec.Command("cmd.exe", "/c", command).Start(); err != nil {
		return fmt.Errorf("open console window: %w", err)
	}
	return nil
}

func windowsShellQuote(value string) string {
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}
