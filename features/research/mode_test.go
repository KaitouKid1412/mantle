package research

import (
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/config"
	"github.com/KaitouKid1412/mantle/internal/research"
	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

const testSID = "0b9f2c55-3c8e-4d2e-9b7a-6f1e2d3c4b5a"

// forked is q1 with two answered children, q2 and q3; q3 is the engine's tip.
var forked = []fakeNode{
	{id: "q1", prompt: "What is a goroutine?"},
	{id: "q2", parent: "q1", prompt: "How big is its stack?"},
	{id: "q3", parent: "q1", prompt: "How is it scheduled?"},
}

// line is q1 → q2 → q3; q3 is the tip.
var line = []fakeNode{
	{id: "q1", prompt: "What is a goroutine?"},
	{id: "q2", parent: "q1", prompt: "How big is its stack?"},
	{id: "q3", parent: "q2", prompt: "Can it grow?"},
}

// settle runs cmd like the host: tree loads and scope loads go back to the feature.
func (r *rig) settle(cmd tea.Cmd) []tea.Msg {
	var out []tea.Msg
	for _, m := range r.exec(cmd) {
		out = append(out, m)
		switch m.(type) {
		case treeLoadedMsg, ext.UIModeRequestMsg:
			out = append(out, r.settleAll(r.deliver(m))...)
		}
	}
	return out
}

func (r *rig) settleAll(msgs []tea.Msg) []tea.Msg {
	var out []tea.Msg
	for _, m := range msgs {
		out = append(out, m)
		if _, ok := m.(treeLoadedMsg); ok {
			out = append(out, r.settleAll(r.deliver(m))...)
		}
	}
	return out
}

// submit runs the route stage on a prompt.
func (r *rig) submit(text string) (ext.Verdict, []tea.Msg) {
	d := ext.Draft{Text: text, Mode: "prompt"}
	v, cmd := r.f.route(r.ctx, &d)
	return v, r.exec(cmd)
}

func find[T any](msgs []tea.Msg) (T, bool) {
	for _, m := range msgs {
		if t, ok := m.(T); ok {
			return t, true
		}
	}
	var zero T
	return zero, false
}

// withSession points the rig at a temporary config dir holding nodes as session
// testSID, and a temporary mantle dir for sidecars.
func (r *rig) withSession(nodes []fakeNode) (mantleDir string) {
	r.t.Helper()
	cfg, work := r.t.TempDir(), r.t.TempDir()
	mantleDir = r.t.TempDir()
	l := sessions.Layout{ConfigDir: cfg}
	dir := l.ProjectDir(sessions.CanonicalPath(work))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.t.Fatal(err)
	}
	if nodes != nil {
		if err := os.WriteFile(sessions.SessionFile(dir, testSID), []byte(fakeJSONL(nodes)), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
	r.f.env = sessionEnv{
		layout: l,
		paths:  func() config.Paths { return config.Paths{MantleDir: mantleDir} },
		getenv: func(string) string { return "" },
		getwd:  func() (string, error) { return work, nil },
	}
	r.ctx.SessionValue = ext.SessionInfo{EngineID: ext.MainEngine, SessionID: testSID, Cwd: work, PermissionMode: "default"}
	return mantleDir
}

func (r *rig) sessionChanged() []tea.Msg {
	return r.deliver(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: r.ctx.SessionValue})
}

func TestRouteOffIsTransparent(t *testing.T) {
	r := newRig(t, forked, 100, 30)
	if v, msgs := r.submit("anything"); v != ext.Continue || len(msgs) != 0 {
		t.Fatalf("inactive: %v %v", v, msgs)
	}
	r.enter("q2")
	for _, text := range []string{"/compact", "  ", "<bash-input>ls</bash-input>"} {
		if v, _ := r.submit(text); v != ext.Continue {
			t.Errorf("%q: verdict %v, want Continue", text, v)
		}
	}
	d := ext.Draft{Text: "again", Mode: "prompt", Resubmit: true}
	if v, _ := r.f.route(r.ctx, &d); v != ext.Continue {
		t.Errorf("resubmit: verdict %v", v)
	}
}

func TestRoutePlainAtTip(t *testing.T) {
	r := newRig(t, forked, 100, 30)
	r.enter("q3")
	if v, msgs := r.submit("next question"); v != ext.Continue || len(msgs) != 0 {
		t.Fatalf("tip: %v %v", v, msgs)
	}
}

func TestRouteRewindThenResubmit(t *testing.T) {
	r := newRig(t, line, 100, 30)
	r.f.sid = testSID
	r.enter("q1")
	v, msgs := r.submit("a sibling of q2")
	if v != ext.Consumed {
		t.Fatalf("verdict %v, want Consumed", v)
	}
	req, ok := find[ext.BranchRequestMsg](msgs)
	if !ok {
		t.Fatalf("no BranchRequestMsg in %v", msgs)
	}
	want := ext.BranchRequestMsg{EngineID: ext.MainEngine, SessionID: testSID, At: "q1-a", DropPrompt: "q2", Tag: req.Tag}
	if req != want || req.Tag == "" {
		t.Fatalf("request %+v, want %+v", req, want)
	}
	// A second follow-up waits for the first.
	if v, msgs := r.submit("too soon"); v != ext.Reject {
		t.Errorf("second follow-up: %v", v)
	} else if s, _ := find[ext.EditorSetTextMsg](msgs); s.Text != "too soon" {
		t.Errorf("text not given back: %v", msgs)
	}
	// A reply for someone else's request is ignored.
	r.deliver(ext.BranchedMsg{Tag: "other"})
	if len(r.ctx.Submitted) != 0 {
		t.Fatal("submitted on a foreign reply")
	}
	out := r.deliver(ext.BranchedMsg{EngineID: ext.MainEngine, SessionID: testSID, At: "q1-a", Tag: req.Tag})
	sub, ok := find[exttest.SubmitMsg](out)
	if !ok || !sub.Draft.Resubmit || sub.Draft.Text != "a sibling of q2" {
		t.Fatalf("resubmit: %+v (%v)", sub, out)
	}
	if r.f.tree.EngineLeaf != "q1" {
		t.Errorf("engine leaf %q, want q1", r.f.tree.EngineLeaf)
	}
	// The re-submitted prompt passes, and its echo becomes q1's second child, on screen.
	if v, _ := r.f.route(r.ctx, &sub.Draft); v != ext.Continue {
		t.Errorf("resubmitted draft: %v", v)
	}
	r.deliver(echo("q4", "a sibling of q2"))
	q1 := r.f.tree.Node("q1")
	if len(q1.Children) != 2 || q1.Children[1].ID != "q4" || r.f.viewing != "q4" {
		t.Fatalf("q1 children %v, viewing %q", ids(q1.Children), r.f.viewing)
	}
}

func TestRouteResumeAt(t *testing.T) {
	r := newRig(t, forked, 100, 30)
	r.f.sid = testSID
	r.enter("q2")
	v, msgs := r.submit("> big\n\nhow big exactly?")
	req, _ := find[ext.BranchRequestMsg](msgs)
	if v != ext.Consumed || req.At != "q2-a" || req.DropPrompt != "" {
		t.Fatalf("verdict %v, request %+v", v, req)
	}
	out := r.deliver(ext.BranchedMsg{Tag: req.Tag, Err: errors.New("engine exited")})
	if s, ok := find[ext.EditorSetTextMsg](out); !ok || s.Text != "> big\n\nhow big exactly?" {
		t.Fatalf("text not given back: %v", out)
	}
	if len(r.ctx.Submitted) != 0 || r.f.branching != nil {
		t.Fatal("failed branch still pending or submitted")
	}
	if n := r.ctx.Notices[len(r.ctx.Notices)-1]; n.Level != ext.NoticeError || !strings.Contains(n.Text, "engine exited") {
		t.Errorf("notice %+v", n)
	}
}

func TestRouteBlocked(t *testing.T) {
	r := newRig(t, forked, 100, 30)
	r.enter("q2")
	r.f.busy = true
	v, msgs := r.submit("while busy")
	if v != ext.Reject {
		t.Fatalf("verdict %v", v)
	}
	if s, _ := find[ext.EditorSetTextMsg](msgs); s.Text != "while busy" {
		t.Errorf("text not given back: %v", msgs)
	}
	if n := r.ctx.Notices[len(r.ctx.Notices)-1]; !strings.Contains(n.Text, research.ReasonBusy) {
		t.Errorf("notice %q", n.Text)
	}
}

func TestPromptAndTurnFollowLive(t *testing.T) {
	r := newRig(t, forked, 100, 30)
	r.enter("q3")
	r.deliver(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.SessionStateChanged{State: proto.StateRunning}})
	if !r.f.busy {
		t.Fatal("not busy after session_state running")
	}
	user := &proto.User{Envelope: proto.Envelope{Type: proto.TypeUser, UUID: "q4"}, Message: proto.UserMessage{Role: "user", Content: proto.TextContent("deeper")}}
	r.deliver(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: user})
	n := r.f.tree.Node("q4")
	if n == nil || n.Parent.ID != "q3" || n.State != research.Running || r.f.viewing != "q4" || r.f.tree.EngineLeaf != "q4" {
		t.Fatalf("q4 %+v, viewing %q", n, r.f.viewing)
	}
	// A subagent's or tool's user message is not a node.
	sub := &proto.User{Envelope: proto.Envelope{UUID: "x"}, ParentToolUseID: "tu1", Message: proto.UserMessage{Content: proto.TextContent("inner")}}
	r.deliver(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: sub})
	if r.f.tree.Node("x") != nil {
		t.Fatal("subagent prompt became a node")
	}
	r.deliver(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.Result{IsError: true}})
	if r.f.busy || n.State != research.Failed {
		t.Fatalf("after result: busy %v, state %v", r.f.busy, n.State)
	}
}

