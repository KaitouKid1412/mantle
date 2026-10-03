package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func inputFeature(in *box, submitted *[]string) ext.Feature {
	return ext.Feature{ID: "test.input", Order: 10, Setup: func(r ext.Registrar) error {
		r.AddComponent(ext.SlotInput, focusBox{in}, ext.SlotOpts{})
		r.AddAction(ext.Action{ID: ext.ActChatSubmit, Run: func(c ext.Ctx) (bool, tea.Cmd) {
			*submitted = append(*submitted, "submit")
			return true, nil
		}})
		r.AddAction(ext.Action{ID: ext.ActChatKillAgents, Run: func(c ext.Ctx) (bool, tea.Cmd) {
			*submitted = append(*submitted, "kill")
			return true, nil
		}})
		r.AddAction(ext.Action{ID: ext.ActHistoryPrevious, Run: func(c ext.Ctx) (bool, tea.Cmd) {
			return false, nil // declines: the component gets the raw key
		}})
		r.AddDialog("dialog.test", func(c ext.Ctx, args any) (ext.Dialog, error) {
			return &testDialog{focusBox: focusBox{&box{id: "dialog.test#1", text: "Allow? " + args.(string), ctx: ext.ContextConfirmation}}}, nil
		})
		return nil
	}}
}

func TestKeyRouting(t *testing.T) {
	in := &box{id: "test.input", text: "> ", ctx: ext.ContextChat, handle: map[ext.ActionID]bool{}}
	var got []string
	r, _ := newRoot(t, inputFeature(in, &got))
	if r.Ctx().Focused() != "test.input" {
		t.Fatalf("focus = %q", r.Ctx().Focused())
	}
	drive(t, r, key("a"), key("enter"), key("ctrl+x"), key("ctrl+k"), key("up"))
	if strings.Join(got, ",") != "submit,kill" {
		t.Fatalf("actions = %v", got)
	}
	if strings.Join(in.keys, ",") != "a,up" {
		t.Fatalf("raw keys = %v (ctrl+x must be swallowed as a chord prefix; up must fall through)", in.keys)
	}
	// HandleAction on the focused component wins over the registered action.
	in.handle[ext.ActChatSubmit] = true
	drive(t, r, key("enter"))
	if len(got) != 2 || in.actions[len(in.actions)-1] != ext.ActChatSubmit {
		t.Fatalf("HandleAction not preferred: got=%v actions=%v", got, in.actions)
	}
}

func TestDoubleCtrlCExits(t *testing.T) {
	in := &box{id: "test.input", ctx: ext.ContextChat}
	var got []string
	r, clk := newRoot(t, inputFeature(in, &got))
	out := drive(t, r, key("ctrl+c"))
	if len(r.Notices()) == 0 || !strings.Contains(r.Notices()[0].Text, "again to exit") {
		t.Fatalf("notices = %v", r.Notices())
	}
	_ = out
	clk.t = clk.t.Add(ExitConfirmWindow / 2)
	out = drive(t, r, key("ctrl+c"))
	quit := false
	for _, m := range out {
		_, quit = m.(tea.QuitMsg)
	}
	if !quit || r.ExitCode() != 0 {
		t.Fatalf("second ctrl+c did not quit: %v", out)
	}
}

func TestDialogStack(t *testing.T) {
	in := &box{id: "test.input", text: "> prompt", ctx: ext.ContextChat}
	var got []string
	r, _ := newRoot(t, inputFeature(in, &got))
	drive(t, r, ext.AddressedMsg{To: "nobody"}) // no-op
	exec := func(c tea.Cmd) { drive(t, r, cmdMsgs(c)...) }
	exec(r.Ctx().OpenDialog("dialog.test", "rm -rf build"))
	if v := r.View().Content; !strings.Contains(v, "Allow? rm -rf build") || strings.Contains(v, "> prompt") {
		t.Fatalf("inline dialog must replace the input:\n%s", v)
	}
	drive(t, r, key("x"), key("enter")) // x goes to the dialog; enter = confirm:yes closes it
	if len(r.Dialogs()) != 0 {
		t.Fatalf("dialog still open: %v", r.Dialogs())
	}
	if len(in.keys) != 0 {
		t.Fatalf("input got keys while the dialog was open: %v", in.keys)
	}
	var closed *ext.DialogClosedMsg
	for _, m := range in.got {
		if c, ok := m.(ext.DialogClosedMsg); ok {
			closed = &c
		}
	}
	if closed == nil || closed.Result != "yes" || closed.ID != "dialog.test" {
		t.Fatalf("DialogClosedMsg = %+v", closed)
	}
	if !strings.Contains(r.View().Content, "> prompt") {
		t.Fatal("input not restored")
	}
}

