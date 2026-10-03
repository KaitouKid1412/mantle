package claudecli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// EnvBinary names the environment variable that overrides which claude binary
// mantle runs. The engine (plan 02) and the CLI passthrough (plan 11) resolve the
// binary the same way, through ResolveBinary.
const EnvBinary = "MANTLE_CLAUDE_BIN"

// ErrNotFound means no claude binary could be resolved.
var ErrNotFound = errors.New("claude binary not found (set " + EnvBinary + " or add claude to PATH)")

// ResolveBinary returns the claude binary to run: $MANTLE_CLAUDE_BIN when set (a
// path, or a name looked up on PATH), otherwise "claude" on PATH.
func ResolveBinary() (string, error) {
	return resolveBinary(os.Getenv, exec.LookPath)
}

func resolveBinary(getenv func(string) string, lookPath func(string) (string, error)) (string, error) {
	if p := getenv(EnvBinary); p != "" {
		if strings.ContainsRune(p, filepath.Separator) {
			if st, err := os.Stat(p); err != nil || st.IsDir() {
				return "", fmt.Errorf("%s=%s: %w", EnvBinary, p, ErrNotFound)
			}
			return p, nil
		}
		if lp, err := lookPath(p); err == nil {
			return lp, nil
		}
		return "", fmt.Errorf("%s=%s: %w", EnvBinary, p, ErrNotFound)
	}
	if lp, err := lookPath("claude"); err == nil {
		return lp, nil
	}
	return "", ErrNotFound
}

// Runner runs claude subcommands. The zero value is ready to use.
type Runner struct {
	// Bin is the claude binary. Empty means ResolveBinary on every call.
	Bin string
	// Dir is the working directory; empty means the current directory. Several
	// subcommands (mcp, plugin scopes, import) read the project in the cwd.
	Dir string
	// Env is the base environment; nil means os.Environ().
	Env []string
	// Timeout, when positive, replaces each subcommand's default timeout.
	Timeout time.Duration
	// MaxOutput caps captured stdout and stderr each; 0 means 64 MiB.
	MaxOutput int
}

// Default is the Runner used by the package-level functions.
var Default = &Runner{}

// Call is one captured run.
type Call struct {
	Sub  Subcommand
	Args []string
	// Stdin is passed only to subcommands that declare stdin; otherwise stdin is
	// /dev/null.
	Stdin []byte
	// Env holds extra KEY=VALUE entries; only names the subcommand declares are
	// accepted (for example MCP_CLIENT_SECRET for mcp add).
	Env []string
}

