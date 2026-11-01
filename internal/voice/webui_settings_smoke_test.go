package voice

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/config"

	"github.com/chromedp/chromedp"
)

type webUISettingsState struct {
	Label          string  `json:"label"`
	SubtitleHidden bool    `json:"subtitleHidden"`
	Volume         float64 `json:"volume"`
	StartHidden    bool    `json:"startHidden"`
	StartDisabled  bool    `json:"startDisabled"`
	Connection     string  `json:"connection"`
	VoiceState     string  `json:"voiceState"`
}

func TestSpeakWebUILoadsConfiguredSettingsSmoke(t *testing.T) {
	if os.Getenv("DUCKAI_VOICE_WEBUI_SMOKE") != "1" {
		t.Skip("set DUCKAI_VOICE_WEBUI_SMOKE=1 to check the voice settings UI in Chromium")
	}
	settings := config.SpeakConfig{
		Voice:          "voice-3",
		StartMuted:     true,
		ShowTranscript: false,
		Volume:         35,
		ReadinessSound: false,
		AutoStart:      false,
	}
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	handler, err := NewHandler(Dependencies{
		Proof:     &testProofProvider{},
		Signaling: &testSignalingClient{},
		Speak:     settings,
	}, testVoiceToken, origin)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	server.Config.Handler = handler
	server.Start()
	defer server.Close()

	timeoutCtx, cancelTimeout := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelTimeout()
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options,
		chromedp.Flag("headless", "new"),
		chromedp.Flag("mute-audio", true),
	)
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(timeoutCtx, options...)
	defer cancelAllocator()
	ctx, cancel := chromedp.NewContext(allocatorCtx)
	defer cancel()

	err = chromedp.Do(ctx,
		chromedp.EmulateViewport(480, 500),
		chromedp.Navigate(server.URL+"/#"+testVoiceToken),
		chromedp.Poll[chromedp.Void](`(() => {
			const label = document.querySelector("#voice-label");
			const start = document.querySelector("#start-button");
			return Boolean(label && start && label.textContent === "Voice 3 · Confident and optimistic" && !start.disabled);
		})()`, chromedp.WithPollingTimeout(10*time.Second)),
	)
	if err != nil {
		t.Fatalf("load voice settings UI: %v", err)
	}
	got, err := chromedp.Run(ctx, chromedp.Evaluate[webUISettingsState](`(() => ({
		label: document.querySelector("#voice-label").textContent,
		subtitleHidden: document.querySelector("#subtitle-slot").hidden,
		volume: document.querySelector("#remote-audio").volume,
		startHidden: document.querySelector("#start-button").hidden,
		startDisabled: document.querySelector("#start-button").disabled,
		connection: document.querySelector("#connection-state").textContent,
		voiceState: document.querySelector("#voice-state").textContent,
	}))()`))
	if err != nil {
		t.Fatalf("read voice settings UI: %v", err)
	}
	if got.Label != "Voice 3 · Confident and optimistic" {
		t.Errorf("voice label = %q, want configured voice", got.Label)
	}
	if !got.SubtitleHidden {
		t.Error("subtitle slot is visible although show_transcript is false")
	}
	if got.Volume < 0.349 || got.Volume > 0.351 {
		t.Errorf("remote audio volume = %v, want 0.35", got.Volume)
	}
	if got.StartHidden || got.StartDisabled {
		t.Errorf("start button hidden=%v disabled=%v, want manual start available", got.StartHidden, got.StartDisabled)
	}
	if got.Connection != "Ready" {
		t.Errorf("connection state = %q, want Ready", got.Connection)
	}
	if got.VoiceState != "Voice companion ready" {
		t.Errorf("voice state = %q, want idle companion state before manual start", got.VoiceState)
	}
}
