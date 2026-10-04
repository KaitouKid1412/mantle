package input

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor"
)

// fakeEngine records what the feature sends.
type fakeEngine struct {
	mu       sync.Mutex
	sent     []ext.Prompt
	controls []control
	reply    func(subtype string, req any) (json.RawMessage, error)
}

type control struct {
	subtype string
	req     any
}

func (f *fakeEngine) Send(p ext.Prompt) tea.Cmd {
	f.mu.Lock()
	f.sent = append(f.sent, p)
	f.mu.Unlock()
	return nil
}

func (f *fakeEngine) Interrupt(bool) tea.Cmd { return nil }

func (f *fakeEngine) Control(subtype string, req any) tea.Cmd {
	f.mu.Lock()
	f.controls = append(f.controls, control{subtype, req})
	reply := f.reply
	f.mu.Unlock()
	if reply == nil {
		return nil
	}
	return func() tea.Msg {
		resp, err := reply(subtype, req)
		return ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: subtype, Resp: resp, Err: err}
	}
}

func (f *fakeEngine) Supports(string) bool          { return true }
func (f *fakeEngine) Restart(ext.SpawnOpts) tea.Cmd { return nil }

func (f *fakeEngine) prompts() []ext.Prompt {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ext.Prompt(nil), f.sent...)
}

func (f *fakeEngine) controlsOf(sub string) []control {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []control
	for _, c := range f.controls {
		if c.subtype == sub {
			out = append(out, c)
		}
	}
	return out
}

// rigCtx runs Submit synchronously through the stages, like the host.
type rigCtx struct {
	*exttest.Ctx
	r *rig
}

func (c rigCtx) Submit(d ext.Draft) tea.Cmd {
	c.Ctx.Submitted = append(c.Ctx.Submitted, d)
	return c.r.pipeline(d)
}

// rig is a state wired to a fake Ctx and engine, with the feature's stages
// run like the host runs them.
type rig struct {
	t      *testing.T
	s      *state
	c      rigCtx
	eng    *fakeEngine
	reg    *exttest.Registrar
	msgs   []tea.Msg // every message produced by Cmds
	runCmd int

	deliverTimeouts bool
}

func newRig(t *testing.T, claude map[string]any) *rig {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	s := newState()
	s.histPath = filepath.Join(dir, "history.jsonl")
	s.cwd = "/work/demo"
	ec := exttest.NewCtx()
	c := ec
	c.SettingsV = exttest.NewSettings(claude)
	eng := &fakeEngine{}
	c.Engines[ext.MainEngine] = eng
	c.FocusedID = ComponentID
	c.CommandList = []ext.Command{
		{Name: "compact", Description: "Compact the conversation", Source: ext.SourceEngine},
		{Name: "config", Description: "Open settings", Source: ext.SourceBuiltin, Run: func(ext.Ctx, string) tea.Cmd { return nil }},
		{Name: "cost", Description: "Show cost", Source: ext.SourceEngine},
		{Name: "tasks", Description: "List background tasks", Aliases: []string{"bashes"}, Source: ext.SourceBuiltin},
		{Name: "model", Description: "Switch model", ArgHint: "[model]", Source: ext.SourceBuiltin,
			Complete: func(_ ext.Ctx, p string) []ext.Completion {
				var out []ext.Completion
				for _, m := range []string{"opus", "sonnet", "haiku"} {
					if strings.HasPrefix(m, p) {
						out = append(out, ext.Completion{Value: m})
					}
				}
				return out
			}},
	}
	reg := exttest.NewRegistrar()
	register(reg, s)
	r := &rig{t: t, s: s, eng: eng, reg: reg}
	r.c = rigCtx{Ctx: ec, r: r}
	r.run(s.start(r.c))
	return r
}

// run executes a Cmd and feeds the messages back: Submit drafts go through
// the stages, other messages through the component's Update.
func (r *rig) run(cmd tea.Cmd) {
	r.runCmd++
	if r.runCmd > 50 {
		r.t.Fatal("message loop")
	}
	defer func() { r.runCmd-- }()
	for _, m := range exttest.Exec(cmd) {
		r.msgs = append(r.msgs, m)
		if _, ok := m.(editor.VimTimeoutMsg); ok && !r.deliverTimeouts {
			continue // keys arrive faster than the remap timeout
		}
		// The host broadcasts every message to Update.
		r.run(r.s.update(r.c, m))
	}
}

