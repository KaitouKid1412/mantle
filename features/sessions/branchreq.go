package sessions

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Branch requests (ext.BranchRequestMsg, from research mode) move an engine to an
// earlier message of the same session so the next prompt starts a sibling branch:
// in place with rewind_conversation when the request names a prompt to drop and the
// engine supports it, otherwise by restarting on the session at that message
// (--resume-session-at, never --resume-drops-turn). The reply (ext.BranchedMsg) comes
// once the history up to that message is shown and, after a restart, the restarted
// engine has answered initialize on the requested session. The prompt is not
// refilled: the requester re-submits it.
//
// A resume-at the engine cannot honour (At is not on the chain it resumes) prints an
// error result before initialize and exits, and the engine package then falls back to
// a fresh session (claude 2.1.292). Such a restart fails the request and the engine
// goes back to the session's own branch (without --resume-session-at).

// rewinder is implemented by engines with the unstable rewind_conversation request
// (plan 02's Engine.Rewind).
type rewinder interface {
	Rewind(ctx context.Context, target, precedingAssistant string) (engine.RewindResult, error)
}

// rewindTimeout bounds a rewind_conversation round trip.
const rewindTimeout = 30 * time.Second

var (
	errBranchBusy    = errors.New("a turn is running; wait for it to finish")
	errBranchPending = errors.New("another branch request is in progress")
	errBranchNoSess  = errors.New("no conversation to branch from")
	errBranchNoAt    = errors.New("no message to branch from")
	errBranchTasks   = errors.New("background tasks are running and would stop; send again to continue")
)

// pendingBranch is the branch request in flight.
type pendingBranch struct {
	req       ext.BranchRequestMsg
	restarted bool // the restart path (else the in-place rewind)
	started   bool // the engine restart was sent; the reply waits for its initialize
	attached  bool // the restarted engine attached
	session   string
}

func (f *feature) registerBranchRequest(r ext.Registrar) {
	ext.Subscribe(r, "sessions.branch-request", f.onBranchRequest)
	ext.Subscribe(r, "sessions.branch-rewound", f.onBranchRewound)
	ext.Subscribe(r, "sessions.branch-attach", f.onBranchAttach)
	ext.Subscribe(r, "sessions.branch-exited", f.onBranchExited)
	ext.Subscribe(r, "sessions.branch-event", f.onBranchEvent)
	ext.Subscribe(r, "sessions.branch-session", f.onBranchSessionID)
	ext.Subscribe(r, "sessions.branch-init", f.onBranchInit)
}

func (f *feature) onBranchRequest(ctx ext.Ctx, m ext.BranchRequestMsg) tea.Cmd {
	if m.EngineID == "" {
		m.EngineID = ext.MainEngine
	}
	st := f.engine(m.EngineID)
	current := st.session
	if m.EngineID == ext.MainEngine {
		if sid := ctx.Session().SessionID; sid != "" {
			current = sid
		}
	}
	if m.SessionID == "" {
		m.SessionID = current
	}
	switch {
	case st.state == proto.StateRunning || st.state == proto.StateRequiresAction:
		return branched(m, false, errBranchBusy)
	case f.branch != nil || st.loading || f.pendingShow != nil:
		return branched(m, false, errBranchPending)
	case m.SessionID == "":
		return branched(m, false, errBranchNoSess)
	case m.At == "":
		return branched(m, false, errBranchNoAt)
	}

	// Fast path: drop the prompt after At in place, on the session the engine runs.
	eng := ctx.Engine(m.EngineID)
	if rw, ok := eng.(rewinder); ok && m.DropPrompt != "" && m.SessionID == current &&
		eng.Supports(engine.SubRewindConversation) {
		f.branch = &pendingBranch{req: m}
		target := m.DropPrompt
		return func() tea.Msg {
			c, cancel := context.WithTimeout(context.Background(), rewindTimeout)
			defer cancel()
			res, err := rw.Rewind(c, target, "")
			return branchRewoundMsg{res: res, err: err}
		}
	}
	return f.branchRestart(ctx, m)
}

// branchRestart restarts the engine on the session at m.At (modeSwitch).
func (f *feature) branchRestart(ctx ext.Ctx, m ext.BranchRequestMsg) tea.Cmd {
	f.branch = nil
	if warn, ok := f.restartGuard(ctx, m.EngineID, "branch-request", "the follow-up"); !ok {
		return tea.Batch(warn, branched(m, false, errBranchTasks))
	}
	f.branch = &pendingBranch{req: m, restarted: true}
	f.engine(m.EngineID).loading = true
	return f.loadCmd(loadReq{
		engineID: m.EngineID, mode: modeSwitch, id: m.SessionID, cwd: f.cwd(ctx),
		title: f.titles[m.SessionID], leaf: m.At,
		sw: switchOpts{at: m.At, command: "the follow-up", branch: true},
	})
}

type branchRewoundMsg struct {
	res engine.RewindResult
	err error
}

