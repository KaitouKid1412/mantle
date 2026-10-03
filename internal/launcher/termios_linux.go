package launcher

import "syscall"

const (
	ioctlGetTermios      = syscall.TCGETS
	ioctlSetTermiosFlush = 0x5404 // TCSETSF; not in package syscall
)
