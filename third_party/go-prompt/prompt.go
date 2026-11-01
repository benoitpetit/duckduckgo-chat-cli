package prompt

import (
	"bytes"
	"os"
	"strings"
	"time"

	"github.com/c-bata/go-prompt/internal/debug"
)

// Executor is called when user input something text.
type Executor func(string)

// ExitChecker is called after user input to check if prompt must stop and exit go-prompt Run loop.
// User input means: selecting/typing an entry, then, if said entry content matches the ExitChecker function criteria:
// - immediate exit (if breakline is false) without executor called
// - exit after typing <return> (meaning breakline is true), and the executor is called first, before exit.
// Exit means exit go-prompt (not the overall Go program)
type ExitChecker func(in string, breakline bool) bool

// Completer should return the suggest item from Document.
type Completer func(Document) []Suggest

// Prompt is core struct of go-prompt.
type Prompt struct {
	in                ConsoleParser
	buf               *Buffer
	renderer          *Render
	executor          Executor
	history           *History
	completion        *CompletionManager
	keyBindings       []KeyBind
	ASCIICodeBindings []ASCIICodeBind
	keyBindMode       KeyBindMode
	completionOnDown  bool
	exitChecker       ExitChecker
	skipTearDown      bool
	resizeCallback    func(*WinSize)
	pasteDecoder      bracketedPasteDecoder
	compactPaste      bool
	pastePayloads     map[rune]string
	nextPasteToken    rune
}

// Exec is the struct contains user input context.
type Exec struct {
	input string
}

