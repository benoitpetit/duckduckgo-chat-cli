package chat

import (
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"duckduckgo-chat-cli/internal/ui"

	"github.com/charmbracelet/x/ansi"
)

const (
	responseFrameMinRailWidth = 32
	responseFrameMinBoxWidth  = 52
	responseFrameMaxWidth     = 112
)

type responseFrameMode uint8

const (
	responseFrameBox responseFrameMode = iota
	responseFrameRail
)

// responseFrame writes one persistent container around a progressively
// rendered answer. It measures terminal cells so ANSI colors and emoji do not
// throw off the right edge.
type responseFrame struct {
	modelName    string
	mode         responseFrameMode
	width        int
	contentWidth int
	indent       string
	ascii        bool
	started      bool
}

func newResponseFrame(modelName string, terminalWidth int) *responseFrame {
	if terminalWidth < responseFrameMinRailWidth {
		return nil
	}
	modelName = strings.Join(strings.Fields(ansi.Strip(normalizeFrameLine(modelName))), " ")
	if modelName == "" {
		modelName = "Assistant"
	}

	frame := &responseFrame{
		modelName: modelName,
		ascii:     os.Getenv("TERM") == "dumb",
	}
	if terminalWidth < responseFrameMinBoxWidth {
		frame.mode = responseFrameRail
		frame.indent = " "
		frame.contentWidth = terminalWidth - 5
		if ansi.StringWidth(frame.modelName) > frame.contentWidth {
			ellipsis := "…"
			if frame.ascii {
				ellipsis = "..."
			}
			frame.modelName = ansi.Truncate(frame.modelName, frame.contentWidth, ellipsis)
		}
		return frame
	}

	frame.mode = responseFrameBox
	frame.width = min(terminalWidth-2, responseFrameMaxWidth)
	frame.contentWidth = frame.width - 4
	frame.indent = " "
	return frame
}

func (f *responseFrame) Write(rendered string, previous bool) {
	if f == nil {
		return
	}
	rendered = strings.Trim(rendered, "\r\n")
	if rendered == "" {
		return
	}
	if !f.started {
		f.open()
	}
	if previous {
		f.writeLine("")
	}
	for _, line := range strings.Split(rendered, "\n") {
		line = normalizeFrameLine(line)
		if ansi.StringWidth(line) > f.contentWidth {
			line = ansi.Hardwrap(line, f.contentWidth, true)
		}
		for _, wrappedLine := range strings.Split(line, "\n") {
			f.writeLine(wrappedLine)
		}
	}
}

// normalizeFrameLine makes the terminal's cursor position match StringWidth.
// Tabs depend on the absolute cursor column and control characters can move
// it backwards, so neither may be emitted inside a measured frame line.
func normalizeFrameLine(line string) string {
	line = safeFrameText(line)
	if !strings.ContainsRune(line, '\t') {
		return line
	}
	var expanded strings.Builder
	column := 0
	parts := strings.Split(line, "\t")
	for i, part := range parts {
		expanded.WriteString(part)
		column += ansi.StringWidth(part)
		if i < len(parts)-1 {
			spaces := 4 - column%4
			expanded.WriteString(strings.Repeat(" ", spaces))
			column += spaces
		}
	}
	return expanded.String()
}

// Keep Glamour's colors and hyperlinks while discarding terminal commands
// that could move the cursor, erase a border, or swallow the rest of a line.
func safeFrameText(line string) string {
	var safe strings.Builder
	for i := 0; i < len(line); {
		if line[i] == '\x1b' {
			if i+1 >= len(line) {
				break
			}
			switch line[i+1] {
			case '[':
				end := i + 2
				for end < len(line) && (line[end] < '@' || line[end] > '~') {
					end++
				}
				if end < len(line) && line[end] == 'm' && safeSGRParams(line[i+2:end]) {
					safe.WriteString(line[i : end+1])
				}
				i = end + 1
			case ']':
				end := i + 2
				for end < len(line) && line[end] != '\x07' && !(line[end] == '\x1b' && end+1 < len(line) && line[end+1] == '\\') {
					end++
				}
				if end == len(line) {
					return safe.String()
				}
				if line[end] == '\x07' {
					end++
				} else {
					end += 2
				}
				if strings.HasPrefix(line[i+2:end], "8;") {
					safe.WriteString(line[i:end])
				}
				i = end
			default:
				i += 2
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if r == '\t' || !unicode.IsControl(r) {
			safe.WriteString(line[i : i+size])
		}
		i += size
	}
	return safe.String()
}

func safeSGRParams(params string) bool {
	for _, char := range params {
		if (char < '0' || char > '9') && char != ';' && char != ':' {
			return false
		}
	}
	return true
}

func (f *responseFrame) Close() {
	if f == nil || !f.started {
		return
	}
	if f.mode == responseFrameRail {
		corner := "╰"
		if f.ascii {
			corner = "+"
		}
		ui.MutedColor.Printf("%s%s\n", f.indent, corner)
		return
	}
	left, horizontal, right := "╰", "─", "╯"
	if f.ascii {
		left, horizontal, right = "+", "-", "+"
	}
	ui.MutedColor.Printf("%s%s%s%s\n", f.indent, left, strings.Repeat(horizontal, f.width-2), right)
}

func (f *responseFrame) open() {
	f.started = true
	if f.mode == responseFrameRail {
		left := "│"
		if f.ascii {
			left = "|"
		}
		ui.MutedColor.Printf("%s%s ", f.indent, left)
		ui.AccentColor.Printf("%s\n", f.modelName)
		return
	}
	left, horizontal, right := "╭", "─", "╮"
	if f.ascii {
		left, horizontal, right = "+", "-", "+"
	}
	label := f.modelName
	ellipsis := "…"
	if f.ascii {
		ellipsis = "..."
	}
	maxLabelWidth := f.width - 6
	if ansi.StringWidth(label) > maxLabelWidth {
		label = ansi.Truncate(label, maxLabelWidth, ellipsis)
	}
	lead := left + horizontal + " " + label + " "
	fill := f.width - ansi.StringWidth(lead+right)
	if fill < 0 {
		fill = 0
	}
	ui.MutedColor.Printf("%s%s", f.indent, left+horizontal+" ")
	ui.AccentColor.Printf("%s", label)
	ui.MutedColor.Printf(" %s%s\n", strings.Repeat(horizontal, fill), right)
}

func (f *responseFrame) writeLine(line string) {
	if f.mode == responseFrameRail {
		left := "│"
		if f.ascii {
			left = "|"
		}
		ui.MutedColor.Printf("%s%s ", f.indent, left)
		fmt.Println(line)
		return
	}
	left, right := "│", "│"
	if f.ascii {
		left, right = "|", "|"
	}
	padding := f.contentWidth - ansi.StringWidth(line)
	if padding < 0 {
		padding = 0
	}
	ui.MutedColor.Printf("%s%s ", f.indent, left)
	fmt.Print(line)
	fmt.Print(strings.Repeat(" ", padding))
	ui.MutedColor.Printf(" %s\n", right)
}