func TestEnterAndLeave(t *testing.T) {
	r := newRig(t, forked, 100, 30)
	r.ctx.LayoutMode = ext.Inline
	r.withSession(forked)
	out := r.settle(ext.Msg(ext.UIModeRequestMsg{Mode: ext.UIModeResearch}))
	if l, ok := find[ext.LayoutRequestMsg](out); !ok || l.Mode != ext.Fullscreen {
		t.Fatalf("no fullscreen request: %v", out)
	}
	if c, ok := find[ext.UIModeChangedMsg](out); !ok || c.Mode != ext.UIModeResearch {
		t.Fatalf("no UIModeChangedMsg: %v", out)
	}
	if !r.f.active || !r.ctx.ActiveCtxs[ext.ContextResearch] {
		t.Fatal("not active")
	}
	if r.f.tree.Len() != 3 || r.f.node().ID != "q3" {
		t.Fatalf("tree %d nodes, viewing %v", r.f.tree.Len(), r.f.node())
	}
	r.f.load = nil // q2 is off the branch; its items are not needed here
	r.ctx.LayoutMode = ext.Fullscreen

	out = r.settle(ext.Msg(ext.UIModeRequestMsg{}))
	if l, ok := find[ext.LayoutRequestMsg](out); !ok || l.Mode != ext.Inline {
		t.Fatalf("layout not restored: %v", out)
	}
	if c, ok := find[ext.UIModeChangedMsg](out); !ok || c.Mode != "" || c.Prev != ext.UIModeResearch {
		t.Fatalf("UIModeChangedMsg %+v", c)
	}
	if s, ok := find[ext.TranscriptScopeMsg](out); !ok || s.Source != nil {
		t.Fatalf("scope not cleared: %v", out)
	}
	if r.f.active || r.ctx.ActiveCtxs[ext.ContextResearch] {
		t.Fatal("still active")
	}
}

