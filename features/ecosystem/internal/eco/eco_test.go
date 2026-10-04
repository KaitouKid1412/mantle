package eco_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

func TestListNavigationAndFilter(t *testing.T) {
	var l eco.List
	l.SetRows([]eco.Row{
		{Key: "info", Label: "explanation", Info: true},
		{Key: "a", Label: "alpha", Section: "One"},
		{Key: "b", Label: "beta", Detail: "second", Section: "One"},
		{Key: "x", Label: "note", Info: true, Section: "Two"},
		{Key: "c", Label: "gamma", Section: "Two"},
	})
	if r, _ := l.Selected(); r.Key != "a" {
		t.Fatalf("initial selection skips info rows, got %q", r.Key)
	}
	l.Move(1)
	l.Move(1)
	if r, _ := l.Selected(); r.Key != "c" {
		t.Errorf("move skips info rows, got %q", r.Key)
	}
	l.Move(5)
	if r, _ := l.Selected(); r.Key != "c" {
		t.Errorf("move stops at the end, got %q", r.Key)
	}
	l.Home()
	if r, _ := l.Selected(); r.Key != "a" {
		t.Errorf("home = %q", r.Key)
	}
	l.End()
	if r, _ := l.Selected(); r.Key != "c" {
		t.Errorf("end = %q", r.Key)
	}

	// Refresh keeps the selected key.
	l.Select("b")
	l.SetRows([]eco.Row{{Key: "z", Label: "zeta"}, {Key: "b", Label: "beta"}})
	if r, _ := l.Selected(); r.Key != "b" {
		t.Errorf("selection lost on refresh: %q", r.Key)
	}

	l.SetRows([]eco.Row{{Key: "1", Label: "plugin-dev", Detail: "tools for plugins"}, {Key: "2", Label: "linter"}})
	l.SetFilter("plug tools")
	if l.Len() != 1 {
		t.Errorf("filter kept %d rows", l.Len())
	}
	l.SetFilter("nomatch")
	th := theme.Default()
	if out := strings.Join(l.Render(&th, 40, 0), "\n"); !strings.Contains(out, `No matches for "nomatch"`) {
		t.Errorf("empty filter render = %q", out)
	}
}

func TestListWindowing(t *testing.T) {
	var l eco.List
	var rows []eco.Row
	for i := 0; i < 50; i++ {
		rows = append(rows, eco.Row{Key: string(rune('A'+i%26)) + string(rune('a'+i/26)), Label: "row"})
	}
	l.SetRows(rows)
	l.Move(25)
	th := theme.Default()
	out := l.Render(&th, 40, 10)
	if len(out) != 10 {
		t.Fatalf("rendered %d lines, want 10", len(out))
	}
	plain := strings.Join(out, "\n")
	if !strings.Contains(plain, "more") || !strings.Contains(plain, "❯") {
		t.Errorf("window = %q", plain)
	}
}

type recView struct {
	eco.Base
	msgs []tea.Msg
}

func (v *recView) Update(ctx ext.Ctx, d *eco.Dialog, m tea.Msg) tea.Cmd {
	v.msgs = append(v.msgs, m)
	return nil
}
func (v *recView) Render(ext.Ctx, *theme.Theme, int, int) []string { return []string{"root body"} }

