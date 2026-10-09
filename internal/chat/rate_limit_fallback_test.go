package chat

import (
	"errors"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/config"
)

func recordingOpener(recorded *[]string) browserOpener {
	return func(url string) error {
		*recorded = append(*recorded, url)
		return nil
	}
}

func rateLimitConfig(openBrowser bool, cooldownMinutes int) *config.Config {
	return &config.Config{RateLimit: config.RateLimitConfig{OpenBrowser: openBrowser, CooldownMinutes: cooldownMinutes}}
}

func TestOfferRateLimitFallbackOpensDuckAIBrowser(t *testing.T) {
	var opened []string
	c := &Chat{}
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

	c.offerRateLimitFallback(rateLimitConfig(true, 10), now, recordingOpener(&opened))

	if len(opened) != 1 {
		t.Fatalf("opened %d URLs, want 1", len(opened))
	}
	if opened[0] != rateLimitFallbackURL {
		t.Fatalf("opened URL = %q, want %q", opened[0], rateLimitFallbackURL)
	}
}

func TestOfferRateLimitFallbackSkipsOpenDuringCooldown(t *testing.T) {
	var opened []string
	c := &Chat{}
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

	c.offerRateLimitFallback(rateLimitConfig(true, 10), now, recordingOpener(&opened))
	c.offerRateLimitFallback(rateLimitConfig(true, 10), now.Add(4*time.Minute), recordingOpener(&opened))

	if len(opened) != 1 {
		t.Fatalf("opened %d URLs, want 1 during cooldown", len(opened))
	}
}

func TestOfferRateLimitFallbackOpensAgainAfterCooldown(t *testing.T) {
	var opened []string
	c := &Chat{}
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

	c.offerRateLimitFallback(rateLimitConfig(true, 10), now, recordingOpener(&opened))
	c.offerRateLimitFallback(rateLimitConfig(true, 10), now.Add(11*time.Minute), recordingOpener(&opened))

	if len(opened) != 2 {
		t.Fatalf("opened %d URLs, want 2 after cooldown", len(opened))
	}
}

func TestOfferRateLimitFallbackStaysQuietWhenDisabled(t *testing.T) {
	var opened []string
	c := &Chat{}
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

	c.offerRateLimitFallback(rateLimitConfig(false, 10), now, recordingOpener(&opened))

	if len(opened) != 0 {
		t.Fatalf("opened %d URLs, want 0 when disabled", len(opened))
	}
}

func TestOfferRateLimitFallbackKeepsCooldownWithMissingConfig(t *testing.T) {
	var opened []string
	c := &Chat{}
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

	c.offerRateLimitFallback(nil, now, recordingOpener(&opened))

	if len(opened) != 0 {
		t.Fatalf("opened %d URLs, want 0 without config", len(opened))
	}
}

func TestOfferRateLimitFallbackRetriesAfterFailedLaunch(t *testing.T) {
	var attempts int
	c := &Chat{}
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	failing := func(string) error {
		attempts++
		return errors.New("no browser")
	}

	c.offerRateLimitFallback(rateLimitConfig(true, 10), now, failing)
	c.offerRateLimitFallback(rateLimitConfig(true, 10), now.Add(time.Minute), failing)

	if attempts != 2 {
		t.Fatalf("launch attempts = %d, want 2 after failed launches", attempts)
	}
}

func TestReportChatFailureOffersBrowserOnRateLimit(t *testing.T) {
	var opened []string
	c := &Chat{}
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

	c.reportChatFailure(rateLimitConfig(true, 10), &RateLimitError{Code: "rate_limited"}, now, recordingOpener(&opened))

	if len(opened) != 1 {
		t.Fatalf("opened %d URLs, want 1 on rate limit", len(opened))
	}
}

func TestReportChatFailureLeavesBrowserAloneForOtherErrors(t *testing.T) {
	var opened []string
	c := &Chat{}
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

	c.reportChatFailure(rateLimitConfig(true, 10), errors.New("connection reset"), now, recordingOpener(&opened))

	if len(opened) != 0 {
		t.Fatalf("opened %d URLs, want 0 for non rate-limit errors", len(opened))
	}
}

func TestBrowserLaunchCommandUsesPlatformOpener(t *testing.T) {
	for _, tc := range []struct {
		goos string
		want []string
	}{
		{goos: "darwin", want: []string{"open", "https://duck.ai/"}},
		{goos: "linux", want: []string{"xdg-open", "https://duck.ai/"}},
		{goos: "windows", want: []string{"rundll32", "url.dll,FileProtocolHandler", "https://duck.ai/"}},
	} {
		cmd := browserLaunchCommand(tc.goos, rateLimitFallbackURL)
		// Args keeps the requested opener name; Path holds the PATH-resolved
		// binary, which varies by host.
		if len(cmd.Args) != len(tc.want) {
			t.Fatalf("%s: args = %q, want %q", tc.goos, cmd.Args, tc.want)
		}
		for i := range tc.want {
			if cmd.Args[i] != tc.want[i] {
				t.Fatalf("%s: arg %d = %q, want %q", tc.goos, i, cmd.Args[i], tc.want[i])
			}
		}
	}
}
