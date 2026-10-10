package tray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"duckduckgo-chat-cli/internal/shortcut"
)

func TestPortalTriggerUsesDesktopAcceleratorSyntax(t *testing.T) {
	parsed, err := shortcut.ParseShortcut("Super+Ctrl+Alt+Shift+F12")
	if err != nil {
		t.Fatal(err)
	}
	got, err := portalTrigger(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if got != "<Ctrl><Alt><Shift><Super>F12" {
		t.Fatalf("portal trigger = %q, want %q", got, "<Ctrl><Alt><Shift><Super>F12")
	}
	space, err := portalTrigger(shortcut.Shortcut{Ctrl: true, Key: "Space"})
	if err != nil || space != "<Ctrl>space" {
		t.Fatalf("space portal trigger = %q, %v; want %q", space, err, "<Ctrl>space")
	}
}

func TestHyprlandShortcutKeyUsesCompositorSyntax(t *testing.T) {
	for input, want := range map[string]string{
		"Ctrl+Alt+T":        "CTRL + ALT + T",
		"Super+Shift+Space": "SHIFT + SUPER + space",
		"Ctrl+Alt+F12":      "CTRL + ALT + F12",
	} {
		parsed, err := shortcut.ParseShortcut(input)
		if err != nil {
			t.Fatal(err)
		}
		if got := hyprlandKeyCombo(parsed); got != want {
			t.Errorf("hyprlandKeyCombo(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsHyprlandSessionRecognizesDesktopAndInstance(t *testing.T) {
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE:Hyprland")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	if !isHyprlandSession() {
		t.Fatal("Hyprland was not detected in the desktop list")
	}

	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "instance-1")
	if !isHyprlandSession() {
		t.Fatal("Hyprland was not detected from its instance signature")
	}

	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	if isHyprlandSession() {
		t.Fatal("KDE-only session was detected as Hyprland")
	}
}

func TestEvalHyprlandLuaPassesExpressionAsOneArgument(t *testing.T) {
	binDir := t.TempDir()
	command := filepath.Join(binDir, "hyprctl")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf '<%s>\\n' \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	expression := `hl.bind("CTRL + ALT + T", hl.dsp.global("org.duckduckgo.chatcli:text-chat"))`
	got, err := evalHyprlandLua(context.Background(), expression)
	if err != nil {
		t.Fatal(err)
	}
	if want := "<eval>\n<" + expression + ">"; got != want {
		t.Fatalf("hyprctl arguments = %q, want %q", got, want)
	}
}

func TestHyprlandBindingTargetsPortalActionAndKeepsRemovalHandle(t *testing.T) {
	parsed, err := shortcut.ParseShortcut("Ctrl+Alt+T")
	if err != nil {
		t.Fatal(err)
	}
	bind := hyprlandBindLua("text-chat", parsed, "org.duckduckgo.chatcli:text-chat", "Open DuckChat text chat")
	for _, want := range []string{
		`hl.bind("CTRL + ALT + T", hl.dsp.global("org.duckduckgo.chatcli:text-chat")`,
		`g["text-chat"] =`,
	} {
		if !strings.Contains(bind, want) {
			t.Errorf("Hyprland bind expression %q does not contain %q", bind, want)
		}
	}
	cleanup := hyprlandUnbindLua("text-chat")
	if !strings.Contains(cleanup, `g["text-chat"]:unbind()`) {
		t.Fatalf("Hyprland cleanup expression %q does not remove only its owned binding", cleanup)
	}
}

func TestStartHyprlandBindingsInstallsAndRemovesConfiguredActions(t *testing.T) {
	text, err := shortcut.ParseShortcut("Ctrl+Alt+T")
	if err != nil {
		t.Fatal(err)
	}
	voice, err := shortcut.ParseShortcut("Ctrl+Alt+V")
	if err != nil {
		t.Fatal(err)
	}
	var expressions []string
	evaluate := func(_ context.Context, expression string) (string, error) {
		expressions = append(expressions, expression)
		return "ok", nil
	}
	stop, err := startHyprlandBindings(context.Background(), text, voice, evaluate, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(expressions) != 2 {
		t.Fatalf("installed %d Hyprland bindings, want 2", len(expressions))
	}
	if !strings.Contains(expressions[0], `global("org.duckduckgo.chatcli:text-chat")`) || !strings.Contains(expressions[1], `global("org.duckduckgo.chatcli:voice-chat")`) {
		t.Fatalf("Hyprland bindings target wrong actions: %q", expressions)
	}
	stop()
	if len(expressions) != 4 {
		t.Fatalf("Hyprland expressions after stop = %d, want 4 (two binds and two removals)", len(expressions))
	}
	if !strings.Contains(expressions[2], `g["voice-chat"]:unbind()`) || !strings.Contains(expressions[3], `g["text-chat"]:unbind()`) {
		t.Fatalf("Hyprland cleanup did not remove owned bindings: %q", expressions[2:])
	}
}

func TestStartHyprlandBindingsRollsBackWhenSecondBindingFails(t *testing.T) {
	text, _ := shortcut.ParseShortcut("Ctrl+Alt+T")
	voice, _ := shortcut.ParseShortcut("Ctrl+Alt+V")
	var expressions []string
	evaluate := func(_ context.Context, expression string) (string, error) {
		expressions = append(expressions, expression)
		if len(expressions) == 2 {
			return "", errors.New("hyprctl rejected voice binding")
		}
		return "ok", nil
	}
	if _, err := startHyprlandBindings(context.Background(), text, voice, evaluate, nil); err == nil {
		t.Fatal("expected failed voice binding to be reported")
	}
	if len(expressions) != 3 || !strings.Contains(expressions[2], `g["text-chat"]:unbind()`) {
		t.Fatalf("failed install did not clean up its partial binding: %q", expressions)
	}
}

func TestX11ShortcutMappingCoversModifiersAndKeys(t *testing.T) {
	parsed, err := shortcut.ParseShortcut("Ctrl+Alt+Shift+Super+7")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := x11ModifierMask(parsed), uint16(1<<2|1<<3|1<<0|1<<6); got != want {
		t.Fatalf("X11 modifier mask = %#x, want %#x", got, want)
	}
	for input, want := range map[string]uint32{
		"Ctrl+Alt+A":     0x0061,
		"Ctrl+Alt+7":     0x0037,
		"Ctrl+Alt+Space": 0x0020,
		"Ctrl+Alt+F12":   0xffc9,
	} {
		parsed, err := shortcut.ParseShortcut(input)
		if err != nil {
			t.Fatal(err)
		}
		got, err := x11Keysym(parsed)
		if err != nil || got != want {
			t.Errorf("x11Keysym(%q) = %#x, %v; want %#x", input, got, err, want)
		}
	}
}

func TestTerminalInvocationKeepsExecutableAndArgumentsSeparate(t *testing.T) {
	program, args, err := terminalInvocation("gnome-terminal", "/opt/Duck Chat/duckchat", "--interactive-child")
	if err != nil {
		t.Fatal(err)
	}
	if program != "gnome-terminal" {
		t.Fatalf("terminal program = %q, want gnome-terminal", program)
	}
	want := []string{"--wait", "--", "/opt/Duck Chat/duckchat", "--interactive-child"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("terminal args = %#v, want %#v", args, want)
	}

	program, args, err = terminalInvocation("xfce4-terminal", "/opt/Duck Chat/duckchat", "--interactive-child")
	if err != nil {
		t.Fatal(err)
	}
	if program != "xfce4-terminal" || !reflect.DeepEqual(args, []string{"--command", "'/opt/Duck Chat/duckchat' '--interactive-child'"}) {
		t.Fatalf("xfce4 invocation = %q %#v, command path was not quoted", program, args)
	}

	program, args, err = terminalInvocation("foot", "/opt/Duck Chat/duckchat", "--interactive-child")
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"--title=DuckChat", "--", "/opt/Duck Chat/duckchat", "--interactive-child"}
	if program != "foot" || !reflect.DeepEqual(args, want) {
		t.Fatalf("foot invocation = %q %#v, want %q %#v", program, args, "foot", want)
	}
}

func TestResolveTerminalExecutableUsesRunningImageWhenGoRunBinaryWasRemoved(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "duckchat-binary-removed-by-go-run")
	got, err := resolveTerminalExecutable(missing)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("/proc/%d/exe", os.Getpid())
	if got != want {
		t.Fatalf("resolved executable = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("resolved executable is not launchable: %v", err)
	}
}

func TestTrayMenuContainsRequiredActions(t *testing.T) {
	items := trayMenuItems()
	want := map[string]Action{
		"Open text chat":                    ActionOpenTextChat,
		"Open voice chat / Show voice chat": ActionOpenOrShowVoice,
		"Minimize voice to tray":            ActionMinimizeVoice,
		"Configure shortcuts":               ActionConfigureShort,
		"Quit DuckChat":                     ActionQuitService,
	}
	for _, item := range items {
		if action, ok := want[item.Label]; ok {
			if action != item.Action {
				t.Errorf("menu item %q dispatches %q, want %q", item.Label, item.Action, action)
			}
			delete(want, item.Label)
		}
	}
	if len(want) != 0 {
		t.Fatalf("tray menu is missing required actions: %#v", want)
	}
}

func TestTrayClickOpensTextOrTogglesActiveVoice(t *testing.T) {
	for _, test := range []struct {
		name        string
		voiceActive bool
		want        []Action
	}{
		{name: "no voice session", want: []Action{ActionVoiceStatus, ActionOpenTextChat}},
		{name: "active voice session", voiceActive: true, want: []Action{ActionVoiceStatus, ActionToggleVoice}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got []Action
			handleTrayIconClick(func(action Action) Response {
				got = append(got, action)
				return Response{OK: true, VoiceActive: test.voiceActive}
			})
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("tray click actions = %#v, want %#v", got, test.want)
			}
		})
	}
}
