package selfmod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/KaitouKid1412/mantle/internal/launcher"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
)

// SmokeConfig configures step 9, the pty smoke boot: start the candidate
// mantle-ui in a pseudo-terminal against a scripted engine, drive it, and
// expect a clean exit.
type SmokeConfig struct {
	// Disabled skips the step.
	Disabled bool
	// Args are mantle-ui's arguments.
	Args []string
	// Env is added to mantle-ui's environment (for example MANTLE_CLAUDE_BIN
	// pointing at fakeclaude and its script).
	Env []string
	// Cols and Rows size the terminal (100x30).
	Cols, Rows int
	// Script is the interaction. Empty means: wait for a first frame, then
	// expect the process to exit on its own.
	Script []SmokeAction
	// ExitCode is the expected exit code (0).
	ExitCode int
	// FakeEngine, if set, is a package built from the candidate (for example
	// "./cmd/fakeclaude") and used as the engine through MANTLE_CLAUDE_BIN.
	FakeEngine string
	// FakeScript is the fake engine's script, written to the build folder and
	// passed as FAKECLAUDE_SCRIPT.
	FakeScript string
}

// smokeEngineScript answers initialize with plan 02's default reply (INIT),
// then replies "smoke reply" to the first prompt (enginefake format). The
// rules answer the control requests mantle sends around startup, in any
// order relative to the prompt (as plan 01's smoke test does); they are
// explicit, not a catch-all, so end_session still reaches the fake's
// built-in handler.
const smokeEngineScript = `# mantle pipeline smoke boot
{"on": {"type": "control_request", "request": {"subtype": "file_suggestions"}}, "respond": {"suggestions": []}}
{"on": {"type": "control_request", "request": {"subtype": "get_context_usage"}}, "respond": {"categories": [], "totalTokens": 0, "maxTokens": 200000, "percentage": 0}}
{"on": {"type": "control_request", "request": {"subtype": "mcp_status"}}, "respond": {"mcpServers": []}}
{"on": {"type": "control_request", "request": {"subtype": "list_models"}}, "respond": {"models": []}}
{"on": {"type": "control_request", "request": {"subtype": "get_settings"}}, "respond": {}}
{"on": {"type": "control_request", "request": {"subtype": "get_usage"}}, "respond": {}}
{"on": {"type": "control_request", "request": {"subtype": "get_hooks_listing"}}, "respond": {}}
{"on": {"type": "control_request", "request": {"subtype": "initialize"}}, "respond": INIT}
{"expect": {"type": "user"}, "timeout": 30000}
{"emit": {"type": "system", "subtype": "init", "session_id": "s-smoke", "uuid": "i1", "cwd": "/tmp", "tools": [], "mcp_servers": [], "model": "claude-test", "permissionMode": "default", "slash_commands": [], "apiKeySource": "none", "claude_code_version": "2.1.288", "output_style": "default"}}
{"emit": {"type": "assistant", "session_id": "s-smoke", "uuid": "a1", "parent_tool_use_id": null, "message": {"id": "msg_smoke", "type": "message", "role": "assistant", "model": "claude-test", "content": [{"type": "text", "text": "smoke reply"}], "stop_reason": "end_turn"}}}
{"emit": {"type": "result", "subtype": "success", "session_id": "s-smoke", "uuid": "r1", "is_error": false, "result": "smoke reply", "num_turns": 1, "duration_ms": 1, "duration_api_ms": 1, "total_cost_usd": 0, "usage": {"input_tokens": 1, "output_tokens": 1}}}
`

