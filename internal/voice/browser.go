package voice

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"sync"
	"time"

	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/config"
	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

type ChromiumOpener struct {
	Settings config.SpeakConfig
}

const chromiumAppStartURL = "data:text/html,%3C!doctype%20html%3E%3Ctitle%3EDuck.ai%20Speak%3C%2Ftitle%3E"

type chromiumSession struct {
	ctx             context.Context
	cancelBrowser   context.CancelFunc
	cancelAllocator context.CancelFunc
	browserLost     <-chan struct{}
	done            <-chan struct{}
	closeOnce       sync.Once
	closeErr        error
}

func (opener ChromiumOpener) Open(ctx context.Context, address string) (BrowserSession, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	origin, err := loopbackVoiceOrigin(address)
	if err != nil {
		return nil, err
	}
	executable, err := chat.BrowserExecutable()
	if err != nil {
		return nil, err
	}
	width, height := opener.Settings.WindowWidth, opener.Settings.WindowHeight
	if width < 320 || width > 1400 {
		width = 480
	}
	if height < 360 || height > 1400 {
		height = 500
	}
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options,
		chromedp.ExecPath(executable),
		chromedp.Flag("headless", false),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-dev-shm-usage", true),
		// Disable Chromium's translate offer in this dedicated voice window.
		// Keep the existing allocator defaults while also disabling TranslateUI,
		// which controls the browser's translation prompt.
		chromedp.Flag("disable-features", "Translate,TranslateUI,BlinkGenPropertyTrees"),
		chromedp.Flag("mute-audio", false),
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
		// about:blank is an internal URL and recent Chromium versions fall back
		// to a regular tabbed window for it. Use a harmless data page with the
		// stable title so app mode and the desktop's title-based popup rule apply
		// on first map. The short-lived bearer token stays out of process args.
		chromedp.Flag("app", chromiumAppStartURL),
		chromedp.WindowSize(width, height),
		chromedp.ModifyCmdFunc(func(cmd *exec.Cmd) {
			prepareChromiumCmd(cmd)
			// chromedp appends a positional about:blank after all flags. Remove it
			// so it cannot request a second regular browser window.
			if len(cmd.Args) > 1 && cmd.Args[len(cmd.Args)-1] == "about:blank" {
				cmd.Args = cmd.Args[:len(cmd.Args)-1]
			}
		}),
	)
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, options...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	session := &chromiumSession{
		ctx:             browserCtx,
		cancelBrowser:   cancelBrowser,
		cancelAllocator: cancelAllocator,
	}
	startupCtx, cancelStartup := context.WithTimeout(ctx, 30*time.Second)
	stopOnStartupCancel := context.AfterFunc(startupCtx, cancelBrowser)
	err = chromedp.Run(browserCtx)
	if err == nil {
		chromedpContext := chromedp.FromContext(browserCtx)
		if chromedpContext == nil || chromedpContext.Browser == nil {
			err = fmt.Errorf("Chromium did not start a browser connection")
		} else {
			// Grant microphone access only to the ephemeral loopback origin used by
			// this Speak window, before loading its auto-starting page.
			browserExecutorCtx := cdp.WithExecutor(startupCtx, chromedpContext.Browser)
			err = cdpbrowser.SetPermission(
				&cdpbrowser.PermissionDescriptor{Name: "microphone"},
				cdpbrowser.PermissionSettingGranted,
			).WithOrigin(origin).Do(browserExecutorCtx)
		}
	}
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
		_ = session.Close()
		return nil, fmt.Errorf("could not open the Duck.ai voice window in Chrome/Chromium: %w", err)
	}
	chromedpContext := chromedp.FromContext(browserCtx)
	browser := chromedpContext.Browser
	if browser == nil {
		_ = session.Close()
		return nil, fmt.Errorf("Chromium did not start a browser connection")
	}
	if err := setChromiumWindowSize(browserCtx, chromedpContext, width, height); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("could not size the Duck.ai voice window: %w", err)
	}
	session.browserLost = browser.LostConnection
	session.done = monitorChromiumDone(browserCtx, session.browserLost)
	return session, nil
}

func loopbackVoiceOrigin(address string) (string, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" {
		return "", fmt.Errorf("voice microphone permission requires an HTTP address on 127.0.0.1")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func setChromiumWindowSize(ctx context.Context, browserContext *chromedp.Context, width, height int) error {
	if browserContext.Target == nil {
		return fmt.Errorf("Chromium did not create a voice window target")
	}
	browserExecutorCtx := cdp.WithExecutor(ctx, browserContext.Browser)
	windowID, _, err := cdpbrowser.GetWindowForTarget().WithTargetID(browserContext.Target.TargetID).Do(browserExecutorCtx)
	if err != nil {
		return err
	}
	return cdpbrowser.SetWindowBounds(windowID, &cdpbrowser.Bounds{
		Width:       int64(width),
		Height:      int64(height),
		WindowState: cdpbrowser.WindowStateNormal,
	}).Do(browserExecutorCtx)
}

func (s *chromiumSession) Close() error {
	s.closeOnce.Do(func() {
		if s.ctx.Err() == nil {
			select {
			case <-s.browserLost:
				// The user already closed the dedicated Chromium window.
			default:
				closeCtx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
				s.closeErr = chromedp.Cancel(closeCtx)
				cancel()
			}
		}
		s.cancelBrowser()
		s.cancelAllocator()
	})
	return s.closeErr
}

func (s *chromiumSession) Done() <-chan struct{} {
	return s.done
}

func monitorChromiumDone(ctx context.Context, browserLost <-chan struct{}) <-chan struct{} {
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
