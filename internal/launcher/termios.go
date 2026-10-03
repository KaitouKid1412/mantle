package launcher

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

// resetSequence undoes the terminal modes a crashed UI may have left on.
const resetSequence = "\x1b[?1049l" + // leave alt screen
	"\x1b[?25h" + // show cursor
	"\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l" + // mouse off
	"\x1b[?1004l" + // focus reporting off
	"\x1b[?2004l" + // bracketed paste off
	"\x1b[<u" + // pop kitty keyboard flags
	"\x1b[?2027l" + // grapheme clustering mode off
	"\x1b]9;4;0\x07" + // clear progress
	"\x1b[0m" + // reset attributes
	"\r\n"

// Terminal saves and restores the controlling terminal around a child run.
type Terminal interface {
	// Save records the current termios. It is a no-op without a terminal.
	Save()
	// Restore puts the saved termios back (flushing pending input) and writes
	// the reset sequences. It is a no-op if Save found no terminal.
	Restore()
}

// ttyTerminal is the real Terminal.
type ttyTerminal struct {
	in    *os.File
	out   io.Writer
	saved *syscall.Termios
}

// NewTerminal returns a Terminal that saves termios of in and writes the
// reset sequences to out.
func NewTerminal(in *os.File, out io.Writer) Terminal {
	return &ttyTerminal{in: in, out: out}
}

func (t *ttyTerminal) Save() {
	if t.in == nil {
		return
	}
	var tio syscall.Termios
	if err := ioctlTermios(t.in.Fd(), ioctlGetTermios, &tio); err != nil {
		return
	}
	t.saved = &tio
}

func (t *ttyTerminal) Restore() {
	if t.saved == nil {
		return
	}
	ioctlTermios(t.in.Fd(), ioctlSetTermiosFlush, t.saved)
	if t.out != nil {
		io.WriteString(t.out, resetSequence)
	}
}

// IsTerminal reports whether f is a terminal.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	var tio syscall.Termios
	return ioctlTermios(f.Fd(), ioctlGetTermios, &tio) == nil
}

func ioctlTermios(fd uintptr, req uintptr, tio *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(tio)))
	if errno != 0 {
		return errno
	}
	return nil
}