// Run starts prompt.
func (p *Prompt) Run() {
	p.skipTearDown = false
	defer debug.Teardown()
	debug.Log("start prompt")
	p.setUp()
	defer p.tearDown()

	if p.completion.showAtStart {
		p.completion.Update(*p.buf.Document())
	}

	p.renderer.Render(p.buf, p.completion)

	bufCh := make(chan []byte, 128)
	stopReadBufCh := make(chan struct{})
	go p.readBuffer(bufCh, stopReadBufCh)

	exitCh := make(chan int)
	winSizeCh := make(chan *WinSize)
	stopHandleSignalCh := make(chan struct{})
	go p.handleSignals(exitCh, winSizeCh, stopHandleSignalCh)

	for {
		select {
		case b := <-bufCh:
			if shouldExit, e := p.feed(b); shouldExit {
				p.renderer.BreakLine(p.buf)
				stopReadBufCh <- struct{}{}
				stopHandleSignalCh <- struct{}{}
				return
			} else if e != nil {
				// Stop goroutine to run readBuffer function
				stopReadBufCh <- struct{}{}
				stopHandleSignalCh <- struct{}{}

				// Unset raw mode
				// Reset to Blocking mode because returned EAGAIN when still set non-blocking mode.
				p.setBracketedPasteMode(false)
				debug.AssertNoError(p.in.TearDown())
				p.executor(e.input)

				p.completion.Update(*p.buf.Document())

				p.renderer.Render(p.buf, p.completion)

				if p.exitChecker != nil && p.exitChecker(e.input, true) {
					p.skipTearDown = true
					return
				}
				// Set raw mode
				debug.AssertNoError(p.in.Setup())
				p.setBracketedPasteMode(true)
				go p.readBuffer(bufCh, stopReadBufCh)
				go p.handleSignals(exitCh, winSizeCh, stopHandleSignalCh)
			} else {
				p.completion.Update(*p.buf.Document())
				p.renderer.Render(p.buf, p.completion)
			}
		case w := <-winSizeCh:
			if p.resizeCallback != nil {
				p.resizeCallback(w)
				p.renderer.previousCursor = 0
			}
			p.renderer.UpdateWinSize(w)
			p.renderer.Render(p.buf, p.completion)
		case code := <-exitCh:
			p.renderer.BreakLine(p.buf)
			p.tearDown()
			os.Exit(code)
		default:
			if handled, shouldExit, _ := p.flushPendingInput(time.Now()); handled {
				if shouldExit {
					p.renderer.BreakLine(p.buf)
					stopReadBufCh <- struct{}{}
					stopHandleSignalCh <- struct{}{}
					return
				}
				p.completion.Update(*p.buf.Document())
				p.renderer.Render(p.buf, p.completion)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func (p *Prompt) feed(b []byte) (shouldExit bool, exec *Exec) {
	return p.feedDecoded(p.pasteDecoder.Feed(b))
}

func (p *Prompt) flushPendingInput(now time.Time) (handled, shouldExit bool, exec *Exec) {
	events := p.pasteDecoder.FlushPending(now)
	if len(events) == 0 {
		return false, false, nil
	}
	shouldExit, exec = p.feedDecoded(events)
	return true, shouldExit, exec
}

func (p *Prompt) feedDecoded(events []decodedInput) (shouldExit bool, exec *Exec) {
	for _, event := range events {
		if event.pasted {
			if event.data == nil || len(event.data) == 0 {
				continue
			}
			p.buf.lastKeyStroke = BracketedPaste
			p.buf.InsertText(p.storePastedText(string(event.data)), false, true)
			p.completion.Reset()
			continue
		}
		if shouldExit, exec = p.feedKey(event.data); shouldExit || exec != nil {
			return shouldExit, exec
		}
	}
	return false, nil
}

func (p *Prompt) storePastedText(text string) string {
	if !p.compactPaste {
		return text
	}
	if p.pastePayloads == nil {
		p.pastePayloads = make(map[rune]string)
	}
	if p.nextPasteToken < pasteTokenStart {
		p.nextPasteToken = pasteTokenStart
	}
	for p.nextPasteToken <= pasteTokenEnd {
		token := p.nextPasteToken
		p.nextPasteToken++
		if _, exists := p.pastePayloads[token]; exists {
			continue
		}
		p.pastePayloads[token] = text
		return string(token)
	}
	return text
}

func (p *Prompt) expandPastedText(text string) string {
	if len(p.pastePayloads) == 0 {
		return text
	}
	var expanded strings.Builder
	for _, char := range text {
		if pasted, ok := p.pastePayloads[char]; ok {
			expanded.WriteString(pasted)
		} else {
			expanded.WriteRune(char)
		}
	}
	return expanded.String()
}

func (p *Prompt) feedKey(b []byte) (shouldExit bool, exec *Exec) {
	key := GetKey(b)
	p.buf.lastKeyStroke = key
	// completion
	completing := p.completion.Completing()
	p.handleCompletionKeyBinding(key, completing)

	switch key {
	case Enter, ControlJ, ControlM:
		inputForHistory := p.buf.Text()
		p.renderer.BreakLine(p.buf)

		exec = &Exec{input: p.expandPastedText(inputForHistory)}
		p.buf = NewBuffer()
		if exec.input != "" {
			p.history.Add(inputForHistory)
		}
	case ControlC:
		p.renderer.BreakLine(p.buf)
		p.buf = NewBuffer()
		p.history.Clear()
	case Up, ControlP:
		if !completing { // Don't use p.completion.Completing() because it takes double operation when switch to selected=-1.
			if newBuf, changed := p.history.Older(p.buf); changed {
				p.buf = newBuf
			}
		}
	case Down, ControlN:
		if !completing { // Don't use p.completion.Completing() because it takes double operation when switch to selected=-1.
			if newBuf, changed := p.history.Newer(p.buf); changed {
				p.buf = newBuf
			}
			return
		}
	case ControlD:
		if p.buf.Text() == "" {
			shouldExit = true
			return
		}
	case NotDefined:
		if p.handleASCIICodeBinding(b) {
			return
		}
		p.buf.InsertText(string(b), false, true)
	}

	shouldExit = p.handleKeyBinding(key)
	return
}

func (p *Prompt) handleCompletionKeyBinding(key Key, completing bool) {
	switch key {
	case Down:
		if completing || p.completionOnDown {
			p.completion.Next()
		}
	case Tab, ControlI:
		p.completion.Next()
	case Up:
		if completing {
			p.completion.Previous()
		}
	case BackTab:
		p.completion.Previous()
	default:
		if s, ok := p.completion.GetSelectedSuggestion(); ok {
			w := p.buf.Document().GetWordBeforeCursorUntilSeparator(p.completion.wordSeparator)
			if w != "" {
				p.buf.DeleteBeforeCursor(len([]rune(w)))
			}
			p.buf.InsertText(s.Text, false, true)
		}
		p.completion.Reset()
	}
}

func (p *Prompt) handleKeyBinding(key Key) bool {
	shouldExit := false
	for i := range commonKeyBindings {
		kb := commonKeyBindings[i]
		if kb.Key == key {
			kb.Fn(p.buf)
		}
	}

	if p.keyBindMode == EmacsKeyBind {
		for i := range emacsKeyBindings {
			kb := emacsKeyBindings[i]
			if kb.Key == key {
				kb.Fn(p.buf)
			}
		}
	}

	// Custom key bindings
	for i := range p.keyBindings {
		kb := p.keyBindings[i]
		if kb.Key == key {
			kb.Fn(p.buf)
		}
	}
	if p.exitChecker != nil && p.exitChecker(p.buf.Text(), false) {
		shouldExit = true
	}
	return shouldExit
}

func (p *Prompt) handleASCIICodeBinding(b []byte) bool {
	checked := false
	for _, kb := range p.ASCIICodeBindings {
		if bytes.Equal(kb.ASCIICode, b) {
			kb.Fn(p.buf)
			checked = true
		}
	}
	return checked
}

// Input just returns user input text.
func (p *Prompt) Input() string {
	defer debug.Teardown()
	debug.Log("start prompt")
	p.setUp()
	defer p.tearDown()

	if p.completion.showAtStart {
		p.completion.Update(*p.buf.Document())
	}

	p.renderer.Render(p.buf, p.completion)
	bufCh := make(chan []byte, 128)
	stopReadBufCh := make(chan struct{})
	go p.readBuffer(bufCh, stopReadBufCh)

	for {
		select {
		case b := <-bufCh:
			if shouldExit, e := p.feed(b); shouldExit {
				p.renderer.BreakLine(p.buf)
				stopReadBufCh <- struct{}{}
				return ""
			} else if e != nil {
				// Stop goroutine to run readBuffer function
				stopReadBufCh <- struct{}{}
				return e.input
			} else {
				p.completion.Update(*p.buf.Document())
				p.renderer.Render(p.buf, p.completion)
			}
		default:
			if handled, shouldExit, _ := p.flushPendingInput(time.Now()); handled {
				if shouldExit {
					p.renderer.BreakLine(p.buf)
					stopReadBufCh <- struct{}{}
					return ""
				}
				p.completion.Update(*p.buf.Document())
				p.renderer.Render(p.buf, p.completion)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func (p *Prompt) readBuffer(bufCh chan []byte, stopCh chan struct{}) {
	debug.Log("start reading buffer")
	for {
		select {
		case <-stopCh:
			debug.Log("stop reading buffer")
			return
		default:
			if b, err := p.in.Read(); err == nil && !(len(b) == 1 && b[0] == 0) {
				bufCh <- b
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (p *Prompt) setUp() {
	p.pasteDecoder = bracketedPasteDecoder{}
	debug.AssertNoError(p.in.Setup())
	p.renderer.Setup()
	p.setBracketedPasteMode(true)
	p.renderer.UpdateWinSize(p.in.GetWinSize())
}

func (p *Prompt) tearDown() {
	p.setBracketedPasteMode(false)
	if !p.skipTearDown {
		debug.AssertNoError(p.in.TearDown())
	}
	p.renderer.TearDown()
}

func (p *Prompt) setBracketedPasteMode(enabled bool) {
	if enabled {
		p.renderer.out.WriteRawStr("\x1b[?2004h")
	} else {
		p.renderer.out.WriteRawStr("\x1b[?2004l")
	}
	debug.AssertNoError(p.renderer.out.Flush())
}
