package parity

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// Term runs a program in a pseudo-terminal attached to an x/vt emulator, so a test can
// read the visible screen and the scrollback the program produced, exactly as a user's
// terminal would show them.
//
// Program output is pumped into the emulator; the emulator's replies to terminal
// queries (device attributes, cursor position, colours) and the keys a test sends are
// written back to the program's input.
type Term struct {
	cmd  *exec.Cmd
	ptmx *os.File

	mu  sync.Mutex
	emu *vt.Emulator
	raw bytes.Buffer
	san stringSanitizer

	started time.Time // when the program was started

	inMu      sync.Mutex
	stopping  atomic.Bool
	replyDone chan struct{}
	outDone   chan struct{}
	exited    chan struct{}
	waitErr   error
}

// StartTerm starts cmd in a w×h pseudo-terminal.
func StartTerm(cmd *exec.Cmd, w, h int) (*Term, error) {
	t := &Term{
		cmd:       cmd,
		emu:       vt.NewEmulator(w, h),
		started:   time.Now(),
		replyDone: make(chan struct{}),
		outDone:   make(chan struct{}),
		exited:    make(chan struct{}),
	}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
	if err != nil {
		_ = t.emu.Close()
		return nil, fmt.Errorf("parity: start %s: %w", cmd.Path, err)
	}
	t.ptmx = ptmx
	go func() { // program output → emulator
		defer close(t.outDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				t.mu.Lock()
				t.raw.Write(buf[:n])
				_, _ = t.emu.Write(t.san.filter(buf[:n]))
				t.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { // emulator replies → program input
		defer close(t.replyDone)
		buf := make([]byte, 4096)
		for {
			n, err := t.emu.Read(buf)
			if t.stopping.Load() || err != nil {
				return
			}
			if n > 0 {
				t.Input(string(buf[:n]))
			}
		}
	}()
	go func() {
		t.waitErr = cmd.Wait()
		close(t.exited)
	}()
	return t, nil
}

// Input writes raw bytes to the program's input.
func (t *Term) Input(s string) {
	t.inMu.Lock()
	defer t.inMu.Unlock()
	_, _ = io.WriteString(t.ptmx, s)
}

// Paste sends text as a bracketed paste when the program enabled bracketed paste
// mode, else as typed text (the emulator decides, like a real terminal).
func (t *Term) Paste(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.emu.Paste(text)
}

// Resize changes the terminal size; the program receives SIGWINCH.
func (t *Term) Resize(w, h int) error {
	t.mu.Lock()
	t.emu.Resize(w, h)
	t.mu.Unlock()
	return pty.Setsize(t.ptmx, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
}

// Size returns the emulator size.
func (t *Term) Size() (w, h int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.emu.Width(), t.emu.Height()
}

// Frame is the visible screen and the scrollback above it, as plain text with trailing
// spaces trimmed.
type Frame struct {
	Screen     []string
	Scrollback []string
	AltScreen  bool
	CursorX    int
	CursorY    int
}

// Snapshot captures the current frame.
func (t *Term) Snapshot() Frame {
	t.mu.Lock()
	defer t.mu.Unlock()
	var f Frame
	w, h := t.emu.Width(), t.emu.Height()
	for y := range h {
		var b strings.Builder
		for x := 0; x < w; x++ {
			c := t.emu.CellAt(x, y)
			switch {
			case c == nil:
				b.WriteByte(' ')
			case c.Content == "" && c.Width == 0:
				// trailing half of a wide cell
			case c.Content == "":
				b.WriteByte(' ')
			default:
				b.WriteString(c.Content)
			}
		}
		f.Screen = append(f.Screen, strings.TrimRight(b.String(), " "))
	}
	if sb := t.emu.Scrollback(); sb != nil {
		for _, line := range sb.Lines() {
			f.Scrollback = append(f.Scrollback, strings.TrimRight(line.String(), " "))
		}
	}
	f.AltScreen = t.emu.IsAltScreen()
	p := t.emu.CursorPosition()
	f.CursorX, f.CursorY = p.X, p.Y
	return f
}

// Screen is the visible screen as one string.
func (t *Term) Screen() string { return strings.Join(t.Snapshot().Screen, "\n") }

// Output returns everything the program wrote.
func (t *Term) Output() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return bytes.Clone(t.raw.Bytes())
}

// Exited is closed when the program has exited.
func (t *Term) Exited() <-chan struct{} { return t.exited }

// ErrTimeout is returned by WaitFor when the condition did not hold in time.
var ErrTimeout = errors.New("parity: timed out")

// WaitFor polls the screen until pred holds, the program exits or timeout passes.
func (t *Term) WaitFor(pred func(Frame) bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if pred(t.Snapshot()) {
			return nil
		}
		select {
		case <-t.exited:
			if pred(t.Snapshot()) {
				return nil
			}
			return fmt.Errorf("parity: program exited (%v) while waiting", t.waitErr)
		default:
		}
		if time.Now().After(deadline) {
			return ErrTimeout
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Settle waits until the screen has not changed for quiet (or timeout passes).
func (t *Term) Settle(quiet, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	last := t.Screen()
	since := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		cur := t.Screen()
		if cur != last {
			last, since = cur, time.Now()
			continue
		}
		if time.Since(since) >= quiet {
			return
		}
	}
}

// Close stops the program (SIGTERM to its process group, then SIGKILL) and the pumps.
func (t *Term) Close() error {
	if t.cmd.Process != nil {
		select {
		case <-t.exited:
		default:
			_ = syscall.Kill(-t.cmd.Process.Pid, syscall.SIGTERM)
			select {
			case <-t.exited:
			case <-time.After(2 * time.Second):
				_ = syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL)
				<-t.exited
			}
		}
	}
	t.stopping.Store(true)
	t.mu.Lock()
	_, _ = t.emu.Write([]byte("\x1b[c")) // make the emulator answer so its Read returns
	t.mu.Unlock()
	select {
	case <-t.replyDone:
		_ = t.emu.Close()
	case <-time.After(time.Second):
	}
	err := t.ptmx.Close()
	<-t.outDone
	return err
}

// Started is when the program was started.
func (t *Term) Started() time.Time { return t.started }

// Pid is the program's process id.
func (t *Term) Pid() int {
	if t.cmd.Process == nil {
		return 0
	}
	return t.cmd.Process.Pid
}

// Usage reports the program's CPU time (user + system) and peak resident set size in
// bytes, once it has exited (after Close); zeros before.
func (t *Term) Usage() (cpu time.Duration, maxRSS int64) {
	select {
	case <-t.exited:
	default:
		return 0, 0
	}
	ps := t.cmd.ProcessState
	if ps == nil {
		return 0, 0
	}
	cpu = ps.UserTime() + ps.SystemTime()
	if ru, ok := ps.SysUsage().(*syscall.Rusage); ok {
		maxRSS = int64(ru.Maxrss)
		if runtime.GOOS != "darwin" {
			maxRSS *= 1024 // Linux reports kilobytes
		}
	}
	return cpu, maxRSS
}

// RSS is the program's current resident set size in bytes (its own process only, not
// its children), or 0 when it can't be read.
func (t *Term) RSS() int64 {
	pid := t.Pid()
	if pid == 0 {
		return 0
	}
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	kb, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}
