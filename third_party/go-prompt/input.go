package prompt

import (
	"bytes"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	bracketedPasteStart    = "\x1b[200~"
	bracketedPasteEnd      = "\x1b[201~"
	incompleteInputTimeout = 100 * time.Millisecond
	pasteTokenStart        = rune(0xF0000)
	pasteTokenEnd          = rune(0xFFFFD)
)

type decodedInput struct {
	data   []byte
	pasted bool
}

type bracketedPasteDecoder struct {
	pending   []byte
	pendingAt time.Time
	paste     []byte
	inPaste   bool
}

func (d *bracketedPasteDecoder) Feed(data []byte) []decodedInput {
	if len(data) > 0 {
		d.pendingAt = time.Now()
	}
	d.pending = append(d.pending, data...)
	var events []decodedInput

	for len(d.pending) > 0 {
		if d.inPaste {
			if end := bytes.Index(d.pending, []byte(bracketedPasteEnd)); end >= 0 {
				d.paste = append(d.paste, d.pending[:end]...)
				events = append(events, decodedInput{data: []byte(sanitizePastedText(d.paste)), pasted: true})
				d.paste = nil
				d.pending = append([]byte(nil), d.pending[end+len(bracketedPasteEnd):]...)
				d.inPaste = false
				continue
			}

			keep := longestSuffixPrefix(d.pending, []byte(bracketedPasteEnd))
			d.paste = append(d.paste, d.pending[:len(d.pending)-keep]...)
			d.pending = append([]byte(nil), d.pending[len(d.pending)-keep:]...)
			return events
		}

		if start := bytes.Index(d.pending, []byte(bracketedPasteStart)); start >= 0 {
			events = append(events, splitNormalInput(d.pending[:start])...)
			d.pending = append([]byte(nil), d.pending[start+len(bracketedPasteStart):]...)
			d.inPaste = true
			continue
		}

		keep := longestIncompleteInputSuffix(d.pending)
		ready := len(d.pending) - keep
		events = append(events, splitNormalInput(d.pending[:ready])...)
		d.pending = append([]byte(nil), d.pending[ready:]...)
		return events
	}

	return events
}

func (d *bracketedPasteDecoder) FlushPending(now time.Time) []decodedInput {
	if d.inPaste || len(d.pending) == 0 || now.Sub(d.pendingAt) < incompleteInputTimeout {
		return nil
	}

	pending := d.pending
	d.pending = nil
	d.pendingAt = time.Time{}
	return splitNormalInput(pending)
}

func longestSuffixPrefix(data, sequence []byte) int {
	limit := len(data)
	if max := len(sequence) - 1; limit > max {
		limit = max
	}
	for size := limit; size > 0; size-- {
		if bytes.Equal(data[len(data)-size:], sequence[:size]) {
			return size
		}
	}
	return 0
}

func longestIncompleteInputSuffix(data []byte) int {
	limit := len(data)
	if max := len(bracketedPasteStart) - 1; limit > max {
		limit = max
	}
	for size := limit; size > 0; size-- {
		suffix := data[len(data)-size:]
		if suffix[0] != 0x1b {
			continue
		}
		if bytes.HasPrefix([]byte(bracketedPasteStart), suffix) {
			return size
		}
		for _, sequence := range ASCIISequences {
			if len(sequence.ASCIICode) > size && bytes.HasPrefix(sequence.ASCIICode, suffix) {
				return size
			}
		}
	}
	return 0
}

func splitNormalInput(data []byte) []decodedInput {
	var events []decodedInput
	textStart := 0
	for i := 0; i < len(data); {
		b := data[i]
		if b >= 0x20 && b != 0x7f && b != 0x1b {
			i++
			continue
		}
		if i > textStart {
			events = append(events, decodedInput{data: append([]byte(nil), data[textStart:i]...)})
		}

		if b == 0x1b {
			sequenceLength := matchedInputSequenceLength(data[i:])
			if sequenceLength == 0 {
				sequenceLength = 1
			}
			events = append(events, decodedInput{data: append([]byte(nil), data[i:i+sequenceLength]...)})
			i += sequenceLength
		} else {
			events = append(events, decodedInput{data: []byte{b}})
			i++
		}
		textStart = i
	}
	if textStart < len(data) {
		events = append(events, decodedInput{data: append([]byte(nil), data[textStart:]...)})
	}
	return events
}

