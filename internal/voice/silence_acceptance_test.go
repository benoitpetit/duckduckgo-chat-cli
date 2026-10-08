package voice

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/chat"
	"github.com/chromedp/chromedp"
)

// This opt-in live check connects with a generated silent microphone and
// records only event types, so it can distinguish server-initiated responses
// from responses following input VAD without transmitting spoken content.
func TestLiveVoiceSilenceDoesNotTriggerResponse(t *testing.T) {
	if os.Getenv("DUCKAI_VOICE_SILENCE_TEST") != "1" {
		t.Skip("set DUCKAI_VOICE_SILENCE_TEST=1 to check Duck.ai silence handling")
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

	wavePath := writeSilentVoiceWAV(t)
	executable, err := chat.BrowserExecutable()
	if err != nil {
		t.Fatalf("find Chromium executable: %v", err)
	}
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options,
		chromedp.ExecPath(executable),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("use-fake-device-for-media-stream", true),
		chromedp.Flag("use-fake-ui-for-media-stream", true),
		chromedp.Flag("use-file-for-fake-audio-capture", wavePath),
	)
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, options...)
	defer cancelAllocator()
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	iceJSON, err := json.Marshal(ice)
	if err != nil {
		t.Fatalf("encode ICE configuration: %v", err)
	}
	setup := `(async () => {
  try {
    window.__voiceEvents = [];
    window.__voiceStream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: false, channelCount: 1, sampleRate: 48000 }
    });
    window.__voicePeer = new RTCPeerConnection({ ...window.__voiceIce, iceTransportPolicy: "relay" });
    window.__voicePeer.addTrack(window.__voiceStream.getAudioTracks()[0], window.__voiceStream);
    const channel = window.__voicePeer.createDataChannel("duckai-voice-session");
    channel.onopen = () => { window.__voiceDataChannelOpen = true; };
    channel.onmessage = event => {
      try { window.__voiceEvents.push(JSON.parse(event.data).type || "<missing-type>"); }
      catch (_) { window.__voiceEvents.push("<invalid-json>"); }
    };
    const offer = await window.__voicePeer.createOffer();
    await window.__voicePeer.setLocalDescription(offer);
    window.__voiceOffer = window.__voicePeer.localDescription.sdp;
  } catch (error) {
    window.__voiceError = String(error);
  } finally {
    window.__voiceSetupReady = true;
  }
})()`
	if err := chromedp.Run(browserCtx,
		chromedp.Navigate(defaultDuckAIBaseURL),
		chromedp.Evaluate("window.__voiceIce = "+string(iceJSON)+"; window.__voiceSetupReady = false; window.__voiceDataChannelOpen = false", nil),
		chromedp.Evaluate(setup, nil),
		chromedp.Poll("window.__voiceSetupReady === true", nil, chromedp.WithPollingTimeout(20*time.Second)),
	); err != nil {
		t.Fatalf("create silent microphone offer: %v", err)
	}
	var offerErr, offer string
	if err := chromedp.Run(browserCtx,
		chromedp.Evaluate("window.__voiceError || ''", &offerErr),
		chromedp.Evaluate("window.__voiceOffer || ''", &offer),
	); err != nil {
		t.Fatalf("read silent microphone offer: %v", err)
	}
	if offerErr != "" {
		t.Fatalf("create silent microphone offer: %s", offerErr)
	}
	answer, err := client.CreateSession(ctx, proof, offer)
	if err != nil {
		t.Fatalf("create live Duck.ai voice session: %v", err)
	}
	encodedAnswer, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("encode SDP answer: %v", err)
	}
	applyAnswer := fmt.Sprintf(`window.__voicePeer.setRemoteDescription({type:"answer",sdp:%s})
  .then(() => { window.__voiceRemoteReady = true; })
  .catch(error => { window.__voiceError = String(error); window.__voiceRemoteReady = true; })`, encodedAnswer)
	if err := chromedp.Run(browserCtx,
		chromedp.Evaluate("window.__voiceRemoteReady = false", nil),
		chromedp.Evaluate(applyAnswer, nil),
		chromedp.Poll("window.__voiceRemoteReady === true", nil, chromedp.WithPollingTimeout(20*time.Second)),
		chromedp.Poll("window.__voiceDataChannelOpen === true", nil, chromedp.WithPollingTimeout(20*time.Second)),
	); err != nil {
		t.Fatalf("connect silent voice session: %v", err)
	}
	var remoteErr string
	if err := chromedp.Run(browserCtx, chromedp.Evaluate("window.__voiceError || ''", &remoteErr)); err != nil {
		t.Fatalf("read session connection state: %v", err)
	}
	if remoteErr != "" {
		t.Fatalf("apply Duck.ai SDP answer: %s", remoteErr)
	}
	if err := chromedp.Run(browserCtx,
		chromedp.Evaluate("window.__voiceSilenceDone = false; setTimeout(() => { window.__voiceSilenceDone = true; }, 10000)", nil),
		chromedp.Poll("window.__voiceSilenceDone === true", nil, chromedp.WithPollingTimeout(15*time.Second)),
	); err != nil {
		t.Fatalf("observe silent voice session: %v", err)
	}

	var events []string
	if err := chromedp.Run(browserCtx, chromedp.Evaluate("window.__voiceEvents", &events)); err != nil {
		t.Fatalf("read sanitized voice event types: %v", err)
	}
	for _, event := range events {
		if event == "input_audio_buffer.speech_started" || event == "response.created" || event == "output_audio_buffer.started" {
			t.Fatalf("Duck.ai emitted %q during 10 seconds of synthetic silence; events: %v", event, events)
		}
	}
}

func writeSilentVoiceWAV(t *testing.T) string {
	t.Helper()
	const sampleRate = 48000
	const durationSeconds = 1
	dataSize := uint32(sampleRate * durationSeconds * 2)
	wave := make([]byte, 44+dataSize)
	copy(wave[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wave[4:8], uint32(len(wave)-8))
	copy(wave[8:12], "WAVE")
	copy(wave[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wave[16:20], 16)
	binary.LittleEndian.PutUint16(wave[20:22], 1)
	binary.LittleEndian.PutUint16(wave[22:24], 1)
	binary.LittleEndian.PutUint32(wave[24:28], sampleRate)
	binary.LittleEndian.PutUint32(wave[28:32], sampleRate*2)
	binary.LittleEndian.PutUint16(wave[32:34], 2)
	binary.LittleEndian.PutUint16(wave[34:36], 16)
	copy(wave[36:40], "data")
	binary.LittleEndian.PutUint32(wave[40:44], dataSize)
	path := filepath.Join(t.TempDir(), "silence.wav")
	if err := os.WriteFile(path, wave, 0o600); err != nil {
		t.Fatalf("write silent audio source: %v", err)
	}
	return path
}
