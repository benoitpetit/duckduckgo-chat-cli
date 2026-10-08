package dashboard

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"duckduckgo-chat-cli/internal/chat"
	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

const dashboardAppStartURL = "data:text/html,%3C!doctype%20html%3E%3Ctitle%3EDuckChat%20Dashboard%3C%2Ftitle%3E"

// ChromiumWindow is a dedicated, tabless app-mode window for the local dashboard.
type ChromiumWindow struct {
	cancelBrowser   context.CancelFunc
	cancelAllocator context.CancelFunc
	browserLost     <-chan struct{}
	done            <-chan struct{}
	closeOnce       sync.Once
	closeErr        error
}

// OpenChromiumApp opens address in a dedicated Chrome/Chromium app window.
func OpenChromiumApp(ctx context.Context, address string) (*ChromiumWindow, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	executable, err := chat.BrowserExecutable()
	if err != nil {
		return nil, err
	}
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options,
		chromedp.ExecPath(executable),
		chromedp.Flag("headless", false),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-features", "Translate,TranslateUI,BlinkGenPropertyTrees"),
		chromedp.Flag("app", dashboardAppStartURL),
		chromedp.WindowSize(1280, 900),
		chromedp.ModifyCmdFunc(func(cmd *exec.Cmd) {
			prepareDashboardChromiumCmd(cmd)
			// chromedp appends about:blank after the flags; omit it so Chromium
			// cannot create a second, regular tabbed window.
			if len(cmd.Args) > 1 && cmd.Args[len(cmd.Args)-1] == "about:blank" {
				cmd.Args = cmd.Args[:len(cmd.Args)-1]
			}
		}),
	)
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, options...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	window := &ChromiumWindow{cancelBrowser: cancelBrowser, cancelAllocator: cancelAllocator}
	startupCtx, cancelStartup := context.WithTimeout(ctx, 30*time.Second)
	stopOnStartupCancel := context.AfterFunc(startupCtx, cancelBrowser)
	err = chromedp.Run(browserCtx)
	if err == nil {
		err = chromedp.Run(browserCtx, chromedp.Navigate(address))
	}
	startupActive := stopOnStartupCancel()
	startupErr := startupCtx.Err()
	cancelStartup()
	if err != nil || !startupActive || startupErr != nil {
		if err == nil {
			err = startupErr
		}
		if err == nil {
			err = fmt.Errorf("browser startup ended unexpectedly")
		}
		_ = window.Close()
		return nil, fmt.Errorf("could not open the dashboard app window in Chrome/Chromium: %w", err)
	}

	cdpContext := chromedp.FromContext(browserCtx)
	if cdpContext == nil || cdpContext.Browser == nil || cdpContext.Target == nil {
		_ = window.Close()
		return nil, fmt.Errorf("Chromium did not create a dashboard window")
	}
	window.browserLost = cdpContext.Browser.LostConnection
	window.done = monitorDashboardChromiumDone(browserCtx, window.browserLost)
	browserExecutorCtx := cdp.WithExecutor(browserCtx, cdpContext.Browser)
	windowID, _, err := cdpbrowser.GetWindowForTarget().WithTargetID(cdpContext.Target.TargetID).Do(browserExecutorCtx)
	if err == nil {
		err = cdpbrowser.SetWindowBounds(windowID, &cdpbrowser.Bounds{
			Width: 1280, Height: 900, WindowState: cdpbrowser.WindowStateNormal,
		}).Do(browserExecutorCtx)
	}
	if err != nil {
		_ = window.Close()
		return nil, fmt.Errorf("could not size the dashboard app window: %w", err)
	}
	return window, nil
}

// Done closes when the user closes the app window or its browser exits.
func (w *ChromiumWindow) Done() <-chan struct{} { return w.done }

// Open reports whether the browser process still owns the app window.
func (w *ChromiumWindow) Open() bool {
	if w == nil || w.done == nil {
		return false
	}
	select {
	case <-w.done:
		return false
	default:
		return true
	}
}

// Close shuts down the dedicated Chromium process.
func (w *ChromiumWindow) Close() error {
	if w == nil {
		return nil
	}
	w.closeOnce.Do(func() {
		if w.cancelBrowser != nil {
			w.cancelBrowser()
		}
		if w.cancelAllocator != nil {
			w.cancelAllocator()
		}
	})
	return w.closeErr
}

func monitorDashboardChromiumDone(ctx context.Context, browserLost <-chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-browserLost:
		}
		close(done)
	}()
	return done
}
