package voice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type BrowserSession interface {
	Close() error
}

type BrowserOpener interface {
	Open(context.Context, string) (BrowserSession, error)
}

type browserSessionDone interface {
	Done() <-chan struct{}
}

// BrowserWindowController exposes visibility controls without closing the
// browser or ending its local voice server and media session.
type BrowserWindowController interface {
	MinimizeWindow() error
	RestoreWindow() error
}

type activeRunState struct {
	cancel          context.CancelFunc
	visibilityMu    sync.Mutex
	window          BrowserWindowController
	windowMinimized bool
}

var activeRunMu sync.Mutex
var activeRun *activeRunState

func Run(ctx context.Context, deps Dependencies, opener BrowserOpener) (runErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if deps.Proof == nil || deps.Signaling == nil {
		return fmt.Errorf("voice proof and signaling dependencies are required")
	}
	if opener == nil {
		return fmt.Errorf("voice browser opener is required")
	}

	runCtx, cancel := context.WithCancel(ctx)
	state := &activeRunState{cancel: cancel}
	activeRunMu.Lock()
	if activeRun != nil {
		activeRunMu.Unlock()
		cancel()
		return fmt.Errorf("a Duck.ai voice session is already active")
	}
	activeRun = state
	activeRunMu.Unlock()
	defer func() {
		cancel()
		activeRunMu.Lock()
		if activeRun == state {
			activeRun = nil
		}
		activeRunMu.Unlock()
	}()

	token, err := newAccessToken()
	if err != nil {
		return fmt.Errorf("create voice access token: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start local voice server: %w", err)
	}
	address := "http://" + listener.Addr().String()
	handler, err := NewHandler(deps, token, address)
	if err != nil {
		_ = listener.Close()
		return err
	}
	localHandler, ok := handler.(*localHandler)
	if !ok {
		_ = listener.Close()
		return fmt.Errorf("unexpected local voice handler implementation")
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	var browser BrowserSession
	defer func() {
		if browser != nil {
			if err := browser.Close(); err != nil && runErr == nil {
				runErr = fmt.Errorf("close voice browser: %w", err)
			}
		}
		localHandler.invalidate()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			if runErr == nil {
				runErr = fmt.Errorf("stop local voice server: %w", err)
			}
		}
	}()

	pageURL := address + "/#" + url.PathEscape(token)
	type openResult struct {
		session BrowserSession
		err     error
	}
	opened := make(chan openResult, 1)
	go func() {
		session, err := opener.Open(runCtx, pageURL)
		opened <- openResult{session: session, err: err}
	}()
	select {
	case <-runCtx.Done():
		return nil
	case <-localHandler.stop:
		return nil
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("local voice server stopped: %w", err)
		}
		return nil
	case result := <-opened:
		if result.err != nil {
			if runCtx.Err() != nil {
				return nil
			}
			return fmt.Errorf("open Duck.ai voice interface: %w", result.err)
		}
		browser = result.session
	}
	if browser == nil {
		return fmt.Errorf("open Duck.ai voice interface: browser opener returned no session")
	}
	if controller, ok := browser.(BrowserWindowController); ok {
		activeRunMu.Lock()
		if activeRun == state {
			state.visibilityMu.Lock()
			state.window = controller
			state.visibilityMu.Unlock()
		}
		activeRunMu.Unlock()
	}

	var browserDone <-chan struct{}
	if session, ok := browser.(browserSessionDone); ok {
		browserDone = session.Done()
	}
	select {
	case <-runCtx.Done():
		return nil
	case <-localHandler.stop:
		return nil
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("local voice server stopped: %w", err)
		}
		return nil
	case <-browserDone:
		return nil
	}
}

func StopActive() bool {
	activeRunMu.Lock()
	state := activeRun
	activeRunMu.Unlock()
	if state == nil {
		return false
	}
	state.cancel()
	return true
}

// Active reports whether a voice launch or session is in progress.
func Active() bool {
	activeRunMu.Lock()
	defer activeRunMu.Unlock()
	return activeRun != nil
}

// MinimizeActive hides the Chromium window while keeping its session alive.
func MinimizeActive() error { return setActiveWindowVisibility(true) }

// RestoreActive makes a minimized Chromium voice window visible again.
func RestoreActive() error {
	activeRunMu.Lock()
	state := activeRun
	activeRunMu.Unlock()
	if state == nil {
		return fmt.Errorf("no active voice session")
	}
	state.visibilityMu.Lock()
	defer state.visibilityMu.Unlock()
	if state.window == nil {
		return fmt.Errorf("voice window is not ready")
	}
	if err := state.window.RestoreWindow(); err != nil {
		return fmt.Errorf("restore voice window: %w", err)
	}
	state.windowMinimized = false
	return nil
}

// ToggleActiveVisibility minimizes or restores the active voice window.
func ToggleActiveVisibility() error {
	activeRunMu.Lock()
	state := activeRun
	activeRunMu.Unlock()
	if state == nil {
		return fmt.Errorf("no active voice session")
	}
	state.visibilityMu.Lock()
	defer state.visibilityMu.Unlock()
	if state.window == nil {
		return fmt.Errorf("voice window is not ready")
	}
	minimize := !state.windowMinimized
	if minimize {
		if err := state.window.MinimizeWindow(); err != nil {
			return fmt.Errorf("minimize voice window: %w", err)
		}
	} else {
		if err := state.window.RestoreWindow(); err != nil {
			return fmt.Errorf("restore voice window: %w", err)
		}
	}
	state.windowMinimized = minimize
	return nil
}

func setActiveWindowVisibility(minimize bool) error {
	activeRunMu.Lock()
	state := activeRun
	activeRunMu.Unlock()
	if state == nil {
		return fmt.Errorf("no active voice session")
	}
	state.visibilityMu.Lock()
	defer state.visibilityMu.Unlock()
	if state.window == nil {
		return fmt.Errorf("voice window is not ready")
	}
	if minimize == state.windowMinimized {
		return nil
	}
	if minimize {
		if err := state.window.MinimizeWindow(); err != nil {
			return fmt.Errorf("minimize voice window: %w", err)
		}
	} else if err := state.window.RestoreWindow(); err != nil {
		return fmt.Errorf("restore voice window: %w", err)
	}
	state.windowMinimized = minimize
	return nil
}

func newAccessToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}

func newJourneyID() (string, error) {
	var journeyID [16]byte
	if _, err := rand.Read(journeyID[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(journeyID[:]), nil
}
