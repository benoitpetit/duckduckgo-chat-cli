package ui

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/fatih/color"
	"golang.org/x/term"
)

//go:embed assets/logo.png
var logoPNG []byte

const (
	// DefaultLogoWidthCells is the logo width, in terminal cells, used whenever
	// the terminal is at least that wide.
	//
	// 36 is the smallest width where the mascot's identifying features stay
	// readable: below it the glasses and bowtie wash out into a grey smear.
	// Below about 30 cells the rendering is not worth printing at all.
	DefaultLogoWidthCells = 36
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
	// logoGap separates the logo from the text printed beside it.
	logoGap = 2
	// minBesideTextWidth is the narrowest column count the help text stays
	// readable at. Below this the logo yields space instead.
	minBesideTextWidth = 34
)

// LogoLayout describes how the logo and the help text share the terminal width.
type LogoLayout struct {
	// Stacked reports whether the logo sits above the text rather than beside
	// it, which is what a terminal too narrow to share falls back to.
	Stacked bool
	// LogoWidth is the logo width in terminal cells.
	LogoWidth int
	// TextWidth is the column count available for text beside the logo, or 0
	// when the layout is stacked.
	TextWidth int
}

// layoutLogoBeside picks a layout for the given terminal width. The logo keeps
// its full quality width while the leftover columns can still hold readable
// text; past that it gives up width, and if even the minimum logo does not fit
// alongside readable text the layout stacks.
func layoutLogoBeside(terminalWidth int) LogoLayout {
	if terminalWidth <= 0 {
		return LogoLayout{Stacked: true, LogoWidth: DefaultLogoWidthCells}
	}

	available := terminalWidth - logoGap
	logoWidth := DefaultLogoWidthCells
	if available-logoWidth < minBesideTextWidth {
		logoWidth = available - minBesideTextWidth
	}
	if logoWidth < minLogoWidthCells {
		return LogoLayout{Stacked: true, LogoWidth: DefaultLogoWidthCells}
	}
	return LogoLayout{LogoWidth: logoWidth, TextWidth: terminalWidth - logoWidth - logoGap}
}

// ansiSequence matches the colour escapes fatih/color wraps around help text.
var ansiSequence = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// visibleWidth counts printable cells, ignoring colour sequences.
func visibleWidth(text string) int {
	return utf8.RuneCountInString(ansiSequence.ReplaceAllString(text, ""))
}

// wrapToken is a run of either whitespace or text that must not be split.
type wrapToken struct {
	text string
	gap  bool
}

// tokenizeWrap splits text into whitespace and text runs, carrying any colour
// sequence into the run it sits inside so escapes stay attached to what they
// colour. Leading escapes and indentation are returned separately as the
// prefix, which every produced line repeats.
func tokenizeWrap(text string) (prefix string, tokens []wrapToken) {
	var run strings.Builder
	inGap := false
	flush := func() {
		if run.Len() > 0 {
			tokens = append(tokens, wrapToken{text: run.String(), gap: inGap})
			run.Reset()
		}
	}

	var head strings.Builder
	at := 0
	for at < len(text) {
		if loc := ansiSequence.FindStringIndex(text[at:]); loc != nil && loc[0] == 0 {
			head.WriteString(text[at : at+loc[1]])
			at += loc[1]
			continue
		}
		if text[at] == ' ' {
			head.WriteByte(' ')
			at++
			continue
		}
		break
	}
	prefix = head.String()

	for at < len(text) {
		if loc := ansiSequence.FindStringIndex(text[at:]); loc != nil && loc[0] == 0 {
			run.WriteString(text[at : at+loc[1]])
			at += loc[1]
			continue
		}
		if text[at] == ' ' {
			if !inGap {
				flush()
				inGap = true
			}
			run.WriteByte(' ')
			at++
			continue
		}
		if inGap {
			flush()
			inGap = false
		}
		run.WriteByte(text[at])
		at++
	}
	flush()

	return prefix, tokens
}

// splitVisible cuts s after width printable cells, leaving any colour sequence
// that follows the cut on the second half.
func splitVisible(s string, width int) (head, tail string) {
	seen := 0
	for at := 0; at < len(s); {
		if loc := ansiSequence.FindStringIndex(s[at:]); loc != nil && loc[0] == 0 {
			at += loc[1]
			continue
		}
		_, size := utf8.DecodeRuneInString(s[at:])
		seen++
		if seen > width {
			return s[:at], s[at:]
		}
		at += size
	}
	return s, ""
}

