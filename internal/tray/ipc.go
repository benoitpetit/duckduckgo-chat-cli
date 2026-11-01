package tray

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"duckduckgo-chat-cli/internal/config"
)

const (
	maxIPCMessageBytes          = 1 << 20
	maxVoiceContextMessages     = 24
	maxVoiceContextBytes        = 48 << 10
	maxVoiceContextMessageBytes = 8 << 10
	maxVoiceTranscriptMessages  = 128
	maxVoiceTranscriptBytes     = 700 << 10
	maxVoiceTranscriptItemBytes = 64 << 10
)

type Action string

const (
	ActionServiceStatus      Action = "service-status"
	ActionOpenTextChat       Action = "open-text-chat"
	ActionOpenDashboard      Action = "open-dashboard"
	ActionOpenOrShowVoice    Action = "open-or-show-voice"
	ActionToggleVoice        Action = "toggle-voice"
	ActionMinimizeVoice      Action = "minimize-voice"
	ActionConfigureShort     Action = "configure-shortcuts"
	ActionReloadConfig       Action = "reload-config"
	ActionVoiceStatus        Action = "voice-status"
	ActionVoiceTranscriptAck Action = "voice-transcript-ack"
	ActionQuitService        Action = "quit-service"
)

type ipcRequest struct {
	Token        string                `json:"token"`
	Action       Action                `json:"action"`
	VoiceContext []ConversationMessage `json:"voice_context,omitempty"`
	VoiceOwnerID string                `json:"voice_owner_id,omitempty"`
}

// ConversationMessage carries text between the CLI and its local voice session.
type ConversationMessage struct {
	ID      string `json:"id,omitempty"`
	Role    string `json:"role"`
	Content string `json:"content"`
	Omitted bool   `json:"omitted,omitempty"`
}

// Response is returned to tray actions and CLI callers over the local channel.
type Response struct {
	OK                     bool                  `json:"ok"`
	Error                  string                `json:"error,omitempty"`
	Message                string                `json:"message,omitempty"`
	VoiceActive            bool                  `json:"voice_active,omitempty"`
	TextShortcut           string                `json:"text_shortcut,omitempty"`
	VoiceShortcut          string                `json:"voice_shortcut,omitempty"`
	Speak                  *config.SpeakConfig   `json:"speak,omitempty"`
	VoiceMessages          []ConversationMessage `json:"voice_messages,omitempty"`
	VoiceMessagesTruncated bool                  `json:"voice_messages_truncated,omitempty"`
	VoiceSessionStarted    bool                  `json:"voice_session_started,omitempty"`
}

type voiceContextKey struct{}
type voiceOwnerKey struct{}

// ActionHandler executes a validated local tray or IPC action.
type ActionHandler func(context.Context, Action) (Response, error)

func Send(ctx context.Context, action Action) (Response, error) {
	path, err := defaultEndpointPath()
	if err != nil {
		return Response{}, err
	}
	endpoint, err := readEndpoint(path)
	if err != nil {
		return Response{}, fmt.Errorf("read tray service endpoint: %w", err)
	}
	return sendToEndpoint(ctx, endpoint, action)
}

// SendVoiceContext opens or restores the voice window with a bounded text snapshot.
func SendVoiceContext(ctx context.Context, messages []ConversationMessage) (Response, error) {
	return sendVoiceRequest(ctx, ActionOpenOrShowVoice, "", messages)
}

// SendVoiceSession starts a voice session owned by one CLI caller and sends its text snapshot.
func SendVoiceSession(ctx context.Context, ownerID string, messages []ConversationMessage) (Response, error) {
	return sendVoiceRequest(ctx, ActionOpenOrShowVoice, ownerID, messages)
}

// SendVoiceStatus checks a voice session and returns its transcript only to the owning CLI caller.
func SendVoiceStatus(ctx context.Context, ownerID string) (Response, error) {
	return sendVoiceRequest(ctx, ActionVoiceStatus, ownerID, nil)
}

// AcknowledgeVoiceTranscript releases the completed transcript after its CLI chat is saved.
func AcknowledgeVoiceTranscript(ctx context.Context, ownerID string) (Response, error) {
	return sendVoiceRequest(ctx, ActionVoiceTranscriptAck, ownerID, nil)
}

