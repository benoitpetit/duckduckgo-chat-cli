package tray

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestIPCRejectsUnauthenticatedRequests(t *testing.T) {
	dispatched := false
	response := processRequest(context.Background(), ipcRequest{Token: "wrong", Action: ActionOpenTextChat}, "secret", func(context.Context, Action) (Response, error) {
		dispatched = true
		return Response{OK: true}, nil
	})
	if response.OK || response.Error != "unauthorized" {
		t.Fatalf("unauthorized response = %+v, want unauthorized", response)
	}
	if dispatched {
		t.Fatal("unauthenticated action was dispatched")
	}
}

func TestIPCRejectsUnknownActions(t *testing.T) {
	dispatched := false
	response := processRequest(context.Background(), ipcRequest{Token: "secret", Action: Action("run-shell-command")}, "secret", func(context.Context, Action) (Response, error) {
		dispatched = true
		return Response{OK: true}, nil
	})
	if response.OK || response.Error != "unknown action" {
		t.Fatalf("unknown action response = %+v, want unknown action", response)
	}
	if dispatched {
		t.Fatal("unknown action was dispatched")
	}
}

func TestIPCDispatchesAuthenticatedAction(t *testing.T) {
	response := processRequest(context.Background(), ipcRequest{Token: "secret", Action: ActionOpenTextChat}, "secret", func(_ context.Context, action Action) (Response, error) {
		if action != ActionOpenTextChat {
			t.Fatalf("dispatched action = %q, want %q", action, ActionOpenTextChat)
		}
		return Response{Message: "opened"}, nil
	})
	if !response.OK || response.Message != "opened" {
		t.Fatalf("dispatch response = %+v, want successful open response", response)
	}
}

func TestIPCRejectsOversizedRequestLine(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	responseReady := make(chan Response, 1)
	go func() {
		responseReady <- handleIPCConnection(context.Background(), loopbackConn{Conn: server}, "secret", nil)
	}()
	go func() {
		_, _ = client.Write([]byte("{\"token\":\"secret\",\"action\":\"open-text-chat\",\"extra\":\"" + strings.Repeat("x", maxIPCMessageBytes) + "\"}\n"))
	}()
	var response Response
	if err := json.NewDecoder(client).Decode(&response); err != nil {
		t.Fatal(err)
	}
	<-responseReady
	if response.OK || response.Error != "request too large" {
		t.Fatalf("oversized request response = %+v, want request too large", response)
	}
}

type loopbackConn struct{ net.Conn }

