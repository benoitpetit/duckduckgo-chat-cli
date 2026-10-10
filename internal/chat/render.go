package chat

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"unicode/utf8"

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
	renderer      *glamour.TermRenderer
	firstRenderer *glamour.TermRenderer
	frame         *responseFrame
	modelName     string
}

// NewStreamRenderer creates a renderer using the active CLI theme and terminal
// width. The width is taken from stdout so wrapping matches the visible output.
func NewStreamRenderer(modelName string, frameResponses bool) (*StreamRenderer, error) {
	return newStreamRendererAtWidth(modelName, frameResponses, getTerminalWidthSafe())
}

func newStreamRendererAtWidth(modelName string, frameResponses bool, width int) (*StreamRenderer, error) {
	theme := ui.CurrentTheme()
	customStyles := styles.DarkStyleConfig
	customStyles.Document.StylePrimitive.Color = stringToPtr(theme.Colors.Foreground)
	customStyles.Heading.StylePrimitive.Color = stringToPtr(theme.Colors.Accent)
	customStyles.H1.StylePrimitive.Color = stringToPtr(theme.Colors.Accent)
	customStyles.H2.StylePrimitive.Color = stringToPtr(theme.Colors.Accent)
	customStyles.H3.StylePrimitive.Color = stringToPtr(theme.Colors.Accent)
	customStyles.H4.StylePrimitive.Color = stringToPtr(theme.Colors.Accent)
	customStyles.H5.StylePrimitive.Color = stringToPtr(theme.Colors.Accent)
	customStyles.H6.StylePrimitive.Color = stringToPtr(theme.Colors.Accent)
	customStyles.H1.StylePrimitive.Bold = boolToPtr(true)
	customStyles.H2.StylePrimitive.Bold = boolToPtr(true)
	customStyles.H3.StylePrimitive.Bold = boolToPtr(true)
	customStyles.H4.StylePrimitive.Bold = boolToPtr(true)
	customStyles.H5.StylePrimitive.Bold = boolToPtr(true)
	customStyles.H6.StylePrimitive.Bold = boolToPtr(true)
	customStyles.H1.Prefix = ""
	customStyles.H2.Prefix = ""
	customStyles.H3.Prefix = ""
	customStyles.H4.Prefix = ""
	customStyles.H5.Prefix = ""
	customStyles.H6.Prefix = ""
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

	var frame *responseFrame
	if frameResponses && term.IsTerminal(int(os.Stdout.Fd())) {
		frame = newResponseFrame(modelName, width)
	}
	wrapWidth := width - 4
	if wrapWidth < 8 {
		wrapWidth = 8
	}
	if frame != nil {
		wrapWidth = frame.contentWidth
	}
	colorProfile := termenv.EnvColorProfile()
	if !term.IsTerminal(int(os.Stdout.Fd())) || termenv.EnvNoColor() {
		colorProfile = termenv.Ascii
	}
	newRenderer := func(width int) (*glamour.TermRenderer, error) {
		return glamour.NewTermRenderer(
			glamour.WithStyles(customStyles),
			glamour.WithWordWrap(width),
			glamour.WithColorProfile(colorProfile),
		)
	}
	renderer, err := newRenderer(wrapWidth)
	if err != nil {
		return nil, err
	}
	var firstRenderer *glamour.TermRenderer
	if frame == nil {
		firstLineWidth := wrapWidth - utf8.RuneCountInString(modelName) - len(": ")
		if firstLineWidth < 8 {
			firstLineWidth = 8
		}
		firstRenderer, err = newRenderer(firstLineWidth)
		if err != nil {
			return nil, err
		}
	}

	return &StreamRenderer{
		renderer:      renderer,
		firstRenderer: firstRenderer,
		frame:         frame,
		modelName:     modelName,
	}, nil
}

