package ui

import (
	"io"
	"os"
	"sync"
	"sync/atomic"

	agentspinner "github.com/benoitpetit/agent-spinner"
	"golang.org/x/term"
)

var spinnerDisabled atomic.Bool

var loaderSpinnerRegistry = agentspinner.NewDefaultRegistry()

// ProgressStage names the user-facing kind of work represented by a spinner.
type ProgressStage string

const (
	ProgressConnecting ProgressStage = "connecting"
	ProgressPreparing  ProgressStage = "preparing"
	ProgressSearching  ProgressStage = "searching"
	ProgressSources    ProgressStage = "sources"
	ProgressResponding ProgressStage = "responding"
	ProgressImage      ProgressStage = "image"
	ProgressComplete   ProgressStage = "complete"
	ProgressFailed     ProgressStage = "failed"
	ProgressCancelled  ProgressStage = "cancelled"
)

// SetSpinnerDisabled suppresses terminal progress animation in machine-readable modes.
func SetSpinnerDisabled(disabled bool) {
	spinnerDisabled.Store(disabled)
}

// Spinner provides one replaceable terminal status line while a request runs.
type Spinner struct {
	mu        sync.Mutex
	indicator *agentspinner.Instance
	stopped   bool
	stage     ProgressStage
	style     agentspinner.Name
	label     string
	initial   string
}

// StartSpinner starts an agent-spinner animation with the supplied message.
// It stays silent when stdout is not an interactive terminal.
func StartSpinner(label string) *Spinner {
	return startSpinner(ProgressConnecting, label)
}

// StartSpinnerWithModel starts with the model name and places the loader after it.
func StartSpinnerWithModel(model string) *Spinner {
	return startSpinner(ProgressConnecting, modelSpinnerMessage(model))
}

func startSpinner(stage ProgressStage, message string) *Spinner {
	enabled := !spinnerDisabled.Load() && term.IsTerminal(int(os.Stdout.Fd()))
	spinner := &Spinner{
		stage:   stage,
		style:   spinnerStyleForStage(stage),
		initial: message,
	}
	if enabled {
		spinner.indicator = agentspinner.StartCustom(
			message,
			loaderSpinnerDefinitionForStage(stage),
			agentspinner.WithRenderer(newAgentSpinnerRenderer(os.Stdout)),
		)
	}
	return spinner
}

// ClearForRedraw removes the current animation without ending its progress
// state. A terminal repaint can then restore it below the new content.
func (s *Spinner) ClearForRedraw() {
	if s == nil {
		return
	}
	s.mu.Lock()
	indicator := s.indicator
	s.indicator = nil
	s.mu.Unlock()
	if indicator != nil {
		indicator.Stop("")
	}
}

func (s *Spinner) ResumeAfterRedraw() {
	if s == nil || spinnerDisabled.Load() || !term.IsTerminal(int(os.Stdout.Fd())) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.indicator != nil {
		return
	}
	message := s.initial
	if s.label != "" {
		message = eventProgressMessage(s.stage, s.label)
	}
	s.indicator = agentspinner.StartCustom(
		message,
		loaderSpinnerDefinitionForStage(s.stage),
		agentspinner.WithRenderer(newAgentSpinnerRenderer(os.Stdout)),
	)
}

// SetProgress replaces the current line with the event icon and its status.
// The model is deliberately omitted once the stream reports an event.
func (s *Spinner) SetProgress(stage ProgressStage, label string) {
	if s == nil || label == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || (s.stage == stage && s.label == label) {
		return
	}
	style := spinnerStyleForStage(stage)
	s.stage = stage
	s.label = label
	if s.indicator != nil {
		message := eventProgressMessage(stage, label)
		if style != s.style {
			s.indicator.Stop("")
			s.indicator = agentspinner.StartCustom(
				message,
				loaderSpinnerDefinitionForStage(stage),
				agentspinner.WithRenderer(newAgentSpinnerRenderer(os.Stdout)),
			)
		} else {
			s.indicator.Update(message)
		}
	}
	s.style = style
}

