package sessions

import (
	"context"
	"errors"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// fakeRewinder is a fake engine with rewind_conversation.
type fakeRewinder struct {
	*fakeEngine
	calls [][2]string
	res   engine.RewindResult
	err   error
}

func (e *fakeRewinder) Rewind(_ context.Context, target, preceding string) (engine.RewindResult, error) {
	e.calls = append(e.calls, [2]string{target, preceding})
	return e.res, e.err
}

// branchReq asks to continue the plain fixture session from its first answer,
// dropping its second prompt.
func branchReq() ext.BranchRequestMsg {
	return ext.BranchRequestMsg{SessionID: sidPlain, At: u("1s", 1), DropPrompt: u("1u", 2), Tag: "t1"}
}

func branchHarness(t *testing.T) *harness {
	h := newHarness(t)
	h.ctx.SessionValue.SessionID = sidPlain
	return h
}

// replies returns the BranchedMsgs sent so far.
func replies(h *harness) []ext.BranchedMsg { return find[ext.BranchedMsg](h) }

func TestBranchRequestRestart(t *testing.T) {
	h := branchHarness(t)
	h.run(ext.Msg(branchReq()))

	start := find[ext.EngineStartMsg](h)
	if len(start) != 1 {
		t.Fatalf("starts = %+v", start)
	}
	o := start[0].Opts
	if o.Resume != sidPlain || o.ResumeSessionAt != u("1s", 1) || o.ForkSession || o.ResumeDropsTurn ||
		o.SessionID != "" || o.Continue || o.Model != "opus" {
		t.Fatalf("spawn opts = %+v", o)
	}
	hist := find[ext.TranscriptHistoryMsg](h)
	if len(hist) != 2 || !hist[0].Reset || len(hist[1].Items) != 4 || hist[1].Items[3].ID != "result:"+u("1s", 1) {
		t.Fatalf("history = %+v", hist)
	}
	if len(replies(h)) != 0 {
		t.Fatalf("replied before the engine attached: %+v", replies(h))
	}
	if len(find[ext.EditorSetTextMsg](h)) != 0 || len(h.ctx.Notices) != 0 {
		t.Fatalf("prompt refilled or notice shown: %v %+v", h.out, h.ctx.Notices)
	}

	h.run(ext.Msg(ext.EngineAttachMsg{EngineID: ext.MainEngine, Engine: h.eng}))
	h.run(ext.Msg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{EngineID: ext.MainEngine, SessionID: sidPlain}}))
	if len(replies(h)) != 0 {
		t.Fatalf("replied before initialize: %+v", replies(h))
	}
	h.run(ext.Msg(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubInitialize}))
	r := replies(h)
	if len(r) != 1 || r[0].Tag != "t1" || r[0].At != u("1s", 1) || r[0].SessionID != sidPlain ||
		r[0].EngineID != ext.MainEngine || !r[0].Restarted || r[0].Err != nil {
		t.Fatalf("reply = %+v", r)
	}
	// A later start does not reply again.
	h.run(ext.Msg(ext.EngineAttachMsg{EngineID: ext.MainEngine, Engine: h.eng}))
	h.run(ext.Msg(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubInitialize}))
	if len(replies(h)) != 1 || len(find[ext.EngineStartMsg](h)) != 1 {
		t.Fatal("replied or restarted twice")
	}
}

// TestBranchRequestResumeFailure: the engine cannot resume at the message (claude
// prints an error result and exits; the engine package falls back to a fresh
// session). The request fails and the engine goes back to the session's own branch.
func TestBranchRequestResumeFailure(t *testing.T) {
	notFound := "No message found with message.uuid of: " + u("1s", 1)
	cases := map[string]func(h *harness){
		"error result": func(h *harness) {
			h.run(ext.Msg(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.Result{
				Envelope: proto.Envelope{Type: proto.TypeResult, Subtype: "error_during_execution"},
				IsError:  true, Errors: []string{notFound},
			}}))
		},
		"fallback start": func(h *harness) {
			h.run(ext.Msg(ext.EngineAttachMsg{EngineID: ext.MainEngine, Engine: h.eng}))
		},
		"new session": func(h *harness) {
			h.run(ext.Msg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{EngineID: ext.MainEngine, SessionID: sidOther}}))
		},
		"initialize error": func(h *harness) {
			h.run(ext.Msg(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubInitialize, Err: errors.New("exited")}))
		},
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			h := branchHarness(t)
			h.run(ext.Msg(branchReq()))
			h.run(ext.Msg(ext.EngineAttachMsg{EngineID: ext.MainEngine, Engine: h.eng}))
			h.reset()
			fail(h)
			// Whatever follows (the fallback's initialize) changes nothing.
			h.run(ext.Msg(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubInitialize}))
			r := replies(h)
			if len(r) != 1 || r[0].Err == nil || r[0].Restarted || r[0].Tag != "t1" {
				t.Fatalf("reply = %+v", r)
			}
			if name == "error result" && r[0].Err.Error() != notFound {
				t.Fatalf("err = %v", r[0].Err)
			}
			start := find[ext.EngineStartMsg](h)
			if len(start) != 1 || start[0].Opts.Resume != sidPlain || start[0].Opts.ResumeSessionAt != "" ||
				start[0].Opts.ForkSession || start[0].Opts.ResumeDropsTurn {
				t.Fatalf("back to the session: %+v", start)
			}
			if hist := find[ext.TranscriptHistoryMsg](h); len(hist) != 2 || !hist[0].Reset {
				t.Fatalf("history = %+v", hist)
			}
		})
	}
}

