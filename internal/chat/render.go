package chat

import (
	"fmt"
	"os"
	"strings"

	"duckduckgo-chat-cli/internal/ui"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// StreamRenderer renders complete Markdown blocks as they arrive, so content
// already written to the terminal never needs to be moved or repainted.
type StreamRenderer struct {
	renderer  *glamour.TermRenderer
	modelName string
}

// NewStreamRenderer creates a renderer using the active CLI theme and terminal
// width. The width is taken from stdout so wrapping matches the visible output.
func NewStreamRenderer(modelName string) (*StreamRenderer, error) {
	width := getTerminalWidthSafe()
	theme := ui.CurrentTheme()
	customStyles := styles.DarkStyleConfig
	customStyles.Document.StylePrimitive.Color = stringToPtr(theme.Colors.Foreground)
	customStyles.Heading.StylePrimitive.Color = stringToPtr(theme.Colors.Accent)
	customStyles.H1.StylePrimitive.Color = stringToPtr(theme.Colors.Accent)
	customStyles.H2.StylePrimitive.Color = stringToPtr(theme.Colors.Info)
	customStyles.H3.StylePrimitive.Color = stringToPtr(theme.Colors.Success)
	customStyles.H4.StylePrimitive.Color = stringToPtr(theme.Colors.Warning)
	customStyles.H5.StylePrimitive.Color = stringToPtr(theme.Colors.Accent)
	customStyles.H6.StylePrimitive.Color = stringToPtr(theme.Colors.Info)
	customStyles.H1.StylePrimitive.Bold = boolToPtr(true)
	customStyles.H2.StylePrimitive.Bold = boolToPtr(true)
	customStyles.H3.StylePrimitive.Bold = boolToPtr(true)
	customStyles.H4.StylePrimitive.Bold = boolToPtr(true)
	customStyles.H1.Prefix = ""
	customStyles.H2.Prefix = ""
	customStyles.H3.Prefix = ""
	customStyles.H4.Prefix = ""
	customStyles.H5.Prefix = ""
	customStyles.H1.Suffix = ""
	customStyles.H1.BackgroundColor = nil
	customStyles.HorizontalRule.Color = stringToPtr(theme.Colors.Muted)
	customStyles.Link.Color = stringToPtr(theme.Colors.Accent)
	customStyles.LinkText.Color = stringToPtr(theme.Colors.Accent)
	customStyles.Image.Color = stringToPtr(theme.Colors.Info)
	customStyles.ImageText.Color = stringToPtr(theme.Colors.Muted)
	customStyles.Code.Color = stringToPtr(theme.Colors.Warning)
	customStyles.Code.BackgroundColor = nil
	customStyles.CodeBlock.Color = stringToPtr(theme.Colors.Foreground)
	customStyles.CodeBlock.Chroma = themeChroma(theme.Colors)

	wrapWidth := width - 4
	if wrapWidth < 8 {
		wrapWidth = 8
	}
	colorProfile := termenv.EnvColorProfile()
	if !term.IsTerminal(int(os.Stdout.Fd())) || termenv.EnvNoColor() {
		colorProfile = termenv.Ascii
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(customStyles),
		glamour.WithWordWrap(wrapWidth),
		glamour.WithColorProfile(colorProfile),
	)
	if err != nil {
		return nil, err
	}

	return &StreamRenderer{
		renderer:  renderer,
		modelName: modelName,
	}, nil
}

func themeChroma(palette ui.Palette) *ansi.Chroma {
	chroma := *styles.DarkStyleConfig.CodeBlock.Chroma
	set := func(style *ansi.StylePrimitive, value string) {
		style.Color = stringToPtr(value)
	}
	set(&chroma.Text, palette.Foreground)
	set(&chroma.Error, palette.Error)
	chroma.Error.BackgroundColor = nil
	set(&chroma.Comment, palette.Muted)
	set(&chroma.CommentPreproc, palette.Warning)
	set(&chroma.Keyword, palette.Accent)
	set(&chroma.KeywordReserved, palette.Accent)
	set(&chroma.KeywordNamespace, palette.Info)
	set(&chroma.KeywordType, palette.Success)
	set(&chroma.Operator, palette.Warning)
	set(&chroma.Punctuation, palette.Info)
	set(&chroma.Name, palette.Foreground)
	set(&chroma.NameBuiltin, palette.Info)
	set(&chroma.NameTag, palette.Accent)
	set(&chroma.NameAttribute, palette.Success)
	set(&chroma.NameClass, palette.Accent)
	set(&chroma.NameDecorator, palette.Warning)
	set(&chroma.NameFunction, palette.Success)
	set(&chroma.NameOther, palette.Foreground)
	set(&chroma.Literal, palette.Info)
	set(&chroma.LiteralNumber, palette.Success)
	set(&chroma.LiteralDate, palette.Info)
	set(&chroma.LiteralString, palette.Warning)
	set(&chroma.LiteralStringEscape, palette.Success)
	set(&chroma.GenericDeleted, palette.Error)
	set(&chroma.GenericInserted, palette.Success)
	set(&chroma.GenericSubheading, palette.Muted)
	set(&chroma.Background, palette.Foreground)
	chroma.Background.BackgroundColor = nil
	return &chroma
}

// RenderStream renders a streamed answer and returns the original Markdown.
func RenderStream(stream <-chan string, modelName string, spinner *ui.Spinner) string {
	renderer, err := NewStreamRenderer(modelName)
	if err != nil {
		return renderStreamFallback(stream, modelName, spinner)
	}
	return renderer.processStream(stream, spinner)
}

// ProcessStream progressively renders completed Markdown blocks.
func (sr *StreamRenderer) ProcessStream(stream <-chan string) string {
	return sr.processStream(stream, nil)
}

func (sr *StreamRenderer) processStream(stream <-chan string, spinner *ui.Spinner) string {
	return processMarkdownStream(stream, sr.modelName, spinner, func(piece markdownPiece, previous bool) {
		sr.renderPiece(piece, previous)
	})
}

func (sr *StreamRenderer) renderPiece(piece markdownPiece, previous bool) {
	rendered, err := sr.renderer.Render(piece.text)
	if err != nil {
		rendered = piece.text
	}
	writeRenderedPiece(rendered, previous, piece.continuing)
}

// renderStreamFallback keeps streaming usable if Glamour cannot initialize.
func renderStreamFallback(stream <-chan string, modelName string, spinner *ui.Spinner) string {
	return processMarkdownStream(stream, modelName, spinner, func(piece markdownPiece, previous bool) {
		writeRenderedPiece(piece.text, previous, piece.continuing)
	})
}

func processMarkdownStream(stream <-chan string, modelName string, spinner *ui.Spinner, renderPiece func(markdownPiece, bool)) string {
	var content strings.Builder
	var blocks markdownBlockStream
	contentStarted := false
	renderedAny := false
	startContent := func() {
		if contentStarted {
			return
		}
		if spinner != nil {
			spinner.Stop()
		}
		if modelName != "" {
			ui.AccentColor.Printf("%s:\n", modelName)
		}
		contentStarted = true
	}
	for chunk := range stream {
		content.WriteString(chunk)
		for _, piece := range blocks.Push(chunk) {
			startContent()
			renderPiece(piece, renderedAny)
			renderedAny = true
		}
	}

	for _, piece := range blocks.Flush() {
		startContent()
		renderPiece(piece, renderedAny)
		renderedAny = true
	}

	if spinner != nil {
		spinner.Stop()
	}
	if !renderedAny {
		fmt.Println("No response received.")
	}
	return content.String()
}

func writeRenderedPiece(rendered string, previous, continuing bool) {
	rendered = strings.Trim(rendered, "\n\r")
	if rendered == "" {
		return
	}
	if previous && !continuing {
		fmt.Println()
	}
	fmt.Print(rendered)
	fmt.Println()
}

// getTerminalWidthSafe uses stdout's terminal dimensions, with a stable width
// for pipes, redirected output, and terminals that do not report their size.
func getTerminalWidthSafe() int {
	width := 80
	if term.IsTerminal(int(os.Stdout.Fd())) {
		if terminalWidth, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && terminalWidth > 0 {
			width = terminalWidth
		}
	}
	if width < 8 {
		width = 8
	}
	if width > 200 {
		width = 200
	}
	return width
}

func stringToPtr(value string) *string { return &value }
func boolToPtr(value bool) *bool       { return &value }