func matchedInputSequenceLength(data []byte) int {
	matched := 0
	for _, sequence := range ASCIISequences {
		if len(sequence.ASCIICode) > matched && bytes.HasPrefix(data, sequence.ASCIICode) {
			matched = len(sequence.ASCIICode)
		}
	}
	return matched
}

func sanitizePastedText(data []byte) string {
	text := strings.ToValidUTF8(string(data), "\uFFFD")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, text)
}

func pastedTextReference(text string) string {
	count := utf8.RuneCountInString(text)
	return fmt.Sprintf("[pasted · %d chars]", count)
}

// WinSize represents the width and height of terminal.
type WinSize struct {
	Row uint16
	Col uint16
}

// ConsoleParser is an interface to abstract input layer.
type ConsoleParser interface {
	// Setup should be called before starting input
	Setup() error
	// TearDown should be called after stopping input
	TearDown() error
	// GetWinSize returns WinSize object to represent width and height of terminal.
	GetWinSize() *WinSize
	// Read returns byte array.
	Read() ([]byte, error)
}

// GetKey returns Key correspond to input byte codes.
func GetKey(b []byte) Key {
	for _, k := range ASCIISequences {
		if bytes.Equal(k.ASCIICode, b) {
			return k.Key
		}
	}
	return NotDefined
}

