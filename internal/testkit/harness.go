package testkit

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// Harness runs a tea.Program against an in-memory terminal emulator (charmbracelet/x/vt),
// so tests can assert both the visible screen and the scrollback, which is the only way
// to test inline rendering and tea.Println behaviour.
//
// By default the program talks to a real pseudo-terminal, so Bubble Tea takes the same
// code path as in production (raw mode, no newline mapping); the pty's master side is
// pumped into the emulator. WithPipe uses plain pipes instead.
//
// All methods are safe to call from the test goroutine while the program runs.
type Harness struct {
	tb   testing.TB
	term *lockedTerm
	prog *tea.Program
	done chan struct{}

	mu     sync.Mutex
	final  tea.Model
	runErr error
}

// Option configures a Harness.
type Option func(*config)

type config struct {
	w, h    int
	profile colorprofile.Profile
	env     []string
	opts    []tea.ProgramOption
	pipe    bool
}

// WithSize sets the initial terminal size (default 80×24).
func WithSize(w, h int) Option { return func(c *config) { c.w, c.h = w, h } }

// WithProfile sets the colour profile (default TrueColor, so styles are kept).
func WithProfile(p colorprofile.Profile) Option { return func(c *config) { c.profile = p } }

// WithEnv sets the program's environment (default TERM=xterm-256color).
func WithEnv(env ...string) Option { return func(c *config) { c.env = env } }

// WithProgramOptions adds extra tea.ProgramOptions.
func WithProgramOptions(o ...tea.ProgramOption) Option {
	return func(c *config) { c.opts = append(c.opts, o...) }
}

// WithPipe connects the program through pipes instead of a pty. Output newlines are
// mapped to CRLF, as a cooked terminal would.
func WithPipe() Option { return func(c *config) { c.pipe = true } }