func cmdMsgs(c tea.Cmd) []tea.Msg {
	if c == nil {
		return nil
	}
	return []tea.Msg{c()}
}

func TestPanickingComponentIsDisabled(t *testing.T) {
	bad := &box{id: "bad.comp", text: "bad"}
	good := &box{id: "good.comp", text: "still here"}
	var disabled []string
	h := NewHost([]ext.Feature{
		{ID: "bad.feature", Order: 10, Setup: func(r ext.Registrar) error {
			r.AddComponent(ext.SlotStatus, bad, ext.SlotOpts{})
			r.AddCommand(ext.Command{Name: "bad"})
			return nil
		}},
		{ID: "good.feature", Order: 10, Setup: func(r ext.Registrar) error {
			r.AddComponent(ext.SlotStatus, good, ext.SlotOpts{})
			return nil
		}},
	}, HostOptions{Core: CoreFeatures()})
	r := New(Options{Host: h, NoBackgroundQuery: true, Clock: &manualClock{}, OnDisable: func(f, reason string) { disabled = append(disabled, f) }})
	r.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	r.View()
	bad.panicView = true
	r.invalidate("bad.comp")
	v := r.View().Content // must not panic
	if strings.Contains(v, "bad\n") {
		t.Fatalf("panicking component still rendered:\n%s", v)
	}
	v = r.View().Content
	if !strings.Contains(v, "still here") || !strings.Contains(v, "bad.feature was turned off") {
		t.Fatalf("view after panic:\n%s", v)
	}
	if strings.Join(disabled, ",") != "bad.feature" {
		t.Fatalf("OnDisable = %v", disabled)
	}
	if _, ok := r.Ctx().Command("bad"); ok {
		t.Fatal("commands of a disabled feature must disappear")
	}
	// The app keeps routing messages.
	drive(t, r, tea.WindowSizeMsg{Width: 70, Height: 10})
	if bad.views != 2 { // one good render, one that panicked
		t.Fatalf("disabled component rendered again (%d views)", bad.views)
	}
}

func TestPanickingCmdIsCaught(t *testing.T) {
	f := ext.Feature{ID: "cmdpanic.feature", Order: 10, Setup: func(r ext.Registrar) error {
		ext.Subscribe(r, "cmdpanic.sub", func(c ext.Ctx, m ext.SettingsMsg) tea.Cmd {
			return tea.Batch(func() tea.Msg { panic("in a cmd") })
		})
		return nil
	}}
	r, _ := newRoot(t, f)
	drive(t, r, ext.SettingsMsg{Changed: []string{"x"}})
	if !r.host.Disabled("cmdpanic.feature") {
		t.Fatal("feature should be disabled after its Cmd panicked")
	}
}

func TestThemePreview(t *testing.T) {
	r, _ := newRoot(t)
	if r.Ctx().Theme().Name != "dark" {
		t.Fatalf("default theme %q", r.Ctx().Theme().Name)
	}
	drive(t, r, ext.ThemePreviewMsg{Name: "light"})
	if r.Ctx().Theme().Name != "light" {
		t.Fatalf("preview theme %q", r.Ctx().Theme().Name)
	}
	yes := true
	drive(t, r, ext.ThemePreviewMsg{Name: "light", SyntaxHighlight: &yes})
	if v, _ := r.Ctx().Settings().Claude("syntaxHighlightingDisabled"); v != false {
		t.Fatalf("syntax preview = %v", v)
	}
	drive(t, r, ext.ThemePreviewMsg{})
	if r.Ctx().Theme().Name != "dark" {
		t.Fatal("preview end must restore the settings theme")
	}
	drive(t, r, ext.ThemePreviewMsg{Name: "auto"}, tea.BackgroundColorMsg{})
	if r.Ctx().Theme().Name != "dark" { // zero colour is dark
		t.Fatalf("auto on dark background = %q", r.Ctx().Theme().Name)
	}
}
