package sessions

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestRegistrations(t *testing.T) {
	h := newHarness(t)
	for _, name := range []string{"resume", "clear", "compact", "handoff"} {
		if _, ok := h.r.Command(name); !ok {
			t.Errorf("command %q not registered", name)
		}
	}
	resume, _ := h.r.Command("resume")
	clear, _ := h.r.Command("clear")
	handoff, _ := h.r.Command("handoff")
	if !slices.Contains(resume.Aliases, "continue") || !slices.Equal(clear.Aliases, []string{"reset", "new"}) || !handoff.Hidden {
		t.Fatalf("aliases/hidden: %+v %+v %+v", resume, clear, handoff)
	}
	if resume.ID != "cmd.resume" || handoff.ID != "cmd.handoff" {
		t.Fatalf("ids: %s %s", resume.ID, handoff.ID)
	}
	for _, id := range []string{DialogResume, DialogHandoff} {
		if h.r.Dialogs[id] == nil {
			t.Errorf("dialog %s missing", id)
		}
	}
	if len(h.r.Starts) != 1 || len(h.r.Stories) == 0 {
		t.Fatalf("starts=%d stories=%d", len(h.r.Starts), len(h.r.Stories))
	}
}

// Startup: the host set the resumed session before OnStart; its history is printed
// (no reset, no engine restart) and its title reaches the session info.
func TestStartupHistory(t *testing.T) {
	h := newHarness(t)
	h.ctx.SessionValue.SessionID = sidPlain
	h.run(h.r.Starts[0].Value.(func(ext.Ctx) tea.Cmd)(h.ctx))

	hist := find[ext.TranscriptHistoryMsg](h)
	if len(hist) != 1 || hist[0].Reset || len(hist[0].Items) != 7 || hist[0].EngineID != ext.MainEngine {
		t.Fatalf("history = %+v", hist)
	}
	if len(find[ext.EngineStartMsg](h)) != 0 || h.ctx.Reprints != 0 {
		t.Fatal("startup must not restart or reprint")
	}
	if h.ctx.SessionValue.Title != "Explain the build system" {
		t.Fatalf("title = %q", h.ctx.SessionValue.Title)
	}

	// A fresh session (no transcript yet) shows nothing and no error.
	h2 := newHarness(t)
	h2.ctx.SessionValue.SessionID = "99999999-9999-4999-8999-999999999999"
	h2.run(h2.r.Starts[0].Value.(func(ext.Ctx) tea.Cmd)(h2.ctx))
	if len(find[ext.TranscriptHistoryMsg](h2)) != 0 || len(h2.ctx.Notices) != 0 {
		t.Fatalf("fresh session: %v %v", h2.out, h2.ctx.Notices)
	}
}

// Fallback: the engine reports a session while the store is empty.
func TestSessionChangedLoadsHistory(t *testing.T) {
	h := newHarness(t)
	h.run(ext.Msg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{EngineID: ext.MainEngine, SessionID: sidCompact, Cwd: "/work/demo"}}))
	hist := find[ext.TranscriptHistoryMsg](h)
	if len(hist) != 1 || len(hist[0].Items) != 3 {
		t.Fatalf("history = %+v", hist)
	}
	// Seen once; a repeat does not load again.
	h.reset()
	h.run(ext.Msg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{SessionID: sidCompact}}))
	if len(find[ext.TranscriptHistoryMsg](h)) != 0 {
		t.Fatal("history loaded twice")
	}
}

