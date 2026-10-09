package chat

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/models"
)

// This smoke test exercises the proof capture and the actual chat transport.
func TestLivePlainChat(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_TEST=1 for the live Duck.ai request")
	}
	t.Cleanup(func() {
		if err := ShutdownBrowser(); err != nil {
			t.Errorf("ShutdownBrowser() failed: %v", err)
		}
	})
	chat := NewChat("", "", "", "", models.GPT54Mini, &config.Config{ExportDir: t.TempDir(), Dashboard: config.DashboardConfig{RetentionDays: 30}})
	chat.Messages = []Message{{Role: "user", Content: "Reply PONG only"}}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	response, err := chat.FetchContext(ctx, "Reply PONG only")
	if err != nil {
		t.Fatalf("live chat transport failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "data: [DONE]") || !strings.Contains(string(body), `"message":`) {
		t.Fatalf("chat response did not complete: %s", body)
	}
}
