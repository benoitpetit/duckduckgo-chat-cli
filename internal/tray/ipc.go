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
)

const maxIPCMessageBytes = 4096

type Action string

const (
	ActionServiceStatus   Action = "service-status"
	ActionOpenTextChat    Action = "open-text-chat"
	ActionOpenOrShowVoice Action = "open-or-show-voice"
	ActionToggleVoice     Action = "toggle-voice"
	ActionMinimizeVoice   Action = "minimize-voice"
	ActionConfigureShort  Action = "configure-shortcuts"
	ActionReloadConfig    Action = "reload-config"
	ActionVoiceStatus     Action = "voice-status"
	ActionQuitService     Action = "quit-service"
)

type ipcRequest struct {
	Token  string `json:"token"`
	Action Action `json:"action"`
}

// Response is returned to tray actions and CLI callers over the local channel.
type Response struct {
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
	Message     string `json:"message,omitempty"`
	VoiceActive bool   `json:"voice_active,omitempty"`
}

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

func sendToEndpoint(ctx context.Context, endpoint serviceEndpoint, action Action) (Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateEndpoint(endpoint); err != nil {
		return Response{}, err
	}
	if !validAction(action) {
		return Response{}, fmt.Errorf("unknown tray service action %q", action)
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
	request := ipcRequest{Token: endpoint.Token, Action: action}
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
	if request.Action == ActionServiceStatus {
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
	case ActionServiceStatus, ActionOpenTextChat, ActionOpenOrShowVoice, ActionToggleVoice,
		ActionMinimizeVoice, ActionConfigureShort, ActionReloadConfig, ActionVoiceStatus,
		ActionQuitService:
		return true
	default:
		return false
	}
}
