//go:build linux

package voice

import (
	"os"
	"os/exec"
	"syscall"
)

func prepareChromiumCmd(cmd *exec.Cmd) {
	if _, ok := os.LookupEnv("LAMBDA_TASK_ROOT"); ok {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = new(syscall.SysProcAttr)
	}
	// Match chromedp's default: do not leave Chromium running if the CLI exits
	// unexpectedly before it can close the session cleanly.
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
