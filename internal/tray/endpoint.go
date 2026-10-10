package tray

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

type serviceEndpoint struct {
	Address string `json:"address"`
	Token   string `json:"token"`
	PID     int    `json:"pid"`
}

func defaultEndpointPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(configDir, "duckduckgo-chat-cli", "tray-service.json"), nil
}

func newServiceToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("create tray service token: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}

func createEndpoint(path string, endpoint serviceEndpoint) error {
	if err := validateEndpoint(endpoint); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create tray service directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tray-service-*")
	if err != nil {
		return fmt.Errorf("create tray endpoint temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure tray endpoint: %w", err)
	}
	if err := json.NewEncoder(tmp).Encode(endpoint); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write tray endpoint: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync tray endpoint: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tray endpoint: %w", err)
	}
	if err := os.Link(tmpPath, path); err != nil {
		return err
	}
	return nil
}

func readEndpoint(path string) (serviceEndpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return serviceEndpoint{}, err
	}
	var endpoint serviceEndpoint
	if err := json.Unmarshal(data, &endpoint); err != nil {
		return serviceEndpoint{}, fmt.Errorf("decode tray endpoint: %w", err)
	}
	if err := validateEndpoint(endpoint); err != nil {
		return serviceEndpoint{}, fmt.Errorf("invalid tray endpoint: %w", err)
	}
	return endpoint, nil
}

func validateEndpoint(endpoint serviceEndpoint) error {
	host, port, err := net.SplitHostPort(endpoint.Address)
	if err != nil || host != "127.0.0.1" {
		return fmt.Errorf("tray endpoint must use loopback TCP")
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("tray endpoint has an invalid port")
	}
	token, err := hex.DecodeString(endpoint.Token)
	if err != nil || len(token) != 32 {
		return fmt.Errorf("tray endpoint has an invalid authentication token")
	}
	if endpoint.PID < 1 {
		return fmt.Errorf("tray endpoint has an invalid process ID")
	}
	return nil
}

func removeEndpointIfOwned(path, token string) error {
	endpoint, err := readEndpoint(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if endpoint.Token != token {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func removeStaleEndpointAt(path string, processAlive func(int) bool) (bool, error) {
	endpoint, err := readEndpoint(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return false, removeErr
		}
		return true, nil
	}
	if processAlive(endpoint.PID) {
		return false, nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, nil
}
