package ui

import "testing"

func TestDefaultFrameOptionsPreserveCurrentAppearance(t *testing.T) {
	if got, want := DefaultFrameOptions(), (FrameOptions{
		WidthMode:  FrameWidthMaximum,
		MaxWidth:   112,
		LineWeight: FrameLineSingle,
	}); got != want {
		t.Fatalf("DefaultFrameOptions() = %#v, want %#v", got, want)
	}
}

func TestNormalizeFrameOptionsUsesSafeDefaultsAndAcceptsSupportedWidths(t *testing.T) {
	if got, want := NormalizeFrameOptions(FrameOptions{
		WidthMode:  "invalid",
		MaxWidth:   111,
		LineWeight: "invalid",
	}), DefaultFrameOptions(); got != want {
		t.Fatalf("invalid options normalized to %#v, want %#v", got, want)
	}

	for _, width := range []int{80, 96, 112, 128, 144, 160} {
		got := NormalizeFrameOptions(FrameOptions{
			WidthMode:  FrameWidthMaximum,
			MaxWidth:   width,
			LineWeight: FrameLineHeavy,
		})
		if got.MaxWidth != width || got.LineWeight != FrameLineHeavy {
			t.Errorf("valid maximum width %d normalized to %#v", width, got)
		}
	}
}

func TestResolveFrameWidthUsesFullOrMaximumWidthAndClampsToTerminal(t *testing.T) {
	tests := []struct {
		name    string
		options FrameOptions
		width   int
		want    int
	}{
		{name: "full width", options: FrameOptions{WidthMode: FrameWidthFull, MaxWidth: 112}, width: 148, want: 148},
		{name: "wide full terminal", options: FrameOptions{WidthMode: FrameWidthFull, MaxWidth: 112}, width: 240, want: 240},
		{name: "maximum below cap", options: FrameOptions{WidthMode: FrameWidthMaximum, MaxWidth: 112}, width: 96, want: 96},
		{name: "maximum at cap", options: FrameOptions{WidthMode: FrameWidthMaximum, MaxWidth: 112}, width: 160, want: 112},
		{name: "fallback terminal width", options: FrameOptions{WidthMode: FrameWidthFull, MaxWidth: 112}, width: 0, want: 80},
	}
	prior := CurrentFrameOptions()
	t.Cleanup(func() { SetFrameOptions(prior) })

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			SetFrameOptions(test.options)
			if got := ResolveFrameWidth(test.width); got != test.want {
				t.Fatalf("ResolveFrameWidth(%d) = %d, want %d", test.width, got, test.want)
			}
		})
	}
}

func TestResolveFrameGlyphsSelectsSingleHeavyAndASCIIBorders(t *testing.T) {
	prior := CurrentFrameOptions()
	t.Cleanup(func() { SetFrameOptions(prior) })

	SetFrameOptions(FrameOptions{WidthMode: FrameWidthMaximum, MaxWidth: 112, LineWeight: FrameLineSingle})
	if got, want := ResolveFrameGlyphs(false), (FrameGlyphSet{
		TopLeft: "╭", TopRight: "╮", BottomLeft: "╰", BottomRight: "╯", Horizontal: "─", Vertical: "│",
	}); got != want {
		t.Fatalf("single glyphs = %#v, want %#v", got, want)
	}

	SetFrameOptions(FrameOptions{WidthMode: FrameWidthMaximum, MaxWidth: 112, LineWeight: FrameLineHeavy})
	if got, want := ResolveFrameGlyphs(false), (FrameGlyphSet{
		TopLeft: "┏", TopRight: "┓", BottomLeft: "┗", BottomRight: "┛", Horizontal: "━", Vertical: "┃",
	}); got != want {
		t.Fatalf("heavy glyphs = %#v, want %#v", got, want)
	}
	if got, want := ResolveFrameGlyphs(true), (FrameGlyphSet{
		TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+", Horizontal: "-", Vertical: "|",
	}); got != want {
		t.Fatalf("ASCII glyphs = %#v, want %#v", got, want)
	}
}