func sendVoiceRequest(ctx context.Context, action Action, ownerID string, messages []ConversationMessage) (Response, error) {
	path, err := defaultEndpointPath()
	if err != nil {
		return Response{}, err
	}
	endpoint, err := readEndpoint(path)
	if err != nil {
		return Response{}, fmt.Errorf("read tray service endpoint: %w", err)
	}
	return sendToEndpointWithVoiceOwner(ctx, endpoint, action, messages, ownerID)
}

func sendToEndpoint(ctx context.Context, endpoint serviceEndpoint, action Action) (Response, error) {
	return sendToEndpointWithVoiceContext(ctx, endpoint, action, nil)
}

func sendToEndpointWithVoiceContext(ctx context.Context, endpoint serviceEndpoint, action Action, voiceContext []ConversationMessage) (Response, error) {
	return sendToEndpointWithVoiceOwner(ctx, endpoint, action, voiceContext, "")
}

func sendToEndpointWithVoiceOwner(ctx context.Context, endpoint serviceEndpoint, action Action, voiceContext []ConversationMessage, ownerID string) (Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateEndpoint(endpoint); err != nil {
		return Response{}, err
	}
	if !validAction(action) {
		return Response{}, fmt.Errorf("unknown tray service action %q", action)
	}
	if action != ActionOpenOrShowVoice && len(voiceContext) > 0 {
		return Response{}, fmt.Errorf("voice context can only be sent when opening voice chat")
	}
	if ownerID != "" && action != ActionOpenOrShowVoice && action != ActionVoiceStatus && action != ActionVoiceTranscriptAck {
		return Response{}, fmt.Errorf("voice owner can only be sent when opening, checking, or acknowledging voice chat")
	}
	if (action == ActionVoiceStatus || action == ActionVoiceTranscriptAck) && ownerID == "" {
		return Response{}, fmt.Errorf("voice owner is required for this action")
	}
	if len(ownerID) > 128 {
		return Response{}, fmt.Errorf("voice owner ID is too long")
	}
	connection, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", endpoint.Address)
	if err != nil {
		return Response{}, fmt.Errorf("connect to tray service: %w", err)
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	} else {
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	}
	request := ipcRequest{Token: endpoint.Token, Action: action, VoiceContext: BoundVoiceContext(voiceContext), VoiceOwnerID: ownerID}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return Response{}, fmt.Errorf("send tray service request: %w", err)
	}
	var response Response
	decoder := json.NewDecoder(io.LimitReader(connection, maxIPCMessageBytes+1))
	if err := decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("read tray service response: %w", err)
	}
	if response.Error != "" {
		return response, nil
	}
	return response, nil
}

func decodeIPCRequest(reader *bufio.Reader) (ipcRequest, error) {
	line, err := reader.ReadString('\n')
	if len(line) > maxIPCMessageBytes {
		return ipcRequest{}, errors.New("request too large")
	}
	if err != nil {
		return ipcRequest{}, err
	}
	var request ipcRequest
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &request); err != nil {
		return ipcRequest{}, fmt.Errorf("invalid request: %w", err)
	}
	return request, nil
}

func processRequest(ctx context.Context, request ipcRequest, token string, handler ActionHandler) Response {
	if len(request.Token) != len(token) || subtle.ConstantTimeCompare([]byte(request.Token), []byte(token)) != 1 {
		return Response{Error: "unauthorized"}
	}
	if !validAction(request.Action) {
		return Response{Error: "unknown action"}
	}
	if request.Action != ActionOpenOrShowVoice && len(request.VoiceContext) > 0 {
		return Response{Error: "voice context can only be sent when opening voice chat"}
	}
	if request.VoiceOwnerID != "" && request.Action != ActionOpenOrShowVoice && request.Action != ActionVoiceStatus && request.Action != ActionVoiceTranscriptAck {
		return Response{Error: "voice owner can only be sent when opening, checking, or acknowledging voice chat"}
	}
	if (request.Action == ActionVoiceStatus || request.Action == ActionVoiceTranscriptAck) && request.VoiceOwnerID == "" {
		return Response{Error: "voice owner is required for this action"}
	}
	if len(request.VoiceOwnerID) > 128 {
		return Response{Error: "voice owner ID is too long"}
	}
	if request.Action == ActionOpenOrShowVoice || request.Action == ActionVoiceStatus || request.Action == ActionVoiceTranscriptAck {
		ctx = context.WithValue(ctx, voiceContextKey{}, BoundVoiceContext(request.VoiceContext))
		ctx = context.WithValue(ctx, voiceOwnerKey{}, request.VoiceOwnerID)
	}
	if request.Action == ActionServiceStatus && handler == nil {
		return Response{OK: true}
	}
	if handler == nil {
		return Response{Error: "action unavailable"}
	}
	response, err := handler(ctx, request.Action)
	if err != nil {
		return Response{Error: err.Error()}
	}
	if response.Error != "" {
		response.OK = false
		return response
	}
	response.OK = true
	return response
}

