package engine

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// BaseArgs are always passed: the SDK's stream-json trio plus mantle's extras.
// Never add --system-prompt: the default Claude Code prompt must stay.
var BaseArgs = []string{
	"--output-format", "stream-json",
	"--input-format", "stream-json",
	"--verbose",
	"--include-partial-messages",
	"--permission-prompt-tool", "stdio",
	"--include-hook-events",
	"--forward-subagent-text",
	"--replay-user-messages",
}

// BuildArgs returns the claude arguments for o, in the SDK's style (equals form for
// session ids so a value starting with "-" is never read as a flag).
func BuildArgs(o ext.SpawnOpts) []string {
	args := append([]string(nil), BaseArgs...)
	if o.Name != "" {
		args = append(args, "-n", o.Name)
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.PermissionMode != "" {
		args = append(args, "--permission-mode", o.PermissionMode)
	}
	switch {
	case o.Resume != "":
		args = append(args, "--resume="+o.Resume)
	case o.Continue:
		args = append(args, "--continue")
	}
	if o.Resume != "" || o.Continue {
		if o.ForkSession {
			args = append(args, "--fork-session")
		}
		if o.ResumeSessionAt != "" {
			args = append(args, "--resume-session-at="+o.ResumeSessionAt)
			if o.ResumeDropsTurn {
				args = append(args, "--resume-drops-turn="+o.ResumeSessionAt)
			}
		}
	}
	if o.SessionID != "" {
		args = append(args, "--session-id="+o.SessionID)
	}
	for _, d := range o.AddDirs {
		args = append(args, "--add-dir", d)
	}
	if o.Settings != "" {
		args = append(args, "--settings", o.Settings)
	}
	return append(args, o.ExtraArgs...)
}

// Environment variables removed from the engine's environment.
var dropEnv = []string{
	"CLAUDECODE",         // the child must not think it runs inside Claude Code
	"NODE_OPTIONS",       // as the SDK does
	"DEBUG",              // as the SDK does
	"CLAUDE_CODE_SIMPLE", // set by bare mode: drops hooks, skills, plugins, MCP, CLAUDE.md
}

// BuildEnv returns the engine environment: base minus dropEnv, o.UnsetEnv (and
// CLAUDE_CODE_SAFE_MODE unless o.SafeMode), plus mantle's settings, plus o.Env
// (o.Env wins over UnsetEnv).
func BuildEnv(base []string, o ext.SpawnOpts) []string {
	drop := map[string]bool{}
	for _, k := range dropEnv {
		drop[k] = true
	}
	if !o.SafeMode {
		drop["CLAUDE_CODE_SAFE_MODE"] = true
	}
	for _, k := range o.UnsetEnv {
		drop[k] = true
	}
	set := map[string]string{
		"CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS":     "1",
		"CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING": "true",
	}
	if o.SafeMode {
		set["CLAUDE_CODE_SAFE_MODE"] = "1"
	}
	if o.Cwd != "" {
		set["PWD"] = o.Cwd
	}
	for k, v := range o.Env {
		set[k] = v
	}
	out := make([]string, 0, len(base)+len(set))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if drop[k] {
			continue
		}
		if _, override := set[k]; override {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range set {
		out = append(out, k+"="+v)
	}
	return out
}

// SpawnSpec is one process to start.
type SpawnSpec struct {
	Path string
	Args []string
	Env  []string
	Dir  string
}

// Proc is a running engine process.
type Proc interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Stderr() io.Reader
	Pid() int
	// Signal sends sig to the process group.
	Signal(sig syscall.Signal) error
	// Wait waits for exit; call once.
	Wait() error
}

// Spawner starts engine processes. ExecSpawner runs real binaries; tests use
// internal/engine/enginetest.
type Spawner interface {
	Spawn(SpawnSpec) (Proc, error)
}

// ExecSpawner starts real processes in their own process group, so terminal
// signals (ctrl+c, ctrl+z) never reach the engine.
type ExecSpawner struct{}

// Spawn implements Spawner.
func (ExecSpawner) Spawn(s SpawnSpec) (Proc, error) {
	// Our own pipes (not cmd.StdoutPipe): Wait must not close the read ends before the
	// reader has drained them.
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		closeAll(inR, inW)
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		closeAll(inR, inW, outR, outW)
		return nil, err
	}
	cmd := exec.Command(s.Path, s.Args...)
	cmd.Env = s.Env
	cmd.Dir = s.Dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		closeAll(inR, inW, outR, outW, errR, errW)
		return nil, err
	}
	closeAll(inR, outW, errW) // the child has its copies
	return &execProc{cmd: cmd, stdin: inW, stdout: outR, stderr: errR}, nil
}

func closeAll(fs ...*os.File) {
	for _, f := range fs {
		_ = f.Close()
	}
}

type execProc struct {
	cmd    *exec.Cmd
	stdin  *os.File
	stdout *os.File
	stderr *os.File
}

func (p *execProc) Stdin() io.WriteCloser { return p.stdin }
func (p *execProc) Stdout() io.Reader     { return p.stdout }
func (p *execProc) Stderr() io.Reader     { return p.stderr }
func (p *execProc) Pid() int              { return p.cmd.Process.Pid }

func (p *execProc) Signal(sig syscall.Signal) error {
	// With Setpgid the group id is the pid.
	return syscall.Kill(-p.cmd.Process.Pid, sig)
}

// Wait waits for the process. The read ends stay open: the caller closes them via
// CloseOutput once it has drained them (or gave up on a grandchild holding them).
func (p *execProc) Wait() error { return p.cmd.Wait() }

// CloseOutput closes our stdout/stderr read ends, unblocking readers.
func (p *execProc) CloseOutput() {
	_ = p.stdout.Close()
	_ = p.stderr.Close()
}

// OutputCloser is implemented by Procs whose output pipes must be closed by the
// owner after exit.
type OutputCloser interface {
	CloseOutput()
}

// FindBinary returns the claude binary to run: $MANTLE_CLAUDE_BIN, else the binary
// pinned in ~/.mantle/state/engines.json, else "claude" on PATH.
func FindBinary() (string, error) {
	return ResolveBinary(DefaultEngineStatePath())
}