// New starts m in a new emulator. The program is killed when the test ends.
func New(tb testing.TB, m tea.Model, opts ...Option) *Harness {
	tb.Helper()
	cfg := config{w: 80, h: 24, profile: colorprofile.TrueColor, env: []string{"TERM=xterm-256color"}}
	for _, o := range opts {
		o(&cfg)
	}
	term, in, out, err := newLockedTerm(cfg.w, cfg.h, cfg.pipe)
	if err != nil {
		tb.Fatalf("testkit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	popts := []tea.ProgramOption{
		tea.WithContext(ctx),
		tea.WithInput(in),
		tea.WithOutput(out),
		tea.WithWindowSize(cfg.w, cfg.h),
		tea.WithColorProfile(cfg.profile),
		tea.WithEnvironment(cfg.env),
		tea.WithoutSignals(),
	}
	popts = append(popts, cfg.opts...)
	h := &Harness{tb: tb, term: term, prog: tea.NewProgram(m, popts...), done: make(chan struct{})}
	go func() {
		final, err := h.prog.Run()
		h.mu.Lock()
		h.final, h.runErr = final, err
		h.mu.Unlock()
		close(h.done)
	}()
	tb.Cleanup(func() {
		// Quit (not Kill): Bubble Tea then waits for its input reader to stop before
		// closing it, so nothing still reads the pty when it is closed below. Kill only
		// if the program does not exit.
		h.prog.Quit()
		select {
		case <-h.done:
		case <-time.After(3 * time.Second):
			cancel()
			h.prog.Kill()
			select {
			case <-h.done:
			case <-time.After(2 * time.Second):
			}
		}
		cancel()
		term.close()
	})
	return h
}

// Program returns the running program (for Send of custom messages).
func (h *Harness) Program() *tea.Program { return h.prog }

// SendMsg delivers a message to the program.
func (h *Harness) SendMsg(m tea.Msg) {
	if h.prog == nil {
		h.tb.Fatal("testkit: SendMsg needs an in-process program (New), not a Process")
	}
	h.prog.Send(m)
}

// Send types keys, each written as the bytes a terminal would send. Keys use the
// keybindings.json syntax: "a", "enter", "ctrl+c", "shift+tab", "alt+p", "ctrl+x ctrl+k"
// (a space separates the keys of a chord; " " alone is the space key).
func (h *Harness) Send(keys ...string) {
	h.tb.Helper()
	for _, k := range keys {
		for _, part := range splitChord(k) {
			seq, ok := KeySeq(part)
			if !ok {
				h.tb.Fatalf("testkit: unknown key %q", part)
			}
			h.term.input(seq)
			if seq == "\x1b" {
				// A lone escape is disambiguated by a timeout; don't let the next key
				// merge into an alt-sequence.
				time.Sleep(80 * time.Millisecond)
			}
		}
	}
}

// Type types text verbatim (no key-name parsing).
func (h *Harness) Type(s string) { h.term.input(s) }

// Paste pastes text as a bracketed paste (Bubble Tea enables bracketed paste by
// default).
func (h *Harness) Paste(s string) {
	h.term.input(ansi.BracketedPasteStart + s + ansi.BracketedPasteEnd)
}

// Resize resizes the terminal and tells the program.
func (h *Harness) Resize(w, hgt int) {
	h.term.resize(w, hgt)
	if h.prog != nil {
		h.prog.Send(tea.WindowSizeMsg{Width: w, Height: hgt})
	}
}

// Size returns the terminal size.
func (h *Harness) Size() (w, hgt int) { return h.term.size() }

// Screen returns the visible screen as plain text: trailing spaces trimmed on each
// line, trailing blank lines removed.
func (h *Harness) Screen() string { return joinTrim(h.term.snapshot().screen) }

// ScreenLines returns every visible row (trailing spaces trimmed).
func (h *Harness) ScreenLines() []string { return h.term.snapshot().screen }

// Scrollback returns the lines that scrolled off the top, oldest first, trailing
// spaces trimmed.
func (h *Harness) Scrollback() []string { return h.term.snapshot().scrollback }

// All returns scrollback followed by the screen, with trailing blank lines removed:
// everything a user could scroll through.
func (h *Harness) All() []string {
	s := h.term.snapshot()
	all := append(append([]string(nil), s.scrollback...), s.screen...)
	for len(all) > 0 && all[len(all)-1] == "" {
		all = all[:len(all)-1]
	}
	return all
}

// Styled returns the visible screen with ANSI styles.
func (h *Harness) Styled() string { return h.term.styled() }

// AltScreen reports whether the program is in the alternate screen.
func (h *Harness) AltScreen() bool { return h.term.altScreen() }

// Cursor returns the cursor position on the screen.
func (h *Harness) Cursor() (x, y int) { return h.term.cursor() }

// Output returns every byte the program wrote so far (for low-level assertions).
func (h *Harness) Output() []byte { return h.term.output() }

// WaitFor polls until pred(screen) is true, or fails the test after timeout with a dump
// of the screen and scrollback.
func (h *Harness) WaitFor(pred func(screen string) bool, timeout time.Duration) {
	h.tb.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if pred(h.Screen()) {
			return
		}
		if time.Now().After(deadline) {
			h.tb.Fatalf("testkit: condition not met after %v\n--- scrollback ---\n%s\n--- screen ---\n%s",
				timeout, strings.Join(h.Scrollback(), "\n"), h.Screen())
		}
		select {
		case <-h.done:
			time.Sleep(20 * time.Millisecond) // let the pump drain the last frame
			if pred(h.Screen()) {
				return
			}
			h.tb.Fatalf("testkit: program exited (err=%v) before condition was met\n--- screen ---\n%s", h.err(), h.Screen())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// WaitForText waits until the screen contains s.
func (h *Harness) WaitForText(s string, timeout time.Duration) {
	h.tb.Helper()
	h.WaitFor(func(screen string) bool { return strings.Contains(screen, s) }, timeout)
}

// Settle waits until the screen and scrollback stop changing for quiet (the frame
// ticker flushes at ~60 fps), or until timeout.
func (h *Harness) Settle(quiet, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	last := ""
	stableSince := time.Now()
	for time.Now().Before(deadline) {
		cur := strings.Join(h.All(), "\n")
		if cur != last {
			last, stableSince = cur, time.Now()
		} else if time.Since(stableSince) >= quiet {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Quit asks the program to quit and waits for it.
func (h *Harness) Quit() (tea.Model, error) {
	h.tb.Helper()
	if h.prog == nil {
		h.tb.Fatal("testkit: Quit needs an in-process program; signal a Process instead")
	}
	h.prog.Quit()
	return h.Wait(5 * time.Second)
}

// Wait waits for the program to exit and returns its final model.
func (h *Harness) Wait(timeout time.Duration) (tea.Model, error) {
	h.tb.Helper()
	select {
	case <-h.done:
	case <-time.After(timeout):
		h.tb.Fatalf("testkit: program did not exit within %v", timeout)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.final, h.runErr
}

// Done is closed when the program exits.
func (h *Harness) Done() <-chan struct{} { return h.done }

func (h *Harness) err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.runErr
}

// lockedTerm owns the emulator. Program output is written into it under mu; the test
// reads snapshots under mu. Input (keys, pastes and the emulator's replies to terminal
// queries) flows the other way without taking mu, so a program blocked on drawing can
// never deadlock a test sending keys.
type lockedTerm struct {
	mu  sync.Mutex
	emu *vt.Emulator
	raw bytes.Buffer // everything the program wrote

	stopping  atomic.Bool
	replyDone chan struct{}

	inMu  sync.Mutex
	inW   io.Writer // where input goes: the pty master, or the pipe queue
	ptmx  *os.File  // pty mode
	tty   *os.File  // pty mode
	queue *queue    // pipe mode
}

// newLockedTerm returns the terminal plus the program's input and output.
func newLockedTerm(w, h int, pipe bool) (*lockedTerm, io.Reader, io.Writer, error) {
	t := &lockedTerm{emu: vt.NewEmulator(w, h)}
	var in io.Reader
	var out io.Writer
	if pipe {
		t.queue = newQueue()
		t.inW = t.queue
		in, out = t.queue, crlfWriter{t}
	} else {
		ptmx, tty, err := pty.Open()
		if err != nil {
			return nil, nil, nil, err
		}
		if err := pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)}); err != nil {
			return nil, nil, nil, err
		}
		t.ptmx, t.tty, t.inW = ptmx, tty, ptmx
		in, out = tty, tty
		go func() { // program output → emulator
			buf := make([]byte, 32*1024)
			for {
				n, err := ptmx.Read(buf)
				if n > 0 {
					_, _ = t.Write(buf[:n])
				}
				if err != nil {
					return
				}
			}
		}()
	}
	t.replyDone = make(chan struct{})
	go func() { // emulator replies → program input
		defer close(t.replyDone)
		buf := make([]byte, 4096)
		for {
			n, err := t.emu.Read(buf)
			if t.stopping.Load() || err != nil {
				return
			}
			if n > 0 {
				t.input(string(buf[:n]))
			}
		}
	}()
	return t, in, out, nil
}

func (t *lockedTerm) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.raw.Write(p)
	return t.emu.Write(p)
}

func (t *lockedTerm) input(s string) {
	t.inMu.Lock()
	defer t.inMu.Unlock()
	_, _ = io.WriteString(t.inW, s)
}

// close stops the pumps. vt.Emulator.Close is not safe while another goroutine is in
// Read, so the reply pump is stopped first: flag it, then make the emulator answer a
// device-attributes query so the blocked Read returns.
func (t *lockedTerm) close() {
	t.stopping.Store(true)
	t.mu.Lock()
	_, _ = t.emu.Write([]byte("\x1b[c"))
	t.mu.Unlock()
	select {
	case <-t.replyDone:
		// The output pump may still be writing: close under the lock.
		t.mu.Lock()
		_ = t.emu.Close()
		t.mu.Unlock()
	case <-time.After(time.Second):
		// Leave the emulator open rather than race; the goroutine leaks.
	}
	if t.queue != nil {
		t.queue.close()
	}
	if t.tty != nil {
		_ = t.tty.Close()
	}
	if t.ptmx != nil {
		_ = t.ptmx.Close()
	}
}

func (t *lockedTerm) resize(w, h int) {
	if t.ptmx != nil {
		_ = pty.Setsize(t.ptmx, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.emu.Resize(w, h)
}

func (t *lockedTerm) size() (int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.emu.Width(), t.emu.Height()
}

func (t *lockedTerm) styled() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.emu.Render()
}

func (t *lockedTerm) altScreen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.emu.IsAltScreen()
}

func (t *lockedTerm) cursor() (int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.emu.CursorPosition()
	return p.X, p.Y
}

func (t *lockedTerm) output() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return bytes.Clone(t.raw.Bytes())
}

type snapshot struct {
	screen     []string
	scrollback []string
}

func (t *lockedTerm) snapshot() snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	var s snapshot
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
		s.screen = append(s.screen, strings.TrimRight(b.String(), " "))
	}
	if sb := t.emu.Scrollback(); sb != nil {
		for _, line := range sb.Lines() {
			s.scrollback = append(s.scrollback, strings.TrimRight(line.String(), " "))
		}
	}
	return s
}

// crlfWriter maps \n to \r\n, as a terminal in cooked output mode does.
type crlfWriter struct{ t *lockedTerm }

func (c crlfWriter) Write(p []byte) (int, error) {
	if _, err := c.t.Write(bytes.ReplaceAll(p, []byte("\n"), []byte("\r\n"))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// queue is an unbounded byte FIFO with a blocking Read (pipe mode input).
type queue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

func newQueue() *queue {
	q := &queue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *queue) Write(p []byte) (int, error) {
	q.mu.Lock()
	q.buf = append(q.buf, p...)
	q.mu.Unlock()
	q.cond.Broadcast()
	return len(p), nil
}

func (q *queue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
}

func (q *queue) Read(p []byte) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.buf) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, q.buf)
	q.buf = q.buf[n:]
	return n, nil
}

func joinTrim(lines []string) string {
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