func renderMarkdownAtWidth(markdown, modelName string, width int, framed bool) {
	renderer, err := newStreamRendererAtWidth(modelName, framed, width)
	if err != nil {
		fmt.Printf("%s: %s\n", modelName, markdown)
		return
	}
	chunks := make(chan string, 1)
	chunks <- markdown
	close(chunks)
	renderer.ProcessStream(chunks)
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
func RenderStream(stream <-chan string, modelName string, spinner *ui.Spinner, frameResponses bool) string {
	return RenderStreamWithView(stream, modelName, spinner, frameResponses, nil)
}

// RenderStreamWithView also repaints the in-progress answer on SIGWINCH when
// the interactive prompt has temporarily handed control to its executor.
func RenderStreamWithView(stream <-chan string, modelName string, spinner *ui.Spinner, frameResponses bool, view *ConversationView) string {
	renderer, err := NewStreamRenderer(modelName, frameResponses)
	if err != nil {
		return renderStreamFallback(stream, modelName, spinner)
	}
	if view != nil && term.IsTerminal(int(os.Stdout.Fd())) {
		return renderer.processResponsiveStream(stream, spinner, frameResponses, view)
	}
	return renderer.processStream(stream, spinner)
}

func (sr *StreamRenderer) processResponsiveStream(stream <-chan string, spinner *ui.Spinner, framed bool, view *ConversationView) string {
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	defer signal.Stop(resize)

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
		if sr.frame == nil && sr.modelName != "" {
			ui.AccentColor.Printf("%s: ", sr.modelName)
		}
		contentStarted = true
	}
	for stream != nil {
		select {
		case chunk, ok := <-stream:
			if !ok {
				stream = nil
				break
			}
			content.WriteString(chunk)
			for _, piece := range blocks.Push(chunk) {
				startContent()
				sr.renderPiece(piece, renderedAny)
				renderedAny = true
			}
		case <-resize:
			if spinner != nil {
				spinner.ClearForRedraw()
			}
			width := getTerminalWidthSafe()
			view.Redraw(width)
			newRenderer, err := newStreamRendererAtWidth(sr.modelName, framed, width)
			if err != nil {
				continue
			}
			*sr = *newRenderer
			blocks = markdownBlockStream{}
			renderedAny = false
			contentStarted = false
			for _, piece := range blocks.Push(content.String()) {
				startContent()
				sr.renderPiece(piece, renderedAny)
				renderedAny = true
			}
			if !renderedAny && spinner != nil {
				spinner.ResumeAfterRedraw()
			}
		}
	}
	for _, piece := range blocks.Flush() {
		startContent()
		sr.renderPiece(piece, renderedAny)
		renderedAny = true
	}
	if spinner != nil {
		spinner.Stop()
	}
	if sr.frame != nil {
		sr.frame.Close()
	}
	return content.String()
}

// ProcessStream progressively renders completed Markdown blocks.
func (sr *StreamRenderer) ProcessStream(stream <-chan string) string {
	return sr.processStream(stream, nil)
}

func (sr *StreamRenderer) processStream(stream <-chan string, spinner *ui.Spinner) string {
	content := processMarkdownStream(stream, sr.modelName, spinner, sr.frame != nil, func(piece markdownPiece, previous bool) {
		sr.renderPiece(piece, previous)
	})
	if sr.frame != nil {
		sr.frame.Close()
	}
	return content
}

func (sr *StreamRenderer) renderPiece(piece markdownPiece, previous bool) {
	renderer := sr.renderer
	if !previous && sr.firstRenderer != nil {
		renderer = sr.firstRenderer
	}
	rendered, err := renderer.Render(piece.text)
	if err != nil {
		rendered = piece.text
	}
	if sr.frame != nil {
		sr.frame.Write(rendered, previous)
		return
	}
	writeRenderedPiece(rendered, previous)
}

// renderStreamFallback keeps streaming usable if Glamour cannot initialize.
func renderStreamFallback(stream <-chan string, modelName string, spinner *ui.Spinner) string {
	return processMarkdownStream(stream, modelName, spinner, false, func(piece markdownPiece, previous bool) {
		writeRenderedPiece(piece.text, previous)
	})
}

func processMarkdownStream(stream <-chan string, modelName string, spinner *ui.Spinner, framed bool, renderPiece func(markdownPiece, bool)) string {
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
		if modelName != "" && !framed {
			ui.AccentColor.Printf("%s: ", modelName)
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
	return content.String()
}

func writeRenderedPiece(rendered string, previous bool) {
	rendered = strings.Trim(rendered, "\n\r")
	if rendered == "" {
		return
	}
	if previous {
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
