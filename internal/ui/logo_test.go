package ui

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"regexp"
	"strings"
	"testing"
)

var ansiSequence = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes SGR escape sequences so tests can count visible cells.
func stripANSI(s string) string {
	return ansiSequence.ReplaceAllString(s, "")
}

func solidImage(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestRenderLogoEmitsOneLinePerPixelRowPair(t *testing.T) {
	img := solidImage(4, 2, color.RGBA{R: 10, G: 20, B: 30, A: 255})

	got := renderLogo(img, 4)

	lines := strings.Split(got, "\n")
	if len(lines) != 1 {
		t.Fatalf("2 pixel rows should collapse into 1 terminal line, got %d lines: %q", len(lines), got)
	}
	if !strings.Contains(got, "▀") {
		t.Errorf("expected upper half block U+2580, got %q", stripANSI(got))
	}
}

func TestRenderLogoColorsTopPixelAsForegroundAndBottomAsBackground(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(0, 1, color.RGBA{B: 255, A: 255})

	got := renderLogo(img, 1)

	if !strings.Contains(got, "\x1b[38;2;255;0;0m") {
		t.Errorf("expected red foreground SGR for top pixel, got %q", got)
	}
	if !strings.Contains(got, "\x1b[48;2;0;0;255m") {
		t.Errorf("expected blue background SGR for bottom pixel, got %q", got)
	}
}

func TestRenderLogoLineCountIsHalfThePixelRows(t *testing.T) {
	tests := []struct {
		name     string
		pixels   int
		wantRows int
	}{
		{name: "even rows", pixels: 8, wantRows: 4},
		{name: "odd rows round up", pixels: 7, wantRows: 4},
		{name: "single row", pixels: 1, wantRows: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img := solidImage(4, tt.pixels, color.RGBA{A: 255})

			got := renderLogo(img, 4)

			if rows := len(strings.Split(got, "\n")); rows != tt.wantRows {
				t.Errorf("pixel rows = %d, want %d terminal lines, got %q", tt.pixels, tt.wantRows, got)
			}
		})
	}
}

func TestRenderLogoUsesRequestedColumnCount(t *testing.T) {
	img := solidImage(64, 64, color.RGBA{A: 255})

	for _, width := range []int{4, 8, 28, 60} {
		got := renderLogo(img, width)

		for i, line := range strings.Split(got, "\n") {
			if cells := len([]rune(stripANSI(line))); cells != width {
				t.Errorf("width %d: line %d has %d visible cells, want %d", width, i, cells, width)
			}
		}
	}
}

func TestRenderLogoRejectsNonPositiveWidthByFallingBackToDefault(t *testing.T) {
	img := solidImage(64, 64, color.RGBA{A: 255})

	for _, width := range []int{0, -5} {
		got := renderLogo(img, width)

		cells := len([]rune(stripANSI(strings.Split(got, "\n")[0])))
		if cells != DefaultLogoWidthCells {
			t.Errorf("width %d: got %d cells per line, want the %d-cell default", width, cells, DefaultLogoWidthCells)
		}
	}
}

func TestRenderLogoRendersSpaceWhenTopPixelIsTransparent(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 2))
	img.Set(0, 0, color.RGBA{})
	img.Set(0, 1, color.RGBA{G: 255, A: 255})

	got := renderLogo(img, 1)

	if !strings.Contains(stripANSI(got), " ") {
		t.Errorf("expected a blank cell for the transparent top pixel, got %q", stripANSI(got))
	}
}

func TestRenderLogoReadsFullyTransparentPixelAsBackgroundColor(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(0, 1, color.RGBA{A: 0})

	got := renderLogo(img, 1)

	if !strings.Contains(got, "\x1b[48;2;0;0;0m") {
		t.Errorf("expected a fully transparent bottom pixel to read as the background, got %q", got)
	}
}

func TestRenderLogoEndsEveryLineWithColorReset(t *testing.T) {
	img := solidImage(8, 4, color.RGBA{G: 255, A: 255})

	got := renderLogo(img, 8)

	for i, line := range strings.Split(got, "\n") {
		if !strings.HasSuffix(line, "\x1b[0m") {
			t.Errorf("line %d does not end with a color reset: %q", i, line)
		}
	}
}

