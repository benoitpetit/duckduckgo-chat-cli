//go:build linux

package tray

import (
	"testing"

	"github.com/jezek/xgb/xproto"
)

func TestX11KeycodeCountRejectsUnrepresentableRanges(t *testing.T) {
	tests := []struct {
		name      string
		minimum   xproto.Keycode
		maximum   xproto.Keycode
		wantCount byte
		wantError bool
	}{
		{name: "normal range", minimum: 8, maximum: 255, wantCount: 248},
		{name: "reversed range", minimum: 255, maximum: 8, wantError: true},
		{name: "full byte range", minimum: 0, maximum: 255, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := x11KeycodeCount(test.minimum, test.maximum)
			if (err != nil) != test.wantError {
				t.Fatalf("x11KeycodeCount() error = %v, wantError %t", err, test.wantError)
			}
			if got != test.wantCount {
				t.Fatalf("x11KeycodeCount() = %d, want %d", got, test.wantCount)
			}
		})
	}
}

func TestX11KeycodeIgnoresTruncatedMapping(t *testing.T) {
	mapping := &xproto.GetKeyboardMappingReply{
		KeysymsPerKeycode: 2,
		Keysyms:           []xproto.Keysym{0x0061},
	}
	if got := x11Keycode(mapping, 8, 8, 0x0061); got != 0 {
		t.Fatalf("x11Keycode() = %d, want 0 for a truncated mapping", got)
	}
}
