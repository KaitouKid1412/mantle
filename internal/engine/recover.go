package engine

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Restart safety net. A (re)start that resumes a session can die at once: headless
// claude only writes a transcript after the first turn, so `--resume <id>` of a
// session that never had a turn exits with "No conversation found with session ID".
// When a resumed or restarted engine exits before it is up, the engine falls back,
// one step at a time, and tells the user:
//
//  1. same id, fresh: --session-id=<id> instead of --resume=<id> (only after "No
//     conversation found", and only for a UUID that isn't being forked);
//  2. plain fresh session: every resume option cleared, everything else kept;
//  3. minimal session: also without the forwarded user flags, model and permission
//     mode (the startup-gate settings, cwd and env stay).
//
// Steps whose options equal the failed attempt's are skipped. If the last one fails
// too, the engine reports the failure as a notice and an EngineExitedMsg. Messages
// sent meanwhile are held and go to the session that comes up (see outgoing).
const (
	stepOriginal = iota
	stepSameID
	stepFresh
	stepMinimal
)

// recovery says why the engine is falling back (from the first failed attempt).
type recovery struct {
	noConversation bool   // the resume target had no saved messages
	reason         string // first stderr line or error, for the notice
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// isNoConversation reports the engine's error for a resume target without a transcript
// ("No conversation found with session ID: …", "No conversation found to continue").
func isNoConversation(stderr string) bool { return strings.Contains(stderr, "No conversation found") }

func hasResume(o ext.SpawnOpts) bool {
	return o.Resume != "" || o.Continue || o.ForkSession || o.ResumeSessionAt != ""
}

func freshOpts(o ext.SpawnOpts) ext.SpawnOpts {
	o.Resume, o.Continue, o.ForkSession = "", false, false
	o.ResumeSessionAt, o.ResumeDropsTurn, o.SessionID, o.Stop = "", false, "", false
	return o
}

func minimalOpts(o ext.SpawnOpts) ext.SpawnOpts {
	o = freshOpts(o)
	o.ExtraArgs, o.PermissionMode, o.Model = nil, "", ""
	return o
}

// nextAttempt returns the fallback after attempt (o, step) failed with output.
func nextAttempt(o ext.SpawnOpts, step int, output string) (ext.SpawnOpts, int, bool) {
	type cand struct {
		step int
		opts ext.SpawnOpts
	}
	var cs []cand
	if step < stepSameID && isNoConversation(output) && o.Resume != "" && !o.ForkSession && uuidRe.MatchString(o.Resume) {
		same := freshOpts(o)
		same.SessionID = o.Resume
		cs = append(cs, cand{stepSameID, same})
	}
	if step < stepFresh {
		cs = append(cs, cand{stepFresh, freshOpts(o)})
	}
	if step < stepMinimal {
		cs = append(cs, cand{stepMinimal, minimalOpts(o)})
	}
	for _, c := range cs {
		if !reflect.DeepEqual(c.opts, o) {
			return c.opts, c.step, true
		}
	}
	return o, step, false
}

// firstLine is the first non-empty line of s, shortened.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if len(l) > 160 {
				l = l[:160] + "…"
			}
			return l
		}
	}
	return ""
}

func (why recovery) notice(engineID string, step int) ext.Notice {
	text := "Started a new session: the previous one had no saved messages"
	if !why.noConversation {
		text = "Couldn't restart claude"
		if why.reason != "" {
			text += " (" + why.reason + ")"
		}
		text += "; started a new session"
	}
	if step == stepMinimal {
		text += " without your extra flags, model and permission mode"
	}
	return ext.Notice{Key: "engine:" + engineID + ":recovery", Text: text, Level: ext.NoticeWarning, Source: "engine." + engineID}
}

func (why recovery) giveUpNotice(engineID string, err error) ext.Notice {
	text := "claude failed to start"
	if r := firstLine(why.reason); r != "" {
		text += ": " + r
	} else if err != nil {
		text += ": " + err.Error()
	}
	return ext.Notice{Key: "engine:" + engineID + ":recovery", Text: text, Level: ext.NoticeError, Timeout: -1, Source: "engine." + engineID}
}

// eligible reports whether r's failure should fall back: it died before it was up
// (or because its resume target had no messages), and it was resuming, restarting or
// already falling back.
func (r *run) eligible(err error, output string) bool {
	if err == nil || r.isStopping() {
		return false
	}
	early := !r.initOK.Load() || (isNoConversation(output) && hasResume(r.opts))
	return early && (hasResume(r.opts) || r.restart || r.step > stepOriginal)
}