// smokeHome prepares an isolated HOME for the smoke boot, with Claude Code's
// config in it and the project already trusted, so the startup gates pass
// without a dialog and nothing of the user's state is read or written.
func smokeHome(home, project string) ([]string, error) {
	if real, err := filepath.EvalSymlinks(project); err == nil {
		project = real
	}
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return nil, err
	}
	trust, _ := json.Marshal(map[string]any{
		"hasCompletedOnboarding": true,
		"projects":               map[string]any{project: map[string]any{"hasTrustDialogAccepted": true}},
	})
	for _, p := range []string{filepath.Join(claudeDir, ".claude.json"), filepath.Join(home, ".claude.json")} {
		if err := os.WriteFile(p, trust, 0o600); err != nil {
			return nil, err
		}
	}
	return []string{
		"HOME=" + home,
		"CLAUDE_CONFIG_DIR=" + claudeDir,
		launcher.EnvHome + "=" + filepath.Join(home, ".mantle"),
		"MANTLE_DRIFT_CHECK=off",
	}, nil
}

// DefaultSmokeConfig is the M1 smoke boot: against the candidate's own
// fakeclaude, wait for the first frame, type a prompt, see the scripted
// reply, open /help, quit with ctrl+c twice, expect exit 0.
func DefaultSmokeConfig() SmokeConfig {
	return SmokeConfig{
		FakeEngine: "./cmd/fakeclaude",
		FakeScript: strings.Replace(smokeEngineScript, "INIT", string(enginefake.DefaultInitializeResponse), 1),
		Script: []SmokeAction{
			Expect(`\S`),
			Send("hello\r"),
			Expect(`smoke reply`),
			Send("/help\r"),
			Expect(`(?i)help|commands`),
			Send("\x1b"), // close the help view
			Sleep(200 * time.Millisecond),
			Send("\x03"),
			Sleep(100 * time.Millisecond),
			Send("\x03"),
		},
	}
}

// SmokeAction is one scripted interaction. Exactly one field is set.
type SmokeAction struct {
	// Expect waits until this regexp matches the screen output (escape
	// sequences removed) produced since the previous Expect.
	Expect string `json:"expect,omitempty"`
	// Send types these bytes.
	Send string `json:"send,omitempty"`
	// Sleep pauses.
	Sleep time.Duration `json:"sleep,omitempty"`
}

// Expect, Send and Sleep build smoke actions.
func Expect(re string) SmokeAction      { return SmokeAction{Expect: re} }
func Send(s string) SmokeAction         { return SmokeAction{Send: s} }
func Sleep(d time.Duration) SmokeAction { return SmokeAction{Sleep: d} }
func firstFrame() SmokeAction           { return SmokeAction{Expect: `\S`} }
func (a SmokeAction) String() string {
	switch {
	case a.Expect != "":
		return "expect " + a.Expect
	case a.Send != "":
		return fmt.Sprintf("send %q", a.Send)
	}
	return "sleep " + a.Sleep.String()
}

// smoke is step 9.
func (sc *stepCtx) smoke(ctx context.Context) error {
	cfg := sc.p.cfg.Smoke
	if cfg.Disabled {
		return errSkip{"smoke boot disabled"}
	}
	bin, err := sc.binary()
	if err != nil {
		return err
	}
	extra := append(sc.safetyEnv(), "TERM=xterm-256color", "COLORTERM=truecolor")
	if cfg.FakeEngine != "" {
		fake := filepath.Join(sc.r.OutDir, "fakeclaude")
		if out, err := sc.exec(ctx, sc.r.Dir, sc.env(nil), sc.p.cfg.Go, "build", "-o", fake, cfg.FakeEngine); err != nil {
			return &stepFailure{summary: "cannot build the fake engine " + cfg.FakeEngine, lines: extractErrors(out)}
		}
		script := filepath.Join(sc.r.LogDir, "smoke-engine.jsonl")
		if err := os.WriteFile(script, []byte(cfg.FakeScript), 0o644); err != nil {
			return err
		}
		extra = append(extra, launcher.EnvClaudeBin+"="+fake, "FAKECLAUDE_SCRIPT="+script)
		home, err := smokeHome(filepath.Join(sc.r.LogDir, "smoke-home"), sc.r.Dir)
		if err != nil {
			return err
		}
		extra = append(extra, home...)
	}
	env := sc.env(append(extra, cfg.Env...))
	if err := RunSmoke(ctx, bin, sc.r.Dir, env, cfg, sc.log); err != nil {
		var se *SmokeError
		if errors.As(err, &se) {
			lines := []string{se.Reason, "last screen output:"}
			for _, ln := range strings.Split(strings.TrimSpace(se.Screen), "\n") {
				if ln = strings.TrimSpace(ln); ln != "" {
					lines = append(lines, "  "+ln)
				}
			}
			return &stepFailure{summary: "smoke boot: " + se.Reason, lines: lines}
		}
		return err
	}
	return nil
}

