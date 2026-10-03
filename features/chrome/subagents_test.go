package chrome

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/term/statusline"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// fakeEngine records control requests.
type fakeEngine struct {
	controls []string
	reqs     []any
	nope     bool // Supports returns false
}

func (e *fakeEngine) Send(ext.Prompt) tea.Cmd       { return nil }
func (e *fakeEngine) Interrupt(bool) tea.Cmd        { return nil }
func (e *fakeEngine) Restart(ext.SpawnOpts) tea.Cmd { return nil }
func (e *fakeEngine) Supports(string) bool          { return !e.nope }
func (e *fakeEngine) Control(sub string, req any) tea.Cmd {
	e.controls = append(e.controls, sub)
	e.reqs = append(e.reqs, req)
	return nil
}

func TestTaskTracker(t *testing.T) {
	now := time.Unix(1000, 0)
	tr := newTaskTracker()
	tr.observe(now, &proto.TaskStarted{TaskID: "a", ToolUseID: "tu", SubagentType: "Explore", Description: "look"})
	tr.observe(now, &proto.TaskStarted{TaskID: "b", TaskType: "local_bash", Description: "sleep", IsBackgrounded: true})
	tr.observe(now, &proto.TaskStarted{TaskID: "c", SubagentType: "x", Ambient: true})
	tr.observe(now.Add(time.Second), &proto.TaskProgress{TaskID: "a", LastToolName: "Read",
		Usage: &proto.TaskUsage{TotalTokens: 1500, DurationMS: 1000}})
	if rows := tr.agents(now, subagentLinger); len(rows) != 1 || rows[0].ID != "a" || rows[0].LastTool != "Read" {
		t.Fatalf("agents = %+v", rows)
	}
	if bg := tr.backgroundTasks(now, subagentLinger); len(bg) != 1 || bg[0].ID != "b" {
		t.Errorf("background = %+v", bg)
	}
	tr.observe(now.Add(2*time.Second), &proto.TaskUpdated{TaskID: "a", Patch: json.RawMessage(`{"status":"failed","error":"boom"}`)})
	a := tr.tasks["a"]
	if a.Running() || a.Status != TaskFailed || a.Error != "boom" || a.End.IsZero() {
		t.Errorf("patched = %+v", a)
	}
	// Finished rows linger, then go.
	if len(tr.agents(now.Add(20*time.Second), subagentLinger)) != 1 {
		t.Error("finished row should linger")
	}
	if len(tr.agents(now.Add(40*time.Second), subagentLinger)) != 0 {
		t.Error("row should go after the linger")
	}
	if !tr.prune(now.Add(40*time.Second), subagentLinger) || tr.tasks["a"] != nil {
		t.Error("prune")
	}
	tr.observe(now, &proto.TaskNotification{TaskID: "b", Status: "killed"})
	if tr.tasks["b"].Status != TaskStopped {
		t.Error("killed maps to stopped")
	}
	tr.observe(now, &proto.ConversationReset{})
	if len(tr.tasks) != 0 {
		t.Error("reset")
	}
	if formatTokens(950) != "950" || formatTokens(12_345) != "12.3k" || formatTokens(2_000_000) != "2M" {
		t.Error("formatTokens")
	}
}

func startAgents(ctx *exttest.Ctx, p *subagentPanel) {
	p.Update(ctx, ev(&proto.TaskStarted{TaskID: "a1", ToolUseID: "tu1", SubagentType: "Explore", Description: "one"}))
	p.Update(ctx, ev(&proto.TaskStarted{TaskID: "a2", ToolUseID: "tu2", SubagentType: "Plan", Description: "two"}))
}

