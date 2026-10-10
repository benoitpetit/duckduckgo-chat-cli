//go:build darwin

package tray

import (
	"context"

	"duckduckgo-chat-cli/internal/config"

	"github.com/gogpu/systray"
	"golang.design/x/mainthread"
)

func runTrayPlatform(ctx context.Context, configured config.TrayConfig, dispatch TrayActionDispatcher, reloads <-chan config.TrayConfig) error {
	var err error
	mainthread.Init(func() { err = runTrayLoop(ctx, configured, dispatch, reloads) })
	return err
}

func newNativeTray() *systray.SystemTray {
	var native *systray.SystemTray
	mainthread.Call(func() { native = systray.New() })
	return native
}

func setupNativeTray(native *systray.SystemTray, menu *systray.Menu, onClick func()) {
	mainthread.Call(func() {
		native.SetAppName("DuckChat").SetIcon(trayIcon).SetTooltip("DuckChat").SetMenu(menu).OnClick(onClick).Show()
	})
}

func runNativeTray(native *systray.SystemTray) error {
	var err error
	mainthread.Call(func() { err = native.Run() })
	return err
}

func removeNativeTray(native *systray.SystemTray) {
	if native == nil {
		return
	}
	native.Remove()
}
