package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// fakeEngine records control requests and prompts and answers controls from canned
// responses.
type fakeEngine struct {
	controls  []controlCall
	sent      []string
	responses map[string]any
	errs      map[string]error
	missing   map[string]bool // subtypes Supports reports false for
}

type controlCall struct {
	Subtype string
	Payload any
}

func (e *fakeEngine) Send(p ext.Prompt) tea.Cmd {
	var parts []string
	for _, b := range p.Blocks {
		parts = append(parts, b.Text)
	}
	e.sent = append(e.sent, strings.Join(parts, ""))
	return nil
}
func (e *fakeEngine) Interrupt(bool) tea.Cmd        { return nil }
func (e *fakeEngine) Restart(ext.SpawnOpts) tea.Cmd { return nil }
func (e *fakeEngine) Supports(s string) bool        { return !e.missing[s] }
func (e *fakeEngine) Control(sub string, req any) tea.Cmd {
	e.controls = append(e.controls, controlCall{sub, req})
	resp, _ := json.Marshal(e.responses[sub])
	if e.responses[sub] == nil {
		resp = []byte("{}")
	}
	return ext.Msg(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: sub, Resp: resp, Err: e.errs[sub]})
}

func (e *fakeEngine) subtypes() []string {
	var out []string
	for _, c := range e.controls {
		out = append(out, c.Subtype)
	}
	return out
}

// rigCtx is exttest's Ctx plus the args of OpenDialog, which the host passes to the
// dialog factory.
type rigCtx struct {
	*exttest.Ctx
	args map[string]any
}

func (c *rigCtx) OpenDialog(id string, args any) tea.Cmd {
	c.args[id] = args
	return c.Ctx.OpenDialog(id, args)
}

// fakeTranscript is a transcript with n finished items.
type fakeTranscript struct{ items []*ext.Item }

func (t fakeTranscript) Items() []*ext.Item { return t.items }
func (t fakeTranscript) Get(string) *ext.Item {
	return nil
}
func (t fakeTranscript) Committed() int { return len(t.items) }

// rig runs the settings features against exttest's Ctx and Registrar, opening dialogs
// the way the host does and feeding every resulting message back in.
type rig struct {
	t      *testing.T
	a      *area
	c      *rigCtx
	r      *exttest.Registrar
	eng    *fakeEngine
	home   string
	writes []patch.Patch
	dialog ext.Dialog
	dlgID  string
	closed []ext.DialogClosedMsg
}

func newRig(t *testing.T) *rig {
	t.Helper()
	home := t.TempDir()
	g := &rig{t: t, home: home, eng: &fakeEngine{responses: map[string]any{}, errs: map[string]error{}, missing: map[string]bool{}}}
	g.a = newArea()
	g.a.env = func(ext.Ctx) patch.Env { return patch.Env{Home: home, ProjectRoot: filepath.Join(home, "repo")} }
	g.a.write = func(env patch.Env, p patch.Patch) (string, error) {
		g.writes = append(g.writes, p)
		return env.Path(p.Scope)
	}
	g.r = exttest.NewRegistrar()
	for _, f := range g.a.features() {
		g.r.Feature = f.ID
		if err := f.Setup(g.r); err != nil {
			t.Fatalf("%s: %v", f.ID, err)
		}
	}
	g.c = &rigCtx{Ctx: exttest.NewCtx(), args: map[string]any{}}
	g.c.W, g.c.H = 100, 40
	g.c.Engines[ext.MainEngine] = g.eng
	g.c.CommandList = g.r.Commands
	return g
}

// loadModels primes the engine cache from the model fixture, as initialize would.
func (g *rig) loadModels() {
	g.t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "08", "model", "init_models.json"))
	if err != nil {
		g.t.Fatal(err)
	}
	var init proto.InitializeResponse
	if err := json.Unmarshal(b, &init); err != nil {
		g.t.Fatal(err)
	}
	g.eng.responses[proto.SubListModels] = proto.ModelsResponse{Models: init.Models}
	g.deliver(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubInitialize, Resp: b})
}

