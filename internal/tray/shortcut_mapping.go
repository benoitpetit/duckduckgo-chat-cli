package tray

import (
	"fmt"
	"strconv"
	"strings"

	"duckduckgo-chat-cli/internal/shortcut"
)

func portalTrigger(binding shortcut.Shortcut) (string, error) {
	parts := make([]string, 0, 5)
	if binding.Ctrl {
		parts = append(parts, "<Ctrl>")
	}
	if binding.Alt {
		parts = append(parts, "<Alt>")
	}
	if binding.Shift {
		parts = append(parts, "<Shift>")
	}
	if binding.Super {
		parts = append(parts, "<Super>")
	}
	key := strings.TrimSpace(binding.Key)
	if key == "" {
		return "", fmt.Errorf("shortcut key is empty")
	}
	if strings.EqualFold(key, "space") {
		key = "space"
	} else if len(key) == 1 {
		key = strings.ToLower(key)
	}
	return strings.Join(parts, "") + key, nil
}

func x11ModifierMask(binding shortcut.Shortcut) uint16 {
	var mask uint16
	if binding.Shift {
		mask |= 1 << 0 // ShiftMask
	}
	if binding.Ctrl {
		mask |= 1 << 2 // ControlMask
	}
	if binding.Alt {
		mask |= 1 << 3 // Mod1Mask
	}
	if binding.Super {
		mask |= 1 << 6 // Mod4Mask
	}
	return mask
}

func x11Keysym(binding shortcut.Shortcut) (uint32, error) {
	key := strings.TrimSpace(binding.Key)
	if strings.EqualFold(key, "space") {
		return 0x0020, nil
	}
	if len(key) == 1 {
		if key[0] >= 'A' && key[0] <= 'Z' {
			return uint32(key[0] - 'A' + 'a'), nil
		}
		if key[0] >= 'a' && key[0] <= 'z' || key[0] >= '0' && key[0] <= '9' {
			return uint32(key[0]), nil
		}
	}
	if len(key) >= 2 && (key[0] == 'F' || key[0] == 'f') {
		fn, err := strconv.Atoi(key[1:])
		if err == nil && fn >= 1 && fn <= 12 {
			return uint32(0xffbd + fn), nil
		}
	}
	return 0, fmt.Errorf("unsupported X11 key %q", key)
}