func TestDialogStack(t *testing.T) {
	ctx := exttest.NewCtx()
	root := &recView{}
	d := eco.NewDialog("dialog.test", "Test", root)
	top := &eco.TextView{Lines: []string{"detail"}, Sub: "Details"}
	d.Push(ctx, top)
	if d.Depth() != 2 || d.KeyContext() != ext.ContextSelect {
		t.Fatalf("depth=%d ctx=%s", d.Depth(), d.KeyContext())
	}
	if s := ecotest.Screen(ctx, d, 60); !strings.Contains(s, "Test · Details") || !strings.Contains(s, "detail") {
		t.Errorf("screen = %q", s)
	}
	d.Update(ctx, ext.AddressedMsg{To: "dialog.test", Msg: "hello"})
	if len(root.msgs) != 1 || root.msgs[0] != "hello" {
		t.Errorf("root under the stack should still get messages: %v", root.msgs)
	}
	ecotest.Press(t, ctx, d, "esc")
	if d.Depth() != 1 || d.Closed() {
		t.Fatalf("esc should pop: depth=%d closed=%v", d.Depth(), d.Closed())
	}
	ecotest.Press(t, ctx, d, "esc")
	if !d.Closed() || len(ctx.Closed) != 1 {
		t.Errorf("esc on the root closes: closed=%v %v", d.Closed(), ctx.Closed)
	}
	d.SetStatus(ctx, "boom", theme.Error)
	if s := ecotest.Screen(ctx, d, 60); !strings.Contains(s, "boom") {
		t.Errorf("status missing: %q", s)
	}
}

func TestConfirmAndMenu(t *testing.T) {
	ctx := exttest.NewCtx()
	yes := 0
	d := eco.NewDialog("dialog.test", "Test", &recView{})
	confirm := &eco.ConfirmView{Question: []string{"Really?"}, OnYes: func(ext.Ctx, *eco.Dialog) tea.Cmd { yes++; return nil }}
	d.Push(ctx, confirm)
	ecotest.Press(t, ctx, d, "enter") // defaults to No
	if yes != 0 || d.Depth() != 1 {
		t.Fatalf("enter on the default should decline: yes=%d depth=%d", yes, d.Depth())
	}
	d.Push(ctx, confirm)
	ecotest.Press(t, ctx, d, "y")
	if yes != 1 || d.Depth() != 1 {
		t.Fatalf("y should accept: yes=%d depth=%d", yes, d.Depth())
	}
	d.Push(ctx, confirm)
	ecotest.Press(t, ctx, d, "up", "enter")
	if yes != 2 {
		t.Fatalf("moving to Yes and pressing enter accepts: yes=%d", yes)
	}

	picked := ""
	menu := eco.NewMenu("Pick", nil,
		eco.MenuItem{Label: "First", Run: func(ext.Ctx, *eco.Dialog) tea.Cmd { picked = "first"; return nil }},
		eco.MenuItem{Label: "Off", Disabled: "not available"},
		eco.MenuItem{Label: "Third", Run: func(ext.Ctx, *eco.Dialog) tea.Cmd { picked = "third"; return nil }},
	)
	d.Push(ctx, menu)
	ecotest.Press(t, ctx, d, "down", "enter")
	if picked != "third" {
		t.Errorf("down skips the disabled item: picked %q", picked)
	}
	ecotest.Press(t, ctx, d, "1")
	if picked != "first" {
		t.Errorf("digit shortcut: picked %q", picked)
	}
	ecotest.Press(t, ctx, d, "2")
	if picked != "first" {
		t.Errorf("disabled item must not run")
	}
}

func TestForm(t *testing.T) {
	ctx := exttest.NewCtx()
	var got []string
	form := &eco.FormView{
		Fields: []eco.Field{
			{Label: "Name"},
			{Label: "Kind", Value: "stdio", Options: []string{"stdio", "http"}},
			{Label: "Secret", Secret: true, Optional: true},
		},
		OnSubmit: func(ctx ext.Ctx, d *eco.Dialog, v []string) tea.Cmd { got = v; return nil },
	}
	d := eco.NewDialog("dialog.test", "Form", form)
	ecotest.Press(t, ctx, d, "ctrl+s")
	if got != nil || form.Err == "" {
		t.Fatalf("required field not enforced: %v %q", got, form.Err)
	}
	ecotest.Press(t, ctx, d, "j", "x", "backspace", "s", "tab", "space", "tab")
	d.HandlePaste(ctx, tea.PasteMsg{Content: "s3\ncret"})
	if s := ecotest.Screen(ctx, d, 60); strings.Contains(s, "s3") || !strings.Contains(s, "•••••••") {
		t.Errorf("secret shown in clear: %q", s)
	}
	ecotest.Press(t, ctx, d, "enter")
	if len(got) != 3 || got[0] != "js" || got[1] != "http" || got[2] != "s3 cret" {
		t.Errorf("submitted %q", got)
	}
}

