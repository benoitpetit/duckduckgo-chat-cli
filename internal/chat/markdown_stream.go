package chat

import (
	"strings"
)

type markdownPiece struct {
	text string
}

// markdownBlockStream keeps Markdown constructs intact while allowing plain
// prose to be rendered paragraph by paragraph as it arrives.
type markdownBlockStream struct {
	line      strings.Builder
	block     strings.Builder
	fence     rune
	fenceSize int
}

// Push adds stream text and returns complete Markdown paragraphs or blocks.
func (s *markdownBlockStream) Push(chunk string) []markdownPiece {
	var ready []markdownPiece
	for _, r := range chunk {
		if r != '\n' {
			s.line.WriteRune(r)
			continue
		}

		line := s.line.String()
		s.line.Reset()
		fullLine := line + "\n"

		if s.fence != 0 {
			s.block.WriteString(fullLine)
			if isClosingMarkdownFence(line, s.fence, s.fenceSize) {
				s.fence = 0
				s.fenceSize = 0
				ready = append(ready, markdownPiece{text: s.takeBlock()})
			}
			continue
		}

		if marker, size, ok := markdownFence(line); ok {
			if s.hasContent() {
				ready = append(ready, markdownPiece{text: s.takeBlock()})
			}
			s.block.WriteString(fullLine)
			s.fence = marker
			s.fenceSize = size
			continue
		}

		if strings.TrimSpace(line) == "" {
			if s.hasContent() {
				ready = append(ready, markdownPiece{text: s.takeBlock()})
			} else {
				s.block.Reset()
			}
			continue
		}

		s.block.WriteString(fullLine)
	}
	return ready
}

// Flush returns the final incomplete Markdown block after the stream closes.
func (s *markdownBlockStream) Flush() []markdownPiece {
	if s.line.Len() > 0 {
		s.block.WriteString(s.line.String())
		s.line.Reset()
	}
	if !s.hasContent() {
		s.block.Reset()
		return nil
	}
	return []markdownPiece{{text: s.takeBlock()}}
}

func (s *markdownBlockStream) hasContent() bool {
	return strings.TrimSpace(s.block.String()) != ""
}

func (s *markdownBlockStream) takeBlock() string {
	block := s.block.String()
	s.block.Reset()
	return block
}

func markdownFence(line string) (rune, int, bool) {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if indent > 3 {
		return 0, 0, false
	}
	trimmed := line[indent:]
	if len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, false
	}
	marker := rune(trimmed[0])
	size := 0
	for size < len(trimmed) && rune(trimmed[size]) == marker {
		size++
	}
	if size < 3 {
		return 0, 0, false
	}
	return marker, size, true
}

func isClosingMarkdownFence(line string, marker rune, minimumSize int) bool {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if indent > 3 {
		return false
	}
	trimmed := line[indent:]
	size := 0
	for size < len(trimmed) && rune(trimmed[size]) == marker {
		size++
	}
	return size >= minimumSize && strings.TrimSpace(trimmed[size:]) == ""
}
