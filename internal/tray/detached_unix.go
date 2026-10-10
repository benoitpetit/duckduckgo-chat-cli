//go:build linux || darwin

package tray

import (
	"os"
	"os/exec"
	"syscall"
)

func startDetached(executable string, args ...string) error {
	command := exec.Command(executable, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	device, err := os.Open(os.DevNull)
	if err == nil {
		defer device.Close()
		command.Stdin = device
		command.Stdout = device
		command.Stderr = device
	}
	return command.Start()
}