func TestResumeByID(t *testing.T) {
	h := newHarness(t)
	h.command("resume", sidTools)

	seq := []string{}
	for _, m := range h.out {
		switch m.(type) {
		case ext.TranscriptHistoryMsg:
			seq = append(seq, "history")
		case ext.ScreenClearedMsg:
			seq = append(seq, "reprint")
		case ext.SessionChangedMsg:
			seq = append(seq, "session")
		case ext.EngineStartMsg:
			seq = append(seq, "start")
		}
	}
	if want := []string{"history", "reprint", "session", "start"}; !slices.Equal(seq, want) {
		t.Fatalf("sequence = %v (out %v)", seq, h.out)
	}
	hist := find[ext.TranscriptHistoryMsg](h)[0]
	if !hist.Reset || len(hist.Items) != 7 {
		t.Fatalf("history = reset %v, %d items", hist.Reset, len(hist.Items))
	}
	start := find[ext.EngineStartMsg](h)[0]
	o := start.Opts
	if start.EngineID != ext.MainEngine || o.Resume != sidTools || o.Model != "opus" || o.Settings != `{"x":1}` ||
		!slices.Equal(o.ExtraArgs, []string{"--verbose"}) || o.ForkSession {
		t.Fatalf("start opts = %+v", o)
	}
	if h.ctx.SessionValue.SessionID != sidTools || h.ctx.SessionValue.Title != "List files and summarize them" {
		t.Fatalf("session = %+v", h.ctx.SessionValue)
	}
}

func TestResumeSearchAndErrors(t *testing.T) {
	h := newHarness(t)
	h.command("resume", "")
	if !slices.Equal(h.ctx.Opened, []string{DialogResume}) {
		t.Fatalf("empty arg opened %v", h.ctx.Opened)
	}
	h.command("resume", "build")
	if len(h.ctx.Opened) != 2 {
		t.Fatalf("search opened %v", h.ctx.Opened)
	}
	h.command("resume", "99999999-9999-4999-8999-999999999999")
	if len(h.ctx.Notices) != 1 || !strings.Contains(h.ctx.Notices[0].Text, "No conversation found") {
		t.Fatalf("notices = %+v", h.ctx.Notices)
	}
	// By unique name.
	h.reset()
	h.command("resume", "my renamed session")
	if s := find[ext.EngineStartMsg](h); len(s) != 1 || s[0].Opts.Resume != sidMessy {
		t.Fatalf("by name: %v", h.out)
	}
}

// A session of another project is not resumed here: the user gets the command to run.
func TestResumeForeignProject(t *testing.T) {
	h := newHarness(t)
	h.command("resume", sidOther)
	if len(find[ext.EngineStartMsg](h)) != 0 {
		t.Fatal("foreign session restarted the engine")
	}
	if len(h.ctx.Notices) != 1 || !strings.Contains(h.ctx.Notices[0].Text, "cd /work/moved && mantle --resume "+sidOther) {
		t.Fatalf("notices = %+v", h.ctx.Notices)
	}
}

func TestRestartGuardBackgroundTasks(t *testing.T) {
	h := newHarness(t)
	h.run(ext.Msg(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.BackgroundTasksChanged{
		Tasks: []proto.BackgroundTask{{TaskID: "a"}, {TaskID: "b", Ambient: true}}}}))
	h.command("resume", sidTools)
	if len(find[ext.EngineStartMsg](h)) != 0 || len(h.ctx.Notices) != 1 ||
		!strings.Contains(h.ctx.Notices[0].Text, "1 background task is running") {
		t.Fatalf("first attempt: %v %+v", h.out, h.ctx.Notices)
	}
	h.command("resume", sidTools)
	if len(find[ext.EngineStartMsg](h)) != 1 {
		t.Fatal("repeat did not confirm")
	}
}

func TestClearAndCompact(t *testing.T) {
	h := newHarness(t)
	h.command("clear", "")
	h.command("compact", "  keep the API notes ")
	if !slices.Equal(h.eng.sent, []string{"/clear", "/compact keep the API notes"}) {
		t.Fatalf("sent = %q", h.eng.sent)
	}
	old := h.ctx.SessionValue.SessionID
	h.run(ext.Msg(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.ConversationReset{NewConversationID: "new-id", Trigger: "clear"}}))
	if h.ctx.Reprints != 1 || h.ctx.SessionValue.SessionID != "new-id" || h.f.lastCleared() != old {
		t.Fatalf("reprints=%d session=%+v cleared=%v", h.ctx.Reprints, h.ctx.SessionValue, h.f.cleared)
	}

	delete(h.ctx.Engines, ext.MainEngine)
	h.command("clear", "")
	if len(h.ctx.Notices) == 0 || !strings.Contains(h.ctx.Notices[len(h.ctx.Notices)-1].Text, "not running") {
		t.Fatalf("no-engine notice = %+v", h.ctx.Notices)
	}
}