func TestRenderLogoProducesNoTrailingNewline(t *testing.T) {
	img := solidImage(4, 4, color.RGBA{A: 255})

	if got := renderLogo(img, 4); strings.HasSuffix(got, "\n") {
		t.Errorf("renderLogo must not append a trailing newline, got %q", got)
	}
}

func TestRenderLogoEmptyImageProducesEmptyOutput(t *testing.T) {
	if got := renderLogo(image.NewRGBA(image.Rect(0, 0, 0, 0)), 8); got != "" {
		t.Errorf("empty image should render nothing, got %q", got)
	}
}

func TestLogoColorSuppressed(t *testing.T) {
	tests := []struct {
		name   string
		getenv func(string) string
		want   bool
	}{
		{
			name: "color allowed",
			getenv: func(key string) string {
				if key == "TERM" {
					return "xterm-256color"
				}
				return ""
			},
			want: false,
		},
		{
			name: "NO_COLOR set",
			getenv: func(key string) string {
				if key == "NO_COLOR" {
					return "1"
				}
				return ""
			},
			want: true,
		},
		{
			name: "TERM dumb",
			getenv: func(key string) string {
				if key == "TERM" {
					return "dumb"
				}
				return ""
			},
			want: true,
		},
		{
			name:   "empty environment",
			getenv: func(string) string { return "" },
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := logoColorSuppressed(tt.getenv); got != tt.want {
				t.Errorf("logoColorSuppressed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClampLogoWidth(t *testing.T) {
	tests := []struct {
		name          string
		terminalWidth int
		want          int
	}{
		{name: "unknown width falls back to the default", terminalWidth: 0, want: DefaultLogoWidthCells},
		{name: "negative width falls back to the default", terminalWidth: -1, want: DefaultLogoWidthCells},
		{name: "wide terminal uses the default", terminalWidth: 200, want: DefaultLogoWidthCells},
		{name: "narrow terminal shrinks the logo", terminalWidth: 12, want: 12},
		{name: "very narrow terminal has a floor", terminalWidth: 4, want: minLogoWidthCells},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampLogoWidth(tt.terminalWidth); got != tt.want {
				t.Errorf("clampLogoWidth(%d) = %d, want %d", tt.terminalWidth, got, tt.want)
			}
		})
	}
}

func TestIsTerminalWriterRejectsNonFileWriters(t *testing.T) {
	if isTerminalWriter(&bytes.Buffer{}) {
		t.Error("a bytes.Buffer is not a terminal")
	}
}

func TestIsTerminalWriterRejectsRegularFiles(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "logo")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer f.Close()

	if isTerminalWriter(f) {
		t.Error("a regular file is not a terminal")
	}
}

func TestPrintLogoWritesNothingWhenTheWriterIsNotATerminal(t *testing.T) {
	var out bytes.Buffer

	if err := PrintLogo(&out); err != nil {
		t.Fatalf("PrintLogo returned an error for a non-terminal writer: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output for a non-terminal writer, got %q", out.String())
	}
}

func TestEmbeddedLogoDecodesAndIsRoughlySquare(t *testing.T) {
	decoded := decodeLogo()
	if decoded == nil {
		t.Fatal("the embedded logo must decode as a PNG")
	}

	b := decoded.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		t.Fatalf("embedded logo has empty bounds %v", b)
	}
	ratio := float64(b.Dx()) / float64(b.Dy())
	if ratio < 0.5 || ratio > 2 {
		t.Errorf("embedded logo aspect ratio %.2f is not roughly square: %v", ratio, b)
	}
	if !hasVisiblePixels(decoded) {
		t.Error("embedded logo is fully transparent; nothing would be drawn")
	}
}

// hasVisiblePixels reports whether at least one pixel is not fully transparent.
func hasVisiblePixels(img image.Image) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0 {
				return true
			}
		}
	}
	return false
}
