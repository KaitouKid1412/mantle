package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Shutdown grace periods (end_session, then stdin EOF, then SIGTERM, then SIGKILL).
var (
	EndSessionGrace = 3 * time.Second
	StdinGrace      = 5 * time.Second
	TermGrace       = 5 * time.Second
	// OutputDrainGrace bounds how long stdout may stay open after the engine exited
	// (a grandchild holding the pipe).
	OutputDrainGrace = 2 * time.Second
)

// ErrNotRunning is returned when an engine has no live process.
var ErrNotRunning = errors.New("engine: not running")

// Engine is one engine slot ("main", "builder", "btw", ...). It implements ext.Engine
// and survives restarts: each process lifetime is a run.
type Engine struct {
	id  string
	mgr *Manager

	life sync.Mutex // serializes start / stop / restart

	mu   sync.Mutex
	opts ext.SpawnOpts
	cur  *run
	last *run // most recent run, kept after exit for Snapshot/StderrTail

	pump *pump
}

var _ ext.Engine = (*Engine)(nil)

// ID returns the engine id.
func (e *Engine) ID() string { return e.id }

func (e *Engine) current() *run {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cur
}

// Running reports whether a process is live.
func (e *Engine) Running() bool { return e.current() != nil }

// Options returns the options of the latest start.
func (e *Engine) Options() ext.SpawnOpts {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.opts
}

// Snapshot returns the tracked session state of the current (or last) run.
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	r := e.last
	e.mu.Unlock()
	if r == nil {
		return Snapshot{Info: ext.SessionInfo{EngineID: e.id}}
	}
	return r.track.Snapshot()
}

// StderrTail returns the last n lines of the engine's stderr.
func (e *Engine) StderrTail(n int) string {
	e.mu.Lock()
	r := e.last
	e.mu.Unlock()
	if r == nil {
		return ""
	}
	return r.stderr.Tail(n)
}

// ---- ext.Engine ----

// Send writes a user message. It stamps a uuid (if missing) and origin human.
// A failure arrives as ControlResultMsg{Subtype: "user", RequestID: <uuid>, Err}.
func (e *Engine) Send(p ext.Prompt) tea.Cmd {
	if p.UUID == "" {
		p.UUID = uuid.NewString()
	}
	return func() tea.Msg {
		if err := e.SendPrompt(p); err != nil {
			return ext.ControlResultMsg{EngineID: e.id, Subtype: proto.TypeUser, RequestID: p.UUID, Err: err}
		}
		return nil
	}
}

// SendPrompt writes a user message now (never blocks on the pipe).
func (e *Engine) SendPrompt(p ext.Prompt) error {
	r := e.current()
	if r == nil {
		return ErrNotRunning
	}
	if p.UUID == "" {
		p.UUID = uuid.NewString()
	}
	in := proto.NewUserInput(p.UUID, p.Blocks...)
	in.Priority = p.Priority
	in.ClientComposed = p.Composed
	in.Origin = &proto.Origin{Kind: proto.OriginHuman}
	line, err := in.MarshalLine()
	if err != nil {
		return err
	}
	return r.tr.Send(line)
}

// Interrupt stops the running turn (interrupt control request).
func (e *Engine) Interrupt(cancelQueued bool) tea.Cmd {
	return e.Control(proto.SubInterrupt, proto.InterruptRequest{CancelQueued: cancelQueued})
}

// Control sends a control request; the reply arrives as ext.ControlResultMsg. req is
// a proto.Request, any JSON-marshalable value (its fields), or nil.
func (e *Engine) Control(subtype string, req any) tea.Cmd {
	return func() tea.Msg {
		id, resp, err := e.request(context.Background(), subtype, req)
		return ext.ControlResultMsg{EngineID: e.id, Subtype: subtype, RequestID: id, Resp: resp, Err: err}
	}
}

// Request sends a control request and waits for the reply (for non-UI callers).
func (e *Engine) Request(ctx context.Context, req proto.Request) (json.RawMessage, error) {
	_, resp, err := e.request(ctx, req.ControlSubtype(), req)
	return resp, err
}

func (e *Engine) request(ctx context.Context, subtype string, req any) (string, json.RawMessage, error) {
	r := e.current()
	if r == nil {
		return "", nil, ErrNotRunning
	}
	pr, err := asRequest(subtype, req)
	if err != nil {
		return "", nil, err
	}
	id, resp, err := r.corr.Request(ctx, pr, TimeoutFor(subtype))
	if err != nil {
		if proto.IsUnsupported(err) {
			r.caps.Disable(subtype)
		}
		return id, resp, err
	}
	r.afterControl(subtype, req)
	return id, resp, nil
}

func asRequest(subtype string, req any) (proto.Request, error) {
	if pr, ok := req.(proto.Request); ok && pr.ControlSubtype() == subtype {
		return pr, nil
	}
	var fields json.RawMessage
	if req != nil {
		b, err := json.Marshal(req)
		if err != nil {
			return nil, err
		}
		fields = b
	}
	return proto.RawRequest{Subtype: subtype, Fields: fields}, nil
}