func TestEnterRefused(t *testing.T) {
	for name, set := range map[string]func(*rig){
		"screen reader": func(r *rig) { r.ctx.A11y.ScreenReader = true },
		"no alt screen": func(r *rig) {
			r.f.env.getenv = func(k string) string {
				if k == "CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN" {
					return "1"
				}
				return ""
			}
		},
		"plan mode": func(r *rig) { r.ctx.SessionValue.PermissionMode = "plan" },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, forked, 100, 30)
			r.withSession(forked)
			set(r)
			out := r.settle(r.f.command(r.ctx, ""))
			if r.f.active || len(r.ctx.Notices) == 0 {
				t.Fatalf("entered anyway (%v)", out)
			}
			if _, ok := find[ext.UIModeChangedMsg](out); ok {
				t.Fatal("announced a mode change")
			}
		})
	}
}

func TestExternalModeChangeLeaves(t *testing.T) {
	r := newRig(t, forked, 100, 30)
	r.withSession(forked)
	r.settle(r.f.command(r.ctx, "on"))
	if !r.f.active {
		t.Fatal("not active")
	}
	r.ctx.SessionValue.PermissionMode = "acceptEdits"
	out := r.sessionChanged()
	if r.f.active {
		t.Fatal("still active after the mode changed")
	}
	if c, _ := find[ext.UIModeChangedMsg](out); c.Prev != ext.UIModeResearch {
		t.Fatalf("no UIModeChangedMsg: %v", out)
	}
	if n := r.ctx.Notices[len(r.ctx.Notices)-1]; !strings.Contains(n.Text, "acceptEdits") {
		t.Errorf("notice %q", n.Text)
	}
}

