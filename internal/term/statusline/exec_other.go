//go:build !unix

package statusline

import "os/exec"

func setProcessGroup(*exec.Cmd) {}
