//go:build linux

package tray

import (
	"context"
	"fmt"
	"os"
	"strings"

	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/shortcut"
)

func startPlatformShortcuts(ctx context.Context, configured config.TrayConfig, dispatch TrayActionDispatcher, updateStatus func(string)) (func(), error) {
	textShortcut, textErr := shortcut.ParseShortcut(configured.TextShortcut)
	voiceShortcut, voiceErr := shortcut.ParseShortcut(configured.VoiceShortcut)
	if textErr != nil || voiceErr != nil || shortcut.ValidateShortcuts(configured.TextShortcut, configured.VoiceShortcut) != nil {
		updateStatus("Shortcuts unavailable: configuration is invalid")
		return func() {}, fmt.Errorf("configuration is invalid")
	}

	if os.Getenv("WAYLAND_DISPLAY") != "" || strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
		stop, err := startPortalShortcuts(ctx, textShortcut, voiceShortcut, dispatch, updateStatus)
		if err != nil {
			updateStatus("Shortcuts unavailable: " + err.Error())
			return func() {}, err
		}
		updateStatus("Shortcuts ready: " + configured.TextShortcut + " / " + configured.VoiceShortcut)
		return stop, nil
	}
	if os.Getenv("DISPLAY") != "" {
		stop, err := startX11Shortcuts(ctx, textShortcut, voiceShortcut, dispatch)
		if err != nil {
			updateStatus("Shortcuts unavailable: " + err.Error())
			return func() {}, err
		}
		updateStatus("Shortcuts ready: " + configured.TextShortcut + " / " + configured.VoiceShortcut)
		return stop, nil
	}
	updateStatus("Shortcuts unavailable: no Wayland or X11 session")
	return func() {}, fmt.Errorf("no Wayland or X11 session")
}
