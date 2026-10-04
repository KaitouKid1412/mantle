package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// HandoffState is what a new mantle-ui needs to adopt a running engine after the old
// one exec'ed it in place (plan 10's instant restart, MT-32). The engine process and
// its pipes survive the exec; this carries the engine's state across.
type HandoffState struct {
	EngineID     string          `json:"id"`
	PID          int             `json:"pid"`
	PGID         int             `json:"pgid"`
	StdinFD      int             `json:"stdin_fd"`
	StdoutFD     int             `json:"stdout_fd"`
	StderrFD     int             `json:"stderr_fd"`
	Init         json.RawMessage `json:"init,omitempty"` // the initialize response
	Session      ext.SessionInfo `json:"session"`
	Capabilities []string        `json:"capabilities,omitempty"`
	// Pending is engine stdout read but not yet processed when the reader stopped.
	Pending []byte        `json:"pending,omitempty"`
	Opts    ext.SpawnOpts `json:"opts"` // for later restarts

	// The pipe files (this process only; not serialised).
	Stdin, Stdout, Stderr *os.File `json:"-"`
}

// Errors from PrepareHandoff.
var (
	ErrNotHandoffable = errors.New("engine: this engine's pipes can't be handed off")
	ErrBusy           = errors.New("engine: busy (pending control requests or prompts); hand off when idle")
)

// PrepareHandoff readies the engine to survive an exec of mantle-ui: it flushes and
// detaches the writer, stops the stdout and stderr readers at a read deadline
// (keeping any unprocessed bytes in Pending), clears close-on-exec on the three pipe
// fds and returns the state the new process passes to Manager.Adopt. The engine
// must be idle. Nothing is closed and the engine is not stopped; after this call
// this Engine no longer reads or writes, so exec (or Adopt in tests) should follow.
func (e *Engine) PrepareHandoff(ctx context.Context) (HandoffState, error) {
	e.life.Lock()
	defer e.life.Unlock()
	r := e.current()
	if r == nil {
		return HandoffState{}, ErrNotRunning
	}
	p, ok := r.proc.(*execProc)
	if !ok {
		return HandoffState{}, ErrNotHandoffable
	}
	if r.corr.Len() > 0 || len(r.inbound.drainPeek()) > 0 {
		return HandoffState{}, ErrBusy
	}
	r.setHandedOff()

	// Writer: flush what is queued, then stop without closing stdin.
	r.tr.Detach()
	select {
	case <-r.tr.WriteDone():
	case <-ctx.Done():
		return HandoffState{}, ctx.Err()
	}
	// Readers: wake them with a deadline; they stop without closing.
	now := time.Now()
	_ = p.stdout.SetReadDeadline(now)
	_ = p.stderr.SetReadDeadline(now)
	select {
	case <-r.tr.ReadDone():
	case <-ctx.Done():
		return HandoffState{}, ctx.Err()
	}
	select {
	case <-r.stderrDone:
	case <-time.After(time.Second):
	}
	r.coal.Flush()
	_ = p.stdout.SetReadDeadline(time.Time{})
	_ = p.stderr.SetReadDeadline(time.Time{})

	st := HandoffState{
		EngineID: e.id, PID: p.Pid(), PGID: p.Pid(),
		Init: r.initRaw(), Session: r.track.Info(), Capabilities: r.caps.list(),
		Pending: r.tr.Leftover(), Opts: e.Options(),
		Stdin: p.stdin, Stdout: p.stdout, Stderr: p.stderr,
	}
	for _, f := range []struct {
		file *os.File
		fd   *int
	}{{p.stdin, &st.StdinFD}, {p.stdout, &st.StdoutFD}, {p.stderr, &st.StderrFD}} {
		fd, err := keepOnExec(f.file)
		if err != nil {
			return st, err
		}
		*f.fd = fd
	}
	e.mu.Lock()
	if e.cur == r {
		e.cur = nil
	}
	e.mu.Unlock()
	return st, nil
}

// keepOnExec clears FD_CLOEXEC on f (without switching it to blocking mode) and
// returns its descriptor number.
func keepOnExec(f *os.File) (int, error) {
	rc, err := f.SyscallConn()
	if err != nil {
		return 0, err
	}
	var fd int
	var ferr error
	if err := rc.Control(func(d uintptr) {
		fd = int(d)
		_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, d, syscall.F_SETFD, 0)
		if errno != 0 {
			ferr = errno
		}
	}); err != nil {
		return 0, err
	}
	return fd, ferr
}

// AdoptSpec describes an engine inherited across exec.
type AdoptSpec struct {
	PID, PGID             int
	Stdin, Stdout, Stderr *os.File
	Init                  json.RawMessage
	Session               ext.SessionInfo
	Capabilities          []string
	Pending               []byte
	Opts                  ext.SpawnOpts
}

// SpecFromState turns a hand-off state read in the new process into an AdoptSpec,
// wrapping the inherited descriptors.
func SpecFromState(s HandoffState) AdoptSpec {
	return AdoptSpec{
		PID: s.PID, PGID: s.PGID,
		Stdin:  os.NewFile(uintptr(s.StdinFD), "engine-stdin"),
		Stdout: os.NewFile(uintptr(s.StdoutFD), "engine-stdout"),
		Stderr: os.NewFile(uintptr(s.StderrFD), "engine-stderr"),
		Init:   s.Init, Session: s.Session, Capabilities: s.Capabilities,
		Pending: s.Pending, Opts: s.Opts,
	}
}

