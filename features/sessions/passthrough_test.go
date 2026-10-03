package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// fakeTranscript is a fixed transcript store.
type fakeTranscript struct{ items []*ext.Item }

func (t *fakeTranscript) Items() []*ext.Item { return t.items }
func (t *fakeTranscript) Get(id string) *ext.Item {
	for _, it := range t.items {
		if it.ID == id {
			return it
		}
	}
	return nil
}
func (t *fakeTranscript) Committed() int { return 0 }

func promptItem(id, text string) *ext.Item {
	return &ext.Item{ID: id, Key: ext.KeyUserPrompt, State: ext.Done,
		Data: &proto.User{Message: proto.UserMessage{Role: "user", Content: proto.TextContent(text)}}}
}

func TestGoal(t *testing.T) {
	h := newHarness(t)
	h.command("goal", " all tests pass ")
	if !slices.Equal(h.eng.sent, []string{"/goal all tests pass"}) {
		t.Fatalf("sent = %q", h.eng.sent)
	}
	g := &goalLine{f: h.f}
	raw := json.RawMessage(`{"type":"active_goal","value":{"condition":"all tests pass","iterations":2,"set_at":1},"uuid":"u","session_id":"s"}`)
	h.run(ext.Msg(event(&proto.ActiveGoal{Envelope: proto.Envelope{Type: "active_goal", Raw: raw}})))
	if v := ansi.Strip(g.View(h.ctx, ext.Area{Width: 80}).Text); v != "◎ Goal: all tests pass · checked 2 times" {
		t.Fatalf("goal line = %q", v)
	}
	raw = json.RawMessage(`{"type":"active_goal","value":null}`)
	h.run(ext.Msg(event(&proto.ActiveGoal{Envelope: proto.Envelope{Type: "active_goal", Raw: raw}})))
	if g.View(h.ctx, ext.Area{Width: 80}).Text != "" {
		t.Fatal("cleared goal still shown")
	}
}

func TestPlan(t *testing.T) {
	h := newHarness(t)
	h.command("plan", "")
	if len(h.eng.controls) != 1 || h.eng.controls[0].req.(proto.SetPermissionModeRequest).Mode != proto.ModePlan {
		t.Fatalf("controls = %+v", h.eng.controls)
	}
	if h.ctx.SessionValue.PermissionMode != proto.ModePlan {
		t.Fatalf("mode = %q", h.ctx.SessionValue.PermissionMode)
	}
	h.command("plan", "refactor the parser")
	if !slices.Equal(h.eng.sent, []string{"refactor the parser"}) {
		t.Fatalf("sent = %q", h.eng.sent)
	}
	// open: no plan in this session.
	h.command("plan", "open")
	if n := h.ctx.Notices; len(n) == 0 || !strings.Contains(n[len(n)-1].Text, "No plan") {
		t.Fatalf("notices = %+v", n)
	}
}