func TestLayoutChangeLeaves(t *testing.T) {
	r := newRig(t, forked, 100, 30)
	r.withSession(forked)
	r.settle(r.f.command(r.ctx, "on"))
	out := r.deliver(ext.LayoutChangedMsg{Mode: ext.Inline})
	if r.f.active {
		t.Fatal("still active in inline layout")
	}
	if _, ok := find[ext.LayoutRequestMsg](out); ok {
		t.Fatal("fought the user's layout switch")
	}
}

func TestSidecarRoundTrip(t *testing.T) {
	r := newRig(t, forked, 100, 30)
	r.f.tree = nil
	dir := r.withSession(forked)
	p := config.Paths{MantleDir: dir}
	if err := research.SaveSidecar(p, testSID, research.Sidecar{Active: true, Viewing: "q2", LastChild: map[string]string{"q1": "q2"}}); err != nil {
		t.Fatal(err)
	}
	r.settleAll(r.sessionChanged())
	if !r.f.active || r.f.viewing != "q2" {
		t.Fatalf("active %v viewing %q, want research on q2", r.f.active, r.f.viewing)
	}
	// Navigation is saved.
	r.settle(r.f.navigate(r.ctx, "q1"))
	s, ok := research.LoadSidecar(p, testSID)
	if !ok || !s.Active || s.Viewing != "q1" || s.LastChild["q1"] != "q2" {
		t.Fatalf("sidecar %+v %v", s, ok)
	}
	r.settle(r.f.command(r.ctx, "off"))
	if s, _ := research.LoadSidecar(p, testSID); s.Active {
		t.Fatal("sidecar still active after leaving")
	}
}

func TestFreshSessionDoesNotOverwriteSidecar(t *testing.T) {
	r := newRig(t, forked, 100, 30)
	dir := r.withSession(forked)
	p := config.Paths{MantleDir: dir}
	_ = research.SaveSidecar(p, testSID, research.Sidecar{Viewing: "q2"})
	r.f.sid, r.f.sideLoaded = testSID, false
	if cmd := r.f.saveSidecar(); cmd != nil {
		t.Fatal("saves before reading")
	}
}