// wrapText breaks text into lines no wider than width.
//
// The help text is column-aligned and colourised, so wrapping preserves the
// leading indentation and the spacing between words, measures lines by their
// printable width, and never splits a colour sequence. A line that already
// fits is returned untouched.
func wrapText(text string, width int) []string {
	if text == "" {
		// A blank line in the source is a deliberate spacer, not nothing.
		return []string{""}
	}
	if width < 1 || visibleWidth(text) <= width {
		return []string{text}
	}

	prefix, tokens := tokenizeWrap(text)
	budget := width - visibleWidth(prefix)
	if budget < 1 {
		budget = 1
	}

	var lines []string
	current := prefix
	pendingGap := ""
	hasWord := false

	flush := func() {
		lines = append(lines, current)
		current = prefix
		pendingGap = ""
		hasWord = false
	}

	for _, token := range tokens {
		if token.gap {
			if hasWord {
				pendingGap = token.text
			}
			continue
		}

		word := token.text
		// A word wider than the budget has to be cut across lines.
		for visibleWidth(word) > budget {
			if hasWord {
				flush()
			}
			var head string
			head, word = splitVisible(word, budget)
			current = prefix + head
			flush()
		}

		candidate := current + pendingGap + word
		if hasWord && visibleWidth(candidate) > width {
			flush()
			current = prefix + word
			hasWord = true
			continue
		}
		current = candidate
		hasWord = true
	}

	lines = append(lines, current)

	// An open colour sequence survives a row break, so close every wrapped line.
	// The prefix already re-opens the colour on continuation lines.
	if ansiSequence.MatchString(text) {
		for i, line := range lines {
			if !strings.HasSuffix(line, colorReset) {
				lines[i] = line + colorReset
			}
		}
	}

	return lines
}

// PrintLogo writes the embedded logo to w using 24-bit ANSI colours, followed
// by a blank separator line.
//
// It writes nothing when w is not a terminal, when colour is suppressed, or
// when the embedded image cannot be decoded.
func PrintLogo(w io.Writer) error {
	return PrintLogoBeside(w, nil)
}

// PrintLogoBeside writes the help text in lines to w, with the embedded logo
// running down its right-hand side. When the terminal is too narrow to share,
// the logo moves above the text.
//
// The text is always written. If w cannot show a logo — a pipe, a pager,
// NO_COLOR, TERM=dumb — the lines are written verbatim at full width so
// redirected output is byte-for-byte the same as it ever was.
func PrintLogoBeside(w io.Writer, lines []string) error {
	return PrintLogoBesideInset(w, lines, 0)
}

// PrintLogoBesideInset is PrintLogoBeside with the text starting inset rows
// below the top of the logo, which keeps a short block optically centred
// against the image. The inset only applies when a logo is actually drawn.
func PrintLogoBesideInset(w io.Writer, lines []string, inset int) error {
	img, canDraw := printableLogo(w)
	if !canDraw {
		return writeTextLines(w, lines, 0)
	}

	layout := layoutLogoBeside(terminalWidth(w))
	// Stacked still prints the logo, just above the text instead of beside it,
	// so its width has to be measured against the terminal rather than taken
	// from the layout, which reports the default width when it gives up.
	logoWidth := layout.LogoWidth
	if layout.Stacked {
		logoWidth = terminalWidth(w)
	}
	block := renderLogo(img, clampLogoWidth(logoWidth))
	if block == "" {
		return writeTextLines(w, lines, 0)
	}

	if len(lines) == 0 {
		_, err := io.WriteString(w, block+"\n\n")
		return err
	}

	// Only wrap once a logo is genuinely being drawn: without one the caller's
	// lines are written verbatim so piped output stays escape-free.
	textWidth := layout.TextWidth
	if layout.Stacked {
		textWidth = terminalWidthOrDefault(w)
	}
	text := make([]string, 0, len(lines)+inset)
	for i := 0; i < inset; i++ {
		text = append(text, "")
	}
	for _, line := range lines {
		text = append(text, wrapText(line, textWidth)...)
	}

	if layout.Stacked {
		if _, err := io.WriteString(w, block+"\n\n"); err != nil {
			return err
		}
		return writeTextLines(w, text, 0)
	}

	// Text that fits shares a row with the logo; the rest continues below it.
	logoLines := strings.Split(block, "\n")
	var out strings.Builder
	for row := range logoLines {
		out.WriteString(logoLines[row])
		if row < len(text) {
			out.WriteString(strings.Repeat(" ", logoGap))
			out.WriteString(text[row])
		}
		if row < len(logoLines)-1 {
			out.WriteString("\n")
		}
	}
	if _, err := io.WriteString(w, out.String()+"\n"); err != nil {
		return err
	}
	if len(text) <= len(logoLines) {
		return nil
	}
	return writeTextLines(w, text[len(logoLines):], 0)
}

