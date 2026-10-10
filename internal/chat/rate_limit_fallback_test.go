package chat

import (
	"context"
	"errors"
	"testing"

	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/models"
)

func TestLegacyBrowserFallbackSettingNeverEnablesAutomaticBrowser(t *testing.T) {
	cfg := &config.Config{
		ExportDir: t.TempDir(),
		Dashboard: config.DashboardConfig{RetentionDays: 30},
		RateLimit: config.RateLimitConfig{OpenBrowser: true},
	}
	chat := NewChat("", "", "", "", models.GPT54Mini, cfg)
	if chat.BrowserRetry != nil || chat.visibleProofBrowserFactory != nil {
		t.Fatal("legacy setting enabled a visible browser or Chrome extension retry")
	}
	chat.reportChatFailure(context.Background(), &RateLimitError{Code: "ERR_RATE_LIMIT"})
	if chat.BrowserRetry != nil || chat.visibleProofBrowserFactory != nil {
		t.Fatal("reporting a rate limit enabled browser fallback")
	}
}

func TestReportChatFailureSkipsCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	(&Chat{}).reportChatFailure(ctx, errors.Join(ErrRateLimited, context.Canceled))
}