// ASCIISequences holds mappings of the key and byte array.
var ASCIISequences = []*ASCIICode{
	{Key: Escape, ASCIICode: []byte{0x1b}},

	{Key: ControlSpace, ASCIICode: []byte{0x00}},
	{Key: ControlA, ASCIICode: []byte{0x1}},
	{Key: ControlB, ASCIICode: []byte{0x2}},
	{Key: ControlC, ASCIICode: []byte{0x3}},
	{Key: ControlD, ASCIICode: []byte{0x4}},
	{Key: ControlE, ASCIICode: []byte{0x5}},
	{Key: ControlF, ASCIICode: []byte{0x6}},
	{Key: ControlG, ASCIICode: []byte{0x7}},
	{Key: ControlH, ASCIICode: []byte{0x8}},
	//{Key: ControlI, ASCIICode: []byte{0x9}},
	//{Key: ControlJ, ASCIICode: []byte{0xa}},
	{Key: ControlK, ASCIICode: []byte{0xb}},
	{Key: ControlL, ASCIICode: []byte{0xc}},
	{Key: ControlM, ASCIICode: []byte{0xd}},
	{Key: ControlN, ASCIICode: []byte{0xe}},
	{Key: ControlO, ASCIICode: []byte{0xf}},
	{Key: ControlP, ASCIICode: []byte{0x10}},
	{Key: ControlQ, ASCIICode: []byte{0x11}},
	{Key: ControlR, ASCIICode: []byte{0x12}},
	{Key: ControlS, ASCIICode: []byte{0x13}},
	{Key: ControlT, ASCIICode: []byte{0x14}},
	{Key: ControlU, ASCIICode: []byte{0x15}},
	{Key: ControlV, ASCIICode: []byte{0x16}},
	{Key: ControlW, ASCIICode: []byte{0x17}},
	{Key: ControlX, ASCIICode: []byte{0x18}},
	{Key: ControlY, ASCIICode: []byte{0x19}},
	{Key: ControlZ, ASCIICode: []byte{0x1a}},

	{Key: ControlBackslash, ASCIICode: []byte{0x1c}},
	{Key: ControlSquareClose, ASCIICode: []byte{0x1d}},
	{Key: ControlCircumflex, ASCIICode: []byte{0x1e}},
	{Key: ControlUnderscore, ASCIICode: []byte{0x1f}},
	{Key: Backspace, ASCIICode: []byte{0x7f}},

	{Key: Up, ASCIICode: []byte{0x1b, 0x5b, 0x41}},
	{Key: Down, ASCIICode: []byte{0x1b, 0x5b, 0x42}},
	{Key: Right, ASCIICode: []byte{0x1b, 0x5b, 0x43}},
	{Key: Left, ASCIICode: []byte{0x1b, 0x5b, 0x44}},
	{Key: Home, ASCIICode: []byte{0x1b, 0x5b, 0x48}},
	{Key: Home, ASCIICode: []byte{0x1b, 0x30, 0x48}},
	{Key: End, ASCIICode: []byte{0x1b, 0x5b, 0x46}},
	{Key: End, ASCIICode: []byte{0x1b, 0x30, 0x46}},

	{Key: Enter, ASCIICode: []byte{0xa}},
	{Key: Delete, ASCIICode: []byte{0x1b, 0x5b, 0x33, 0x7e}},
	{Key: ShiftDelete, ASCIICode: []byte{0x1b, 0x5b, 0x33, 0x3b, 0x32, 0x7e}},
	{Key: ControlDelete, ASCIICode: []byte{0x1b, 0x5b, 0x33, 0x3b, 0x35, 0x7e}},
	{Key: Home, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x7e}},
	{Key: End, ASCIICode: []byte{0x1b, 0x5b, 0x34, 0x7e}},
	{Key: PageUp, ASCIICode: []byte{0x1b, 0x5b, 0x35, 0x7e}},
	{Key: PageDown, ASCIICode: []byte{0x1b, 0x5b, 0x36, 0x7e}},
	{Key: Home, ASCIICode: []byte{0x1b, 0x5b, 0x37, 0x7e}},
	{Key: End, ASCIICode: []byte{0x1b, 0x5b, 0x38, 0x7e}},
	{Key: Tab, ASCIICode: []byte{0x9}},
	{Key: BackTab, ASCIICode: []byte{0x1b, 0x5b, 0x5a}},
	{Key: Insert, ASCIICode: []byte{0x1b, 0x5b, 0x32, 0x7e}},

	{Key: F1, ASCIICode: []byte{0x1b, 0x4f, 0x50}},
	{Key: F2, ASCIICode: []byte{0x1b, 0x4f, 0x51}},
	{Key: F3, ASCIICode: []byte{0x1b, 0x4f, 0x52}},
	{Key: F4, ASCIICode: []byte{0x1b, 0x4f, 0x53}},

	{Key: F1, ASCIICode: []byte{0x1b, 0x4f, 0x50, 0x41}}, // Linux console
	{Key: F2, ASCIICode: []byte{0x1b, 0x5b, 0x5b, 0x42}}, // Linux console
	{Key: F3, ASCIICode: []byte{0x1b, 0x5b, 0x5b, 0x43}}, // Linux console
	{Key: F4, ASCIICode: []byte{0x1b, 0x5b, 0x5b, 0x44}}, // Linux console
	{Key: F5, ASCIICode: []byte{0x1b, 0x5b, 0x5b, 0x45}}, // Linux console

	{Key: F1, ASCIICode: []byte{0x1b, 0x5b, 0x11, 0x7e}}, // rxvt-unicode
	{Key: F2, ASCIICode: []byte{0x1b, 0x5b, 0x12, 0x7e}}, // rxvt-unicode
	{Key: F3, ASCIICode: []byte{0x1b, 0x5b, 0x13, 0x7e}}, // rxvt-unicode
	{Key: F4, ASCIICode: []byte{0x1b, 0x5b, 0x14, 0x7e}}, // rxvt-unicode

	{Key: F5, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x35, 0x7e}},
	{Key: F6, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x37, 0x7e}},
	{Key: F7, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x38, 0x7e}},
	{Key: F8, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x39, 0x7e}},
	{Key: F9, ASCIICode: []byte{0x1b, 0x5b, 0x32, 0x30, 0x7e}},
	{Key: F10, ASCIICode: []byte{0x1b, 0x5b, 0x32, 0x31, 0x7e}},
	{Key: F11, ASCIICode: []byte{0x1b, 0x5b, 0x32, 0x32, 0x7e}},
	{Key: F12, ASCIICode: []byte{0x1b, 0x5b, 0x32, 0x34, 0x7e, 0x8}},
	{Key: F13, ASCIICode: []byte{0x1b, 0x5b, 0x25, 0x7e}},
	{Key: F14, ASCIICode: []byte{0x1b, 0x5b, 0x26, 0x7e}},
	{Key: F15, ASCIICode: []byte{0x1b, 0x5b, 0x28, 0x7e}},
	{Key: F16, ASCIICode: []byte{0x1b, 0x5b, 0x29, 0x7e}},
	{Key: F17, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x7e}},
	{Key: F18, ASCIICode: []byte{0x1b, 0x5b, 0x32, 0x7e}},
	{Key: F19, ASCIICode: []byte{0x1b, 0x5b, 0x33, 0x7e}},
	{Key: F20, ASCIICode: []byte{0x1b, 0x5b, 0x34, 0x7e}},

	// Xterm
	{Key: F13, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x3b, 0x32, 0x50}},
	{Key: F14, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x3b, 0x32, 0x51}},
	// &ASCIICode{Key: F15, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x3b, 0x32, 0x52}},  // Conflicts with CPR response
	{Key: F16, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x3b, 0x32, 0x52}},
	{Key: F17, ASCIICode: []byte{0x1b, 0x5b, 0x15, 0x3b, 0x32, 0x7e}},
	{Key: F18, ASCIICode: []byte{0x1b, 0x5b, 0x17, 0x3b, 0x32, 0x7e}},
	{Key: F19, ASCIICode: []byte{0x1b, 0x5b, 0x18, 0x3b, 0x32, 0x7e}},
	{Key: F20, ASCIICode: []byte{0x1b, 0x5b, 0x19, 0x3b, 0x32, 0x7e}},
	{Key: F21, ASCIICode: []byte{0x1b, 0x5b, 0x20, 0x3b, 0x32, 0x7e}},
	{Key: F22, ASCIICode: []byte{0x1b, 0x5b, 0x21, 0x3b, 0x32, 0x7e}},
	{Key: F23, ASCIICode: []byte{0x1b, 0x5b, 0x23, 0x3b, 0x32, 0x7e}},
	{Key: F24, ASCIICode: []byte{0x1b, 0x5b, 0x24, 0x3b, 0x32, 0x7e}},

	{Key: ControlUp, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x3b, 0x35, 0x41}},
	{Key: ControlDown, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x3b, 0x35, 0x42}},
	{Key: ControlRight, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x3b, 0x35, 0x43}},
	{Key: ControlLeft, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x3b, 0x35, 0x44}},

	{Key: ShiftUp, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x3b, 0x32, 0x41}},
	{Key: ShiftDown, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x3b, 0x32, 0x42}},
	{Key: ShiftRight, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x3b, 0x32, 0x43}},
	{Key: ShiftLeft, ASCIICode: []byte{0x1b, 0x5b, 0x31, 0x3b, 0x32, 0x44}},

	// Tmux sends following keystrokes when control+arrow is pressed, but for
	// Emacs ansi-term sends the same sequences for normal arrow keys. Consider
	// it a normal arrow press, because that's more important.
	{Key: Up, ASCIICode: []byte{0x1b, 0x4f, 0x41}},
	{Key: Down, ASCIICode: []byte{0x1b, 0x4f, 0x42}},
	{Key: Right, ASCIICode: []byte{0x1b, 0x4f, 0x43}},
	{Key: Left, ASCIICode: []byte{0x1b, 0x4f, 0x44}},

	{Key: ControlUp, ASCIICode: []byte{0x1b, 0x5b, 0x35, 0x41}},
	{Key: ControlDown, ASCIICode: []byte{0x1b, 0x5b, 0x35, 0x42}},
	{Key: ControlRight, ASCIICode: []byte{0x1b, 0x5b, 0x35, 0x43}},
	{Key: ControlLeft, ASCIICode: []byte{0x1b, 0x5b, 0x35, 0x44}},

	{Key: ControlRight, ASCIICode: []byte{0x1b, 0x5b, 0x4f, 0x63}}, // rxvt
	{Key: ControlLeft, ASCIICode: []byte{0x1b, 0x5b, 0x4f, 0x64}},  // rxvt

	{Key: Ignore, ASCIICode: []byte{0x1b, 0x5b, 0x45}}, // Xterm
	{Key: Ignore, ASCIICode: []byte{0x1b, 0x5b, 0x46}}, // Linux console
}
