//go:build darwin

package tray

import (
	"fmt"
	"os/exec"
	"strings"
)

// LaunchTerminal opens the default macOS Terminal app and runs an interactive
// child without tying its lifetime to this service process.
func LaunchTerminal(executable string, args ...string) error {
	if strings.TrimSpace(executable) == "" {
		return fmt.Errorf("chat executable is empty")
	}
	command := "exec " + shellQuote(executable)
	for _, arg := range args {
		command += " " + shellQuote(arg)
	}
	script := "tell application \"Terminal\" to do script " + appleScriptQuote(command) + "\nactivate application \"Terminal\""
	if err := exec.Command("/usr/bin/osascript", "-e", script).Start(); err != nil {
		return fmt.Errorf("open Terminal: %w", err)
	}
	return nil
}

func appleScriptQuote(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