func TestHandoff(t *testing.T) {
	h := newHarness(t)
	sid := h.ctx.SessionValue.SessionID
	h.command("handoff", "/feedback it works")
	if !slices.Equal(h.ctx.Opened, []string{DialogHandoff}) {
		t.Fatalf("opened = %v", h.ctx.Opened)
	}
	d := h.openDialog(DialogHandoff, []string{"/feedback it works"})
	if v := d.View(h.ctx, ext.Area{Width: 80}).Text; !strings.Contains(v, "/feedback it works") || !strings.Contains(v, "esc cancel") {
		t.Fatalf("view = %q", v)
	}
	h.key(tea.KeyPressMsg{Code: tea.KeyEnter})

	// The engine is stopped before claude runs.
	if len(h.eng.restarts) != 1 || !h.eng.restarts[0].Stop {
		t.Fatalf("restarts = %+v", h.eng.restarts)
	}
	if h.f.ho == nil || !slices.Equal(h.f.ho.args, []string{"--resume", sid, "/feedback it works"}) {
		t.Fatalf("handoff = %+v", h.f.ho)
	}
	p := handoffProcess(h.f.ho)
	if p.Path != "/usr/local/bin/claude-stub" || p.Dir != "/work/demo" {
		t.Fatalf("process = %s in %s", p.Path, p.Dir)
	}
	for _, kv := range p.Env {
		if strings.HasPrefix(kv, "CLAUDECODE=") {
			t.Fatal("CLAUDECODE leaked into the hand-off")
		}
	}

	// Claude exits: the engine restarts on the session with the user's options.
	h.reset()
	h.run(ext.Msg(handoffDoneMsg{}))
	start := find[ext.EngineStartMsg](h)
	if len(start) != 1 || start[0].Opts.Resume != sid || start[0].Opts.Model != "opus" || h.f.ho != nil {
		t.Fatalf("after: %v", h.out)
	}
}

func TestHandoffWaitsForTurnAndCancel(t *testing.T) {
	h := newHarness(t)
	h.run(ext.Msg(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.SessionStateChanged{State: proto.StateRunning}}))
	h.openDialog(DialogHandoff, nil)
	h.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(h.eng.restarts) != 0 || h.f.ho == nil || !h.f.ho.pending {
		t.Fatalf("started during a turn: %+v", h.eng.restarts)
	}
	h.run(ext.Msg(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.SessionStateChanged{State: proto.StateIdle}}))
	if len(h.eng.restarts) != 1 || !h.eng.restarts[0].Stop {
		t.Fatalf("not started on idle: %+v", h.eng.restarts)
	}

	h2 := newHarness(t)
	h2.openDialog(DialogHandoff, nil)
	h2.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if h2.f.ho != nil || !slices.Equal(h2.ctx.Closed, []string{DialogHandoff}) {
		t.Fatalf("cancel: ho=%v closed=%v", h2.f.ho, h2.ctx.Closed)
	}
}

func TestStripSessionArgs(t *testing.T) {
	got := stripSessionArgs([]string{"--model", "x", "-c", "--resume", "id", "--session-id=abc", "-n", "name", "--fork-session", "-r", "--verbose"})
	if want := []string{"--model", "x", "--verbose"}; !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestStartupSpawnOptions(t *testing.T) {
	h := newHarness(t)
	delete(h.ctx.Engines, ext.MainEngine)
	h.f.startupSpawn = func() (ext.SpawnOpts, bool) {
		return ext.SpawnOpts{Cwd: "/work/demo", ForkSession: true, SessionID: "chosen", Model: "m"}, true
	}
	o := h.f.spawnOpts(h.ctx, ext.MainEngine, "/work/demo", func(o *ext.SpawnOpts) { o.Resume = "picked" })
	if o.Resume != "picked" || !o.ForkSession || o.SessionID != "chosen" || o.Model != "m" {
		t.Fatalf("opts = %+v", o)
	}
	_ = exttest.Epoch
}
