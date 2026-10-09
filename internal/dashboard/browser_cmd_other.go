//go:build !linux

package dashboard

import "os/exec"

func prepareDashboardChromiumCmd(cmd *exec.Cmd) {}
