//go:build !linux

package voice

import "context"

func resizeHyprlandVoiceWindow(context.Context, int, int) (bool, error) {
	return false, nil
}