// SetLabel updates the current event label without changing its icon.
func (s *Spinner) SetLabel(label string) {
	if s == nil || label == "" {
		return
	}
	s.mu.Lock()
	stage := s.stage
	s.mu.Unlock()
	s.SetProgress(stage, label)
}

// Stop clears the active line so the model response can take its place.
func (s *Spinner) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	indicator := s.indicator
	s.indicator = nil
	s.mu.Unlock()
	if indicator != nil {
		indicator.Stop("")
	}
}

func modelSpinnerMessage(model string) string {
	if model == "" {
		return ""
	}
	return model + ":"
}

func eventProgressMessage(stage ProgressStage, label string) string {
	if label == "" {
		return progressGlyph(stage)
	}
	return progressGlyph(stage) + " " + label
}

func spinnerStyleForStage(stage ProgressStage) agentspinner.Name {
	// Keep the ASCII fallback stable on terminals that cannot render the
	// library's Unicode animations.
	if os.Getenv("TERM") == "dumb" {
		return agentspinner.Braille
	}
	switch stage {
	case ProgressPreparing:
		return agentspinner.Breathe
	case ProgressSearching:
		return agentspinner.Radar
	case ProgressSources:
		return agentspinner.Scan
	case ProgressResponding:
		return agentspinner.Typing
	case ProgressImage:
		return agentspinner.Sparkle
	case ProgressComplete:
		return agentspinner.Star
	case ProgressFailed, ProgressCancelled:
		return agentspinner.Cross
	default:
		return agentspinner.Braille
	}
}

func progressGlyph(stage ProgressStage) string {
	if os.Getenv("TERM") == "dumb" {
		switch stage {
		case ProgressConnecting, ProgressPreparing:
			return "~"
		case ProgressSearching:
			return "S"
		case ProgressSources:
			return "#"
		case ProgressResponding:
			return "*"
		case ProgressImage:
			return "I"
		case ProgressComplete:
			return "+"
		case ProgressFailed, ProgressCancelled:
			return "!"
		default:
			return "."
		}
	}
	switch stage {
	case ProgressConnecting, ProgressPreparing:
		return "↔"
	case ProgressSearching:
		return "⌕"
	case ProgressSources:
		return "≡"
	case ProgressResponding:
		return "✦"
	case ProgressImage:
		return "▧"
	case ProgressComplete:
		return "✓"
	case ProgressFailed, ProgressCancelled:
		return "!"
	default:
		return "•"
	}
}

type serializedSpinnerRenderer struct {
	mu       sync.Mutex
	renderer *agentspinner.RawRenderer
	stopped  bool
}

func newAgentSpinnerRenderer(output io.Writer) *serializedSpinnerRenderer {
	renderer := agentspinner.NewRawRendererWithOutput(output)
	// agent-spinner passes (frame, message) to FormatFrame. Indexed formatting
	// keeps the animation at the right edge of the message.
	renderer.FormatFrame = "\r\033[K%[2]s %[1]s"
	// A blank final message clears transient progress without leaving an event row.
	renderer.FormatFinal = "\r\033[K%[2]s"
	return &serializedSpinnerRenderer{renderer: renderer}
}

func (r *serializedSpinnerRenderer) HideCursor() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renderer.HideCursor()
}

func (r *serializedSpinnerRenderer) ShowCursor() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renderer.ShowCursor()
}

func (r *serializedSpinnerRenderer) RenderFrame(frame, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.stopped {
		r.renderer.RenderFrame(frame, message)
	}
}

func (r *serializedSpinnerRenderer) RenderFinal(symbol, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	r.stopped = true
	r.renderer.RenderFinal(symbol, message)
}

func loaderSpinnerDefinition() agentspinner.Spinner {
	return loaderSpinnerDefinitionForStage(ProgressConnecting)
}

func loaderSpinnerDefinitionForStage(stage ProgressStage) agentspinner.Spinner {
	if os.Getenv("TERM") == "dumb" {
		return agentspinner.Spinner{Frames: []string{"|", "/", "-", "\\"}, Interval: 120}
	}
	return loaderSpinnerRegistry.Get(spinnerStyleForStage(stage))
}