func TestTracker(t *testing.T) {
	tr := eco.NewTracker()
	init := &proto.SystemInit{SlashCommands: []string{"compact", "pause-memory"},
		MCPServers: []proto.MCPServerInfo{{Name: "a", Status: "connected"}, {Name: "b", Status: "needs-auth"}, {Name: "c", Status: "failed"}}}
	before, after, ok := tr.Observe("", init)
	if !ok || before.Total != 0 || after.NeedsAuth != 1 || after.Failed != 1 || after.Connected != 1 {
		t.Errorf("counts before=%+v after=%+v ok=%v", before, after, ok)
	}
	s := tr.Engine(ext.MainEngine)
	if !s.HasCommand("/pause-memory") || s.HasCommand("mcp") {
		t.Error("HasCommand from init")
	}
	tr.Observe("main", &proto.CommandsChanged{Commands: []proto.SlashCommand{{Name: "mcp", Aliases: []string{"m"}}}})
	if !s.HasCommand("m") || s.HasCommand("compact") {
		t.Error("commands_changed replaces the list")
	}
	tr.Observe("", &proto.BackgroundTasksChanged{Tasks: []proto.BackgroundTask{{TaskID: "1"}, {TaskID: "2", Ambient: true}}})
	if n := len(s.ForegroundTasks()); n != 1 {
		t.Errorf("foreground tasks = %d", n)
	}
	tr.Forget("")
	if tr.Engine("").Init != nil {
		t.Error("Forget")
	}
}

func TestHandoffAndRestart(t *testing.T) {
	ecotest.ResetState(t)
	eng := ecotest.NewEngine()
	ctx := ecotest.NewCtx(eng, "/work")

	eco.Handoff(ctx, " /teleport ")
	eco.Handoff(ctx, "")
	if len(ctx.Opened) != 2 || ctx.Opened[0] != eco.HandoffDialogID {
		t.Errorf("handoff opens plan 06's dialog: %v", ctx.Opened)
	}
	ctx.Opened = nil

	exttest.Exec(eco.RestartEngine(ctx, "to load the new login"))
	if len(eng.Restarts) != 1 || eng.Restarts[0].Resume != ctx.SessionValue.SessionID || eng.Restarts[0].Cwd != "/work" {
		t.Fatalf("restart = %+v", eng.Restarts)
	}
	ecotest.Observe(&proto.BackgroundTasksChanged{Tasks: []proto.BackgroundTask{{TaskID: "t1", Description: "npm run dev"}}})
	exttest.Exec(eco.RestartEngine(ctx, "to load plugins"))
	if len(eng.Restarts) != 1 || len(ctx.Opened) != 1 || ctx.Opened[0] != eco.RestartDialogID {
		t.Fatalf("restart with tasks should ask first: restarts=%d opened=%v", len(eng.Restarts), ctx.Opened)
	}
	d, _ := eco.NewRestartDialog(ctx, eco.RestartArgs{Reason: "to load plugins"})
	if s := ecotest.Screen(ctx, d, 60); !strings.Contains(s, "npm run dev") || !strings.Contains(s, "to load plugins") {
		t.Errorf("restart dialog = %q", s)
	}
	ecotest.Press(t, ctx, d, "y")
	if len(eng.Restarts) != 2 {
		t.Errorf("confirming restarts: %d", len(eng.Restarts))
	}

	delete(ctx.Engines, ext.MainEngine)
	exttest.Exec(eco.RestartEngine(ctx, ""))
	if last := ctx.Notices[len(ctx.Notices)-1]; !strings.Contains(last.Text, "isn't running") {
		t.Errorf("no engine notice = %+v", last)
	}
}
