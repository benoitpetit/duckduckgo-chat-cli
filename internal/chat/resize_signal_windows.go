//go:build windows

package chat

import "os"

func subscribeTerminalResize() (<-chan os.Signal, func()) {
	return nil, func() {}
}
