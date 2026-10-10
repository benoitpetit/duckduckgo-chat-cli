package tray

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"duckduckgo-chat-cli/internal/config"

	"github.com/gogpu/systray"
)

//go:embed tray_icon.png
var trayIcon []byte

func runTray(ctx context.Context, configured config.TrayConfig, dispatch TrayActionDispatcher, reloads <-chan config.TrayConfig) error {
	return runTrayPlatform(ctx, configured, dispatch, reloads)
}

// RunTray creates the resident native tray icon and processes shortcut reloads
// until the service context is canceled or the tray loop exits.
func RunTray(ctx context.Context, configured config.TrayConfig, dispatch TrayActionDispatcher, reloads <-chan config.TrayConfig) error {
	return runTray(ctx, configured, dispatch, reloads)
}

func runTrayLoop(ctx context.Context, configured config.TrayConfig, dispatch TrayActionDispatcher, reloads <-chan config.TrayConfig) error {
	if ctx == nil {
		ctx = context.Background()
	}
	nativeTray := newNativeTray()
	menu := systray.NewMenu()
	statusItem := menu.Add("Global shortcuts: starting…", nil)
	menu.AddSeparator()
	for _, item := range trayMenuItems() {
		item := item
		menu.Add(item.Label, func() {
			if dispatch == nil {
				return
			}
			response := dispatch(item.Action)
			if response.Error != "" {
				setTrayStatus(statusItem, "Action failed: "+response.Error)
			}
		})
	}
	status := func(message string) {
		setTrayStatus(statusItem, message)
	}
	setupNativeTray(nativeTray, menu, func() {
		response := handleTrayIconClick(dispatch)
		if response.Error != "" {
			status("Action failed: " + response.Error)
		}
	})
	shortcutManagerStop := make(chan struct{})
	shortcutManagerDone := make(chan struct{})
	go func() {
		defer close(shortcutManagerDone)
		current := configured
		stopShortcuts, _ := startPlatformShortcuts(ctx, current, dispatch, status)
		if stopShortcuts == nil {
			stopShortcuts = func() {}
		}
		defer stopShortcuts()
		for {
			select {
			case <-ctx.Done():
				return
			case <-shortcutManagerStop:
				return
			case next, ok := <-reloads:
				if !ok {
					return
				}
				stopShortcuts()
				nextStop, err := startPlatformShortcuts(ctx, next, dispatch, status)
				if err != nil {
					status("New shortcuts unavailable; restoring the previous shortcuts")
					restoredStop, restoreErr := startPlatformShortcuts(ctx, current, dispatch, status)
					if restoredStop == nil {
						restoredStop = func() {}
					}
					stopShortcuts = restoredStop
					if restoreErr != nil {
						status("Shortcuts unavailable; tray actions remain available")
					} else {
						status("Previous shortcuts restored")
					}
					continue
				}
				stopShortcuts = nextStop
				current = next
			}
		}
	}()
	defer func() {
		close(shortcutManagerStop)
		<-shortcutManagerDone
	}()

	var removeOnce sync.Once
	remove := func() { removeOnce.Do(func() { removeNativeTray(nativeTray) }) }
	watchStop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			remove()
		case <-watchStop:
		}
	}()
	defer func() {
		close(watchStop)
		remove()
	}()
	if err := runNativeTray(nativeTray); err != nil {
		return fmt.Errorf("run tray icon: %w", err)
	}
	return nil
}

func setTrayStatus(item *systray.MenuItem, message string) {
	if item == nil {
		return
	}
	message = strings.TrimSpace(strings.ReplaceAll(message, "\n", " "))
	if len(message) > 110 {
		message = message[:107] + "..."
	}
	item.SetLabel(message)
}
