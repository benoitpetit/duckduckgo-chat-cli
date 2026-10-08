package ui

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/term"
)

var spinnerDisabled atomic.Bool

// SetSpinnerDisabled suppresses terminal progress animation in machine-readable modes.
func SetSpinnerDisabled(disabled bool) {
	spinnerDisabled.Store(disabled)
}

// Spinner provides feedback while work is happening before a streamed
// response is available, such as the Duck.ai browser proof bootstrap.
type Spinner struct {
	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
	updates  chan string
	enabled  bool
}

// StartSpinner starts a terminal spinner. It stays silent when stdout is not
// an interactive terminal so logs and API-driven processes remain clean.
func StartSpinner(label string) *Spinner {
	spinner := &Spinner{
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		updates: make(chan string, 1),
		enabled: !spinnerDisabled.Load() && term.IsTerminal(int(os.Stdout.Fd())),
	}
	if !spinner.enabled {
		close(spinner.done)
		return spinner
	}

	go func() {
		defer close(spinner.done)
		frames := []rune{'|', '/', '-', '\\'}
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		frame := 0
		currentLabel := label
		draw := func() { fmt.Printf("\r\033[K%s %c", currentLabel, frames[frame]) }
		draw()
		for {
			select {
			case <-spinner.stop:
				return
			case currentLabel = <-spinner.updates:
				draw()
			case <-ticker.C:
				frame = (frame + 1) % len(frames)
				draw()
			}
		}
	}()
	return spinner
}

// SetLabel updates the active spinner text without interrupting its animation.
func (s *Spinner) SetLabel(label string) {
	if s == nil || !s.enabled || label == "" {
		return
	}
	select {
	case s.updates <- label:
	default:
		select {
		case <-s.updates:
		default:
		}
		select {
		case s.updates <- label:
		default:
		}
	}
}

// Stop stops the spinner and clears its terminal line.
func (s *Spinner) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.enabled {
			close(s.stop)
		}
		<-s.done
		if s.enabled {
			fmt.Print("\r\033[K")
		}
	})
}