// Adopt takes over a running engine inherited from the previous mantle-ui (same pid,
// after exec). It does not send initialize again: the capability cache, session and
// command list come from the spec. Messages follow as for Start: EngineAttachMsg,
// SessionChangedMsg, CommandsMsg and a ControlResultMsg for "initialize" carrying the
// original response, so features rebuild their state the same way.
func (m *Manager) Adopt(id string, s AdoptSpec) (*Engine, error) {
	if id == "" {
		id = ext.MainEngine
	}
	if s.Stdin == nil || s.Stdout == nil || s.Stderr == nil || s.PID <= 0 {
		return nil, fmt.Errorf("engine %s: incomplete adopt spec", id)
	}
	m.mu.Lock()
	e := m.engines[id]
	if e == nil {
		e = &Engine{id: id, mgr: m, pump: newPump(m.send)}
		m.engines[id] = e
	}
	m.mu.Unlock()

	e.life.Lock()
	defer e.life.Unlock()
	if e.current() != nil {
		return e, fmt.Errorf("engine %s: already running", id)
	}
	pgid := s.PGID
	if pgid == 0 {
		pgid = s.PID
	}
	proc := &adoptedProc{pid: s.PID, pgid: pgid, stdin: s.Stdin, stdout: s.Stdout, stderr: s.Stderr}
	r := &run{
		e:       e,
		proc:    proc,
		stderr:  NewRing(StderrRingSize),
		caps:    newCapabilities(),
		track:   newTracker(id, s.Opts),
		inbound: newInboundSet(),
		exited:  make(chan struct{}),
	}
	var stdout io.Reader = s.Stdout
	if len(s.Pending) > 0 {
		stdout = io.MultiReader(bytes.NewReader(s.Pending), s.Stdout)
	}
	r.tr = NewTransport(stdout, s.Stdin)
	r.tr.Tap = m.Tap
	r.corr = newCorrelator(r.tr.Send)
	r.coal = newCoalescer(id, m.CoalesceInterval, e.pump.Enqueue)
	r.caps.AddCapabilities(s.Capabilities)
	r.caps.SetVersion(s.Session.ClaudeVersion)
	var ir proto.InitializeResponse
	haveInit := len(s.Init) > 0 && json.Unmarshal(s.Init, &ir) == nil
	if haveInit {
		r.caps.AddCapabilities(ir.Capabilities)
		r.track.ObserveInitialize(&ir)
		r.saveInit(s.Init)
	}
	r.track.restore(s.Session)

	e.mu.Lock()
	e.opts = s.Opts
	e.cur, e.last = r, r
	e.mu.Unlock()

	msgs := []tea.Msg{
		ext.EngineAttachMsg{EngineID: id, Engine: e},
		ext.SessionChangedMsg{EngineID: id, Info: r.track.Info()},
	}
	if haveInit {
		msgs = append(msgs, r.commandsMsg(ir.Commands),
			ext.ControlResultMsg{EngineID: id, Subtype: proto.SubInitialize, Resp: s.Init})
	}
	e.pump.Enqueue(msgs)
	bin := m.Binary
	r.runFile = m.writeRunFile(id, bin, s.PID)
	r.stderrDone = make(chan struct{})
	go func() {
		_, _ = io.Copy(r.stderr, s.Stderr)
		close(r.stderrDone)
	}()
	r.tr.Start(r.onEvent, func(err error) { m.logf("engine %s: %v", id, err) })
	go r.wait(r.stderrDone)
	return e, nil
}

// adoptedProc is an engine process inherited across exec: still our child, so it can
// be waited for, but not started by this process's exec.Cmd.
type adoptedProc struct {
	pid, pgid             int
	stdin, stdout, stderr *os.File
}

func (p *adoptedProc) Stdin() io.WriteCloser { return p.stdin }
func (p *adoptedProc) Stdout() io.Reader     { return p.stdout }
func (p *adoptedProc) Stderr() io.Reader     { return p.stderr }
func (p *adoptedProc) Pid() int              { return p.pid }

func (p *adoptedProc) Signal(sig syscall.Signal) error { return syscall.Kill(-p.pgid, sig) }

func (p *adoptedProc) Wait() error {
	proc, err := os.FindProcess(p.pid)
	if err != nil {
		return err
	}
	st, err := proc.Wait()
	if err != nil {
		return err
	}
	if !st.Success() {
		return fmt.Errorf("engine exited: %s", st)
	}
	return nil
}

func (p *adoptedProc) CloseOutput() {
	_ = p.stdout.Close()
	_ = p.stderr.Close()
}

func (r *run) saveInit(raw json.RawMessage) {
	r.initMu.Lock()
	r.initRsp = bytes.Clone(raw)
	r.initMu.Unlock()
}

func (r *run) initRaw() json.RawMessage {
	r.initMu.Lock()
	defer r.initMu.Unlock()
	return r.initRsp
}

func (r *run) setHandedOff() {
	r.initMu.Lock()
	r.handed = true
	r.initMu.Unlock()
}

func (r *run) handedOff() bool {
	r.initMu.Lock()
	defer r.initMu.Unlock()
	return r.handed
}

// HandoffFile is $MANTLE_HOME/run/<pid>.handoff.json.
type HandoffFile struct {
	Engines []HandoffState `json:"engines"`
	Argv    []string       `json:"argv,omitempty"`
}

// WriteHandoffFile writes f for the next mantle-ui.
func WriteHandoffFile(path string, f HandoffFile) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// AdoptFile adopts every engine listed in a hand-off file, then deletes the file.
func (m *Manager) AdoptFile(path string) ([]*Engine, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	var f HandoffFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	var out []*Engine
	var errs []error
	for _, st := range f.Engines {
		e, err := m.Adopt(st.EngineID, SpecFromState(st))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, e)
	}
	return out, errors.Join(errs...)
}
