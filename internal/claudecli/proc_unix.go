//go:build unix

package claudecli

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts the child in its own process group and kills the whole
// group on cancel, so servers spawned by "mcp list" health checks die with it.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