// VoiceContextFromContext returns the normalized conversation snapshot attached to an IPC request.
func VoiceContextFromContext(ctx context.Context) []ConversationMessage {
	if ctx == nil {
		return nil
	}
	messages, _ := ctx.Value(voiceContextKey{}).([]ConversationMessage)
	return append([]ConversationMessage(nil), messages...)
}

// VoiceOwnerFromContext returns the CLI caller associated with a voice start or status request.
func VoiceOwnerFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	ownerID, _ := ctx.Value(voiceOwnerKey{}).(string)
	return ownerID
}

// AppendVoiceTranscript retains finalized turns separately from the smaller outbound context window.
// Its encoded-size bound leaves room for the surrounding IPC response under maxIPCMessageBytes.
func AppendVoiceTranscript(messages []ConversationMessage, message ConversationMessage) ([]ConversationMessage, bool) {
	if message.Omitted {
		return messages, true
	}
	if (message.Role != "user" && message.Role != "assistant") || strings.TrimSpace(message.Content) == "" {
		return messages, false
	}
	if message.ID != "" {
		for _, existing := range messages {
			if existing.ID == message.ID {
				return messages, false
			}
		}
	}
	if len(message.Content) > maxVoiceTranscriptItemBytes || len(messages) >= maxVoiceTranscriptMessages {
		return messages, true
	}
	usedBytes := 2 // JSON array brackets.
	for index, existing := range messages {
		encoded, err := json.Marshal(existing)
		if err != nil {
			return messages, true
		}
		usedBytes += len(encoded)
		if index > 0 {
			usedBytes++
		}
	}
	encoded, err := json.Marshal(message)
	if err != nil || usedBytes+len(encoded)+1 > maxVoiceTranscriptBytes {
		return messages, true
	}
	return append(messages, message), false
}

// BoundVoiceContext keeps the newest user/assistant text small enough for both IPC and WebRTC.
func BoundVoiceContext(messages []ConversationMessage) []ConversationMessage {
	if len(messages) == 0 {
		return nil
	}
	selected := make([]ConversationMessage, 0, min(len(messages), maxVoiceContextMessages))
	remainingBytes := maxVoiceContextBytes
	for index := len(messages) - 1; index >= 0 && len(selected) < maxVoiceContextMessages && remainingBytes > 0; index-- {
		message := messages[index]
		if message.Role != "user" && message.Role != "assistant" || strings.TrimSpace(message.Content) == "" {
			continue
		}
		message.Content = truncateUTF8Tail(message.Content, min(maxVoiceContextMessageBytes, remainingBytes))
		if message.Content == "" {
			continue
		}
		remainingBytes -= len(message.Content)
		selected = append(selected, message)
	}
	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}
	return selected
}

func truncateUTF8Tail(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[len(value)-maxBytes:]
	for len(value) > 0 && !utf8.RuneStart(value[0]) {
		value = value[1:]
	}
	return value
}

func handleIPCConnection(ctx context.Context, connection net.Conn, token string, handler ActionHandler) Response {
	if remote, ok := connection.RemoteAddr().(*net.TCPAddr); !ok || !remote.IP.IsLoopback() {
		response := Response{Error: "loopback connections only"}
		_ = json.NewEncoder(connection).Encode(response)
		return response
	}
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(io.LimitReader(connection, maxIPCMessageBytes+1))
	request, err := decodeIPCRequest(reader)
	var response Response
	if err != nil {
		if err.Error() == "request too large" {
			response = Response{Error: "request too large"}
		} else {
			response = Response{Error: "invalid request"}
		}
	} else {
		response = processRequest(ctx, request, token, handler)
	}
	_ = json.NewEncoder(connection).Encode(response)
	return response
}

func validAction(action Action) bool {
	switch action {
	case ActionServiceStatus, ActionOpenTextChat, ActionOpenDashboard, ActionOpenOrShowVoice, ActionToggleVoice,
		ActionMinimizeVoice, ActionConfigureShort, ActionReloadConfig, ActionVoiceStatus,
		ActionVoiceTranscriptAck, ActionQuitService:
		return true
	default:
		return false
	}
}
