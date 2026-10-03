package launcher

import "syscall"

const (
	ioctlGetTermios      = syscall.TIOCGETA
	ioctlSetTermiosFlush = syscall.TIOCSETAF
)
