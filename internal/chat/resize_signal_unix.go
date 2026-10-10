//go:build !windows

package chat

import (
	"os"
	"os/signal"
	"syscall"
)

func subscribeTerminalResize() (<-chan os.Signal, func()) {
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	return resize, func() { signal.Stop(resize) }
}
