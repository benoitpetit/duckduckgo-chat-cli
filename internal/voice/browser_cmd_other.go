//go:build !linux

package voice

import "os/exec"

func prepareChromiumCmd(cmd *exec.Cmd) {}