// Supports reports whether a control subtype or engine capability is available.
func (e *Engine) Supports(subtype string) bool {
	e.mu.Lock()
	r := e.cur
	if r == nil {
		r = e.last
	}
	e.mu.Unlock()
	if r == nil {
		return newCapabilities().Supports(subtype)
	}
	return r.caps.Supports(subtype)
}

// Restart stops the current process (gracefully) and, unless o.Stop, starts a new one
// with o (resume, fork, resume-at, ...). A spawn failure arrives as EngineExitedMsg.
func (e *Engine) Restart(o ext.SpawnOpts) tea.Cmd {
	return func() tea.Msg {
		if err := e.RestartNow(context.Background(), o); err != nil {
			return ext.EngineExitedMsg{EngineID: e.id, Err: err}
		}
		return nil
	}
}

// RestartNow is Restart without the Cmd wrapper.
func (e *Engine) RestartNow(ctx context.Context, o ext.SpawnOpts) error {
	e.life.Lock()
	defer e.life.Unlock()
	e.stopLocked(ctx)
	if o.Stop {
		return nil
	}
	return e.startLocked(o)
}

// Stop ends the process gracefully: end_session, stdin EOF, SIGTERM, SIGKILL.
func (e *Engine) Stop(ctx context.Context) {
	e.life.Lock()
	defer e.life.Unlock()
	e.stopLocked(ctx)
}

func (e *Engine) start(o ext.SpawnOpts) error {
	e.life.Lock()
	defer e.life.Unlock()
	return e.startLocked(o)
}

func (e *Engine) startLocked(o ext.SpawnOpts) error {
	if e.current() != nil {
		return fmt.Errorf("engine %s: already running", e.id)
	}
	m := e.mgr
	bin := m.Binary
	if bin == "" {
		var err error
		if bin, err = FindBinary(); err != nil {
			return err
		}
	}
	spec := SpawnSpec{Path: bin, Args: BuildArgs(o), Env: BuildEnv(os.Environ(), o), Dir: o.Cwd}
	proc, err := m.spawner().Spawn(spec)
	if err != nil {
		return fmt.Errorf("engine %s: start %s: %w", e.id, bin, err)
	}
	r := &run{
		e:       e,
		proc:    proc,
		stderr:  NewRing(StderrRingSize),
		caps:    newCapabilities(),
		track:   newTracker(e.id, o),
		inbound: newInboundSet(),
		exited:  make(chan struct{}),
	}
	r.tr = NewTransport(proc.Stdout(), proc.Stdin())
	r.tr.Tap = m.Tap
	r.corr = newCorrelator(r.tr.Send)
	r.coal = newCoalescer(e.id, m.CoalesceInterval, e.pump.Enqueue)

	e.mu.Lock()
	e.opts = o
	e.cur, e.last = r, r
	e.mu.Unlock()

	e.pump.Enqueue([]tea.Msg{
		ext.EngineAttachMsg{EngineID: e.id, Engine: e},
		ext.SessionChangedMsg{EngineID: e.id, Info: r.track.Info()},
	})
	r.runFile = m.writeRunFile(e.id, bin, proc.Pid())
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(r.stderr, proc.Stderr())
		close(stderrDone)
	}()
	r.tr.Start(r.onEvent, func(err error) { m.logf("engine %s: %v", e.id, err) })
	go r.wait(stderrDone)
	go r.initialize(m.DialogKinds)
	return nil
}

func (e *Engine) stopLocked(ctx context.Context) {
	r := e.current()
	if r == nil {
		return
	}
	r.stop(ctx)
}

// ---- one process lifetime ----

type run struct {
	e       *Engine
	proc    Proc
	tr      *Transport
	corr    *correlator
	caps    *capabilities
	track   *tracker
	coal    *coalescer
	stderr  *Ring
	inbound *inboundSet
	runFile string

	stopMu   sync.Mutex
	stopping bool

	exited  chan struct{}
	exitErr error
}

// VersionTimeout bounds the get_binary_version request sent with initialize.
var VersionTimeout = 5 * time.Second

func (r *run) initialize(dialogKinds []string) {
	// The version gates unstable subtypes; system/init only reports it on the first
	// turn, so ask now (zero tokens) alongside initialize.
	verDone := make(chan struct{})
	go func() {
		defer close(verDone)
		_, resp, err := r.corr.Request(context.Background(), proto.GetBinaryVersionRequest{}, VersionTimeout)
		var v proto.BinaryVersion
		if err == nil && json.Unmarshal(resp, &v) == nil {
			r.caps.SetVersion(v.Version)
		}
	}()
	req := proto.InitializeRequest{PromptSuggestions: true, SupportedDialogKinds: dialogKinds}
	id, resp, err := r.corr.Request(context.Background(), req, InitializeTimeout)
	<-verDone
	if err == nil {
		var ir proto.InitializeResponse
		if derr := json.Unmarshal(resp, &ir); derr == nil {
			r.caps.AddCapabilities(ir.Capabilities)
			if r.track.ObserveInitialize(&ir) {
				r.coal.PushMsg(ext.SessionChangedMsg{EngineID: r.e.id, Info: r.track.Info()})
			}
		}
	}
	r.coal.PushMsg(ext.ControlResultMsg{EngineID: r.e.id, Subtype: proto.SubInitialize, RequestID: id, Resp: resp, Err: err})
}

