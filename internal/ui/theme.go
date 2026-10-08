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