// recoverFrom starts the fallback attempt after failed run r, on its own goroutine.
// It owns one count of e.recovering.
func (e *Engine) recoverFrom(r *run, o ext.SpawnOpts, step int, why recovery) {
	e.life.Lock()
	defer e.life.Unlock()
	e.mu.Lock()
	stale := e.cur != nil || e.last != r
	e.mu.Unlock()
	if stale {
		// Someone (re)started the engine meanwhile; that start holds the queue now.
		e.endRecovery(nil)
		return
	}
	for {
		err := e.spawnRun(o, step, true, why)
		if err == nil {
			break
		}
		var ok bool
		if o, step, ok = nextAttempt(o, step, err.Error()); !ok {
			e.pump.Enqueue([]tea.Msg{
				ext.NoticeMsg{Notice: why.giveUpNotice(e.id, err)},
				ext.EngineExitedMsg{EngineID: e.id, Err: err},
			})
			e.endRecovery(err)
			return
		}
	}
	// The new run holds the queue until its initialize succeeds, and then raises the
	// notice (the session is really usable then); see run.initialize.
	e.endRecovery(nil)
}

// ---- messages held while the engine (re)starts ----
//
// Every start, restart and fallback holds a count of e.recovering: a new run until
// its initialize reply, Restart while it stops the old process and spawns, a fallback
// while it spawns. While any count is held, prompts, environment updates and control
// requests wait in e.queued, in order, and go to the run whose initialize succeeds (a
// fallback session if the first attempt dies). If every attempt fails, prompts fail
// with a ControlResultMsg (the input gives the text back) and control requests with
// their error.

// outgoing is one held message: a stdin line, or a control request whose caller
// waits to learn where it went.
type outgoing struct {
	line  []byte // a prompt or environment update
	uuid  string // the prompt's uuid ("" for other lines)
	req   proto.Request
	ready chan sentRequest
}

type sentRequest struct {
	r    *run
	id   string
	wait func(context.Context) (proto.ControlResponseBody, error)
	err  error
}

// hold queues o while the engine is (re)starting. Otherwise it returns the run to
// send o to now (nil: not running).
func (e *Engine) hold(o *outgoing) (*run, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.recovering > 0 {
		e.queued = append(e.queued, o)
		return nil, true
	}
	return e.cur, false
}

// unhold removes o from the queue (its caller gave up); false when it was already
// taken for delivery.
func (e *Engine) unhold(o *outgoing) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, q := range e.queued {
		if q == o {
			e.queued = append(e.queued[:i:i], e.queued[i+1:]...)
			return true
		}
	}
	return false
}

// endRecovery releases one count. The last one delivers the queue to the current run
// (up by then: a run that isn't holds a count of its own) or, given err, fails it.
// Messages sent while it delivers queue behind.
func (e *Engine) endRecovery(err error) {
	for {
		e.mu.Lock()
		if e.recovering > 1 || len(e.queued) == 0 {
			e.recovering--
			e.mu.Unlock()
			return
		}
		items, to := e.queued, e.cur
		e.queued = nil
		e.mu.Unlock()
		if err != nil {
			to = nil
		}
		for _, o := range items {
			e.deliver(o, to, err)
		}
	}
}

func (e *Engine) deliver(o *outgoing, to *run, cause error) {
	if to == nil && cause == nil {
		cause = ErrNotRunning
	}
	if o.req != nil {
		s := sentRequest{r: to, err: cause}
		if to != nil {
			s.id, s.wait, s.err = to.corr.begin(o.req, 0, to.corr.send)
		}
		o.ready <- s
		return
	}
	if to != nil {
		if cause = to.tr.Send(o.line); cause == nil {
			return
		}
	}
	if o.uuid == "" {
		e.mgr.logf("engine %s: held line not sent: %v", e.id, cause)
		return
	}
	e.pump.Enqueue([]tea.Msg{ext.ControlResultMsg{EngineID: e.id, Subtype: proto.TypeUser, RequestID: o.uuid,
		Err: fmt.Errorf("engine: prompt not sent: %w", cause)}})
}

// awaitTurn waits until a held control request was sent (or failed).
func (e *Engine) awaitTurn(ctx context.Context, o *outgoing) sentRequest {
	select {
	case s := <-o.ready:
		return s
	case <-ctx.Done():
		if e.unhold(o) {
			return sentRequest{err: ctx.Err()}
		}
		s := <-o.ready // being delivered right now
		if s.r != nil && s.err == nil {
			s.r.corr.cancel(s.id)
		}
		return sentRequest{id: s.id, err: ctx.Err()}
	}
}

// releaseQueue gives up r's count, once: after a successful initialize (err nil), or
// failing the queue with err when it was the last count.
func (r *run) releaseQueue(err error) {
	e := r.e
	e.mu.Lock()
	owns := r.holdsQueue
	r.holdsQueue = false
	e.mu.Unlock()
	if owns {
		e.endRecovery(err)
	}
}