func TestSubagentPanelNavigation(t *testing.T) {
	ctx := exttest.NewCtx()
	eng := &fakeEngine{}
	ctx.Engines[ext.MainEngine] = eng
	p := newSubagentPanel(newTaskTracker())
	p.Init(ctx)
	if ok, _ := p.selectFirst(ctx); ok {
		t.Fatal("no rows: footerSelect declines")
	}
	startAgents(ctx, p)
	if got := ansi.Strip(p.View(ctx, ext.Area{Width: 80}).Text); !strings.Contains(got, "Explore: one") || !strings.Contains(got, "Plan: two") {
		t.Fatalf("rows = %q", got)
	}
	if ok, _ := p.selectFirst(ctx); !ok || ctx.FocusedID != SubagentsID {
		t.Fatal("footerSelect focuses the panel")
	}
	p.OnFocus(ctx)
	p.HandleAction(ctx, ext.ActFooterDown)
	if p.sel != 1 || !strings.Contains(ansi.Strip(p.View(ctx, ext.Area{Width: 80}).Text), "❯ ◐ Plan") {
		t.Errorf("selection = %d", p.sel)
	}
	p.HandleAction(ctx, ext.ActFooterDown) // stays on the last row
	if p.sel != 1 {
		t.Error("down past the end")
	}

	_, cmd := p.HandleAction(ctx, ext.ActFooterOpenSelected)
	if ctx.Opened[len(ctx.Opened)-1] != SubagentViewID || cmd == nil {
		t.Errorf("enter opens the transcript: %v", ctx.Opened)
	}
	p.HandleAction(ctx, ext.ActFooterClose)
	if !slices.Equal(eng.controls, []string{proto.SubStopTask}) || eng.reqs[0].(proto.StopTaskRequest).TaskID != "a2" {
		t.Errorf("x stops: %v %v", eng.controls, eng.reqs)
	}

	p.HandleAction(ctx, ext.ActFooterUp)
	p.HandleAction(ctx, ext.ActFooterUp) // from the first row: back to the prompt
	if ctx.FocusedID != EditorID {
		t.Errorf("focus = %q", ctx.FocusedID)
	}
	p.OnBlur(ctx)
	ctx.FocusedID = SubagentsID
	p.HandleKey(ctx, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if ctx.FocusedID != EditorID {
		t.Error("typing returns to the prompt")
	}
}

func TestSubagentPanelLingerAndDismiss(t *testing.T) {
	ctx := exttest.NewCtx()
	p := newSubagentPanel(newTaskTracker())
	p.Init(ctx)
	startAgents(ctx, p)
	cmd := p.Update(ctx, ev(&proto.TaskNotification{TaskID: "a1", Status: TaskCompleted}))
	var prune []tea.Msg
	for _, m := range exttest.Exec(cmd) {
		if am, ok := m.(ext.AddressedMsg); ok && am.To == SubagentsID {
			prune = append(prune, am.Msg)
		}
	}
	if len(prune) != 1 {
		t.Fatalf("a linger timer should be armed: %v", prune)
	}
	if !strings.Contains(ansi.Strip(p.View(ctx, ext.Area{Width: 80}).Text), "✓ Explore") {
		t.Error("finished row lingers")
	}
	ctx.ClockV.Advance(subagentLinger)
	p.Update(ctx, prune[0])
	if strings.Contains(ansi.Strip(p.View(ctx, ext.Area{Width: 80}).Text), "Explore") {
		t.Error("finished row removed after the linger")
	}

	// x on a finished row dismisses it without a control request.
	eng := &fakeEngine{}
	ctx.Engines[ext.MainEngine] = eng
	p.Update(ctx, ev(&proto.TaskNotification{TaskID: "a2", Status: TaskFailed}))
	p.focused, p.sel = true, 0
	p.HandleAction(ctx, ext.ActFooterClose)
	if len(eng.controls) != 0 || p.tr.tasks["a2"] != nil {
		t.Error("dismiss finished")
	}

	// An engine without stop_task gets a notice.
	p.Update(ctx, ev(&proto.TaskStarted{TaskID: "a3", SubagentType: "x"}))
	eng.nope = true
	p.HandleAction(ctx, ext.ActFooterClose)
	if len(ctx.Notices) == 0 || !strings.Contains(ctx.Notices[len(ctx.Notices)-1].Text, "can't stop") {
		t.Errorf("notices = %+v", ctx.Notices)
	}
}

type fakeSubSL struct {
	mu    sync.Mutex
	stdin []byte
	out   string
}

func (f *fakeSubSL) exec(_ context.Context, req statusline.ExecRequest) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stdin = req.Stdin
	return []byte(f.out), nil
}

