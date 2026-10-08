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

var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// foregroundSequence captures the 24-bit foreground colour of a cell.
var foregroundSequence = regexp.MustCompile(`\x1b\[38;2;(\d+;\d+;\d+)m`)

// stripANSI removes SGR escape sequences so tests can count visible cells.
func stripANSI(s string) string {
	return sgrPattern.ReplaceAllString(s, "")
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

func TestRenderLogoSamplesTheWholeImageHeight(t *testing.T) {
	// Top half red, bottom half blue: the last rendered line must resolve to
	// the blue bottom of the image, not a repeat of the red top.
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		pixel := color.RGBA{B: 255, A: 255}
		if y < 32 {
			pixel = color.RGBA{R: 255, A: 255}
		}
		for x := 0; x < 64; x++ {
			img.Set(x, y, pixel)
		}
	}

	lines := strings.Split(renderLogo(img, DefaultLogoWidthCells), "\n")

	if got := effectiveForegroundAt(lines, 0); !strings.HasSuffix(got, "255;0;0") {
		t.Errorf("first line should start from the red top of the image, got %q", got)
	}
	if got := effectiveForegroundAt(lines, len(lines)-1); !strings.HasSuffix(got, "0;0;255") {
		t.Errorf("last line should sample the blue bottom of the image, got %q", got)
	}
}

// effectiveForegroundAt resolves the foreground colour in effect on a rendered
// line. Repeated colours are not re-emitted, so the bytes on that line alone do
// not say what the user sees.
func effectiveForegroundAt(lines []string, index int) string {
	current := ""
	for i := 0; i <= index; i++ {
		for _, match := range foregroundSequence.FindAllStringSubmatch(lines[i], -1) {
			current = match[1]
		}
	}
	return current
}

func TestRenderLogoSamplesTheWholeImageWidth(t *testing.T) {
	// Left half green, right half yellow.
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			pixel := color.RGBA{R: 255, G: 255, A: 255}
			if x < 32 {
				pixel = color.RGBA{G: 255, A: 255}
			}
			img.Set(x, y, pixel)
		}
	}

	first := strings.Split(renderLogo(img, DefaultLogoWidthCells), "\n")[0]

	if !strings.Contains(first, "\x1b[38;2;0;255;0m") {
		t.Errorf("first line should start with the green left edge, got %q", stripANSI(first))
	}
	if !strings.Contains(first, "\x1b[38;2;255;255;0m") {
		t.Errorf("first line should reach the yellow right edge, got %q", stripANSI(first))
	}
}

// TestRenderLogoFitsWithinTheHelpScreenBudget keeps the logo from crowding the
// help text: it must stay short enough to read as a header, not a wallpaper.
// The ceiling is deliberately just above the chosen 36 cells so nobody widens
// it again without noticing.
func TestRenderLogoFitsWithinTheHelpScreenBudget(t *testing.T) {
	const maxLogoLines = 20
	const maxLogoCells = 38

	img := solidImage(64, 64, color.RGBA{A: 255})

	lines := strings.Split(renderLogo(img, DefaultLogoWidthCells), "\n")
	if len(lines) > maxLogoLines {
		t.Errorf("logo takes %d terminal lines at the default width, budget is %d; "+
			"shrink DefaultLogoWidthCells", len(lines), maxLogoLines)
	}
	if cells := len([]rune(stripANSI(lines[0]))); cells > maxLogoCells {
		t.Errorf("logo is %d cells wide, budget is %d; shrink DefaultLogoWidthCells", cells, maxLogoCells)
	}
}

