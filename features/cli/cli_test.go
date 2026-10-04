package cli

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	icli "github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

func feature() ext.Feature {
	return ext.Feature{ID: FeatureID, Setup: setup}
}

func run(c ext.Ctx, r *exttest.Registrar) {
	for _, s := range r.Starts {
		drain(s.Value.(func(ext.Ctx) tea.Cmd)(c))
	}
}

// drain runs a Cmd tree and returns the messages it produced.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, drain(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// fakeDrift replaces the runtime drift check for a test (no claude, no ~/.mantle).
func fakeDrift(t *testing.T, s icli.DriftState, ran bool) {
	t.Helper()
	t.Setenv("MANTLE_HOME", t.TempDir())
	old := runtimeDrift
	runtimeDrift = func(context.Context, string) (icli.DriftState, bool, error) { return s, ran, nil }
	t.Cleanup(func() { runtimeDrift = old })
}

func withStartup(t *testing.T, argv ...string) {
	t.Helper()
	fakeDrift(t, icli.DriftState{}, false)
	p, err := icli.Parse(argv)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.Startup("/w", icli.ResolverFuncs{
		ContinueFunc: func(string) (string, error) { return "s-1", nil },
		ResolveFunc:  func(_, arg string) (string, string, error) { return "", arg, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	icli.SetCurrent(s)
	t.Cleanup(icli.ClearCurrent)
}

func TestNothingWithoutCommandLine(t *testing.T) {
	icli.ClearCurrent()
	r, err := exttest.Setup(feature())
	if err != nil || len(r.Starts) != 0 || len(r.Subs) != 0 {
		t.Fatalf("registered %d starts, %d subs, err %v", len(r.Starts), len(r.Subs), err)
	}
}

func TestPromptSubmittedOnceAfterAttach(t *testing.T) {
	withStartup(t, "--model", "sonnet", "/init")
	r, err := exttest.Setup(feature())
	if err != nil {
		t.Fatal(err)
	}
	c := exttest.NewCtx()
	run(c, r)
	if len(c.Submitted) != 0 {
		t.Fatal("submitted before the engine attached")
	}
	drain(r.Dispatch(c, ext.EngineAttachMsg{EngineID: "builder"}))
	if len(c.Submitted) != 0 {
		t.Fatal("submitted on another engine's attach")
	}
	drain(r.Dispatch(c, ext.EngineAttachMsg{EngineID: ext.MainEngine}))
	drain(r.Dispatch(c, ext.EngineAttachMsg{EngineID: ext.MainEngine})) // restart
	if len(c.Submitted) != 1 || c.Submitted[0].Text != "/init" || c.Submitted[0].Mode != "prompt" {
		t.Errorf("submitted %+v", c.Submitted)
	}
}

func TestNoPromptNothingSubmitted(t *testing.T) {
	withStartup(t, "--verbose")
	r, _ := exttest.Setup(feature())
	c := exttest.NewCtx()
	run(c, r)
	drain(r.Dispatch(c, ext.EngineAttachMsg{EngineID: ext.MainEngine}))
	if len(c.Submitted) != 0 || len(c.Notices) != 0 {
		t.Errorf("submitted %+v notices %+v", c.Submitted, c.Notices)
	}
}

func TestWarningsShown(t *testing.T) {
	withStartup(t, "--bare", "hello", "world")
	r, _ := exttest.Setup(feature())
	c := exttest.NewCtx()
	run(c, r)
	if len(c.Notices) != 2 || c.Notices[0].Level != ext.NoticeWarning || c.Notices[0].Key == c.Notices[1].Key {
		t.Errorf("notices %+v", c.Notices)
	}
}

func TestResumePickerOpensFirst(t *testing.T) {
	withStartup(t, "-r", "auth bug", "then fix it")
	r, _ := exttest.Setup(feature())
	c := exttest.NewCtx()
	c.CommandList = []ext.Command{{Name: "resume"}}
	run(c, r)
	if len(c.Submitted) != 1 || c.Submitted[0].Text != "/resume auth bug" {
		t.Fatalf("submitted %+v", c.Submitted)
	}
	// The picker starts the engine; then the prompt follows.
	drain(r.Dispatch(c, ext.EngineAttachMsg{EngineID: ext.MainEngine}))
	if len(c.Submitted) != 2 || c.Submitted[1].Text != "then fix it" {
		t.Errorf("submitted %+v", c.Submitted)
	}
}

func TestDriftNotice(t *testing.T) {
	withStartup(t)
	fakeDrift(t, icli.DriftState{EngineVersion: "2.1.290", NewCommands: []string{"a", "b"}, NewFlags: []icli.HelpFlag{{Long: "--zap"}}}, true)
	r, _ := exttest.Setup(feature())
	c := exttest.NewCtx()
	var msgs []tea.Msg
	for _, s := range r.Starts {
		msgs = append(msgs, drain(s.Value.(func(ext.Ctx) tea.Cmd)(c))...)
	}
	for _, m := range msgs {
		drain(r.Dispatch(c, m))
	}
	if len(c.Notices) != 1 || c.Notices[0].Text != "Claude Code 2.1.290 adds 2 commands and 1 flag mantle doesn't know yet; they work as engine passthrough" {
		t.Errorf("notices %+v", c.Notices)
	}

	// Same version as last time (ran == false): no notice.
	fakeDrift(t, icli.DriftState{EngineVersion: "2.1.290", NewCommands: []string{"a"}}, false)
	c = exttest.NewCtx()
	for _, s := range r.Starts {
		for _, m := range drain(s.Value.(func(ext.Ctx) tea.Cmd)(c)) {
			drain(r.Dispatch(c, m))
		}
	}
	if len(c.Notices) != 0 {
		t.Errorf("notices %+v", c.Notices)
	}
}

func TestResumeWithoutPickerStartsNewSession(t *testing.T) {
	withStartup(t, "-r", "--model", "opus")
	r, _ := exttest.Setup(feature())
	c := exttest.NewCtx()
	var started *ext.EngineStartMsg
	for _, s := range r.Starts {
		for _, m := range drain(s.Value.(func(ext.Ctx) tea.Cmd)(c)) {
			if sm, ok := m.(ext.EngineStartMsg); ok {
				started = &sm
			}
		}
	}
	if started == nil || started.EngineID != ext.MainEngine || started.Opts.Model != "opus" || started.Opts.Resume != "" {
		t.Fatalf("start %+v", started)
	}
	if len(c.Notices) != 1 {
		t.Errorf("notices %+v", c.Notices)
	}
}