func TestSubagentStatusLine(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.SettingsV.ClaudeM["subagentStatusLine"] = map[string]any{"type": "command", "command": "sub"}
	fx := &fakeSubSL{out: `{"id":"a1","content":"custom row"}` + "\n" + `{"id":"a2","content":""}`}
	p := newSubagentPanel(newTaskTracker())
	p.newRunner = func(cfg statusline.Config) *statusline.Runner {
		cfg.Exec, cfg.Debounce = fx.exec, time.Millisecond
		return statusline.NewRunner(cfg)
	}
	p.Init(ctx)
	p.Update(ctx, ext.SessionChangedMsg{Info: ext.SessionInfo{SessionID: "s", Cwd: "/w"}})
	startAgents(ctx, p)
	defer p.stopRunner()
	select {
	case res := <-p.runner.Results():
		p.Update(ctx, subagentSLMsg{gen: p.gen, res: res})
	case <-time.After(5 * time.Second):
		t.Fatal("no subagentStatusLine run")
	}
	got := ansi.Strip(p.View(ctx, ext.Area{Width: 80}).Text)
	if !strings.Contains(got, "custom row") || strings.Contains(got, "Plan") {
		t.Errorf("view = %q", got)
	}
	var payload map[string]any
	fx.mu.Lock()
	_ = json.Unmarshal(fx.stdin, &payload)
	fx.mu.Unlock()
	tasks, _ := payload["tasks"].([]any)
	if payload["session_id"] != "s" || len(tasks) != 2 || payload["columns"] == nil {
		t.Errorf("payload = %v", payload)
	}
}

func TestTasksDialog(t *testing.T) {
	ctx := exttest.NewCtx()
	eng := &fakeEngine{}
	ctx.Engines[ext.MainEngine] = eng
	tr := storyTracker(ctx.ClockV.Now())
	dlg, err := newTasksDialogFactory(tr)(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := dlg.(*tasksDialog)
	if d.Placement() != ext.PlaceInline || d.KeyContext() != ext.ContextSelect {
		t.Error("placement/context")
	}
	d.HandleAction(ctx, ext.ActSelectNext)
	d.HandleAction(ctx, ext.ActSelectAccept)
	if d.viewing != "b1" || !slices.Equal(eng.controls, []string{proto.SubGetTaskOutput}) {
		t.Fatalf("viewing %q, controls %v", d.viewing, eng.controls)
	}
	resp, _ := json.Marshal(proto.TaskOutput{Output: "line1\n\x1b[31mline2\x1b[0m\n", TotalBytes: 12})
	cmd := d.Update(ctx, ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubGetTaskOutput, Resp: resp})
	out := ansi.Strip(d.View(ctx, ext.Area{Width: 80}).Text)
	if !strings.Contains(out, "line1") || !strings.Contains(out, "  line2") {
		t.Errorf("output view = %q", out)
	}
	// The poll timer requests the output again.
	for _, m := range exttest.Exec(cmd) {
		if am, ok := m.(ext.AddressedMsg); ok {
			d.Update(ctx, am.Msg)
		}
	}
	if len(eng.controls) != 2 {
		t.Errorf("poll: %v", eng.controls)
	}
	d.HandleKey(ctx, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if eng.controls[len(eng.controls)-1] != proto.SubStopTask {
		t.Errorf("x stops the viewed task: %v", eng.controls)
	}
	d.HandleAction(ctx, ext.ActSelectCancel) // back to the list
	if d.viewing != "" {
		t.Error("esc returns to the list")
	}
	stale := d.pollGen - 1
	d.Update(ctx, taskPollMsg{gen: stale})
	d.HandleAction(ctx, ext.ActSelectCancel)
	if ctx.Closed[len(ctx.Closed)-1] != TasksDialogID {
		t.Error("esc closes the dialog")
	}
}

func TestTasksCommandRegistered(t *testing.T) {
	for _, f := range ext.Pending() {
		if f.ID != SubagentsID {
			continue
		}
		r, err := exttest.Setup(f)
		if err != nil {
			t.Fatal(err)
		}
		cmd, ok := r.Command("tasks")
		if !ok || !slices.Contains(cmd.Aliases, "bashes") || cmd.Source != ext.SourceBuiltin {
			t.Fatalf("command = %+v", cmd)
		}
		ctx := exttest.NewCtx()
		cmd.Run(ctx, "")
		if !slices.Equal(ctx.Opened, []string{TasksDialogID}) {
			t.Errorf("opened = %v", ctx.Opened)
		}
		if r.Dialogs[SubagentViewID] == nil || r.Dialogs[TasksDialogID] == nil {
			t.Error("dialogs not registered")
		}
		return
	}
	t.Fatal("chrome.subagents not registered")
}
