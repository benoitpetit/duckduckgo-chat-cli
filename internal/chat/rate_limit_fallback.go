package chat

import (
	"context"
	"strings"

	"duckduckgo-chat-cli/internal/ui"

	"github.com/charmbracelet/x/ansi"
)

// reportChatFailure leaves the user's browser untouched. Recovery has already
// been attempted in the isolated headless session before this is called.
func (c *Chat) reportChatFailure(requestCtx context.Context, err error) {
	if requestCtx != nil && requestCtx.Err() != nil {
		return
	}
	writeWrappedChatError(err)
}

func writeWrappedChatError(err error) {
	const prefix = "Error: "
	width := getTerminalWidthSafe()
	messageWidth := width - ansi.StringWidth(prefix)
	if messageWidth < 1 {
		messageWidth = 1
	}

	lines := strings.Split(ansi.Hardwrap(err.Error(), messageWidth, true), "\n")
	ui.ErrorColor.Printf("%s%s\n", prefix, lines[0])
	indent := strings.Repeat(" ", ansi.StringWidth(prefix))
	for _, line := range lines[1:] {
		ui.ErrorColor.Printf("%s%s\n", indent, line)
	}
}
