//go:build linux

package dashboard

import (
	"os"
	"os/exec"
	"syscall"
)

func prepareDashboardChromiumCmd(cmd *exec.Cmd) {
	if _, ok := os.LookupEnv("LAMBDA_TASK_ROOT"); ok {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = new(syscall.SysProcAttr)
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