// TestRenderLogoAveragesPixelsWithinEachCell distinguishes real area averaging
// from point sampling: the last cell straddles a colour boundary, so it must
// come out as a blend rather than one of the two source colours.
func TestRenderLogoAveragesPixelsWithinEachCell(t *testing.T) {
	// 8px wide: six red columns then two blue. At 3 cells wide the last cell
	// covers columns 5..7, which is one red and two blue.
	img := image.NewRGBA(image.Rect(0, 0, 8, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 8; x++ {
			pixel := color.RGBA{R: 255, A: 255}
			if x >= 6 {
				pixel = color.RGBA{B: 255, A: 255}
			}
			img.Set(x, y, pixel)
		}
	}

	got := renderLogo(img, 3)

	// One red plus two blue pixels averages to a third of each channel.
	if !strings.Contains(got, "\x1b[38;2;85;0;170m") {
		t.Errorf("boundary cell should blend to 85;0;170, got %q", stripANSI(got))
	}
}

// TestRenderLogoWeightsTransparencyInTheAverage checks that partially covered
// cells keep their hue instead of being darkened by the transparent pixels.
func TestRenderLogoWeightsTransparencyInTheAverage(t *testing.T) {
	// Two columns: one opaque red, one fully transparent. Averaging the raw
	// channels would give a dark 128;0;0, weighting by alpha keeps pure red.
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(0, 1, color.RGBA{R: 255, A: 255})

	got := renderLogo(img, 1)

	if !strings.Contains(got, "\x1b[38;2;255;0;0m") {
		t.Errorf("transparent pixels must not darken the average, got %q", stripANSI(got))
	}
	if strings.Contains(got, "\x1b[38;2;128;0;0m") {
		t.Errorf("average must be alpha-weighted, not a naive channel mean, got %q", stripANSI(got))
	}
}

func TestAveragePixelKeepsHueForPartiallyTransparentPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 128})

	got, visible := averagePixel(img, 0, 0, 2, 2, true)
	if !visible {
		t.Fatal("partially transparent red should remain visible")
	}
	if want := "\x1b[38;2;255;0;0m"; got != want {
		t.Errorf("partially transparent red should retain its hue, got %q, want %q", got, want)
	}
}

