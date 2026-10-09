package voice

import (
	"context"
	"os"
	"testing"
	"time"
)

// This opt-in test checks /speak's actual Chromium window lifecycle without
// requesting microphone access or contacting Duck.ai.
func TestVoiceRunKeepsChromiumWindowOpen(t *testing.T) {
	if os.Getenv("DUCKAI_VOICE_BROWSER_SMOKE") != "1" {
		t.Skip("set DUCKAI_VOICE_BROWSER_SMOKE=1 to open a short-lived voice interface")
	}
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Dependencies{
			Proof:     &testProofProvider{},
			Signaling: &testSignalingClient{},
		}, ChromiumOpener{})
	}()

	select {
	case err := <-done:
		t.Fatalf("voice run returned before the window was closed: %v", err)
	case <-time.After(3 * time.Second):
	}
	if !StopActive() {
		t.Fatal("StopActive() = false, want to stop the live voice window")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("voice run error after StopActive() = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("voice run did not close after StopActive()")
	}
}
