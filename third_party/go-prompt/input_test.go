package prompt

import (
	"strings"
	"testing"
	"time"
)

func TestFeedInsertsBracketedPasteAsOneMultilineMessage(t *testing.T) {
	p := &Prompt{
		buf:      NewBuffer(),
		history:  NewHistory(),
		renderer: &Render{out: &captureConsoleWriter{}, row: 24, col: 80, prefix: "You: ", livePrefixCallback: func() (string, bool) { return "", false }},
		completion: &CompletionManager{
			selected: -1,
			max:      6,
			completer: func(Document) []Suggest {
				return nil
			},
		},
		keyBindMode: EmacsKeyBind,
	}
	chunks := [][]byte{
		[]byte("Please review: "),
		[]byte("\x1b"),
		[]byte("[20"),
		[]byte("0~first line\r"),
		[]byte("\nsecond line\x1bunsafe\x1b[201"),
		[]byte("~"),
	}

	for _, chunk := range chunks {
		shouldExit, exec := p.feed(chunk)
		if shouldExit || exec != nil {
			t.Fatalf("paste chunk submitted input or exited prompt: shouldExit=%t exec=%#v", shouldExit, exec)
		}
	}

	const want = "Please review: first line\nsecond lineunsafe"
	if got := p.buf.Text(); got != want {
		t.Fatalf("buffer after paste = %q, want %q", got, want)
	}
	if strings.ContainsRune(p.buf.Text(), '\x1b') {
		t.Fatalf("buffer contains a terminal escape character: %q", p.buf.Text())
	}

	_, exec := p.feed([]byte{'\n'})
	if exec == nil || exec.input != want {
		t.Fatalf("submitted paste = %#v, want %#v", exec, want)
	}
}

func TestCompactPasteShowsReferenceAndExpandsWhenSubmitted(t *testing.T) {
	writer := &captureConsoleWriter{}
	p := &Prompt{
		buf:      NewBuffer(),
		history:  NewHistory(),
		renderer: &Render{out: writer, prefix: "You: ", livePrefixCallback: func() (string, bool) { return "", false }, row: 24, col: 80},
		completion: &CompletionManager{
			selected:  -1,
			completer: func(Document) []Suggest { return nil },
		},
		keyBindMode: EmacsKeyBind,
	}
	if err := OptionCompactPaste()(p); err != nil {
		t.Fatal(err)
	}

	p.feed([]byte("context: "))
	p.feed([]byte("\x1b[200~secret\ntext\x1b[201~"))
	if strings.Contains(p.buf.Text(), "secret") || strings.ContainsRune(p.buf.Text(), '\n') {
		t.Fatalf("paste content leaked into the visible prompt buffer: %q", p.buf.Text())
	}

	p.renderer.Render(p.buf, p.completion)
	shown := string(writer.output)
	if !strings.Contains(shown, "[pasted · 11 chars]") {
		t.Fatalf("prompt did not show a compact paste reference: %q", shown)
	}
	if strings.Contains(shown, "secret") {
		t.Fatalf("prompt rendered pasted content instead of its reference: %q", shown)
	}

	_, exec := p.feed([]byte{'\n'})
	if exec == nil || exec.input != "context: secret\ntext" {
		t.Fatalf("submitted input = %#v, want expanded paste content", exec)
	}
	if strings.Contains(string(writer.output), "secret") {
		t.Fatalf("submitted prompt line rendered pasted content instead of its reference: %q", writer.output)
	}
	if got := p.history.histories[0]; strings.Contains(got, "secret") || len([]rune(got)) != len([]rune("context: "))+1 {
		t.Fatalf("history did not retain the compact paste reference: %q", got)
	}
}