// fallbackTextWidth is the width used for text when the terminal size is
// unknown, matching the stable default the rest of the help text assumes.
const fallbackTextWidth = 80

// TextWidth reports how many columns text written to w may occupy.
//
// When the logo is drawn beside it, that is the width of the remaining
// column; otherwise it is the whole terminal. Callers should size their
// content with this rather than measuring the terminal themselves, so the
// layout can never disagree with what PrintLogoBeside actually renders.
func TextWidth(w io.Writer) int {
	full := terminalWidthOrDefault(w)
	layout := layoutLogoBeside(full)
	if !layout.Stacked && isTerminalWriter(w) && !color.NoColor && !logoColorSuppressed(os.Getenv) {
		return layout.TextWidth
	}
	return full
}

// terminalWidthOrDefault reports w's width, falling back to a stable default
// for pipes, redirected output, and terminals that do not report their size.
func terminalWidthOrDefault(w io.Writer) int {
	if width := terminalWidth(w); width > 0 {
		return width
	}
	return fallbackTextWidth
}

// printableLogo applies the terminal and colour guards and decodes the asset.
func printableLogo(w io.Writer) (image.Image, bool) {
	if !isTerminalWriter(w) || color.NoColor || logoColorSuppressed(os.Getenv) {
		return nil, false
	}
	img := decodeLogo()
	return img, img != nil
}

// writeTextLines prints each line on its own row at the given indent.
func writeTextLines(w io.Writer, lines []string, indent int) error {
	for _, line := range lines {
		if _, err := io.WriteString(w, strings.Repeat(" ", indent)+line+"\n"); err != nil {
			return err
		}
	}
	return nil
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
	// Source rows are spread across every target row, so the whole image is
	// sampled rather than just its top slice.
	targetPixelRows := lines * cellPixelRows

	var out strings.Builder
	previousFG, previousBG := "", ""

	for line := 0; line < lines; line++ {
		for cell := 0; cell < widthCells; cell++ {
			// Each cell covers a box of source pixels; averaging the whole box
			// keeps fine detail instead of point-sampling it away.
			left := bounds.Min.X + cell*sourceWidth/widthCells
			right := bounds.Min.X + (cell+1)*sourceWidth/widthCells
			top := bounds.Min.Y + line*cellPixelRows*sourceHeight/targetPixelRows
			bottom := bounds.Min.Y + (line*cellPixelRows+cellPixelRows)*sourceHeight/targetPixelRows

			foreground, foregroundVisible := averagePixel(img, left, top, right, bottom, true)
			background, backgroundVisible := averagePixel(img, left, top, right, bottom, false)

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

// averagePixel returns the SGR colour for a box of source pixels, or "" when
// the box is fully transparent. The box is split at the midpoint of its height
// so one call serves both halves of a cell: the top half feeds the foreground
// and the bottom half the background.
//
// The mean is weighted by alpha, so transparent pixels contribute no colour and
// a partly covered box keeps its hue instead of being darkened toward black.
func averagePixel(img image.Image, left, top, right, bottom int, foreground bool) (string, bool) {
	middle := top + (bottom-top)/2
	if foreground {
		bottom = middle
	} else {
		top = middle
	}

	var redSum, greenSum, blueSum, alphaSum uint64
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			red, green, blue, alpha := img.At(x, y).RGBA()
			alphaSum += uint64(alpha)
			// RGBA returns alpha-premultiplied channels, so summing these
			// values and dividing by total alpha yields the visible colour.
			redSum += uint64(red)
			greenSum += uint64(green)
			blueSum += uint64(blue)
		}
	}
	if alphaSum == 0 {
		return "", false
	}

	red := (redSum*255 + alphaSum/2) / alphaSum
	green := (greenSum*255 + alphaSum/2) / alphaSum
	blue := (blueSum*255 + alphaSum/2) / alphaSum

	layer := "48"
	if foreground {
		layer = "38"
	}
	return fmt.Sprintf("\x1b[%s;2;%d;%d;%dm", layer, red, green, blue), true
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
