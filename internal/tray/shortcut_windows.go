//go:build windows

package tray

import (
	"context"
	"fmt"
	"strings"

	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/shortcut"

	"golang.design/x/hotkey"
)

func startPlatformShortcuts(_ context.Context, configured config.TrayConfig, dispatch TrayActionDispatcher, updateStatus func(string)) (func(), error) {
	bindings := []struct {
		value  string
		action Action
	}{{configured.TextShortcut, ActionOpenTextChat}, {configured.VoiceShortcut, ActionOpenOrShowVoice}}
	registered := make([]*hotkey.Hotkey, 0, len(bindings))
	for _, binding := range bindings {
		parsed, err := shortcut.ParseShortcut(binding.value)
		if err != nil {
			unregisterWindowsHotkeys(registered)
			updateStatus("Shortcuts unavailable: configuration is invalid")
			return func() {}, err
		}
		hk, err := windowsHotkey(parsed)
		if err == nil {
			err = hk.Register()
		}
		if err != nil {
			unregisterWindowsHotkeys(registered)
			updateStatus("Shortcuts unavailable: " + err.Error())
			return func() {}, err
		}
		registered = append(registered, hk)
		go func(hk *hotkey.Hotkey, action Action) {
			for range hk.Keydown() {
				if dispatch != nil {
					dispatch(action)
				}
			}
		}(hk, binding.action)
	}
	updateStatus("Shortcuts ready: " + configured.TextShortcut + " / " + configured.VoiceShortcut)
	return func() { unregisterWindowsHotkeys(registered) }, nil
}

func unregisterWindowsHotkeys(hotkeys []*hotkey.Hotkey) {
	for _, hk := range hotkeys {
		_ = hk.Unregister()
	}
}

func windowsHotkey(binding shortcut.Shortcut) (*hotkey.Hotkey, error) {
	var modifiers []hotkey.Modifier
	if binding.Ctrl {
		modifiers = append(modifiers, hotkey.ModCtrl)
	}
	if binding.Alt {
		modifiers = append(modifiers, hotkey.ModAlt)
	}
	if binding.Shift {
		modifiers = append(modifiers, hotkey.ModShift)
	}
	if binding.Super {
		modifiers = append(modifiers, hotkey.ModWin)
	}
	key := strings.ToUpper(binding.Key)
	if key == "SPACE" {
		return hotkey.New(modifiers, hotkey.KeySpace), nil
	}
	if len(key) == 1 && (key[0] >= 'A' && key[0] <= 'Z' || key[0] >= '0' && key[0] <= '9') {
		return hotkey.New(modifiers, hotkey.Key(key[0])), nil
	}
	if len(key) >= 2 && key[0] == 'F' {
		var number int
		if _, err := fmt.Sscanf(key, "F%d", &number); err != nil || number < 1 || number > 12 {
			return nil, fmt.Errorf("unsupported Windows key %q", binding.Key)
		}
		return hotkey.New(modifiers, hotkey.Key(uint32(hotkey.KeyF1)+uint32(number-1))), nil
	}
	return nil, fmt.Errorf("unsupported Windows key %q", binding.Key)
}
