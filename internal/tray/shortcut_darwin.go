//go:build darwin

package tray

import (
	"context"
	"fmt"
	"strings"

	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/shortcut"

	"golang.design/x/hotkey"
	"golang.design/x/mainthread"
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
			unregisterDarwinHotkeys(registered)
			updateStatus("Shortcuts unavailable: configuration is invalid")
			return func() {}, err
		}
		hk, err := darwinHotkey(parsed)
		if err == nil {
			err = registerDarwinHotkey(hk)
		}
		if err != nil {
			unregisterDarwinHotkeys(registered)
			updateStatus("Shortcuts unavailable; check Accessibility/Input Monitoring permission: " + err.Error())
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
	return func() { unregisterDarwinHotkeys(registered) }, nil
}

func registerDarwinHotkey(hk *hotkey.Hotkey) error {
	var err error
	mainthread.Call(func() { err = hk.Register() })
	return err
}

func unregisterDarwinHotkeys(hotkeys []*hotkey.Hotkey) {
	mainthread.Call(func() {
		for _, hk := range hotkeys {
			_ = hk.Unregister()
		}
	})
}

func darwinHotkey(binding shortcut.Shortcut) (*hotkey.Hotkey, error) {
	var modifiers []hotkey.Modifier
	if binding.Ctrl {
		modifiers = append(modifiers, darwinModifierControl)
	}
	if binding.Alt {
		modifiers = append(modifiers, darwinModifierOption)
	}
	if binding.Shift {
		modifiers = append(modifiers, darwinModifierShift)
	}
	if binding.Super {
		modifiers = append(modifiers, darwinModifierCommand)
	}
	key := strings.ToUpper(binding.Key)
	if key == "SPACE" {
		return hotkey.New(modifiers, darwinKeySpace), nil
	}
	if len(key) == 1 && (key[0] >= 'A' && key[0] <= 'Z' || key[0] >= '0' && key[0] <= '9') {
		return hotkey.New(modifiers, hotkey.Key(darwinKeycode(key[0]))), nil
	}
	if len(key) >= 2 && key[0] == 'F' {
		var number int
		if _, err := fmt.Sscanf(key, "F%d", &number); err != nil || number < 1 || number > 12 {
			return nil, fmt.Errorf("unsupported macOS key %q", binding.Key)
		}
		return hotkey.New(modifiers, hotkey.Key(darwinFunctionKeycodes[number-1])), nil
	}
	return nil, fmt.Errorf("unsupported macOS key %q", binding.Key)
}

// Keep the Carbon values local so Darwin cross-compilation also works when
// CGO is disabled (the hotkey package then exposes the types without constants).
const (
	darwinModifierControl hotkey.Modifier = 0x1000
	darwinModifierShift   hotkey.Modifier = 0x0200
	darwinModifierOption  hotkey.Modifier = 0x0800
	darwinModifierCommand hotkey.Modifier = 0x0100
	darwinKeySpace        hotkey.Key      = 49
)

func darwinKeycode(key byte) uint32 {
	if key >= '0' && key <= '9' {
		return []uint32{29, 18, 19, 20, 21, 23, 22, 26, 28, 25}[key-'0']
	}
	return []uint32{
		'A': 0, 'B': 11, 'C': 8, 'D': 2, 'E': 14, 'F': 3, 'G': 5,
		'H': 4, 'I': 34, 'J': 38, 'K': 40, 'L': 37, 'M': 46, 'N': 45,
		'O': 31, 'P': 35, 'Q': 12, 'R': 15, 'S': 1, 'T': 17, 'U': 32,
		'V': 9, 'W': 13, 'X': 7, 'Y': 16, 'Z': 6,
	}[key]
}

var darwinFunctionKeycodes = [...]uint32{0x7a, 0x78, 0x63, 0x76, 0x60, 0x61, 0x62, 0x64, 0x65, 0x6d, 0x67, 0x6f}
