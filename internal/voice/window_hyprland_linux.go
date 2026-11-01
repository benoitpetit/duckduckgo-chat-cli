//go:build linux

package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const hyprlandVoiceWindowTitle = "Duck.ai Speak"

type hyprlandClient struct {
	Address      string `json:"address"`
	InitialTitle string `json:"initialTitle"`
	Title        string `json:"title"`
	Floating     bool   `json:"floating"`
}

func resizeHyprlandVoiceWindow(ctx context.Context, width, height int) (bool, error) {
	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") == "" {
		return false, nil
	}
	hyprctl, err := exec.LookPath("hyprctl")
	if err != nil {
		return true, fmt.Errorf("Hyprland is active but hyprctl is unavailable: %w", err)
	}

	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var lookupErr error

	for {
		// #nosec G204 -- hyprctl is resolved from PATH and invoked without a shell.
		output, err := exec.CommandContext(lookupCtx, hyprctl, "-j", "clients").Output()
		if err == nil {
			var clients []hyprlandClient
			if err := json.Unmarshal(output, &clients); err == nil {
				if client, ok := findHyprlandVoiceClient(clients); ok {
					selector := "address:" + client.Address
					luaSelector := fmt.Sprintf("%q", selector)
					if err := setHyprlandWindowAnimation(lookupCtx, hyprctl, luaSelector, true); err != nil {
						return true, err
					}
					animationDisabled := true
					defer func() {
						if animationDisabled {
							_ = setHyprlandWindowAnimation(lookupCtx, hyprctl, luaSelector, false)
						}
					}()
					resize := fmt.Sprintf("hl.dsp.window.resize({ window = %s, x = %d, y = %d, relative = false })", luaSelector, width, height)
					if err := runHyprlandLuaDispatch(lookupCtx, hyprctl, resize); err != nil {
						return true, err
					}
					center := fmt.Sprintf("hl.dsp.window.center({ window = %s })", luaSelector)
					if err := runHyprlandLuaDispatch(lookupCtx, hyprctl, center); err != nil {
						return true, err
					}
					if err := waitForHyprlandLayout(lookupCtx); err != nil {
						return true, err
					}
					if err := setHyprlandWindowAnimation(lookupCtx, hyprctl, luaSelector, false); err != nil {
						return true, err
					}
					animationDisabled = false
					return true, nil
				}
			} else {
				lookupErr = err
			}
		} else {
			lookupErr = err
		}

		select {
		case <-lookupCtx.Done():
			if lookupErr != nil {
				return true, fmt.Errorf("could not read Hyprland windows: %w", lookupErr)
			}
			return true, fmt.Errorf("could not find the floating Duck.ai Speak window in Hyprland")
		case <-ticker.C:
		}
	}
}

func setHyprlandWindowAnimation(ctx context.Context, hyprctl, selector string, disabled bool) error {
	value := "0"
	if disabled {
		value = "1"
	}
	expression := fmt.Sprintf("hl.dsp.window.set_prop({ window = %s, prop = \"no_anim\", value = %q })", selector, value)
	return runHyprlandLuaDispatch(ctx, hyprctl, expression)
}

func waitForHyprlandLayout(ctx context.Context) error {
	timer := time.NewTimer(120 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func findHyprlandVoiceClient(clients []hyprlandClient) (hyprlandClient, bool) {
	for _, client := range clients {
		if client.Address != "" && client.Floating && client.InitialTitle == hyprlandVoiceWindowTitle {
			return client, true
		}
	}
	for _, client := range clients {
		if client.Address != "" && client.Floating && client.Title == hyprlandVoiceWindowTitle {
			return client, true
		}
	}
	return hyprlandClient{}, false
}

func runHyprlandLuaDispatch(ctx context.Context, hyprctl, expression string) error {
	// #nosec G204 -- the expression is built internally and passed as one argv, without a shell.
	command := exec.CommandContext(ctx, hyprctl, "dispatch", expression)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("Hyprland Lua dispatch failed: %w: %s", err, output)
	}
	return nil
}