func TestBranchRequestRestartClearsStartupSessionFlags(t *testing.T) {
	// A main engine that never started takes the command line's options, whose
	// session flags must not leak into a branch.
	h := branchHarness(t)
	delete(h.ctx.Engines, ext.MainEngine)
	h.f.startupSpawn = func() (ext.SpawnOpts, bool) {
		return ext.SpawnOpts{Cwd: "/work/demo", ForkSession: true, ResumeDropsTurn: true, SessionID: "x"}, true
	}
	h.run(ext.Msg(branchReq()))
	start := find[ext.EngineStartMsg](h)
	if len(start) != 1 || start[0].Opts.ForkSession || start[0].Opts.ResumeDropsTurn || start[0].Opts.SessionID != "" ||
		start[0].Opts.Resume != sidPlain || start[0].Opts.ResumeSessionAt != u("1s", 1) {
		t.Fatalf("starts = %+v", start)
	}
}

func TestBranchRequestFastPath(t *testing.T) {
	h := branchHarness(t)
	rw := &fakeRewinder{fakeEngine: h.eng, res: engine.RewindResult{Rewound: true, PrefillText: "And the tests?"}}
	rw.supports[engine.SubRewindConversation] = true
	h.ctx.Engines[ext.MainEngine] = rw
	h.run(ext.Msg(branchReq()))

	if len(rw.calls) != 1 || rw.calls[0] != [2]string{u("1u", 2), ""} {
		t.Fatalf("rewind calls = %v", rw.calls)
	}
	if len(find[ext.EngineStartMsg](h)) != 0 || len(rw.controls) != 0 {
		t.Fatalf("restarted or restored files: %+v %+v", h.out, rw.controls)
	}
	hist := find[ext.TranscriptHistoryMsg](h)
	if len(hist) != 2 || !hist[0].Reset || len(hist[1].Items) != 4 {
		t.Fatalf("history = %+v", hist)
	}
	r := replies(h)
	if len(r) != 1 || r[0].Restarted || r[0].Err != nil || r[0].Tag != "t1" {
		t.Fatalf("reply = %+v", r)
	}
	if len(find[ext.EditorSetTextMsg](h)) != 0 {
		t.Fatal("prompt refilled")
	}
}

func TestBranchRequestFastPathFallsBack(t *testing.T) {
	h := branchHarness(t)
	rw := &fakeRewinder{fakeEngine: h.eng, err: engine.ErrNeedsTranscript}
	rw.supports[engine.SubRewindConversation] = true
	h.ctx.Engines[ext.MainEngine] = rw
	h.run(ext.Msg(branchReq()))
	if len(rw.calls) != 1 || len(find[ext.EngineStartMsg](h)) != 1 {
		t.Fatalf("calls %v, out %+v", rw.calls, h.out)
	}

	// Without a prompt to drop there is no fast path at all.
	h2 := branchHarness(t)
	rw2 := &fakeRewinder{fakeEngine: h2.eng}
	rw2.supports[engine.SubRewindConversation] = true
	h2.ctx.Engines[ext.MainEngine] = rw2
	req := branchReq()
	req.DropPrompt = ""
	h2.run(ext.Msg(req))
	if len(rw2.calls) != 0 || len(find[ext.EngineStartMsg](h2)) != 1 {
		t.Fatalf("calls %v, out %+v", rw2.calls, h2.out)
	}
}

func TestBranchRequestErrors(t *testing.T) {
	// A running turn refuses.
	h := branchHarness(t)
	h.f.engine(ext.MainEngine).state = proto.StateRunning
	h.run(ext.Msg(branchReq()))
	if r := replies(h); len(r) != 1 || !errors.Is(r[0].Err, errBranchBusy) || r[0].Tag != "t1" || len(find[ext.EngineStartMsg](h)) != 0 {
		t.Fatalf("running: %+v", h.out)
	}

	// The rewind fails.
	h = branchHarness(t)
	boom := errors.New("boom")
	rw := &fakeRewinder{fakeEngine: h.eng, err: boom}
	rw.supports[engine.SubRewindConversation] = true
	h.ctx.Engines[ext.MainEngine] = rw
	h.run(ext.Msg(branchReq()))
	if r := replies(h); len(r) != 1 || !errors.Is(r[0].Err, boom) || len(find[ext.EngineStartMsg](h)) != 0 {
		t.Fatalf("rewind error: %+v", h.out)
	}

	// The restarted engine fails to start.
	h = branchHarness(t)
	h.run(ext.Msg(branchReq()))
	h.run(ext.Msg(ext.EngineExitedMsg{EngineID: ext.MainEngine, Err: boom}))
	if r := replies(h); len(r) != 1 || !errors.Is(r[0].Err, boom) || r[0].Restarted {
		t.Fatalf("exit: %+v", r)
	}

	// The session is not on disk.
	h = branchHarness(t)
	req := branchReq()
	req.SessionID = "88888888-8888-4888-8888-888888888888"
	h.run(ext.Msg(req))
	if r := replies(h); len(r) != 1 || r[0].Err == nil || len(find[ext.EngineStartMsg](h)) != 0 {
		t.Fatalf("missing session: %+v", h.out)
	}

	// Background tasks: the restart guard warns and refuses once, then goes ahead.
	h = branchHarness(t)
	h.f.engine(ext.MainEngine).tasks = 1
	h.run(ext.Msg(branchReq()))
	if r := replies(h); len(r) != 1 || !errors.Is(r[0].Err, errBranchTasks) || len(h.ctx.Notices) != 1 {
		t.Fatalf("guard: %+v %+v", r, h.ctx.Notices)
	}
	h.run(ext.Msg(branchReq()))
	if len(find[ext.EngineStartMsg](h)) != 1 {
		t.Fatalf("guard confirm: %+v", h.out)
	}
}
