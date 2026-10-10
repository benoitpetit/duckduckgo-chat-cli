package tray

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"duckduckgo-chat-cli/internal/config"
)

var errServiceAlreadyRunning = errors.New("tray service is already running")

// TrayActionDispatcher sends a tray gesture through the same action handler
// used by authenticated IPC clients and returns its result for UI feedback.
type TrayActionDispatcher func(Action) Response

// ServiceOptions configures the resident process without coupling the tray
// package to the CLI or voice packages.
type ServiceOptions struct {
	EndpointFile   string
	HandleAction   ActionHandler
	LoadTrayConfig func() config.TrayConfig
	RunTray        func(context.Context, TrayActionDispatcher, <-chan config.TrayConfig) error
}

// EnsureService contacts an existing resident service or launches one in the
// background and waits for its authenticated endpoint to become ready.
func EnsureService(ctx context.Context, executable string) error {
	path, err := defaultEndpointPath()
	if err != nil {
		return err
	}
	return ensureServiceAt(ctx, path, executable, startDetached)
}

func ensureServiceAt(ctx context.Context, endpointFile, executable string, start func(string, ...string) error) error {
	return ensureServiceWithProbe(ctx, endpointFile, executable, start, func(ctx context.Context, endpoint serviceEndpoint) bool {
		response, err := sendToEndpoint(ctx, endpoint, ActionServiceStatus)
		return err == nil && response.OK
	})
}

func ensureServiceWithProbe(ctx context.Context, endpointFile, executable string, start func(string, ...string) error, probe func(context.Context, serviceEndpoint) bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if endpoint, err := readEndpoint(endpointFile); err == nil {
		if probe(ctx, endpoint) {
			return nil
		}
		if processExists(endpoint.PID) {
			return waitForServiceWithProbe(ctx, endpointFile, probe)
		}
		_, _ = removeStaleEndpointAt(endpointFile, processExists)
	} else if !errors.Is(err, os.ErrNotExist) {
		_, _ = removeStaleEndpointAt(endpointFile, processExists)
	}
	if err := start(executable, "--tray-service"); err != nil {
		return fmt.Errorf("start tray service: %w", err)
	}
	return waitForServiceWithProbe(ctx, endpointFile, probe)
}

func waitForService(ctx context.Context, endpointFile string) error {
	return waitForServiceWithProbe(ctx, endpointFile, func(ctx context.Context, endpoint serviceEndpoint) bool {
		response, err := sendToEndpoint(ctx, endpoint, ActionServiceStatus)
		return err == nil && response.OK
	})
}

func waitForServiceWithProbe(ctx context.Context, endpointFile string, probe func(context.Context, serviceEndpoint) bool) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(6 * time.Second)
	defer timeout.Stop()
	for {
		if endpoint, err := readEndpoint(endpointFile); err == nil {
			if probe(ctx, endpoint) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("tray service did not become ready")
		case <-ticker.C:
		}
	}
}

// RunService owns the local IPC listener and runs the tray loop until Quit or
// cancellation. The endpoint becomes visible only after it is complete.
func RunService(ctx context.Context, options ServiceOptions) error {
	return runService(ctx, options, net.Listen)
}

func runService(ctx context.Context, options ServiceOptions, listen func(string, string) (net.Listener, error)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	endpointFile := options.EndpointFile
	if endpointFile == "" {
		var err error
		endpointFile, err = defaultEndpointPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(endpointFile), 0o700); err != nil {
		return fmt.Errorf("create tray service directory: %w", err)
	}
	listener, err := listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for tray service requests: %w", err)
	}
	defer listener.Close()
	token, err := newServiceToken()
	if err != nil {
		return err
	}
	endpoint := serviceEndpoint{Address: listener.Addr().String(), Token: token, PID: os.Getpid()}
	if err := createEndpoint(endpointFile, endpoint); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errServiceAlreadyRunning
		}
		return fmt.Errorf("claim tray service endpoint: %w", err)
	}
	defer func() {
		_ = removeEndpointIfOwned(endpointFile, token)
	}()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	trayConfigReloads := make(chan config.TrayConfig, 1)
	dispatch := func(action Action) (Response, error) {
		var response Response
		var actionErr error
		if options.HandleAction != nil {
			response, actionErr = options.HandleAction(runCtx, action)
		} else if action != ActionServiceStatus {
			actionErr = fmt.Errorf("action %q is unavailable", action)
		}
		if action == ActionReloadConfig && actionErr == nil && options.LoadTrayConfig != nil {
			select {
			case trayConfigReloads <- options.LoadTrayConfig():
			default:
				select {
				case <-trayConfigReloads:
				default:
				}
				trayConfigReloads <- options.LoadTrayConfig()
			}
		}
		if action == ActionQuitService {
			cancel()
		}
		return response, actionErr
	}

	serveErr := make(chan error, 1)
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				if runCtx.Err() != nil || errors.Is(acceptErr, net.ErrClosed) {
					serveErr <- nil
				} else {
					serveErr <- acceptErr
					cancel()
				}
				return
			}
			go handleIPCConnection(runCtx, connection, token, func(requestCtx context.Context, action Action) (Response, error) {
				return dispatch(action)
			})
		}
	}()

	if options.RunTray != nil {
		trayErr := options.RunTray(runCtx, func(action Action) Response {
			response, err := dispatch(action)
			if err != nil {
				return Response{Error: err.Error()}
			}
			if response.Error != "" {
				response.OK = false
				return response
			}
			response.OK = true
			return response
		}, trayConfigReloads)
		if runCtx.Err() == nil {
			cancel()
		}
		if trayErr != nil {
			return trayErr
		}
		select {
		case err := <-serveErr:
			return err
		default:
			return nil
		}
	}
	select {
	case <-runCtx.Done():
		return nil
	case err := <-serveErr:
		return err
	}
}

// EndpointPath reports the private file used by the current user's service.
func EndpointPath() (string, error) {
	return defaultEndpointPath()
}
