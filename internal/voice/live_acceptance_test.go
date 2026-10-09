package voice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/chat"

	"github.com/chromedp/chromedp"
)

// This opt-in check verifies live Duck.ai proof and signaling compatibility.
// It uses Chromium's fake microphone and closes the peer before sending audio.
func TestLiveVoiceSignalingAcceptsCurrentProof(t *testing.T) {
	if os.Getenv("DUCKAI_VOICE_LIVE_TEST") != "1" {
		t.Skip("set DUCKAI_VOICE_LIVE_TEST=1 to verify live Duck.ai voice signaling")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	proof, err := (CurrentProofProvider{}).Capture(ctx)
	if err != nil {
		t.Fatalf("capture live Duck.ai proof: %v", err)
	}
	proof.JourneyID, err = newJourneyID()
	if err != nil {
		t.Fatalf("create voice journey ID: %v", err)
	}
	client := &DuckAIClient{HTTPClient: &http.Client{Timeout: 30 * time.Second}, BaseURL: defaultDuckAIBaseURL}
	ice, err := client.GetICEServers(ctx, proof)
	if err != nil {
		t.Fatalf("request live ICE configuration: %v", err)
	}
	offer, err := makeFakeMicrophoneOffer(ctx, ice)
	if err != nil {
		t.Fatalf("create synthetic voice offer: %v", err)
	}
	answer, err := client.CreateSession(ctx, proof, offer)
	if err != nil {
		t.Fatalf("Duck.ai rejected the current proof or SDP offer: %v", err)
	}
	if !strings.HasPrefix(answer, "v=0\r\n") {
		t.Fatal("Duck.ai returned an invalid SDP answer")
	}
}

func makeFakeMicrophoneOffer(ctx context.Context, ice ICEConfiguration) (string, error) {
	executable, err := chat.BrowserExecutable()
	if err != nil {
		return "", err
	}
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options,
		chromedp.ExecPath(executable),
		chromedp.Flag("headless", "new"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("use-fake-device-for-media-stream", true),
		chromedp.Flag("use-fake-ui-for-media-stream", true),
	)
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, options...)
	defer cancelAllocator()
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()
	iceJSON, err := json.Marshal(ice)
	if err != nil {
		return "", err
	}
	var offer string
	var offerErr string
	setup := `(async () => {
  let stream;
  let peer;
  try {
    stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    peer = new RTCPeerConnection({ ...window.__voiceIce, iceTransportPolicy: "relay" });
    peer.addTrack(stream.getAudioTracks()[0], stream);
    peer.createDataChannel("duckai-voice-session");
    const localOffer = await peer.createOffer();
    await peer.setLocalDescription(localOffer);
    window.__voiceOffer = peer.localDescription.sdp;
  } catch (error) {
    window.__voiceOfferError = String(error);
  } finally {
    if (stream) stream.getTracks().forEach(track => track.stop());
    if (peer) peer.close();
    window.__voiceOfferReady = true;
  }
})()`
	err = chromedp.Run(browserCtx,
		chromedp.Navigate(defaultDuckAIBaseURL),
		chromedp.Evaluate("window.__voiceIce = "+string(iceJSON)+"; window.__voiceOfferReady = false", nil),
		chromedp.Evaluate(setup, nil),
		chromedp.Poll("window.__voiceOfferReady === true", nil, chromedp.WithPollingTimeout(20*time.Second)),
		chromedp.Evaluate("window.__voiceOfferError || ''", &offerErr),
		chromedp.Evaluate("window.__voiceOffer || ''", &offer),
	)
	if err != nil {
		return "", err
	}
	if offerErr != "" {
		return "", errors.New(offerErr)
	}
	if offer == "" {
		return "", errors.New("browser produced an empty offer")
	}
	return offer, nil
}