// run executes cmd and delivers every message it produces, repeatedly.
func (g *rig) run(cmd tea.Cmd) {
	g.t.Helper()
	queue := exttest.Exec(cmd)
	for i := 0; len(queue) > 0; i++ {
		if i > 200 {
			g.t.Fatal("message loop did not settle")
		}
		msg := queue[0]
		queue = queue[1:]
		queue = append(queue, g.deliverOne(msg)...)
	}
}

func (g *rig) deliver(msg tea.Msg) { g.run(ext.Msg(msg)) }

func (g *rig) deliverOne(msg tea.Msg) []tea.Msg {
	switch m := msg.(type) {
	case ext.DialogOpenedMsg:
		f, ok := g.r.Dialogs[m.ID]
		if !ok {
			g.t.Fatalf("no dialog %s", m.ID)
		}
		d, err := f(g.c, g.c.args[m.ID])
		delete(g.c.args, m.ID)
		if err != nil {
			g.t.Fatal(err)
		}
		g.dialog, g.dlgID = d, m.ID
		return exttest.Exec(d.Init(g.c))
	case ext.DialogClosedMsg:
		if g.dialog != nil {
			if rs, ok := g.dialog.(ext.Resulter); ok {
				m.Result = rs.Result()
			}
		}
		g.closed = append(g.closed, m)
		g.dialog, g.dlgID = nil, ""
		return nil
	}
	if am, ok := msg.(ext.AddressedMsg); ok {
		// The host hands addressed messages only to their component, unwrapped.
		if g.dialog != nil && g.dialog.ID() == am.To {
			return exttest.Exec(g.dialog.Update(g.c, am.Msg))
		}
		return nil
	}
	out := exttest.Exec(g.r.Dispatch(g.c, msg))
	if g.dialog != nil {
		out = append(out, exttest.Exec(g.dialog.Update(g.c, msg))...)
	}
	return out
}

// command runs a slash command by name.
func (g *rig) command(name, args string) {
	g.t.Helper()
	cmd, ok := g.r.Command(name)
	if !ok {
		g.t.Fatalf("no command /%s", name)
	}
	g.run(cmd.Run(g.c, args))
}

// action runs a registered action (not a dialog action).
func (g *rig) action(id ext.ActionID) bool {
	g.t.Helper()
	for _, a := range g.r.Actions {
		if a.ID == id {
			handled, cmd := a.Run(g.c)
			g.run(cmd)
			return handled
		}
	}
	g.t.Fatalf("no action %s", id)
	return false
}

// press sends an action to the open dialog.
func (g *rig) press(ids ...ext.ActionID) {
	g.t.Helper()
	for _, id := range ids {
		if g.dialog == nil {
			g.t.Fatalf("no dialog open for %s", id)
		}
		h, ok := g.dialog.(ext.ActionHandler)
		if !ok {
			g.t.Fatalf("dialog %s has no actions", g.dlgID)
		}
		handled, cmd := h.HandleAction(g.c, id)
		if !handled {
			g.t.Fatalf("%s not handled by %s", id, g.dlgID)
		}
		g.run(cmd)
	}
}

// key sends a raw key to the open dialog.
func (g *rig) key(k tea.KeyPressMsg) bool {
	g.t.Helper()
	handled, cmd := g.dialog.HandleKey(g.c, k)
	g.run(cmd)
	return handled
}

// screen renders the open dialog without colours.
func (g *rig) screen(width int) string {
	g.t.Helper()
	if g.dialog == nil {
		return ""
	}
	return ansi.Strip(g.dialog.View(g.c, ext.Area{Width: width}).Text)
}

func (g *rig) mustContain(width int, parts ...string) {
	g.t.Helper()
	s := g.screen(width)
	for _, p := range parts {
		if !strings.Contains(s, p) {
			g.t.Errorf("screen lacks %q:\n%s", p, s)
		}
	}
}

// applied applies every recorded patch to empty documents per scope.
func (g *rig) applied() map[patch.Scope]map[string]any {
	g.t.Helper()
	out := map[patch.Scope]map[string]any{}
	for _, p := range g.writes {
		doc, err := p.Apply(out[p.Scope])
		if err != nil {
			g.t.Fatal(err)
		}
		out[p.Scope] = doc
	}
	return out
}

func (g *rig) noticeTexts() []string {
	var out []string
	for _, n := range g.c.Notices {
		out = append(out, n.Text)
	}
	return out
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return fmt.Sprintf("%s", b)
}
