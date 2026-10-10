package chat

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

type displayEntry struct {
	user     bool
	model    string
	markdown string
}

// ConversationView retains exactly the text needed to lay out the visible
// conversation again after the terminal changes size. It is only used by the
// interactive CLI; the network conversation has its own history.
type ConversationView struct {
	mu      sync.Mutex
	header  string
	help    string
	framed  bool
	entries []displayEntry
}

func NewConversationView(header, help string, framed bool) *ConversationView {
	return &ConversationView{header: header, help: help, framed: framed}
}

func (v *ConversationView) SetHeader(header string) {
	if v == nil {
		return
	}
	v.mu.Lock()
	v.header = header
	v.mu.Unlock()
}

func (v *ConversationView) SetFramed(framed bool) {
	if v == nil {
		return
	}
	v.mu.Lock()
	v.framed = framed
	v.mu.Unlock()
}

func (v *ConversationView) RecordUser(input string) {
	if v == nil || strings.TrimSpace(input) == "" {
		return
	}
	v.mu.Lock()
	v.entries = append(v.entries, displayEntry{user: true, markdown: input})
	v.mu.Unlock()
}

func (v *ConversationView) RecordAssistant(model, markdown string) {
	if v == nil || strings.TrimSpace(markdown) == "" {
		return
	}
	v.mu.Lock()
	v.entries = append(v.entries, displayEntry{model: model, markdown: markdown})
	v.mu.Unlock()
}

func (v *ConversationView) Clear() {
	if v == nil {
		return
	}
	v.mu.Lock()
	v.entries = nil
	v.mu.Unlock()
}

// Redraw clears the visible screen only. It deliberately keeps terminal
// scrollback, which may still contain the older layout.
func (v *ConversationView) Redraw(width int) {
	if v == nil || !term.IsTerminal(int(os.Stdout.Fd())) {
		return
	}
	v.mu.Lock()
	entries := append([]displayEntry(nil), v.entries...)
	header, help, framed := v.header, v.help, v.framed
	v.mu.Unlock()
	fmt.Print("\x1b[2J\x1b[H")
	if header != "" {
		fmt.Println(header)
	}
	if help != "" {
		fmt.Println(help)
	}
	for _, entry := range entries {
		if entry.user {
			printDisplayUser(entry.markdown, width)
			continue
		}
		renderMarkdownAtWidth(entry.markdown, entry.model, width, framed)
	}
}

func printDisplayUser(input string, width int) {
	if width < 8 {
		width = 8
	}
	for index, part := range strings.Split(input, "\n") {
		part = normalizeFrameLine(part)
		if index == 0 {
			part = "You: " + part
		}
		fmt.Println(ansi.Hardwrap(part, width, true))
	}
}
