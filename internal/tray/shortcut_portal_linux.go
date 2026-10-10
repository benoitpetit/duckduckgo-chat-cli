//go:build linux

package tray

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/shortcut"

	"github.com/AshBuk/go-wlportal/shortcuts"
)

const desktopApplicationID = "org.duckduckgo.chatcli"

func startPortalShortcuts(ctx context.Context, text, voice shortcut.Shortcut, dispatch TrayActionDispatcher, updateStatus func(string)) (func(), error) {
	hyprland := isHyprlandSession()
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve DuckChat executable: %w", err)
	}
	if err := ensureDesktopEntry(executable); err != nil {
		return nil, err
	}
	var textTrigger, voiceTrigger string
	if !hyprland {
		textTrigger, err = portalTrigger(text)
		if err != nil {
			return nil, err
		}
		voiceTrigger, err = portalTrigger(voice)
		if err != nil {
			return nil, err
		}
	}
	session, err := shortcuts.New([]shortcuts.Shortcut{
		{ID: "text-chat", Description: "Open DuckChat text chat", PreferredTrigger: textTrigger},
		{ID: "voice-chat", Description: "Open or show DuckChat voice chat", PreferredTrigger: voiceTrigger},
	}, shortcuts.WithAppID(desktopApplicationID), shortcuts.WithCallTimeout(15*time.Second))
	if err != nil {
		return nil, fmt.Errorf("bind Wayland global shortcuts: %w", err)
	}
	var stopHyprlandBindings func()
	if hyprland {
		stopHyprlandBindings, err = startHyprlandBindings(ctx, text, voice, evalHyprlandLua, updateStatus)
		if err != nil {
			_ = session.Close()
			return nil, err
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-session.Events():
				if !ok {
					return
				}
				if !event.Pressed || dispatch == nil {
					continue
				}
				switch event.ID {
				case "text-chat":
					dispatch(ActionOpenTextChat)
				case "voice-chat":
					dispatch(ActionOpenOrShowVoice)
				}
			}
		}
	}()
	return func() {
		if stopHyprlandBindings != nil {
			stopHyprlandBindings()
		}
		_ = session.Close()
		<-done
	}, nil
}

func ensureDesktopEntry(executable string) error {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("find home directory for Wayland app identity: %w", err)
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	applications := filepath.Join(dataHome, "applications")
	if err := os.MkdirAll(applications, 0o755); err != nil {
		return fmt.Errorf("create Wayland app identity directory: %w", err)
	}
	entry := strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=DuckChat",
		"Exec=" + desktopExecQuote(executable) + " --tray-service",
		"Terminal=false",
		"NoDisplay=true",
		"StartupNotify=false",
		"",
	}, "\n")
	path := filepath.Join(applications, desktopApplicationID+".desktop")
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		return fmt.Errorf("write Wayland app identity: %w", err)
	}
	return nil
}

func desktopExecQuote(value string) string {
	value = strings.ReplaceAll(value, "%", "%%")
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}