// SmokeError is a failed smoke boot.
type SmokeError struct {
	Reason string
	// Screen is the tail of the output (escape sequences removed).
	Screen string
}

func (e *SmokeError) Error() string { return e.Reason }

// RunSmoke runs bin in a pty and plays cfg.Script. The raw output and every
// action are written to log. It returns nil on success.
func RunSmoke(ctx context.Context, bin, dir string, env []string, cfg SmokeConfig, log io.Writer) error {
	cols, rows := cfg.Cols, cfg.Rows
	if cols == 0 {
		cols = 100
	}
	if rows == 0 {
		rows = 30
	}
	script := cfg.Script
	if len(script) == 0 {
		script = []SmokeAction{firstFrame()}
	}
	cmd := exec.Command(bin, cfg.Args...)
	cmd.Dir = dir
	cmd.Env = env
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return &SmokeError{Reason: "cannot start mantle-ui in a pty: " + err.Error()}
	}
	defer f.Close()
	// pty.Start makes the child a session leader, so its pid is its group.
	pgid := cmd.Process.Pid
	killed := false
	kill := func() {
		if !killed {
			killed = true
			syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}
	defer kill()

	out := &screenBuffer{}
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 32<<10)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				out.write(buf[:n])
				log.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	fail := func(format string, args ...any) error {
		reason := fmt.Sprintf(format, args...)
		fmt.Fprintf(log, "\n[smoke] FAIL: %s\n", reason)
		kill()
		reapSmokeEngines(env)
		return &SmokeError{Reason: reason, Screen: out.tail(1500)}
	}

	// waitFor polls the output for re until it matches, the process exits
	// (after which the remaining output is checked once more) or ctx ends.
	waitFor := func(re *regexp.Regexp) (ok bool, exitErr error, exitedEarly bool) {
		for {
			if out.consume(re) {
				return true, nil, false
			}
			select {
			case <-ctx.Done():
				return false, nil, false
			case err := <-exited:
				<-readDone
				exited <- err // keep it for the final check
				return out.consume(re), err, true
			case <-time.After(20 * time.Millisecond):
			}
		}
	}

	for i, a := range script {
		fmt.Fprintf(log, "\n[smoke] %d: %s\n", i+1, a)
		switch {
		case a.Expect != "":
			re, err := regexp.Compile(a.Expect)
			if err != nil {
				return fail("bad expect pattern %q: %v", a.Expect, err)
			}
			ok, exitErr, exitedEarly := waitFor(re)
			switch {
			case ok:
			case exitedEarly:
				return fail("mantle-ui exited (%s) before %q appeared (step %d)", exitText(exitErr), a.Expect, i+1)
			default:
				return fail("timed out waiting for %q (step %d)", a.Expect, i+1)
			}
		case a.Send != "":
			if _, err := io.WriteString(f, a.Send); err != nil {
				return fail("cannot type %q: %v", a.Send, err)
			}
		case a.Sleep > 0:
			select {
			case <-ctx.Done():
				return fail("timed out during sleep (step %d)", i+1)
			case <-time.After(a.Sleep):
			}
		}
	}

	select {
	case err := <-exited:
		code := exitCode(err)
		reapSmokeEngines(env)
		if code != cfg.ExitCode {
			return fail("mantle-ui exited with %s, want exit code %d", exitText(err), cfg.ExitCode)
		}
		fmt.Fprintf(log, "\n[smoke] exited with code %d\n", code)
		return nil
	case <-ctx.Done():
		return fail("mantle-ui did not exit after the script")
	}
}

