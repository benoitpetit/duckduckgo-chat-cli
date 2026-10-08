package chat

import (
	"strings"
	"unicode"
)

type markdownPiece struct {
	text       string
	continuing bool
}

// markdownBlockStream keeps Markdown constructs intact while allowing plain
// prose to be rendered sentence by sentence as it arrives.
type markdownBlockStream struct {
	line              strings.Builder
	block             strings.Builder
	fence             rune
	fenceSize         int
	paragraphStreamed bool
}

// Push adds stream text and returns complete prose sentences or Markdown blocks.
func (s *markdownBlockStream) Push(chunk string) []markdownPiece {
	var ready []markdownPiece
	for _, r := range chunk {
		if r != '\n' {
			s.line.WriteRune(r)
			if s.fence == 0 && s.block.Len() == 0 {
				if sentence, rest, ok := s.takePlainSentence(); ok {
					ready = append(ready, markdownPiece{text: sentence, continuing: s.paragraphStreamed})
					s.paragraphStreamed = true
					s.line.Reset()
					s.line.WriteString(rest)
				}
			}
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
				s.paragraphStreamed = false
			}
			continue
		}

		if marker, size, ok := markdownFence(line); ok {
			if s.hasContent() {
				ready = append(ready, markdownPiece{text: s.takeBlock(), continuing: s.paragraphStreamed})
			}
			s.paragraphStreamed = false
			s.block.WriteString(fullLine)
			s.fence = marker
			s.fenceSize = size
			continue
		}
		if s.paragraphStreamed && isMarkdownBlockLine(line) {
			s.paragraphStreamed = false
		}

		if strings.TrimSpace(line) == "" {
			if s.hasContent() {
				ready = append(ready, markdownPiece{text: s.takeBlock(), continuing: s.paragraphStreamed})
			} else {
				s.block.Reset()
			}
			s.paragraphStreamed = false
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
	return []markdownPiece{{text: s.takeBlock(), continuing: s.paragraphStreamed}}
}

func (s *markdownBlockStream) takePlainSentence() (sentence, rest string, ok bool) {
	runes := []rune(s.line.String())
	for i := 1; i < len(runes); i++ {
		if !unicode.IsSpace(runes[i]) {
			continue
		}
		completeSentence := strings.ContainsRune(".!?", runes[i-1])
		if !completeSentence && i < 72 {
			continue
		}
		candidate := strings.TrimSpace(string(runes[:i]))
		if !isPlainMarkdownText(candidate) {
			return "", "", false
		}
		return candidate, string(runes[i:]), true
	}
	return "", "", false
}

func isPlainMarkdownText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "`*_[]!~|<>\\") {
		return false
	}
	return !isMarkdownBlockLine(text)
}

func isMarkdownBlockLine(line string) bool {
	line = strings.TrimLeft(line, " ")
	if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ">") || strings.HasPrefix(line, "|") {
		return true
	}
	if len(line) >= 2 && strings.ContainsRune("-*+", rune(line[0])) && unicode.IsSpace(rune(line[1])) {
		return true
	}
	index := 0
	for index < len(line) && line[index] >= '0' && line[index] <= '9' {
		index++
	}
	return index > 0 && index+1 < len(line) && (line[index] == '.' || line[index] == ')') && unicode.IsSpace(rune(line[index+1]))
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
