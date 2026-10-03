// Package clipboard copies text to the system clipboard.
//
// Locally it pipes the text to the platform tool (pbcopy on macOS; wl-copy, xclip or xsel
// on Linux). Over ssh, or when no tool works, it returns an OSC 52 sequence for the
// caller to write to the terminal (tea.Raw), which reaches the clipboard of the machine
// the terminal runs on. Image paste is not here; plan 04 owns it.
package clipboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
)

// Method names how the text was copied.
type Method string

const (
	Pbcopy Method = "pbcopy"
	WlCopy Method = "wl-copy"
	Xclip  Method = "xclip"
	Xsel   Method = "xsel"
	OSC52  Method = "osc52"
)

// Result describes a copy. When Method is OSC52, the caller must write Sequence to the
// terminal; otherwise Sequence is nil and the text is already on the clipboard.
type Result struct {
	Method   Method
	Sequence []byte
	// Fallback is set when a tool was tried and failed before OSC 52 was chosen.
	Fallback error
}

// MaxOSC52 caps the encoded OSC 52 payload. Many terminals drop larger sequences
// (xterm's default limit is about 100 KB).
const MaxOSC52 = 100_000

// ErrTooLarge is returned when only OSC 52 is available and the text exceeds MaxOSC52.
var ErrTooLarge = errors.New("clipboard: text too large for OSC 52")

// Copier copies text. The zero value uses the real environment and tools.
type Copier struct {
	Env      terminal.Env // default terminal.OS()
	GOOS     string       // default runtime.GOOS
	LookPath func(string) (string, error)
	// Run runs name with args and stdin; default exec.CommandContext.
	Run     func(ctx context.Context, name string, args []string, stdin []byte) error
	Timeout time.Duration // per tool, default 3s
}

type tool struct {
	method Method
	name   string
	args   []string
}

// Copy copies text with the default Copier.
func Copy(ctx context.Context, text string) (Result, error) {
	return Copier{}.Copy(ctx, text)
}

// Copy copies text to the clipboard.
func (c Copier) Copy(ctx context.Context, text string) (Result, error) {
	env := c.env()
	inTmux := terminal.InTmux(env)
	if terminal.IsSSH(env) {
		return osc52Result(text, inTmux, nil)
	}
	var lastErr error
	for _, t := range c.tools(env) {
		if _, err := c.lookPath(t.name); err != nil {
			continue
		}
		if err := c.run(ctx, t, text); err != nil {
			lastErr = fmt.Errorf("%s: %w", t.name, err)
			continue
		}
		return Result{Method: t.method}, nil
	}
	return osc52Result(text, inTmux, lastErr)
}

// Sequence52 encodes OSC 52 (set clipboard "c") for text, wrapped for tmux when inTmux.
func Sequence52(text string, inTmux bool) []byte {
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
	return terminal.Passthrough([]byte(seq), inTmux)
}

func osc52Result(text string, inTmux bool, fallback error) (Result, error) {
	if base64.StdEncoding.EncodedLen(len(text)) > MaxOSC52 {
		return Result{Method: OSC52, Fallback: fallback}, ErrTooLarge
	}
	return Result{Method: OSC52, Sequence: Sequence52(text, inTmux), Fallback: fallback}, nil
}

func (c Copier) tools(env terminal.Env) []tool {
	goos := c.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	switch goos {
	case "darwin":
		return []tool{{Pbcopy, "pbcopy", nil}}
	case "windows":
		return nil
	}
	var ts []tool
	if terminal.Get(env, "WAYLAND_DISPLAY") != "" {
		ts = append(ts, tool{WlCopy, "wl-copy", nil})
	}
	if terminal.Get(env, "DISPLAY") != "" {
		ts = append(ts,
			tool{Xclip, "xclip", []string{"-selection", "clipboard"}},
			tool{Xsel, "xsel", []string{"--clipboard", "--input"}})
	}
	return ts
}

func (c Copier) env() terminal.Env {
	if c.Env != nil {
		return c.Env
	}
	return terminal.OS()
}

func (c Copier) lookPath(name string) (string, error) {
	if c.LookPath != nil {
		return c.LookPath(name)
	}
	return exec.LookPath(name)
}

func (c Copier) run(ctx context.Context, t tool, text string) error {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if c.Run != nil {
		return c.Run(ctx, t.name, t.args, []byte(text))
	}
	cmd := exec.CommandContext(ctx, t.name, t.args...)
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// xclip and wl-copy fork a daemon that keeps the selection alive; don't wait on
	// its inherited pipes. ErrWaitDelay means the tool itself exited 0.
	cmd.WaitDelay = 500 * time.Millisecond
	if err := cmd.Run(); err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, firstLine(msg))
		}
		return err
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