// pipeline runs the registered stages in priority order, like the host.
func (r *rig) pipeline(d ext.Draft) tea.Cmd {
	stages := append([]exttest.Named(nil), r.reg.Stages...)
	sort.SliceStable(stages, func(i, j int) bool { return stages[i].Priority < stages[j].Priority })
	var cmds []tea.Cmd
	for _, st := range stages {
		v, cmd := st.Value.(ext.PromptStage)(r.c, &d)
		cmds = append(cmds, cmd)
		if v != ext.Continue {
			break
		}
	}
	return tea.Batch(cmds...)
}

// keys sends space-separated keystrokes ("ctrl+a", "enter") or, for "'text'",
// types the text.
func (r *rig) keys(seq ...string) {
	for _, k := range seq {
		if strings.HasPrefix(k, "'") && strings.HasSuffix(k, "'") && len(k) >= 2 {
			for _, ch := range k[1 : len(k)-1] {
				r.press(tea.KeyPressMsg{Code: ch, Text: string(ch)})
			}
			continue
		}
		r.press(keyMsg(k))
	}
}

// press routes a key the way the host does: keymap actions first (for the
// prompt's active contexts), then HandleKey.
func (r *rig) press(k tea.KeyPressMsg) {
	p := &promptComp{s: r.s}
	ks := k.Keystroke()
	for _, ctxName := range p.KeyContexts() {
		for _, b := range ext.DefaultBindings {
			if b.Context == ctxName && b.Keys == normalizeKeys(ks) {
				if ok, cmd := p.HandleAction(r.c, b.Action); ok {
					r.run(cmd)
					return
				}
			}
		}
	}
	for _, b := range ext.DefaultBindings {
		if b.Context == ext.ContextGlobal && b.Keys == normalizeKeys(ks) {
			if ok, cmd := p.HandleAction(r.c, b.Action); ok {
				r.run(cmd)
				return
			}
		}
	}
	_, cmd := p.HandleKey(r.c, k)
	r.run(cmd)
}

func normalizeKeys(ks string) string {
	if ks == "esc" {
		return "escape"
	}
	return ks
}

func (r *rig) action(a ext.ActionID) bool {
	ok, cmd := r.s.action(r.c, a)
	r.run(cmd)
	return ok
}

func (r *rig) event(ev any) {
	r.run(r.s.update(r.c, ev))
}

func (r *rig) text() string { return r.s.ed.Display() }

func (r *rig) noticeTexts() []string {
	var out []string
	for _, n := range r.c.Notices {
		out = append(out, n.Text)
	}
	return out
}

// lastMsg returns the newest recorded message of type T.
func lastMsg[T any](r *rig) (T, bool) {
	var zero T
	for i := len(r.msgs) - 1; i >= 0; i-- {
		if m, ok := r.msgs[i].(T); ok {
			return m, true
		}
	}
	return zero, false
}

func keyMsg(s string) tea.KeyPressMsg {
	var k tea.Key
	parts := strings.Split(s, "+")
	name := parts[len(parts)-1]
	for _, m := range parts[:len(parts)-1] {
		switch m {
		case "ctrl":
			k.Mod |= tea.ModCtrl
		case "alt":
			k.Mod |= tea.ModAlt
		case "shift":
			k.Mod |= tea.ModShift
		}
	}
	codes := map[string]rune{
		"enter": tea.KeyEnter, "backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
		"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown,
		"esc": tea.KeyEscape, "tab": tea.KeyTab, "home": tea.KeyHome, "end": tea.KeyEnd,
	}
	if c, ok := codes[name]; ok {
		k.Code = c
		return tea.KeyPressMsg(k)
	}
	k.Code = []rune(name)[0]
	if k.Mod == 0 {
		k.Text = name
	}
	return tea.KeyPressMsg(k)
}
