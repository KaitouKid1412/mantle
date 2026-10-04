package sessions

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestBranch(t *testing.T) {
	h := newHarness(t)
	h.command("branch", "")
	if len(find[ext.EngineStartMsg](h)) != 0 {
		t.Fatal("branched an empty conversation")
	}
	h.ctx.TranscriptV = &fakeTranscript{items: []*ext.Item{promptItem("p", "hi")}}
	old := h.ctx.SessionValue.SessionID
	h.eng.supports[proto.SubRenameSession] = true
	h.command("branch", "experiment")
	start := find[ext.EngineStartMsg](h)
	if len(start) != 1 || start[0].Opts.Resume != old || !start[0].Opts.ForkSession || start[0].Opts.Model != "opus" {
		t.Fatalf("start = %+v", start)
	}
	// The engine reports the fork's id: it gets the name.
	h.run(ext.Msg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{EngineID: ext.MainEngine, SessionID: "fork-id"}}))
	if len(h.eng.controls) != 1 || h.eng.controls[0].req.(proto.RenameSessionRequest).Title != "experiment" {
		t.Fatalf("controls = %+v", h.eng.controls)
	}
	if h.ctx.SessionValue.Title != "experiment" || h.f.branching != nil {
		t.Fatalf("session = %+v", h.ctx.SessionValue)
	}

	// Not during a turn.
	h.run(ext.Msg(event(&proto.SessionStateChanged{State: proto.StateRunning})))
	h.reset()
	h.command("branch", "")
	if len(find[ext.EngineStartMsg](h)) != 0 || !strings.Contains(h.ctx.Notices[0].Text, "Wait") {
		t.Fatalf("during a turn: %v %+v", h.out, h.ctx.Notices)
	}
}

func TestFork(t *testing.T) {
	h := newHarness(t)
	h.ctx.TranscriptV = &fakeTranscript{items: []*ext.Item{promptItem("p", "hi")}}
	var gotArgs []string
	h.f.runBackground = func(bin, cwd string, argv []string) (string, error) {
		gotArgs = argv
		return "Started background session bg-123", nil
	}
	sid := h.ctx.SessionValue.SessionID
	h.command("fork", "try the other approach")
	if want := []string{"--bg", "--resume", sid, "--fork-session", "try the other approach"}; !slices.Equal(gotArgs, want) {
		t.Fatalf("argv = %q", gotArgs)
	}
	last := h.ctx.Notices[len(h.ctx.Notices)-1]
	if !strings.Contains(last.Text, "bg-123") || last.Level != ext.NoticeSuccess {
		t.Fatalf("notice = %+v", last)
	}
	h.f.runBackground = func(string, string, []string) (string, error) { return "error: nope", errors.New("exit 1") }
	h.command("fork", "")
	if last := h.ctx.Notices[len(h.ctx.Notices)-1]; !strings.Contains(last.Text, "Fork failed: error: nope") {
		t.Fatalf("failure notice = %+v", last)
	}
}
