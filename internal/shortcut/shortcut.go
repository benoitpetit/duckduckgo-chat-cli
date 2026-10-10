// Package shortcut parses and validates user-configured global shortcuts.
package shortcut

import (
	"fmt"
	"strconv"
	"strings"
)

// Shortcut is a modifier chord followed by a single key.
type Shortcut struct {
	Ctrl  bool
	Alt   bool
	Shift bool
	Super bool
	Key   string
}

// ParseShortcut parses a shortcut such as "Ctrl+Alt+T" and normalizes its
// spelling and modifier order.
func ParseShortcut(value string) (Shortcut, error) {
	var parsed Shortcut
	parts := strings.Split(value, "+")
	if len(parts) < 2 {
		return Shortcut{}, fmt.Errorf("shortcut must include a modifier and a key")
	}

	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			return Shortcut{}, fmt.Errorf("shortcut contains an empty key or modifier")
		}
		switch strings.ToLower(token) {
		case "ctrl", "control":
			if parsed.Ctrl {
				return Shortcut{}, fmt.Errorf("shortcut repeats Ctrl")
			}
			parsed.Ctrl = true
		case "alt":
			if parsed.Alt {
				return Shortcut{}, fmt.Errorf("shortcut repeats Alt")
			}
			parsed.Alt = true
		case "shift":
			if parsed.Shift {
				return Shortcut{}, fmt.Errorf("shortcut repeats Shift")
			}
			parsed.Shift = true
		case "super", "meta", "command":
			if parsed.Super {
				return Shortcut{}, fmt.Errorf("shortcut repeats Super")
			}
			parsed.Super = true
		default:
			if parsed.Key != "" {
				return Shortcut{}, fmt.Errorf("shortcut must contain exactly one non-modifier key")
			}
			key, ok := normalizeKey(token)
			if !ok {
				return Shortcut{}, fmt.Errorf("unsupported shortcut key %q", token)
			}
			parsed.Key = key
		}
	}

	if !(parsed.Ctrl || parsed.Alt || parsed.Shift || parsed.Super) {
		return Shortcut{}, fmt.Errorf("shortcut must include at least one modifier")
	}
	if parsed.Key == "" {
		return Shortcut{}, fmt.Errorf("shortcut must include one key")
	}
	return parsed, nil
}

func normalizeKey(key string) (string, bool) {
	if strings.EqualFold(key, "space") {
		return "Space", true
	}
	if len(key) == 1 {
		char := key[0]
		if char >= 'a' && char <= 'z' {
			return strings.ToUpper(key), true
		}
		if char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			return strings.ToUpper(key), true
		}
		return "", false
	}
	if len(key) >= 2 && (key[0] == 'f' || key[0] == 'F') {
		number, err := strconv.Atoi(key[1:])
		if err == nil && number >= 1 && number <= 12 {
			return fmt.Sprintf("F%d", number), true
		}
	}
	return "", false
}

// String returns the canonical representation of the shortcut.
func (s Shortcut) String() string {
	parts := make([]string, 0, 5)
	if s.Ctrl {
		parts = append(parts, "Ctrl")
	}
	if s.Alt {
		parts = append(parts, "Alt")
	}
	if s.Shift {
		parts = append(parts, "Shift")
	}
	if s.Super {
		parts = append(parts, "Super")
	}
	if s.Key != "" {
		parts = append(parts, s.Key)
	}
	return strings.Join(parts, "+")
}

// ValidateShortcuts verifies two bindings and rejects equivalent chords.
func ValidateShortcuts(text, voice string) error {
	textShortcut, err := ParseShortcut(text)
	if err != nil {
		return fmt.Errorf("text shortcut: %w", err)
	}
	voiceShortcut, err := ParseShortcut(voice)
	if err != nil {
		return fmt.Errorf("voice shortcut: %w", err)
	}
	if textShortcut == voiceShortcut {
		return fmt.Errorf("text and voice shortcuts must be different")
	}
	return nil
}
