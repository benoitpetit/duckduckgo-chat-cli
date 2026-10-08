package chat

import (
	"errors"
	"os/exec"
	"runtime"
	"time"

	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/ui"
)

// rateLimitFallbackURL is the last-resort destination opened when Duck.ai
// rate limits the CLI. Duck.ai publishes no stable URL that pre-fills a
// prompt, so the plain chat page is the only reliable target.
const rateLimitFallbackURL = "https://duck.ai/"

// browserOpener hands a URL to the user's default browser. It exists so the
// fallback policy can be exercised without launching a real browser.
type browserOpener func(url string) error

// reportChatFailure surfaces a failed request to the user and, when Duck.ai
// rate limited this CLI, offers the browser fallback as a last resort.
func (c *Chat) reportChatFailure(cfg *config.Config, err error, now time.Time, open browserOpener) {
	ui.Errorln("Error: %v", err)
	if errors.Is(err, ErrRateLimited) {
		c.offerRateLimitFallback(cfg, now, open)
	}
}

// offerRateLimitFallback is the last-resort response to a Duck.ai 429: it
// opens duck.ai in the default browser so the conversation can continue there
// instead of stalling in the CLI. A cooldown keeps a burst of consecutive
// rate-limited messages from opening one tab per message. Failures are
// reported and never fatal, because the original rate-limit error has already
// been surfaced to the user.
func (c *Chat) offerRateLimitFallback(cfg *config.Config, now time.Time, open browserOpener) {
	if cfg == nil || !cfg.RateLimit.OpenBrowser {
		return
	}

	cooldown := time.Duration(cfg.RateLimit.CooldownMinutes) * time.Minute
	if !c.rateLimitFallbackOpen.IsZero() && now.Sub(c.rateLimitFallbackOpen) < cooldown {
		ui.Warningln("Duck.ai is still rate limiting this conversation. Continue at %s in your browser.", rateLimitFallbackURL)
		return
	}

	if err := open(rateLimitFallbackURL); err != nil {
		ui.Warningln("Could not open %s automatically (%v). Open it manually to continue.", rateLimitFallbackURL, err)
		return
	}

	c.rateLimitFallbackOpen = now
	ui.Warningln("Duck.ai rate limited this CLI. Opened %s in your browser so you can continue there.", rateLimitFallbackURL)
}

// openDefaultBrowser launches the platform's default browser without waiting
// for it to exit, so a slow or already-running browser cannot stall the CLI.
func openDefaultBrowser(target string) error {
	cmd := browserLaunchCommand(runtime.GOOS, target)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap the launcher process so it does not linger as a zombie; the browser
	// it spawned is unaffected.
	go func() { _ = cmd.Wait() }()
	return nil
}

// browserLaunchCommand builds the platform's default-browser launch command.
// goos is a parameter so each platform's argument list is verifiable without
// running a browser on the host running the tests.
func browserLaunchCommand(goos, target string) *exec.Cmd {
	switch goos {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		return exec.Command("open", target)
	default:
		return exec.Command("xdg-open", target)
	}
}
