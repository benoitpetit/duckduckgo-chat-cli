package ui

import "testing"

func TestDraculaAndNordThemesUseTheirSemanticPalettes(t *testing.T) {
	tests := []struct {
		name string
		want Palette
	}{
		{
			name: "dracula",
			want: Palette{
				User: "#FF79C6", Assistant: "#F8F8F2", Accent: "#BD93F9",
				Info: "#8BE9FD", Muted: "#6272A4", Success: "#50FA7B",
				Warning: "#FFB86C", Error: "#FF5555", Foreground: "#F8F8F2", Surface: "#343746",
			},
		},
		{
			name: "nord",
			want: Palette{
				User: "#88C0D0", Assistant: "#ECEFF4", Accent: "#81A1C1",
				Info: "#5E81AC", Muted: "#D8DEE9", Success: "#A3BE8C",
				Warning: "#EBCB8B", Error: "#BF616A", Foreground: "#ECEFF4", Surface: "#3B4252",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			theme, ok := ResolveTheme(tt.name)
			if !ok {
				t.Fatalf("ResolveTheme(%q) did not find the theme", tt.name)
			}
			if theme.Colors != tt.want {
				t.Fatalf("theme palette = %+v, want %+v", theme.Colors, tt.want)
			}
		})
	}
}
