//go:build linux

package tray

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LaunchTerminal opens an interactive child in a new terminal window.
func LaunchTerminal(executable string, args ...string) error {
	if strings.TrimSpace(executable) == "" {
		return fmt.Errorf("chat executable is empty")
	}
	launchExecutable, err := resolveTerminalExecutable(executable)
	if err != nil {
		return err
	}
	for _, terminal := range []string{"gnome-terminal", "konsole", "xfce4-terminal", "foot", "xterm"} {
		path, err := exec.LookPath(terminal)
		if err != nil {
			continue
		}
		program, terminalArgs, err := terminalInvocation(path, launchExecutable, args...)
		if err != nil {
			return err
		}
		command := exec.CommandContext(context.Background(), program, terminalArgs...)
		if err := command.Start(); err != nil {
			return fmt.Errorf("open %s: %w", terminal, err)
		}
		go func() { _ = command.Wait() }()
		return nil
	}
	return fmt.Errorf("no supported terminal emulator found (install gnome-terminal, konsole, xfce4-terminal, foot, or xterm)")
}

func resolveTerminalExecutable(executable string) (string, error) {
	if _, err := os.Stat(executable); err == nil {
		return executable, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect chat executable %q: %w", executable, err)
	}

	// `go run` removes its temporary binary after the original CLI exits, but a
	// resident tray service can still execute the mapped image through /proc.
	processExecutable := fmt.Sprintf("/proc/%d/exe", os.Getpid())
	if _, err := os.Stat(processExecutable); err != nil {
		return "", fmt.Errorf("chat executable %q is unavailable and the running process cannot be relaunched: %w", executable, err)
	}
	return processExecutable, nil
}

func terminalInvocation(terminal, executable string, args ...string) (string, []string, error) {
	if strings.TrimSpace(executable) == "" {
		return "", nil, fmt.Errorf("chat executable is empty")
	}
	name := filepath.Base(terminal)
	switch name {
	case "gnome-terminal":
		return terminal, append([]string{"--wait", "--", executable}, args...), nil
	case "konsole":
		return terminal, append([]string{"-e", executable}, args...), nil
	case "xfce4-terminal":
		command := make([]string, 0, len(args)+1)
		command = append(command, shellQuote(executable))
		for _, arg := range args {
			command = append(command, shellQuote(arg))
		}
		return terminal, []string{"--command", strings.Join(command, " ")}, nil
	case "foot":
		return terminal, append([]string{"--title=DuckChat", "--", executable}, args...), nil
	case "xterm":
		return terminal, append([]string{"-e", executable}, args...), nil
	default:
		return "", nil, fmt.Errorf("unsupported terminal emulator %q", terminal)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
