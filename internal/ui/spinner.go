package ui

import (
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

// Spinner provides feedback while work is happening before a streamed
// response is available, such as the Duck.ai browser proof bootstrap.
type Spinner struct {
	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
	enabled  bool
}

// StartSpinner starts a terminal spinner. It stays silent when stdout is not
// an interactive terminal so logs and API-driven processes remain clean.
func StartSpinner(label string) *Spinner {
	spinner := &Spinner{
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		enabled: term.IsTerminal(int(os.Stdout.Fd())),
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
		fmt.Printf("%s %c", label, frames[frame])
		for {
			select {
			case <-spinner.stop:
				return
			case <-ticker.C:
				frame = (frame + 1) % len(frames)
				fmt.Printf("\r\033[K%s %c", label, frames[frame])
			}
		}
	}()
	return spinner
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
