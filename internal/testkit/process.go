package testkit

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// Process is a Harness over an external program (mantle-ui, mantle, claude) running on a
// pseudo-terminal: the screen and scrollback work exactly as for in-process programs.
// Used for smoke boots (plan 10's pipeline step 9) and end-to-end tests.
type Process struct {
	*Harness
	cmd *exec.Cmd

	exitErr error
}

// StartProcess runs cmd as the session leader of a new pty of the given size
// (WithSize; default 80×24). cmd.Stdin/Stdout/Stderr are set to the pty. The process
// is killed when the test ends.
func StartProcess(tb testing.TB, cmd *exec.Cmd, opts ...Option) *Process {
	tb.Helper()
	cfg := config{w: 80, h: 24}
	for _, o := range opts {
		o(&cfg)
	}
	term, _, _, err := newLockedTerm(cfg.w, cfg.h, false)
	if err != nil {
		tb.Fatalf("testkit: %v", err)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = term.tty, term.tty, term.tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		term.close()
		tb.Fatalf("testkit: start %s: %v", cmd.Path, err)
	}
	p := &Process{Harness: &Harness{tb: tb, term: term, done: make(chan struct{})}, cmd: cmd}
	go func() {
		err := cmd.Wait()
		p.Harness.mu.Lock()
		p.exitErr = err
		p.Harness.runErr = err
		p.Harness.mu.Unlock()
		close(p.Harness.done)
	}()
	tb.Cleanup(func() {
		select {
		case <-p.Harness.done:
		default:
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			select {
			case <-p.Harness.done:
			case <-time.After(3 * time.Second):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				<-p.Harness.done
			}
		}
		term.close()
	})
	return p
}

// Resize resizes the pty; the kernel sends the process SIGWINCH.
func (p *Process) Resize(w, h int) { p.term.resize(w, h) }

// Signal sends a signal to the process group.
func (p *Process) Signal(sig syscall.Signal) error { return syscall.Kill(-p.cmd.Process.Pid, sig) }

// ExitCode waits for the process to exit and returns its exit code.
func (p *Process) ExitCode(timeout time.Duration) int {
	p.tb.Helper()
	select {
	case <-p.Harness.done:
	case <-time.After(timeout):
		p.tb.Fatalf("testkit: %s did not exit within %v\n--- screen ---\n%s", p.cmd.Path, timeout, p.Screen())
	}
	p.Harness.mu.Lock()
	defer p.Harness.mu.Unlock()
	var ee *exec.ExitError
	if errors.As(p.exitErr, &ee) {
		return ee.ExitCode()
	}
	if p.exitErr != nil {
		return -1
	}
	return 0
}
