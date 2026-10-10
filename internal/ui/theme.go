package ui

import (
	"strings"
	"sync"
)

type ThemeID string

const (
	ThemeOfficial ThemeID = "official"
	ThemeRetro    ThemeID = "retro"
	ThemeMono     ThemeID = "mono"
	ThemeDracula  ThemeID = "dracula"
	ThemeNord     ThemeID = "nord"
)

// Palette defines colors by meaning so every CLI surface can share one theme.
type Palette struct {
	User       string
	Assistant  string
	Accent     string
	Info       string
	Muted      string
	Success    string
	Warning    string
	Error      string
	Foreground string
	Surface    string
}

// Theme contains the palette and a terminal-font recommendation.
type Theme struct {
	ID       ThemeID
	Label    string
	FontHint string
	Colors   Palette
}

var availableThemes = []Theme{
	{
		ID:       ThemeOfficial,
		Label:    "official",
		FontHint: "Terminal default monospace",
		Colors: Palette{
			User:       "#FC4A03",
			Assistant:  "#FFFFFF",
			Accent:     "#FC4A03",
			Info:       "#FC4A03",
			Muted:      "#7F8080",
			Success:    "#07B529",
			Warning:    "#FDC303",
			Error:      "#FC4A03",
			Foreground: "#FFFFFF",
			Surface:    "#292929",
		},
	},
	{
		ID:       ThemeRetro,
		Label:    "retro",
		FontHint: "Cascadia Code",
		Colors: Palette{
			User:       "#9CDCFE",
			Assistant:  "#EAEAEA",
			Accent:     "#EA549F",
			Info:       "#00B6D6",
			Muted:      "#797979",
			Success:    "#1AD69C",
			Warning:    "#CE9178",
			Error:      "#E92888",
			Foreground: "#EAEAEA",
			Surface:    "#252526",
		},
	},
	{
		ID:       ThemeMono,
		Label:    "mono",
		FontHint: "Terminal default monospace",
		Colors: Palette{
			User:       "#EAEAEA",
			Assistant:  "#FFFFFF",
			Accent:     "#FFFFFF",
			Info:       "#D0D0D0",
			Muted:      "#797979",
			Success:    "#FFFFFF",
			Warning:    "#EAEAEA",
			Error:      "#FFFFFF",
			Foreground: "#EAEAEA",
			Surface:    "#1E1E1E",
		},
	},
	{
		ID:       ThemeDracula,
		Label:    "dracula",
		FontHint: "Terminal default monospace",
		Colors: Palette{
			User:       "#FF79C6",
			Assistant:  "#F8F8F2",
			Accent:     "#BD93F9",
			Info:       "#8BE9FD",
			Muted:      "#6272A4",
			Success:    "#50FA7B",
			Warning:    "#FFB86C",
			Error:      "#FF5555",
			Foreground: "#F8F8F2",
			Surface:    "#343746",
		},
	},
	{
		ID:       ThemeNord,
		Label:    "nord",
		FontHint: "Terminal default monospace",
		Colors: Palette{
			User:       "#88C0D0",
			Assistant:  "#ECEFF4",
			Accent:     "#81A1C1",
			Info:       "#5E81AC",
			Muted:      "#D8DEE9",
			Success:    "#A3BE8C",
			Warning:    "#EBCB8B",
			Error:      "#BF616A",
			Foreground: "#ECEFF4",
			Surface:    "#3B4252",
		},
	},
}

var activeThemeState = struct {
	sync.RWMutex
	theme Theme
}{theme: availableThemes[0]}

// Themes returns the supported themes in their display order.
func Themes() []Theme {
	return append([]Theme(nil), availableThemes...)
}

// ResolveTheme looks up a theme name case-insensitively after trimming spaces.
func ResolveTheme(name string) (Theme, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, theme := range availableThemes {
		if string(theme.ID) == name {
			return theme, true
		}
	}
	return Theme{}, false
}

// SetTheme changes the active palette. Unknown names leave the current theme unchanged.
func SetTheme(name string) bool {
	theme, ok := ResolveTheme(name)
	if !ok {
		return false
	}
	activeThemeState.Lock()
	activeThemeState.theme = theme
	activeThemeState.Unlock()
	return true
}

// CurrentTheme returns a copy of the active theme.
func CurrentTheme() Theme {
	activeThemeState.RLock()
	defer activeThemeState.RUnlock()
	return activeThemeState.theme
}
