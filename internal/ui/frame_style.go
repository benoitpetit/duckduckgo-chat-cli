package ui

import "sync"

const DefaultFrameMaxWidth = 112

type FrameWidthMode string

const (
	FrameWidthFull    FrameWidthMode = "full"
	FrameWidthMaximum FrameWidthMode = "maximum"
)

type FrameLineWeight string

const (
	FrameLineSingle FrameLineWeight = "single"
	FrameLineHeavy  FrameLineWeight = "heavy"
)

// FrameOptions controls shared geometry and border styling for framed UI.
type FrameOptions struct {
	WidthMode  FrameWidthMode
	MaxWidth   int
	LineWeight FrameLineWeight
}

// FrameGlyphSet contains the six border glyphs used by framed UI renderers.
type FrameGlyphSet struct {
	TopLeft     string
	TopRight    string
	BottomLeft  string
	BottomRight string
	Horizontal  string
	Vertical    string
}

var frameOptionsState = struct {
	sync.RWMutex
	options FrameOptions
}{options: DefaultFrameOptions()}

// DefaultFrameOptions preserves the existing 112-column, single-line frames.
func DefaultFrameOptions() FrameOptions {
	return FrameOptions{WidthMode: FrameWidthMaximum, MaxWidth: DefaultFrameMaxWidth, LineWeight: FrameLineSingle}
}

// NormalizeFrameOptions replaces unknown values with safe appearance defaults.
func NormalizeFrameOptions(options FrameOptions) FrameOptions {
	if options.WidthMode != FrameWidthFull && options.WidthMode != FrameWidthMaximum {
		options.WidthMode = FrameWidthMaximum
	}
	if !supportedFrameWidth(options.MaxWidth) {
		options.MaxWidth = DefaultFrameMaxWidth
	}
	if options.LineWeight != FrameLineSingle && options.LineWeight != FrameLineHeavy {
		options.LineWeight = FrameLineSingle
	}
	return options
}

func supportedFrameWidth(width int) bool {
	switch width {
	case 80, 96, 112, 128, 144, 160:
		return true
	default:
		return false
	}
}

// SetFrameOptions updates the shared frame style after normalizing its values.
func SetFrameOptions(options FrameOptions) {
	frameOptionsState.Lock()
	frameOptionsState.options = NormalizeFrameOptions(options)
	frameOptionsState.Unlock()
}

// CurrentFrameOptions returns a copy of the active normalized frame style.
func CurrentFrameOptions() FrameOptions {
	frameOptionsState.RLock()
	defer frameOptionsState.RUnlock()
	return frameOptionsState.options
}

// ResolveFrameWidth resolves the selected width against the available columns.
// A non-positive terminal width uses a conservative 80-column fallback.
func ResolveFrameWidth(terminalWidth int) int {
	if terminalWidth <= 0 {
		terminalWidth = 80
	}
	options := CurrentFrameOptions()
	if options.WidthMode == FrameWidthFull {
		return terminalWidth
	}
	if terminalWidth < options.MaxWidth {
		return terminalWidth
	}
	return options.MaxWidth
}

// ResolveFrameGlyphs returns matching border glyphs, or plain ASCII when asked.
func ResolveFrameGlyphs(ascii bool) FrameGlyphSet {
	if ascii {
		return FrameGlyphSet{TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+", Horizontal: "-", Vertical: "|"}
	}
	if CurrentFrameOptions().LineWeight == FrameLineHeavy {
		return FrameGlyphSet{TopLeft: "┏", TopRight: "┓", BottomLeft: "┗", BottomRight: "┛", Horizontal: "━", Vertical: "┃"}
	}
	return FrameGlyphSet{TopLeft: "╭", TopRight: "╮", BottomLeft: "╰", BottomRight: "╯", Horizontal: "─", Vertical: "│"}
}
