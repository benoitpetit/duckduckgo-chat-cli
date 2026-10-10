package shortcut

import "testing"

func TestParseShortcutCanonicalizesModifiers(t *testing.T) {
	got, err := ParseShortcut(" shift + control + alt + t ")
	if err != nil {
		t.Fatalf("ParseShortcut() error = %v", err)
	}
	if got.String() != "Ctrl+Alt+Shift+T" {
		t.Fatalf("canonical shortcut = %q, want %q", got.String(), "Ctrl+Alt+Shift+T")
	}
	if !got.Ctrl || !got.Alt || !got.Shift || got.Super || got.Key != "T" {
		t.Fatalf("parsed shortcut = %+v, want Ctrl+Alt+Shift+T", got)
	}
}

func TestParseShortcutAcceptsSupportedKeys(t *testing.T) {
	for _, input := range []string{"Ctrl+Alt+A", "Ctrl+Alt+7", "Ctrl+Alt+Space", "Ctrl+Alt+F12"} {
		if _, err := ParseShortcut(input); err != nil {
			t.Errorf("ParseShortcut(%q) error = %v", input, err)
		}
	}
}

func TestParseShortcutRejectsMalformedBindings(t *testing.T) {
	for _, input := range []string{"", "T", "Ctrl", "Ctrl+Ctrl+T", "Ctrl+Alt+T+Q", "Ctrl+Alt+F13", "Ctrl+Alt+NoSuchKey"} {
		if _, err := ParseShortcut(input); err == nil {
			t.Errorf("ParseShortcut(%q) succeeded, want validation error", input)
		}
	}
}

func TestValidateShortcutsRejectsEquivalentBindings(t *testing.T) {
	if err := ValidateShortcuts("Ctrl+Alt+T", "alt+control+t"); err == nil {
		t.Fatal("equivalent text and voice shortcuts were accepted")
	}
	if err := ValidateShortcuts("Ctrl+Alt+T", "Ctrl+Alt+V"); err != nil {
		t.Fatalf("distinct shortcuts rejected: %v", err)
	}
}