// reapSmokeEngines kills engine process groups recorded in the run files of
// the smoke boot's isolated mantle home.
func reapSmokeEngines(env []string) {
	home := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, launcher.EnvHome+"="); ok {
			home = v
		}
	}
	if home == "" {
		return
	}
	runs, _ := launcher.Layout{Root: home}.RunFiles()
	for _, rf := range runs {
		for _, pg := range rf.EnginePGIDs {
			if pg > 1 && pg != syscall.Getpgrp() {
				syscall.Kill(-pg, syscall.SIGKILL)
			}
		}
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func exitText(err error) string {
	if err == nil {
		return "exit code 0"
	}
	return err.Error()
}

// screenBuffer accumulates pty output with escape sequences removed, and a
// read position for Expect.
type screenBuffer struct {
	mu        sync.Mutex
	text      strings.Builder
	pos       int
	tailBytes []byte // incomplete escape sequence carried to the next write
}

func (s *screenBuffer) write(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := append(s.tailBytes, p...)
	clean, rest := stripANSI(data)
	s.tailBytes = rest
	s.text.WriteString(clean)
}

// consume reports whether re matches the text after the read position, and
// moves the position past the match.
func (s *screenBuffer) consume(re *regexp.Regexp) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.text.String()[s.pos:]
	loc := re.FindStringIndex(t)
	if loc == nil {
		return false
	}
	s.pos += loc[1]
	return true
}

func (s *screenBuffer) tail(n int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.text.String()
	return t[max(0, len(t)-n):]
}

// stripANSI removes CSI, OSC, DCS and two-byte escape sequences and other
// control characters except newlines. An escape sequence cut off at the end
// is returned as rest, to be completed by the next write.
func stripANSI(p []byte) (clean string, rest []byte) {
	var b strings.Builder
	i := 0
	for i < len(p) {
		c := p[i]
		if c != 0x1b {
			switch {
			case c == '\n':
				b.WriteByte('\n')
			case c == '\r' || c == '\t':
				b.WriteByte(' ')
			case c < 0x20 || c == 0x7f:
			default:
				b.WriteByte(c)
			}
			i++
			continue
		}
		if i+1 >= len(p) {
			return b.String(), append([]byte(nil), p[i:]...)
		}
		switch p[i+1] {
		case '[': // CSI ... final byte 0x40-0x7e
			j := i + 2
			for j < len(p) && (p[j] < 0x40 || p[j] > 0x7e) {
				j++
			}
			if j >= len(p) {
				return b.String(), append([]byte(nil), p[i:]...)
			}
			if p[j] != 'm' {
				b.WriteByte(' ') // cursor moves often separate words; SGR does not move
			}
			i = j + 1
		case ']', 'P', '_', '^': // OSC, DCS, APC, PM: until BEL or ST
			j := i + 2
			for j < len(p) && p[j] != 0x07 && !(p[j] == 0x1b && j+1 < len(p) && p[j+1] == '\\') {
				j++
			}
			if j >= len(p) || (p[j] == 0x1b && j+1 >= len(p)) {
				return b.String(), append([]byte(nil), p[i:]...)
			}
			if p[j] == 0x07 {
				i = j + 1
			} else {
				i = j + 2
			}
		default: // ESC, intermediate bytes 0x20-0x2f, final byte
			j := i + 1
			for j < len(p) && p[j] >= 0x20 && p[j] <= 0x2f {
				j++
			}
			if j >= len(p) {
				return b.String(), append([]byte(nil), p[i:]...)
			}
			i = j + 1
		}
	}
	return b.String(), nil
}
