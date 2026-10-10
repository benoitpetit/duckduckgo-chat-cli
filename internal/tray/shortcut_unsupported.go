//go:build !linux && !windows && !darwin

package tray

import (
	"context"
	"fmt"

	"duckduckgo-chat-cli/internal/config"
)

func startPlatformShortcuts(_ context.Context, _ config.TrayConfig, _ TrayActionDispatcher, updateStatus func(string)) (func(), error) {
	updateStatus("Global shortcuts are not supported on this platform")
	return func() {}, fmt.Errorf("global shortcuts are not supported on this platform")
}
