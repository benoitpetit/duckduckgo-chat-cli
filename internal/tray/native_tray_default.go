//go:build !darwin

package tray

import (
	"context"

	"duckduckgo-chat-cli/internal/config"

	"github.com/gogpu/systray"
)

func runTrayPlatform(ctx context.Context, configured config.TrayConfig, dispatch TrayActionDispatcher, reloads <-chan config.TrayConfig) error {
	return runTrayLoop(ctx, configured, dispatch, reloads)
}

func newNativeTray() *systray.SystemTray { return systray.New() }

func setupNativeTray(native *systray.SystemTray, menu *systray.Menu, onClick func()) {
	native.SetAppName("DuckChat").SetIcon(trayIcon).SetTooltip("DuckChat").SetMenu(menu).OnClick(onClick).Show()
}

func runNativeTray(native *systray.SystemTray) error { return native.Run() }

func removeNativeTray(native *systray.SystemTray) { native.Remove() }
