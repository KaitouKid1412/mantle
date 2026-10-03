//go:build !unix

package claudecli

import "os/exec"

func setProcessGroup(cmd *exec.Cmd) {}
