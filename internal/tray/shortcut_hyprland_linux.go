//go:build linux

package tray

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/shortcut"
)

const hyprlandBindingStore = "__duckchatGlobalShortcutBindings"

type hyprlandLuaEvaluator func(context.Context, string) (string, error)

type hyprlandShortcutBinding struct {
	slot        string
	shortcut    shortcut.Shortcut
	portalID    string
	description string
}

func isHyprlandSession() bool {
	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
		return true
	}
	for _, desktop := range strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":") {
		if strings.EqualFold(strings.TrimSpace(desktop), "hyprland") {
			return true
		}
	}
	return false
}

func hyprlandKeyCombo(binding shortcut.Shortcut) string {
	parts := make([]string, 0, 5)
	if binding.Ctrl {
		parts = append(parts, "CTRL")
	}
	if binding.Alt {
		parts = append(parts, "ALT")
	}
	if binding.Shift {
		parts = append(parts, "SHIFT")
	}
	if binding.Super {
		parts = append(parts, "SUPER")
	}
	key := binding.Key
	if strings.EqualFold(key, "space") {
		key = "space"
	} else {
		key = strings.ToUpper(key)
	}
	if key != "" {
		parts = append(parts, key)
	}
	return strings.Join(parts, " + ")
}

func hyprlandBindLua(slot string, binding shortcut.Shortcut, portalID, description string) string {
	store := strconv.Quote(hyprlandBindingStore)
	quotedSlot := strconv.Quote(slot)
	return fmt.Sprintf(
		"local g = rawget(_G, %s) or {}; rawset(_G, %s, g); if g[%s] then g[%s]:unbind() end; g[%s] = hl.bind(%s, hl.dsp.global(%s), {description = %s})",
		store,
		store,
		quotedSlot,
		quotedSlot,
		quotedSlot,
		strconv.Quote(hyprlandKeyCombo(binding)),
		strconv.Quote(portalID),
		strconv.Quote(description),
	)
}

func hyprlandUnbindLua(slot string) string {
	store := strconv.Quote(hyprlandBindingStore)
	quotedSlot := strconv.Quote(slot)
	return fmt.Sprintf("local g = rawget(_G, %s); if g and g[%s] then g[%s]:unbind(); g[%s] = nil end", store, quotedSlot, quotedSlot, quotedSlot)
}

func startHyprlandBindings(ctx context.Context, text, voice shortcut.Shortcut, evaluate hyprlandLuaEvaluator, updateStatus func(string)) (func(), error) {
	if evaluate == nil {
		evaluate = evalHyprlandLua
	}
	bindings := []hyprlandShortcutBinding{
		{
			slot:        "text-chat",
			shortcut:    text,
			portalID:    desktopApplicationID + ":text-chat",
			description: "Open DuckChat text chat",
		},
		{
			slot:        "voice-chat",
			shortcut:    voice,
			portalID:    desktopApplicationID + ":voice-chat",
			description: "Open or show DuckChat voice chat",
		},
	}

	bound := make([]hyprlandShortcutBinding, 0, len(bindings))
	for _, binding := range bindings {
		if _, err := evaluate(ctx, hyprlandBindLua(binding.slot, binding.shortcut, binding.portalID, binding.description)); err != nil {
			for i := len(bound) - 1; i >= 0; i-- {
				_, _ = evaluate(context.Background(), hyprlandUnbindLua(bound[i].slot))
			}
			return nil, fmt.Errorf("register Hyprland %s shortcut: %w", binding.slot, err)
		}
		bound = append(bound, binding)
	}

	return func() {
		for i := len(bound) - 1; i >= 0; i-- {
			if _, err := evaluate(context.Background(), hyprlandUnbindLua(bound[i].slot)); err != nil && updateStatus != nil {
				updateStatus(fmt.Sprintf("Could not remove Hyprland %s shortcut: %v", bound[i].slot, err))
			}
		}
	}, nil
}

func evalHyprlandLua(ctx context.Context, expression string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	commandCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(commandCtx, "hyprctl", "eval", expression).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return message, err
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