func (c loopbackConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

func TestIPCRequestLineRoundTripsJSON(t *testing.T) {
	request := ipcRequest{
		Token:        "secret",
		Action:       ActionOpenOrShowVoice,
		VoiceOwnerID: "cli-owner-123",
		VoiceContext: []ConversationMessage{
			{Role: "user", Content: "Keep the CLI conversation going."},
			{Role: "assistant", Content: "I will carry it into voice."},
		},
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeIPCRequest(bufio.NewReader(strings.NewReader(string(data) + "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, request) {
		t.Fatalf("decoded request = %+v, want %+v", got, request)
	}
}

func TestProcessRequestMakesBoundedVoiceContextAvailableToHandler(t *testing.T) {
	contextMessages := []ConversationMessage{
		{Role: "user", Content: "Text turn one"},
		{Role: "assistant", Content: "Text reply one"},
	}
	var got []ConversationMessage
	response := processRequest(context.Background(), ipcRequest{
		Token:        "secret",
		Action:       ActionOpenOrShowVoice,
		VoiceContext: contextMessages,
	}, "secret", func(ctx context.Context, action Action) (Response, error) {
		if action != ActionOpenOrShowVoice {
			t.Fatalf("action = %q, want voice launch", action)
		}
		got = VoiceContextFromContext(ctx)
		return Response{VoiceActive: true}, nil
	})
	if !response.OK || !reflect.DeepEqual(got, contextMessages) {
		t.Fatalf("response/context = %+v / %+v, want success / %+v", response, got, contextMessages)
	}
}

func TestProcessRequestAttachesVoiceOwnerToStartAndStatusActions(t *testing.T) {
	for _, action := range []Action{ActionOpenOrShowVoice, ActionVoiceStatus, ActionVoiceTranscriptAck} {
		t.Run(string(action), func(t *testing.T) {
			var got string
			response := processRequest(context.Background(), ipcRequest{Token: "secret", Action: action, VoiceOwnerID: "cli-session-123"}, "secret", func(ctx context.Context, gotAction Action) (Response, error) {
				if gotAction != action {
					t.Fatalf("action = %q, want %q", gotAction, action)
				}
				got = VoiceOwnerFromContext(ctx)
				return Response{OK: true}, nil
			})
			if !response.OK || got != "cli-session-123" {
				t.Fatalf("response/owner = %+v / %q, want success / client owner", response, got)
			}
		})
	}
}

func TestProcessRequestRejectsVoiceOwnerOnOtherActions(t *testing.T) {
	called := false
	response := processRequest(context.Background(), ipcRequest{Token: "secret", Action: ActionOpenTextChat, VoiceOwnerID: "cli-session-123"}, "secret", func(context.Context, Action) (Response, error) {
		called = true
		return Response{OK: true}, nil
	})
	if response.Error == "" || called {
		t.Fatalf("response = %+v, called=%t; want rejection before dispatch", response, called)
	}
}

func TestProcessRequestRequiresOwnerForVoiceResultActions(t *testing.T) {
	for _, action := range []Action{ActionVoiceStatus, ActionVoiceTranscriptAck} {
		t.Run(string(action), func(t *testing.T) {
			called := false
			response := processRequest(context.Background(), ipcRequest{Token: "secret", Action: action}, "secret", func(context.Context, Action) (Response, error) {
				called = true
				return Response{OK: true}, nil
			})
			if response.Error == "" || called {
				t.Fatalf("response = %+v, called=%t; want missing owner to be rejected", response, called)
			}
		})
	}
}

func TestBoundVoiceContextKeepsOnlyRecentTextMessagesWithinLimits(t *testing.T) {
	messages := []ConversationMessage{{Role: "system", Content: "must not be sent"}}
	for i := 0; i < maxVoiceContextMessages+2; i++ {
		messages = append(messages, ConversationMessage{Role: "user", Content: fmt.Sprintf("turn-%02d", i)})
	}
	got := BoundVoiceContext(messages)
	if len(got) != maxVoiceContextMessages || got[0].Content != "turn-02" || got[len(got)-1].Content != "turn-25" {
		t.Fatalf("bounded context = first %q, last %q, %d messages; want turn-02 through turn-25, %d messages", got[0].Content, got[len(got)-1].Content, len(got), maxVoiceContextMessages)
	}
	if got[0].Role != "user" {
		t.Fatalf("role %q was included, want only user/assistant messages", got[0].Role)
	}
}

func TestBoundVoiceContextClipsOldestTextToByteLimitWithoutBreakingUTF8(t *testing.T) {
	content := strings.Repeat("é", maxVoiceContextMessageBytes/2) + "a"
	got := BoundVoiceContext([]ConversationMessage{{Role: "assistant", Content: content}})
	if len(got) != 1 || len(got[0].Content) != maxVoiceContextMessageBytes-1 || !utf8.ValidString(got[0].Content) {
		gotBytes := 0
		valid := false
		if len(got) == 1 {
			gotBytes = len(got[0].Content)
			valid = utf8.ValidString(got[0].Content)
		}
		t.Fatalf("bounded content has %d bytes and valid UTF-8=%t, want %d bytes and valid UTF-8", gotBytes, valid, maxVoiceContextMessageBytes-1)
	}
}

func TestAppendVoiceTranscriptPreservesTurnsBeyondTheInitialContextWindow(t *testing.T) {
	messages := make([]ConversationMessage, 0, maxVoiceContextMessages+1)
	for index := 0; index < maxVoiceContextMessages+1; index++ {
		messages, _ = AppendVoiceTranscript(messages, ConversationMessage{Role: "user", Content: fmt.Sprintf("voice turn %d", index)})
	}
	if len(messages) != maxVoiceContextMessages+1 || messages[0].Content != "voice turn 0" {
		t.Fatalf("stored transcripts = %d first=%q; want all %d turns in order", len(messages), messages[0].Content, maxVoiceContextMessages+1)
	}
}

func TestAppendVoiceTranscriptDeduplicatesRetriesByStableItemID(t *testing.T) {
	first := ConversationMessage{ID: "item-123", Role: "user", Content: "same turn"}
	messages, truncated := AppendVoiceTranscript(nil, first)
	if truncated {
		t.Fatal("first transcript was unexpectedly truncated")
	}
	messages, truncated = AppendVoiceTranscript(messages, first)
	if truncated || len(messages) != 1 || messages[0] != first {
		t.Fatalf("retried transcript = %+v, truncated=%t; want one original item", messages, truncated)
	}
}

func TestAppendVoiceTranscriptAcceptsLongItemWithinTheSessionLimit(t *testing.T) {
	message := ConversationMessage{ID: "long-item", Role: "assistant", Content: strings.Repeat("long reply ", 3000)}
	messages, truncated := AppendVoiceTranscript(nil, message)
	if truncated || len(messages) != 1 || messages[0] != message {
		t.Fatalf("long transcript = %d messages, truncated=%t; want the complete message", len(messages), truncated)
	}
}

func TestAppendVoiceTranscriptRecordsOmissionAndContinuesWithLaterItems(t *testing.T) {
	messages, truncated := AppendVoiceTranscript(nil, ConversationMessage{ID: "long-item", Role: "user", Omitted: true})
	if !truncated || len(messages) != 0 {
		t.Fatalf("omitted transcript = %+v, truncated=%t; want no text and a truncation signal", messages, truncated)
	}
	message := ConversationMessage{ID: "next-item", Role: "assistant", Content: "later reply"}
	messages, laterTruncated := AppendVoiceTranscript(messages, message)
	if laterTruncated || len(messages) != 1 || messages[0] != message {
		t.Fatalf("transcript after omission = %+v, truncated=%t; want the later reply retained", messages, laterTruncated)
	}
}

func TestAppendVoiceTranscriptReportsWhenTransportLimitIsReached(t *testing.T) {
	messages := make([]ConversationMessage, 0, maxVoiceTranscriptMessages)
	truncated := false
	for index := 0; index < maxVoiceTranscriptMessages; index++ {
		messages, truncated = AppendVoiceTranscript(messages, ConversationMessage{Role: "user", Content: "turn"})
		if truncated {
			t.Fatalf("unexpected truncation at message %d", index)
		}
	}
	messages, truncated = AppendVoiceTranscript(messages, ConversationMessage{Role: "assistant", Content: "one more"})
	if !truncated || len(messages) != maxVoiceTranscriptMessages {
		t.Fatalf("stored=%d truncated=%t; want bounded count and an explicit truncation signal", len(messages), truncated)
	}
}

func TestAppendVoiceTranscriptKeepsEscapedIPCResponseBelowFrameLimit(t *testing.T) {
	content := strings.Repeat("<", maxVoiceTranscriptItemBytes)
	var messages []ConversationMessage
	for {
		var truncated bool
		messages, truncated = AppendVoiceTranscript(messages, ConversationMessage{Role: "assistant", Content: content})
		if truncated {
			break
		}
	}
	encoded, err := json.Marshal(Response{VoiceMessages: messages})
	if err != nil {
		t.Fatalf("marshal bounded response: %v", err)
	}
	if len(encoded) > maxIPCMessageBytes {
		t.Fatalf("encoded voice response has %d bytes, over IPC limit %d", len(encoded), maxIPCMessageBytes)
	}
}