func TestCompactPasteReferenceDeletesAsOneCharacter(t *testing.T) {
	p := &Prompt{
		buf:      NewBuffer(),
		history:  NewHistory(),
		renderer: &Render{out: &captureConsoleWriter{}, livePrefixCallback: func() (string, bool) { return "", false }},
		completion: &CompletionManager{
			selected:  -1,
			completer: func(Document) []Suggest { return nil },
		},
		keyBindMode: EmacsKeyBind,
	}
	if err := OptionCompactPaste()(p); err != nil {
		t.Fatal(err)
	}
	p.feed([]byte("\x1b[200~a long pasted block\x1b[201~"))

	if got := len([]rune(p.buf.Text())); got != 1 {
		t.Fatalf("paste reference occupies %d input characters, want 1", got)
	}
	p.feed([]byte{0x7f})
	if got := p.buf.Text(); got != "" {
		t.Fatalf("backspace left part of the paste reference in the buffer: %q", got)
	}
	p.feed([]byte("\x1b[200~a long pasted block\x1b[201~"))
	p.buf.CursorLeft(1)
	p.feed([]byte("\x1b[3~"))
	if got := p.buf.Text(); got != "" {
		t.Fatalf("Delete left part of the paste reference in the buffer: %q", got)
	}
}

func TestPromptFlushesStandaloneEscapeAfterSequenceTimeout(t *testing.T) {
	p := &Prompt{
		buf: NewBuffer(),
		completion: &CompletionManager{
			selected:  -1,
			completer: func(Document) []Suggest { return nil },
		},
	}
	p.feed([]byte{0x1b})

	handled, shouldExit, exec := p.flushPendingInput(time.Now().Add(incompleteInputTimeout))
	if !handled || shouldExit || exec != nil {
		t.Fatalf("flushing standalone Escape = handled %t, shouldExit %t, exec %#v", handled, shouldExit, exec)
	}
	if p.buf.lastKeyStroke != Escape {
		t.Fatalf("last key after standalone Escape = %v, want Escape", p.buf.lastKeyStroke)
	}
}

type captureConsoleWriter struct {
	VT100Writer
	output []byte
}

func (w *captureConsoleWriter) Flush() error {
	w.output = append(w.output, w.buffer...)
	w.buffer = nil
	return nil
}

func TestRenderDisplaysMultilineInputWithoutBreakingPromptLayout(t *testing.T) {
	writer := &captureConsoleWriter{}
	completion := &CompletionManager{selected: -1}
	buffer := NewBuffer()
	buffer.InsertText("first\tline\nsecond line", false, true)
	renderer := &Render{
		out:                writer,
		prefix:             "You: ",
		livePrefixCallback: func() (string, bool) { return "", false },
		row:                24,
		col:                80,
	}

	renderer.Render(buffer, completion)

	if got := string(writer.output); !strings.Contains(got, "first⇥line ↵ second line") {
		t.Fatalf("rendered prompt does not keep pasted lines on one visual line: %q", got)
	}
}

func TestRenderKeepsLongPastedInputWithinTerminalHeight(t *testing.T) {
	writer := &captureConsoleWriter{}
	completion := &CompletionManager{selected: -1, max: 8}
	buffer := NewBuffer()
	buffer.InsertText("START "+strings.Repeat("pasted line\n", 200)+"FINAL_SENTINEL", false, true)
	renderer := &Render{
		out:                writer,
		prefix:             "You: ",
		livePrefixCallback: func() (string, bool) { return "", false },
		row:                24,
		col:                80,
	}

	renderer.Render(buffer, completion)

	got := string(writer.output)
	if strings.Contains(got, "Your console window is too small") {
		t.Fatal("long pasted input replaced the prompt with the terminal size error")
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("long pasted input is not marked as clipped in the prompt: %q", got)
	}
	if !strings.Contains(got, "FINAL_SENTINEL") {
		t.Fatalf("long pasted input preview hides the text at the cursor: %q", got)
	}
}

type testConsoleParser struct{}

func (testConsoleParser) Setup() error          { return nil }
func (testConsoleParser) TearDown() error       { return nil }
func (testConsoleParser) GetWinSize() *WinSize  { return &WinSize{Row: 24, Col: 80} }
func (testConsoleParser) Read() ([]byte, error) { return nil, nil }

func TestPromptEnablesAndRestoresBracketedPasteMode(t *testing.T) {
	writer := &captureConsoleWriter{}
	p := &Prompt{
		in:       testConsoleParser{},
		renderer: &Render{out: writer},
		buf:      NewBuffer(),
	}

	p.setUp()
	if !strings.Contains(string(writer.output), "\x1b[?2004h") {
		t.Fatal("prompt did not enable bracketed paste mode")
	}

	p.tearDown()
	if !strings.Contains(string(writer.output), "\x1b[?2004l") {
		t.Fatal("prompt did not disable bracketed paste mode during teardown")
	}
}
