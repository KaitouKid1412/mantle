// Package enginetest runs internal/engine against in-process enginefake scripts, so
// tests of any plan can drive a real Manager/Engine without spawning claude.
//
//	sp := &enginetest.Spawner{Scripts: []*enginefake.Script{script}}
//	rec := enginetest.NewRecorder()
//	m := engine.NewManager(rec.Send)
//	m.Spawner, m.RunDir = sp, "-"
//	e, _ := m.Start(ext.MainEngine, ext.SpawnOpts{})
//	rec.WaitFor(t, func(msg tea.Msg) bool { _, ok := msg.(ext.EngineExitedMsg); return ok })
//
// Primary owner: plan 02.
package enginetest

import (
	"fmt"
	"io"
	"sync"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
)

// Spawner is an engine.Spawner that runs one script per spawn, in order (the last
// script is reused when they run out). It records every spec.
type Spawner struct {
	Scripts []*enginefake.Script
	// Err, if set, makes Spawn fail.
	Err error
	// ErrFor, if set, can fail spawn number n (0-based) with an error.
	ErrFor func(n int, spec engine.SpawnSpec) error

	mu       sync.Mutex
	specs    []engine.SpawnSpec
	procs    []*enginefake.Proc
	attempts int
}

// Spawn implements engine.Spawner.
func (s *Spawner) Spawn(spec engine.SpawnSpec) (engine.Proc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	if s.ErrFor != nil {
		if err := s.ErrFor(s.attempts, spec); err != nil {
			s.attempts++
			return nil, err
		}
	}
	s.attempts++
	if len(s.Scripts) == 0 {
		return nil, fmt.Errorf("enginetest: no script")
	}
	i := len(s.specs)
	if i >= len(s.Scripts) {
		i = len(s.Scripts) - 1
	}
	p := enginefake.Start(s.Scripts[i])
	s.specs = append(s.specs, spec)
	s.procs = append(s.procs, p)
	return &proc{p: p, pid: 90000 + len(s.procs)}, nil
}

// Specs returns the specs of every spawn so far.
func (s *Spawner) Specs() []engine.SpawnSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]engine.SpawnSpec(nil), s.specs...)
}

// Procs returns every fake process so far (check Wait/Received in tests).
func (s *Spawner) Procs() []*enginefake.Proc {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*enginefake.Proc(nil), s.procs...)
}

type proc struct {
	p   *enginefake.Proc
	pid int
}

func (p *proc) Stdin() io.WriteCloser { return p.p.Stdin }
func (p *proc) Stdout() io.Reader     { return p.p.Stdout }
func (p *proc) Stderr() io.Reader     { return p.p.Stderr }

func (p *proc) Pid() int { return p.pid }

func (p *proc) Signal(sig syscall.Signal) error {
	if sig == syscall.SIGTERM || sig == syscall.SIGKILL {
		p.p.Kill()
	}
	return nil
}

func (p *proc) Wait() error {
	code, err := p.p.Wait()
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("exit status %d", code)
	}
	return nil
}

// Recorder collects messages an engine.Manager delivers (pass Recorder.Send).
type Recorder struct {
	mu   sync.Mutex
	msgs []tea.Msg
	wake chan struct{}
}

// NewRecorder returns an empty recorder.
func NewRecorder() *Recorder { return &Recorder{wake: make(chan struct{}, 1)} }

// Send records m; use it as the Manager's send function.
func (r *Recorder) Send(m tea.Msg) {
	r.mu.Lock()
	r.msgs = append(r.msgs, m)
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Msgs returns everything recorded so far.
func (r *Recorder) Msgs() []tea.Msg {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]tea.Msg(nil), r.msgs...)
}

// WaitFor waits until a recorded message satisfies match and returns it. Messages
// are scanned from the start every time, so earlier matches count too.
func (r *Recorder) WaitFor(t testing.TB, match func(tea.Msg) bool) tea.Msg {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		for _, m := range r.Msgs() {
			if match(m) {
				return m
			}
		}
		select {
		case <-r.wake:
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatalf("enginetest: timed out; got %d messages: %s", len(r.Msgs()), r.Summary())
			return nil
		}
	}
}

// Len returns how many messages were recorded so far (a mark for WaitAfter).
func (r *Recorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.msgs)
}

// WaitAfter is WaitFor over messages recorded after mark (see Len).
func (r *Recorder) WaitAfter(t testing.TB, mark int, match func(tea.Msg) bool) tea.Msg {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		msgs := r.Msgs()
		for i := mark; i < len(msgs); i++ {
			if match(msgs[i]) {
				return msgs[i]
			}
		}
		select {
		case <-r.wake:
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatalf("enginetest: timed out after mark %d; got %d messages: %s", mark, len(r.Msgs()), r.Summary())
			return nil
		}
	}
}

// Summary lists the recorded message types (for failure output).
func (r *Recorder) Summary() string {
	s := ""
	for _, m := range r.Msgs() {
		s += fmt.Sprintf("\n  %T %s", m, describe(m))
	}
	return s
}