// Result is a finished run. ExitCode is -1 when the process did not exit normally.
type Result struct {
	Argv     []string // argv after the binary
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// ExitError is a run that exited non-zero.
type ExitError struct {
	Sub    Subcommand
	Code   int
	Stderr string // ANSI-stripped
}

func (e *ExitError) Error() string {
	msg := firstLine(e.Stderr)
	if msg == "" {
		return fmt.Sprintf("claude %s: exit status %d", e.Sub, e.Code)
	}
	return fmt.Sprintf("claude %s: %s", e.Sub, msg)
}

// Run runs sub with captured output; see Runner.Run.
func Run(ctx context.Context, sub Subcommand, args ...string) (stdout, stderr []byte, err error) {
	return Default.Run(ctx, sub, args...)
}

// Interactive builds an *exec.Cmd for tea.ExecProcess; see Runner.Interactive.
func Interactive(sub Subcommand, args ...string) (*exec.Cmd, error) {
	return Default.Interactive(sub, args...)
}

// Run runs sub with captured output, stdin /dev/null and the subcommand's
// timeout. A non-zero exit returns the output together with an *ExitError.
func (r *Runner) Run(ctx context.Context, sub Subcommand, args ...string) (stdout, stderr []byte, err error) {
	res, err := r.Do(ctx, Call{Sub: sub, Args: args})
	return res.Stdout, res.Stderr, err
}

// Do runs one Call with captured output.
func (r *Runner) Do(ctx context.Context, c Call) (Result, error) {
	if !c.Sub.Captured() {
		if c.Sub.valid() {
			return Result{}, &ArgError{Sub: c.Sub, Msg: "interactive only; use Interactive"}
		}
		return Result{}, &ArgError{Sub: c.Sub, Msg: "unknown subcommand"}
	}
	sp := c.Sub.spec()
	if len(c.Stdin) > 0 && !sp.stdin {
		return Result{}, &ArgError{Sub: c.Sub, Msg: "does not take stdin"}
	}
	for _, kv := range c.Env {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || !contains(sp.env, k) {
			return Result{}, &ArgError{Sub: c.Sub, Msg: fmt.Sprintf("environment variable %q is not allowed", k)}
		}
	}
	argv, err := buildArgv(c.Sub, c.Args)
	if err != nil {
		return Result{}, err
	}
	bin, err := r.bin()
	if err != nil {
		return Result{Argv: argv}, err
	}

	timeout := sp.timeout
	if r.Timeout > 0 {
		timeout = r.Timeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	max := r.MaxOutput
	if max <= 0 {
		max = 64 << 20
	}
	var stdout, stderr cappedBuffer
	stdout.max, stderr.max = max, max

	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Dir = r.Dir
	cmd.Env = append(r.environ(true), c.Env...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if len(c.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(c.Stdin)
	}
	setProcessGroup(cmd)
	cmd.WaitDelay = 2 * time.Second

	runErr := cmd.Run()
	res := Result{Argv: argv, Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: -1}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctxErr := ctx.Err(); ctxErr != nil && runErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return res, fmt.Errorf("claude %s: timed out after %s: %w", c.Sub, timeout, ctxErr)
		}
		return res, fmt.Errorf("claude %s: %w", c.Sub, ctxErr)
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return res, &ExitError{Sub: c.Sub, Code: res.ExitCode, Stderr: ansi.Strip(string(res.Stderr))}
	}
	if runErr != nil {
		return res, fmt.Errorf("claude %s: %w", c.Sub, runErr)
	}
	return res, nil
}

// Interactive builds an *exec.Cmd for flows that need the terminal (auth login,
// mcp login, doctor, attach, …). Stdin, stdout and stderr are left unset so
// tea.ExecProcess attaches the terminal. No timeout applies.
func (r *Runner) Interactive(sub Subcommand, args ...string) (*exec.Cmd, error) {
	if !sub.Interactive() {
		if sub.valid() {
			return nil, &ArgError{Sub: sub, Msg: "not an interactive subcommand; use Run"}
		}
		return nil, &ArgError{Sub: sub, Msg: "unknown subcommand"}
	}
	argv, err := buildArgv(sub, args)
	if err != nil {
		return nil, err
	}
	bin, err := r.bin()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, argv...)
	cmd.Dir = r.Dir
	cmd.Env = r.environ(false)
	return cmd, nil
}

// Help returns sub's --help text. Every Subcommand prefix is a real subcommand
// path, so this never falls back to the root help or a prompt.
func (r *Runner) Help(ctx context.Context, sub Subcommand) (string, error) {
	if !sub.valid() {
		return "", &ArgError{Sub: sub, Msg: "unknown subcommand"}
	}
	if sub == Version {
		return "", &ArgError{Sub: sub, Msg: "no help for --version"}
	}
	bin, err := r.bin()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeoutShort)
	defer cancel()
	argv := append(sub.Prefix(), "--help")
	if sub == RootHelp {
		argv = []string{"--help"}
	}
	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Dir = r.Dir
	cmd.Env = r.environ(true)
	setProcessGroup(cmd)
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("claude %s --help: %w", sub, err)
	}
	return string(out), nil
}

func (r *Runner) bin() (string, error) {
	if r.Bin != "" {
		return r.Bin, nil
	}
	return ResolveBinary()
}

// environ returns the child environment. CLAUDECODE is always removed: it marks
// a process as running inside Claude Code and changes CLI behaviour. Captured
// runs also drop colour so text parsers see plain output.
func (r *Runner) environ(captured bool) []string {
	base := r.Env
	if base == nil {
		base = os.Environ()
	}
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "CLAUDECODE":
			continue
		case captured && (k == "FORCE_COLOR" || k == "NO_COLOR"):
			continue
		}
		out = append(out, kv)
	}
	if captured {
		out = append(out, "NO_COLOR=1")
	}
	return out
}

// cappedBuffer keeps at most max bytes and silently drops the rest, so a runaway
// child cannot exhaust memory while the pipe keeps draining. It deliberately does
// not embed bytes.Buffer: io.Copy would use its ReadFrom and bypass the cap.
type cappedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room < len(p) {
		b.truncated = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) Bytes() []byte { return b.buf.Bytes() }

var _ io.Writer = (*cappedBuffer)(nil)

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