func TestPlanPath(t *testing.T) {
	l := copyConfig(t)
	dir := l.ProjectDir("/work/demo")
	id := "12121212-1212-4212-8212-121212121212"
	line := `{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":"x"},"uuid":"u","slug":"brave-otter","cwd":"/work/demo","sessionId":"` + id + `"}` + "\n"
	if err := os.WriteFile(sessions.SessionFile(dir, id), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := planPath(l, id, "/work/demo"); err == nil {
		t.Fatal("plan file does not exist yet")
	}
	_ = os.MkdirAll(l.PlansDir(), 0o755)
	want := filepath.Join(l.PlansDir(), "brave-otter.md")
	_ = os.WriteFile(want, []byte("# plan"), 0o644)
	if got, err := planPath(l, id, "/work/demo"); err != nil || got != want {
		t.Fatalf("planPath = %q, %v", got, err)
	}
}

func TestAddDirAndCd(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	h.command("add-dir", "/definitely/not/here")
	if len(h.eng.sent) != 0 || len(h.ctx.Notices) != 1 {
		t.Fatalf("bad dir: sent=%q notices=%+v", h.eng.sent, h.ctx.Notices)
	}
	h.eng.supports[proto.SubRegisterRepoRoot] = true
	h.command("add-dir", dir)
	if !slices.Equal(h.eng.sent, []string{"/add-dir " + dir}) || len(h.eng.controls) != 1 ||
		h.eng.controls[0].req.(proto.RegisterRepoRootRequest).Directory != dir {
		t.Fatalf("add-dir: sent=%q controls=%+v", h.eng.sent, h.eng.controls)
	}

	// /cd without set_cwd hands off to Claude Code.
	h.command("cd", dir)
	if !slices.Equal(h.ctx.Opened, []string{DialogHandoff}) {
		t.Fatalf("cd opened %v", h.ctx.Opened)
	}
	// With set_cwd the engine moves the session.
	h.eng.supports[subSetCwd] = true
	h.command("cd", dir)
	if h.ctx.SessionValue.Cwd != dir {
		t.Fatalf("cwd = %q", h.ctx.SessionValue.Cwd)
	}
	last := h.eng.controls[len(h.eng.controls)-1]
	if last.subtype != subSetCwd || !strings.Contains(string(last.req.(proto.RawRequest).Fields), dir) {
		t.Fatalf("set_cwd = %+v", last)
	}
}

func TestRenameAndRecap(t *testing.T) {
	h := newHarness(t)
	h.command("rename", "Parser work") // unsupported: the engine's own /rename
	h.command("recap", "")
	if !slices.Equal(h.eng.sent, []string{"/rename Parser work", "/recap"}) {
		t.Fatalf("sent = %q", h.eng.sent)
	}

	h.eng.supports[proto.SubRenameSession] = true
	h.command("rename", "Parser work")
	if h.ctx.SessionValue.Title != "Parser work" || h.f.titles[h.ctx.SessionValue.SessionID] != "Parser work" {
		t.Fatalf("title = %q", h.ctx.SessionValue.Title)
	}

	h.eng.supports[proto.SubGenerateSessionTitle] = true
	h.eng.reply[proto.SubGenerateSessionTitle] = json.RawMessage(`{"title":"Fix the tokenizer"}`)
	h.command("rename", "")
	if len(h.ctx.Notices) == 0 || !strings.Contains(h.ctx.Notices[len(h.ctx.Notices)-1].Text, "send a prompt first") {
		t.Fatalf("empty conversation: %+v", h.ctx.Notices)
	}
	h.ctx.TranscriptV = &fakeTranscript{items: []*ext.Item{promptItem("p1", "the tokenizer drops quotes")}}
	h.command("rename", "")
	last := h.eng.controls[len(h.eng.controls)-1].req.(proto.GenerateSessionTitleRequest)
	if !last.Persist || !strings.Contains(last.Description, "tokenizer drops quotes") || h.ctx.SessionValue.Title != "Fix the tokenizer" {
		t.Fatalf("generate: req=%+v title=%q", last, h.ctx.SessionValue.Title)
	}
}

func TestAwayRecap(t *testing.T) {
	h := newHarness(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	h.f.now = func() time.Time { return now }
	h.ctx.TranscriptV = &fakeTranscript{items: []*ext.Item{promptItem("p1", "hi")}}

	h.run(ext.Msg(tea.BlurMsg{}))
	now = now.Add(2 * time.Minute)
	h.run(ext.Msg(tea.FocusMsg{}))
	if len(h.eng.sent) != 0 {
		t.Fatal("recap after a short absence")
	}
	h.run(ext.Msg(tea.BlurMsg{}))
	now = now.Add(6 * time.Minute)
	h.run(ext.Msg(tea.FocusMsg{}))
	if !slices.Equal(h.eng.sent, []string{"/recap"}) {
		t.Fatalf("sent = %q", h.eng.sent)
	}
	if !(&contextWarning{f: h.f}).TerminalState(h.ctx).ReportFocus {
		t.Fatal("focus reports not requested")
	}
	// Off by setting.
	t.Setenv("CLAUDE_CODE_ENABLE_AWAY_SUMMARY", "0")
	h.run(ext.Msg(tea.BlurMsg{}))
	now = now.Add(time.Hour)
	h.run(ext.Msg(tea.FocusMsg{}))
	if len(h.eng.sent) != 1 {
		t.Fatal("recap while disabled")
	}
}