func (f *feature) onBranchRewound(ctx ext.Ctx, m branchRewoundMsg) tea.Cmd {
	p := f.branch
	if p == nil || p.restarted {
		return nil
	}
	req := p.req
	switch {
	case errors.Is(m.err, engine.ErrNeedsTranscript), m.err == nil && !m.res.Rewound:
		// The engine could not rewind in place after all: restart instead.
		return f.branchRestart(ctx, req)
	case m.err != nil:
		f.branch = nil
		return branched(req, false, m.err)
	}
	// Show the history up to At, then reply.
	f.engine(req.EngineID).loading = true
	return f.loadCmd(loadReq{
		engineID: req.EngineID, mode: modeRefresh, id: req.SessionID, cwd: f.cwd(ctx),
		leaf: req.At, sw: switchOpts{branch: true},
	})
}

// branchShown runs once a branch request's history is on screen (onHistory). A
// restart replies once the engine is back (onBranchInit); an in-place rewind replies
// now. A failed load fails a restart (the engine was not touched) but not a rewind
// (it was).
func (f *feature) branchShown(req loadReq, err error) tea.Cmd {
	p := f.branch
	if p == nil || !req.sw.branch {
		return nil
	}
	if p.restarted {
		if err != nil {
			f.branch = nil
			return branched(p.req, false, err)
		}
		p.started = true
		return nil
	}
	f.branch = nil
	return branched(p.req, false, nil)
}

// restartWait returns the restart waiting on engine id's start, if any.
func (f *feature) restartWait(id string) *pendingBranch {
	if p := f.branch; p != nil && p.started && p.req.EngineID == id {
		return p
	}
	return nil
}

func (f *feature) onBranchAttach(ctx ext.Ctx, m ext.EngineAttachMsg) tea.Cmd {
	p := f.restartWait(m.EngineID)
	if p == nil {
		return nil
	}
	if p.attached {
		// A second start: the engine gave up on the resume and fell back.
		return f.branchFailed(ctx, errors.New("the engine could not resume at that message"))
	}
	p.attached = true
	return nil
}

func (f *feature) onBranchEvent(ctx ext.Ctx, m ext.EngineEventMsg) tea.Cmd {
	p := f.restartWait(m.EngineID)
	r, ok := m.Event.(*proto.Result)
	if p == nil || !p.attached || !ok || (!r.IsError && r.Subtype == proto.ResultSuccess) {
		return nil
	}
	// No prompt went out yet, so a result is the engine refusing the resume.
	msg := strings.Join(r.Errors, "; ")
	if msg == "" {
		msg = strings.TrimSpace(r.Result)
	}
	if msg == "" {
		msg = "the engine could not resume at that message"
	}
	return f.branchFailed(ctx, errors.New(msg))
}

func (f *feature) onBranchSessionID(ctx ext.Ctx, m ext.SessionChangedMsg) tea.Cmd {
	p := f.restartWait(m.EngineID)
	if p == nil || !p.attached || m.Info.SessionID == "" {
		return nil
	}
	p.session = m.Info.SessionID
	if p.session != p.req.SessionID {
		return f.branchFailed(ctx, errors.New("the engine started a new session instead of resuming"))
	}
	return nil
}

func (f *feature) onBranchInit(ctx ext.Ctx, m ext.ControlResultMsg) tea.Cmd {
	p := f.restartWait(m.EngineID)
	if p == nil || !p.attached || m.Subtype != proto.SubInitialize {
		return nil
	}
	if m.Err != nil {
		return f.branchFailed(ctx, m.Err)
	}
	if p.session != "" && p.session != p.req.SessionID {
		return f.branchFailed(ctx, errors.New("the engine started a new session instead of resuming"))
	}
	f.branch = nil
	return branched(p.req, true, nil)
}

func (f *feature) onBranchExited(ctx ext.Ctx, m ext.EngineExitedMsg) tea.Cmd {
	if f.restartWait(m.EngineID) == nil || m.Err == nil {
		return nil
	}
	return f.branchFailed(ctx, m.Err)
}

// branchFailed fails the restart in flight and puts the engine back on the session's
// own branch (a plain resume), so the user is where they were.
func (f *feature) branchFailed(ctx ext.Ctx, err error) tea.Cmd {
	p := f.branch
	f.branch = nil
	req := p.req
	f.engine(req.EngineID).loading = true
	back := f.loadCmd(loadReq{
		engineID: req.EngineID, mode: modeSwitch, id: req.SessionID, cwd: f.cwd(ctx),
		title: f.titles[req.SessionID], sw: switchOpts{command: "the follow-up"},
	})
	return tea.Sequence(branched(req, false, err), back)
}

func branched(m ext.BranchRequestMsg, restarted bool, err error) tea.Cmd {
	return ext.Msg(ext.BranchedMsg{
		EngineID: m.EngineID, SessionID: m.SessionID, At: m.At, Tag: m.Tag,
		Restarted: restarted && err == nil, Err: err,
	})
}
