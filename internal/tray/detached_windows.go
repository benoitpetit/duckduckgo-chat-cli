package tray

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func startDetached(executable string, args ...string) error {
	command := exec.Command(executable, args...)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
	device, err := os.Open(os.DevNull)
	if err == nil {
		defer device.Close()
		command.Stdin = device
		command.Stdout = device
		command.Stderr = device
	}
	return command.Start()
}
