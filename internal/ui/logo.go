package ui

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"
	"golang.org/x/term"
)

//go:embed assets/logo.png
var logoPNG []byte

const (
	// DefaultLogoWidthCells is the logo width, in terminal cells, used whenever
	// the terminal is at least that wide.
	DefaultLogoWidthCells = 28
	// minLogoWidthCells is the narrowest logo that still reads as an image.
	minLogoWidthCells = 8
	// cellPixelRows is how many pixel rows one terminal line spans. Cells are
	// roughly twice as tall as they are wide, and each line carries two pixel
	// rows at once: one in the foreground colour, one in the background.
	cellPixelRows = 2
	// upperHalfBlock paints the foreground pixel on the top half of a cell.
	upperHalfBlock = "▀"
	// colorReset ends every rendered line so colours never bleed into the
	// surrounding help text.
	colorReset = "\x1b[0m"
)

// PrintLogo writes the embedded logo to w using 24-bit ANSI colours, followed
// by a blank separator line.
//
// It writes nothing when w is not a terminal, when colour is suppressed, or
// when the embedded image cannot be decoded, so piping help output into a
// file, another process, or a pager stays free of escape sequences.
func PrintLogo(w io.Writer) error {
	if !isTerminalWriter(w) || color.NoColor || logoColorSuppressed(os.Getenv) {
		return nil
	}

	img := decodeLogo()
	if img == nil {
		return nil
	}

	block := renderLogo(img, clampLogoWidth(terminalWidth(w)))
	if block == "" {
		return nil
	}
	_, err := io.WriteString(w, block+"\n\n")
	return err
}

// decodeLogo decodes the embedded logo, returning nil when it is unusable.
func decodeLogo() image.Image {
	if len(logoPNG) == 0 {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(logoPNG))
	if err != nil {
		return nil
	}
	return img
}

// renderLogo draws img with upper half block characters, widthCells cells wide.
//
// Every output line holds two pixel rows: the foreground colour paints the top
// half of the cell and the background colour the bottom half. Repeated colours
// are not re-emitted, which keeps the output a few kilobytes. The returned
// string carries no trailing newline. A non-positive widthCells falls back to
// DefaultLogoWidthCells.
func renderLogo(img image.Image, widthCells int) string {
	bounds := img.Bounds()
	if bounds.Empty() {
		return ""
	}
	if widthCells <= 0 {
		widthCells = DefaultLogoWidthCells
	}

	sourceWidth := bounds.Dx()
	sourceHeight := bounds.Dy()

	// Fit the image to widthCells while preserving its aspect ratio, then pack
	// two pixel rows into every terminal line.
	displayedPixelHeight := (sourceHeight*widthCells + sourceWidth/2) / sourceWidth
	lines := (displayedPixelHeight + cellPixelRows - 1) / cellPixelRows
	if lines < 1 {
		lines = 1
	}

	var out strings.Builder
	previousFG, previousBG := "", ""

	for line := 0; line < lines; line++ {
		for cell := 0; cell < widthCells; cell++ {
			x := bounds.Min.X + cell*sourceWidth/widthCells
			topY := bounds.Min.Y + line*cellPixelRows*displayedPixelHeight/(lines*cellPixelRows)
			bottomY := bounds.Min.Y + (line*cellPixelRows+1)*displayedPixelHeight/(lines*cellPixelRows)

			foreground, foregroundVisible := samplePixelForeground(img, x, topY)
			background, backgroundVisible := samplePixelBackground(img, x, bottomY)

			// A transparent top half leaves the cell blank so only the bottom
			// half's colour shows through.
			glyph := upperHalfBlock
			if !foregroundVisible {
				glyph = " "
			}

			fg := "\x1b[38;2;0;0;0m"
			if foregroundVisible {
				fg = foreground
			}
			bg := "\x1b[48;2;0;0;0m"
			if backgroundVisible {
				bg = background
			}

			if fg != previousFG {
				out.WriteString(fg)
				previousFG = fg
			}
			if bg != previousBG {
				out.WriteString(bg)
				previousBG = bg
			}
			out.WriteString(glyph)
		}
		out.WriteString(colorReset)
		if line < lines-1 {
			out.WriteString("\n")
		}
	}

	return out.String()
}

// samplePixelForeground reads a pixel for the top half of a cell, which needs a
// foreground SGR sequence. It reports whether the pixel is visible at all.
func samplePixelForeground(img image.Image, x, y int) (string, bool) {
	red, green, blue, alpha := img.At(x, y).RGBA()
	if alpha == 0 {
		return "", false
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", red>>8, green>>8, blue>>8), true
}

// samplePixelBackground reads a pixel for the bottom half of a cell, which
// needs a background SGR sequence.
func samplePixelBackground(img image.Image, x, y int) (string, bool) {
	red, green, blue, alpha := img.At(x, y).RGBA()
	if alpha == 0 {
		return "", false
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", red>>8, green>>8, blue>>8), true
}

// clampLogoWidth limits the logo to the terminal, never exceeding the default.
func clampLogoWidth(terminalWidth int) int {
	if terminalWidth <= 0 {
		return DefaultLogoWidthCells
	}
	if terminalWidth < minLogoWidthCells {
		return minLogoWidthCells
	}
	if terminalWidth < DefaultLogoWidthCells {
		return terminalWidth
	}
	return DefaultLogoWidthCells
}

// terminalWidth reports the width of w in cells, or 0 when it is unknown.
func terminalWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	width, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return width
}

// isTerminalWriter reports whether w is a terminal that can render colours.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// logoColorSuppressed reports whether the environment asked for no colour.
func logoColorSuppressed(getenv func(string) string) bool {
	return getenv("NO_COLOR") != "" || getenv("TERM") == "dumb"
}