func TestForkCopiesSidecar(t *testing.T) {
	r := newRig(t, forked, 100, 30)
	dir := r.withSession(forked)
	p := config.Paths{MantleDir: dir}
	const from = "11111111-2222-4333-8444-555555555555"
	_ = research.SaveSidecar(p, from, research.Sidecar{Active: true, Viewing: "q2"})
	r.deliver(ext.EngineStartMsg{EngineID: ext.MainEngine, Opts: ext.SpawnOpts{Resume: from, ForkSession: true}})
	r.settleAll(r.sessionChanged())
	if s, ok := research.LoadSidecar(p, testSID); !ok || s.Viewing != "q2" {
		t.Fatalf("fork sidecar %+v %v", s, ok)
	}
	if !r.f.active || r.f.viewing != "q2" {
		t.Fatalf("active %v viewing %q", r.f.active, r.f.viewing)
	}
}

func TestOffBranchLoader(t *testing.T) {
	r := newRig(t, forked, 100, 30)
	r.withSession(forked)
	r.settle(r.f.command(r.ctx, "on"))
	r.ctx.TranscriptV = nil // nothing live: every node loads from the JSONL
	s := r.scope(r.f.navigate(r.ctx, "q2"))
	if got := scopeText(s.Source); !strings.Contains(got, "user:q2") || strings.Contains(got, "user:q3") || strings.Contains(got, "user:q1") {
		t.Fatalf("q2 scope:\n%s", got)
	}
}

func TestQuoteOnSelect(t *testing.T) {
	r := newRig(t, forked, 100, 30)
	if out := r.deliver(ext.SelectionMsg{Text: "x"}); len(out) != 0 {
		t.Fatalf("quoted while off: %v", out)
	}
	r.enter("q2")
	out := r.deliver(ext.SelectionMsg{Source: "fullscreen.viewport", Text: "a stack\nof 8 KB"})
	if q, ok := find[ext.EditorQuoteMsg](out); !ok || q.Text != "> a stack\n> of 8 KB\n\n" {
		t.Fatalf("quote %v", out)
	}
	r.ctx.SettingsV.MantleM = map[string]any{SettingQuoteOnSelect: false}
	if out := r.deliver(ext.SelectionMsg{Text: "y"}); len(out) != 0 {
		t.Fatalf("quoted with the setting off: %v", out)
	}
	if r.f.selection != "y" {
		t.Fatal("selection not kept for the quote key")
	}
}

func TestEnvEntersOnStart(t *testing.T) {
	f := newFeature()
	f.env.getenv = func(k string) string {
		if k == ext.EnvResearch {
			return "1"
		}
		return ""
	}
	reg := exttest.NewRegistrar()
	f.setup(reg)
	ctx := exttest.NewCtx()
	var cmd tea.Cmd
	for _, h := range reg.Starts {
		if h.ID == FeatureID+".env" {
			cmd = h.Value.(func(ext.Ctx) tea.Cmd)(ctx)
		}
	}
	msgs := exttest.Exec(cmd)
	if !f.active {
		t.Fatal("not active")
	}
	if _, ok := find[ext.UIModeChangedMsg](msgs); !ok {
		t.Fatalf("no UIModeChangedMsg: %v", msgs)
	}
}

func echo(id, text string) ext.TranscriptHistoryMsg {
	in := proto.NewUserInput(id, proto.ContentBlock{Type: "text", Text: text})
	return ext.TranscriptHistoryMsg{EngineID: ext.MainEngine, Items: []*ext.Item{{
		ID: "user:" + id, Key: ext.KeyUserPrompt, State: ext.Running,
		Data: &proto.User{Envelope: proto.Envelope{Type: proto.TypeUser, UUID: id}, Message: in.Message},
	}}}
}

func ids(ns []*research.Node) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.ID)
	}
	return out
}