func TestLayoutLogoBeside(t *testing.T) {
	tests := []struct {
		name          string
		terminalWidth int
		wantStacked   bool
		wantLogo      int
		wantText      int
	}{
		{
			name:          "wide terminal keeps the logo at full width",
			terminalWidth: 120,
			wantLogo:      DefaultLogoWidthCells,
			wantText:      120 - DefaultLogoWidthCells - logoGap,
		},
		{
			name:          "default terminal keeps the logo at full width",
			terminalWidth: 80,
			wantLogo:      DefaultLogoWidthCells,
			wantText:      80 - DefaultLogoWidthCells - logoGap,
		},
		{
			name:          "logo stays full width while the text fits",
			terminalWidth: 72,
			wantLogo:      DefaultLogoWidthCells,
			wantText:      72 - DefaultLogoWidthCells - logoGap,
		},
		{
			name:          "logo shrinks once the text would be too narrow",
			terminalWidth: 71,
			wantLogo:      71 - logoGap - minBesideTextWidth,
			wantText:      minBesideTextWidth,
		},
		{
			name:          "narrow terminal shrinks the logo further",
			terminalWidth: 60,
			wantLogo:      60 - logoGap - minBesideTextWidth,
			wantText:      minBesideTextWidth,
		},
		{
			name:          "logo is capped at its minimum width",
			terminalWidth: 44,
			wantLogo:      minLogoWidthCells,
			wantText:      minBesideTextWidth,
		},
		{
			name:          "below the threshold the layout stacks",
			terminalWidth: 43,
			wantStacked:   true,
			wantLogo:      DefaultLogoWidthCells,
			wantText:      0,
		},
		{
			name:          "unknown width falls back to stacking",
			terminalWidth: 0,
			wantStacked:   true,
			wantLogo:      DefaultLogoWidthCells,
			wantText:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := layoutLogoBeside(tt.terminalWidth)

			if got.Stacked != tt.wantStacked {
				t.Errorf("Stacked = %v, want %v", got.Stacked, tt.wantStacked)
			}
			if got.LogoWidth != tt.wantLogo {
				t.Errorf("LogoWidth = %d, want %d", got.LogoWidth, tt.wantLogo)
			}
			if got.TextWidth != tt.wantText {
				t.Errorf("TextWidth = %d, want %d", got.TextWidth, tt.wantText)
			}
		})
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{name: "text that fits stays on one line", text: "Usage: duckchat", width: 40, want: []string{"Usage: duckchat"}},
		{name: "blank line is kept", text: "", width: 40, want: []string{""}},
		{name: "wraps on spaces", text: "alpha beta gamma delta", width: 11, want: []string{"alpha beta", "gamma delta"}},
		{name: "breaks a word longer than the width", text: "abcdefghij", width: 4, want: []string{"abcd", "efgh", "ij"}},
		{
			name:  "keeps leading indentation on a line that fits",
			text:  "      --json          Output one-shot response as JSON",
			width: 80,
			want:  []string{"      --json          Output one-shot response as JSON"},
		},
		{
			name:  "keeps leading indentation on wrapped continuation lines",
			text:  "      --prompt TEXT   Send one prompt and exit now",
			width: 34,
			want: []string{
				"      --prompt TEXT   Send one",
				"      prompt and exit now",
			},
		},
		{
			name:  "keeps the internal column gap",
			text:  "  -h, --help          Show this help message",
			width: 34,
			want: []string{
				"  -h, --help          Show this",
				"  help message",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapText(tt.text, tt.width)
			if len(got) != len(tt.want) {
				t.Fatalf("wrapText(%q, %d) = %q, want %q", tt.text, tt.width, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("line %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestWrapTextKeepsColourSequencesIntact(t *testing.T) {
	const cyan = "\x1b[36m"
	const reset = "\x1b[0m"

	lines := wrapText(cyan+"alpha beta gamma delta"+reset, 11)

	if len(lines) != 2 {
		t.Fatalf("expected the coloured text to wrap onto 2 lines, got %q", lines)
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, cyan) {
			t.Errorf("line %d must keep its opening colour, got %q", i, line)
		}
		if !strings.HasSuffix(line, reset) {
			t.Errorf("line %d must end with a reset, got %q", i, line)
		}
		// A split escape sequence would leave a stray ESC in the middle.
		if strings.Count(line, "\x1b") != 2 {
			t.Errorf("line %d has a broken escape sequence: %q", i, line)
		}
		if visible := visibleWidth(line); visible > 11 {
			t.Errorf("line %d is %d visible cells wide, budget is 11: %q", i, visible, line)
		}
	}
}

func TestVisibleWidth(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "plain", text: "hello", want: 5},
		{name: "colour sequences are zero width", text: "\x1b[38;2;1;2;3mhello\x1b[0m", want: 5},
		{name: "empty", text: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := visibleWidth(tt.text); got != tt.want {
				t.Errorf("visibleWidth(%q) = %d, want %d", tt.text, got, tt.want)
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

// TestPrintLogoBesideStillPrintsTheTextWithoutALogo guards the case that
// matters most: a redirected --help has no logo to draw, so the help text is
// all the user gets. It must be written verbatim, unwrapped.
func TestPrintLogoBesideStillPrintsTheTextWithoutALogo(t *testing.T) {
	var out bytes.Buffer
	long := "      --prompt TEXT   Send one prompt and exit; use - to read from stdin"

	if err := PrintLogoBeside(&out, []string{"Usage: duckchat [options]", "", long}); err != nil {
		t.Fatalf("PrintLogoBeside returned an error: %v", err)
	}

	want := "Usage: duckchat [options]\n\n" + long + "\n"
	if out.String() != want {
		t.Errorf("redirected output = %q, want %q", out.String(), want)
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

// TestEmbeddedLogoHasEnoughResolutionForItsWidth guards the rendered result
// against turning into mush. The renderer averages a box of source pixels per
// cell, so a thin box means a noisy average: keep real detail to average over.
func TestEmbeddedLogoHasEnoughResolutionForItsWidth(t *testing.T) {
	decoded := decodeLogo()
	if decoded == nil {
		t.Fatal("the embedded logo must decode as a PNG")
	}

	const pixelsPerCell = 6
	want := DefaultLogoWidthCells * pixelsPerCell
	if got := decoded.Bounds().Dx(); got < want {
		t.Errorf("embedded logo is %dpx wide, too coarse for %d cells at %d px/cell; "+
			"re-export it at %dpx or wider", got, DefaultLogoWidthCells, pixelsPerCell, want)
	}
}