// onEvent runs on the reader goroutine, in stdout order.
func (r *run) onEvent(ev proto.Event) {
	switch e := ev.(type) {
	case *proto.ControlResponse:
		if err := e.Response.Err(); err != nil && proto.IsUnsupported(err) {
			if sub := r.corr.Subtype(e.Response.RequestID); sub != "" {
				r.caps.Disable(sub)
			}
		}
		if !r.corr.Complete(e.Response) {
			r.e.mgr.logf("engine %s: response for unknown request %s", r.e.id, e.Response.RequestID)
		}
		return
	case *proto.ControlRequest:
		r.handleRequest(e)
		return
	case *proto.ControlCancelRequest:
		r.handleCancel(e.RequestID)
		return
	case *proto.KeepAlive:
		return
	case *proto.SystemInit:
		r.caps.AddCapabilities(e.Capabilities)
		r.caps.SetVersion(e.ClaudeCodeVersion)
	case *proto.Unknown:
		r.e.mgr.logf("engine %s: unknown message %s/%s", r.e.id, e.Type, e.Subtype)
	}
	if env := ev.Env(); env.Mismatch != nil {
		r.e.mgr.logf("engine %s: %s/%s shape drift: %v", r.e.id, env.Type, env.Subtype, env.Mismatch)
	}
	changed := r.track.Observe(ev)
	r.coal.Push(ev)
	if changed {
		r.coal.PushMsg(ext.SessionChangedMsg{EngineID: r.e.id, Info: r.track.Info()})
	}
}

// afterControl records successful state-changing control requests.
func (r *run) afterControl(subtype string, req any) {
	changed := false
	switch v := req.(type) {
	case proto.SetPermissionModeRequest:
		changed = r.track.SetPermissionMode(v.Mode)
	case proto.SetModelRequest:
		changed = r.track.SetModel(v.Model)
	case proto.RenameSessionRequest:
		changed = r.track.SetTitle(v.Title)
	}
	if changed {
		r.coal.PushMsg(ext.SessionChangedMsg{EngineID: r.e.id, Info: r.track.Info()})
	}
}

func (r *run) isStopping() bool {
	r.stopMu.Lock()
	defer r.stopMu.Unlock()
	return r.stopping
}

// wait reaps the process and reports the exit, after all of its output.
func (r *run) wait(stderrDone <-chan struct{}) {
	werr := r.proc.Wait()
	select {
	case <-r.tr.ReadDone():
	case <-time.After(OutputDrainGrace):
		if oc, ok := r.proc.(OutputCloser); ok {
			oc.CloseOutput()
		}
		<-r.tr.ReadDone()
	}
	select {
	case <-stderrDone:
	case <-time.After(OutputDrainGrace):
	}
	if oc, ok := r.proc.(OutputCloser); ok {
		oc.CloseOutput()
	}
	r.tr.CloseInput()
	r.corr.Close(ErrExited)
	r.e.mgr.removeRunFile(r.runFile)

	err := werr
	if r.isStopping() {
		err = nil
	}
	r.exitErr = err
	e := r.e
	e.mu.Lock()
	if e.cur == r {
		e.cur = nil
	}
	e.mu.Unlock()

	for _, id := range r.inbound.drain() {
		r.coal.PushMsg(ext.ControlCancelMsg{EngineID: e.id, RequestID: id})
	}
	r.coal.PushMsg(ext.EngineExitedMsg{EngineID: e.id, Err: err, Stderr: r.stderr.Tail(20)})
	r.coal.Stop()
	close(r.exited)
}

// stop ends the process: end_session, stdin EOF, SIGTERM, SIGKILL.
func (r *run) stop(ctx context.Context) {
	r.stopMu.Lock()
	r.stopping = true
	r.stopMu.Unlock()

	done := func(d time.Duration) bool {
		select {
		case <-r.exited:
			return true
		case <-time.After(d):
			return false
		case <-ctx.Done():
			return false
		}
	}
	endCtx, cancel := context.WithTimeout(ctx, EndSessionGrace)
	go func() {
		defer cancel()
		_, _, _ = r.corr.Request(endCtx, proto.EndSessionRequest{Reason: "mantle"}, EndSessionGrace)
	}()
	if done(EndSessionGrace) {
		return
	}
	r.tr.CloseInput()
	if done(StdinGrace) {
		return
	}
	_ = r.proc.Signal(syscall.SIGTERM)
	if done(TermGrace) {
		return
	}
	_ = r.proc.Signal(syscall.SIGKILL)
	<-r.exited
}
