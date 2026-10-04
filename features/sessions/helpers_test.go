package sessions

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// fakeEngine records what the feature asks of an engine.
type fakeEngine struct {
	opts     ext.SpawnOpts
	sent     []string
	restarts []ext.SpawnOpts
	controls []controlCall
	supports map[string]bool
	reply    map[string]json.RawMessage
	replyErr error
}

type controlCall struct {
	subtype string
	req     any
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{supports: map[string]bool{}, reply: map[string]json.RawMessage{}}
}

func (e *fakeEngine) Send(p ext.Prompt) tea.Cmd {
	for _, b := range p.Blocks {
		e.sent = append(e.sent, b.Text)
	}
	return nil
}
func (e *fakeEngine) Interrupt(bool) tea.Cmd { return nil }
func (e *fakeEngine) Supports(s string) bool { return e.supports[s] }
func (e *fakeEngine) Options() ext.SpawnOpts { return e.opts }
func (e *fakeEngine) Restart(o ext.SpawnOpts) tea.Cmd {
	e.restarts = append(e.restarts, o)
	return nil
}
func (e *fakeEngine) Control(sub string, req any) tea.Cmd {
	e.controls = append(e.controls, controlCall{sub, req})
	resp, err := e.reply[sub], e.replyErr
	return func() tea.Msg {
		return ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: sub, Resp: resp, Err: err}
	}
}

// copyConfig copies the fixture config dir into a temp dir; sidPlain is the newest.
func copyConfig(t *testing.T) sessions.Layout {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join("..", "..", "testdata", "fixtures", "06", "claude")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{sidPlain, sidTools, sidCompact, sidBranch, sidMessy} {
		p := filepath.Join(dst, "projects", "-work-demo", id+".jsonl")
		mt := base.Add(-time.Duration(i) * time.Hour)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	return sessions.Layout{ConfigDir: dst}
}

// harness wires a feature to exttest doubles and runs Cmds to completion: messages of
// types the feature subscribes to are dispatched back to it; addressed messages reach
// the open dialog; everything is recorded in out.
type harness struct {
	t      *testing.T
	f      *feature
	r      *exttest.Registrar
	ctx    *exttest.Ctx
	eng    *fakeEngine
	dialog ext.Dialog
	out    []tea.Msg
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	l := copyConfig(t)
	f := newFeature(l, "")
	f.worktrees = func(string) []string { return nil }
	f.claudePath = func() (string, error) { return "/usr/local/bin/claude-stub", nil }
	f.getwd = func() (string, error) { return "/work/demo", nil }
	f.now = func() time.Time { return exttest.Epoch }
	r := exttest.NewRegistrar()
	r.Feature = FeatureID
	if err := f.setup(r); err != nil {
		t.Fatal(err)
	}
	ctx := exttest.NewCtx()
	ctx.SessionValue = ext.SessionInfo{EngineID: ext.MainEngine, SessionID: "99999999-9999-4999-8999-000000000001", Cwd: "/work/demo"}
	eng := newFakeEngine()
	eng.opts = ext.SpawnOpts{Cwd: "/work/demo", Model: "opus", ExtraArgs: []string{"--verbose", "--resume", "old"}, Settings: `{"x":1}`}
	ctx.Engines[ext.MainEngine] = eng
	return &harness{t: t, f: f, r: r, ctx: ctx, eng: eng}
}

func (h *harness) run(cmd tea.Cmd) {
	for _, m := range exttest.Exec(cmd) {
		if a, ok := m.(ext.AddressedMsg); ok {
			if h.dialog != nil && a.To == h.dialog.ID() {
				h.run(h.dialog.Update(h.ctx, a.Msg))
			}
			continue
		}
		h.out = append(h.out, m)
		if sc, ok := m.(ext.SessionChangedMsg); ok {
			h.ctx.SessionValue = sc.Info
		}
		h.run(h.r.Dispatch(h.ctx, m))
	}
}

// command runs a registered command.
func (h *harness) command(name, args string) {
	c, ok := h.r.Command(name)
	if !ok {
		h.t.Fatalf("no command %q", name)
	}
	h.run(c.Run(h.ctx, args))
}

// openDialog builds a registered dialog and runs its Init.
func (h *harness) openDialog(id string, args any) ext.Dialog {
	d, err := h.r.Dialogs[id](h.ctx, args)
	if err != nil {
		h.t.Fatal(err)
	}
	h.dialog = d
	h.run(d.Init(h.ctx))
	return d
}

func (h *harness) key(k tea.KeyPressMsg) {
	f := h.dialog.(ext.Focusable)
	if ah, ok := h.dialog.(ext.ActionHandler); ok {
		if a := keyAction(k); a != "" {
			if handled, cmd := ah.HandleAction(h.ctx, a); handled {
				h.run(cmd)
				return
			}
		}
	}
	_, cmd := f.HandleKey(h.ctx, k)
	h.run(cmd)
}

// keyAction maps the few global keys the dialogs claim as actions.
func keyAction(k tea.KeyPressMsg) ext.ActionID {
	switch k.String() {
	case "ctrl+r":
		return ext.ActHistorySearch
	case "ctrl+c":
		return ext.ActAppInterrupt
	}
	return ""
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func keyPress(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

// find returns the recorded messages of type T.
func find[T any](h *harness) []T {
	var out []T
	for _, m := range h.out {
		if v, ok := m.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

func (h *harness) reset() {
	h.out = nil
	h.ctx.Notices = nil
}
