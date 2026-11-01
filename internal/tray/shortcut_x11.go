//go:build linux

package tray

import (
	"context"
	"fmt"

	"duckduckgo-chat-cli/internal/shortcut"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

type x11Binding struct {
	keycode xproto.Keycode
	mask    uint16
	action  Action
}

func startX11Shortcuts(ctx context.Context, text, voice shortcut.Shortcut, dispatch TrayActionDispatcher) (func(), error) {
	connection, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("connect to X11: %w", err)
	}
	setup := xproto.Setup(connection)
	screen := setup.DefaultScreen(connection)
	keycodeCount, err := x11KeycodeCount(setup.MinKeycode, setup.MaxKeycode)
	if err != nil {
		connection.Close()
		return nil, err
	}
	mapping, err := xproto.GetKeyboardMapping(connection, setup.MinKeycode, keycodeCount).Reply()
	if err != nil || mapping == nil {
		connection.Close()
		return nil, fmt.Errorf("read X11 keyboard mapping: %w", err)
	}
	bindings := make([]x11Binding, 0, 2)
	for _, item := range []struct {
		shortcut shortcut.Shortcut
		action   Action
	}{{text, ActionOpenTextChat}, {voice, ActionOpenOrShowVoice}} {
		keysym, keyErr := x11Keysym(item.shortcut)
		if keyErr != nil {
			connection.Close()
			return nil, keyErr
		}
		keycode := x11Keycode(mapping, setup.MinKeycode, setup.MaxKeycode, uint32(keysym))
		if keycode == 0 {
			connection.Close()
			return nil, fmt.Errorf("X11 key %q is not present in the current keyboard map", item.shortcut.Key)
		}
		bindings = append(bindings, x11Binding{keycode: keycode, mask: x11ModifierMask(item.shortcut), action: item.action})
	}
	const capsLockMask = uint16(1 << 1)
	const numLockMask = uint16(1 << 4)
	for _, binding := range bindings {
		for _, locks := range []uint16{0, capsLockMask, numLockMask, capsLockMask | numLockMask} {
			if err := xproto.GrabKeyChecked(connection, false, screen.Root, binding.mask|locks, binding.keycode, xproto.GrabModeAsync, xproto.GrabModeAsync).Check(); err != nil {
				connection.Close()
				return nil, fmt.Errorf("register X11 shortcut (%s): %w", actionName(binding.action), err)
			}
		}
	}
	byKeycode := make(map[xproto.Keycode]Action, len(bindings))
	for _, binding := range bindings {
		byKeycode[binding.keycode] = binding.action
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			event, eventErr := connection.WaitForEvent()
			if eventErr != nil {
				return
			}
			if key, ok := event.(xproto.KeyPressEvent); ok {
				if action, found := byKeycode[key.Detail]; found && key.State&^(capsLockMask|numLockMask) == x11StateFor(bindings, key.Detail) && dispatch != nil {
					dispatch(action)
				}
			}
		}
	}()
	return func() {
		connection.Close()
		<-done
	}, nil
}

func x11KeycodeCount(minimum, maximum xproto.Keycode) (byte, error) {
	count := int(maximum) - int(minimum) + 1
	if count <= 0 || count > int(^byte(0)) {
		return 0, fmt.Errorf("invalid X11 keycode range %d-%d", minimum, maximum)
	}
	return byte(count), nil
}

func x11Keycode(mapping *xproto.GetKeyboardMappingReply, minimum, maximum xproto.Keycode, target uint32) xproto.Keycode {
	if mapping == nil || mapping.KeysymsPerKeycode == 0 || maximum < minimum {
		return 0
	}
	perKeycode := int(mapping.KeysymsPerKeycode)
	for code := minimum; ; code++ {
		start := (int(code) - int(minimum)) * perKeycode
		end := start + perKeycode
		if end > len(mapping.Keysyms) {
			return 0
		}
		for _, keysym := range mapping.Keysyms[start:end] {
			if uint32(keysym) == target {
				return code
			}
		}
		if code == maximum {
			break
		}
	}
	if target >= 'a' && target <= 'z' {
		return x11Keycode(mapping, minimum, maximum, target-('a'-'A'))
	}
	return 0
}

func x11StateFor(bindings []x11Binding, keycode xproto.Keycode) uint16 {
	for _, binding := range bindings {
		if binding.keycode == keycode {
			return binding.mask
		}
	}
	return 0
}

func actionName(action Action) string { return string(action) }
