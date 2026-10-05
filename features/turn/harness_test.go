package turn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/features/turn/gates"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// tctx is exttest.Ctx plus a real dialog stack built from the registrar's factories,
// so tests drive dialogs the way the host does.
type tctx struct {
	*exttest.Ctx
	t     *testing.T
	reg   *exttest.Registrar
	stack []ext.Dialog
	ids   []string
}

func (c *tctx) OpenDialog(id string, args any) tea.Cmd {
	c.Ctx.OpenDialog(id, args)
	f, ok := c.reg.Dialogs[id]
	if !ok {
		c.t.Fatalf("no dialog %q registered", id)
	}
	d, err := f(c, args)
	if err != nil {
		c.t.Fatalf("dialog %s: %v", id, err)
	}
	c.stack = append(c.stack, d)
	c.ids = append(c.ids, id)
	return ext.Msg(ext.DialogOpenedMsg{ID: id, Instance: d.ID()})
}

func (c *tctx) CloseDialog(id string) tea.Cmd {
	c.Ctx.CloseDialog(id)
	for i := len(c.stack) - 1; i >= 0; i-- {
		if c.ids[i] != id && c.stack[i].ID() != id {
			continue
		}
		d := c.stack[i]
		c.stack = append(c.stack[:i], c.stack[i+1:]...)
		c.ids = append(c.ids[:i], c.ids[i+1:]...)
		var res any
		if r, ok := d.(ext.Resulter); ok {
			res = r.Result()
		}
		return ext.Msg(ext.DialogClosedMsg{ID: id, Instance: d.ID(), Result: res})
	}
	return nil
}

func (c *tctx) top() ext.Dialog {
	if len(c.stack) == 0 {
		return nil
	}
	return c.stack[len(c.stack)-1]
}

func (c *tctx) topID() string {
	if len(c.ids) == 0 {
		return ""
	}
	return c.ids[len(c.ids)-1]
}

// fakeEngine records what the feature asks of an engine and answers control requests
// with ControlResultMsg.
type fakeEngine struct {
	id          string
	controls    []string
	requests    []any
	interrupts  []bool
	sent        []ext.Prompt
	unsupported map[string]bool
	resp        map[string]string
	errs        map[string]error
}

func newFakeEngine(id string) *fakeEngine {
	return &fakeEngine{id: id, unsupported: map[string]bool{}, resp: map[string]string{}, errs: map[string]error{}}
}

func (e *fakeEngine) Send(p ext.Prompt) tea.Cmd { e.sent = append(e.sent, p); return nil }
func (e *fakeEngine) Interrupt(cq bool) tea.Cmd {
	e.interrupts = append(e.interrupts, cq)
	return nil
}
func (e *fakeEngine) Supports(s string) bool        { return !e.unsupported[s] }
func (e *fakeEngine) Restart(ext.SpawnOpts) tea.Cmd { return nil }
func (e *fakeEngine) Control(sub string, req any) tea.Cmd {
	e.controls = append(e.controls, sub)
	e.requests = append(e.requests, req)
	resp := e.resp[sub]
	if sub == proto.SubSetPermissionMode && resp == "" {
		b, _ := json.Marshal(map[string]string{"mode": req.(proto.SetPermissionModeRequest).Mode})
		resp = string(b)
	}
	return ext.Msg(ext.ControlResultMsg{EngineID: e.id, Subtype: sub, Resp: json.RawMessage(resp), Err: e.errs[sub]})
}

// lastControl returns the most recent control request of a subtype.
func (e *fakeEngine) lastControl(sub string) any {
	for i := len(e.controls) - 1; i >= 0; i-- {
		if e.controls[i] == sub {
			return e.requests[i]
		}
	}
	return nil
}

func (e *fakeEngine) count(sub string) int {
	n := 0
	for _, s := range e.controls {
		if s == sub {
			n++
		}
	}
	return n
}

// h is a test harness around one feature instance.
type h struct {
	t    *testing.T
	st   *state
	reg  *exttest.Registrar
	c    *tctx
	eng  *fakeEngine
	msgs []tea.Msg
	home string
	vars map[string]string
}

func newH(t *testing.T) *h {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	x := &h{t: t, st: newState(), reg: exttest.NewRegistrar(), home: filepath.Join(root, "home"), vars: map[string]string{}}
	for _, d := range []string{x.home, filepath.Join(root, "managed")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := gates.Env{Home: x.home, ManagedDir: filepath.Join(root, "managed"), Getenv: func(k string) string { return x.vars[k] }}
	x.st.env = func() (gates.Env, error) { return env, nil }
	x.reg.Feature = FeatureID
	x.st.setup(x.reg)
	x.c = &tctx{Ctx: exttest.NewCtx(), t: t, reg: x.reg}
	x.eng = newFakeEngine(ext.MainEngine)
	x.c.Engines[ext.MainEngine] = x.eng
	x.c.SessionValue = ext.SessionInfo{EngineID: ext.MainEngine, SessionID: "sess-1", Cwd: "/work", PermissionMode: "default"}
	return x
}

// run executes a Cmd and dispatches every resulting message, recursively.
func (x *h) run(cmd tea.Cmd) {
	x.t.Helper()
	for _, m := range exttest.Exec(cmd) {
		x.deliver(m)
	}
}

func (x *h) deliver(m tea.Msg) {
	x.t.Helper()
	x.msgs = append(x.msgs, m)
	if sc, ok := m.(ext.SessionChangedMsg); ok && engineKey(sc.EngineID) == ext.MainEngine {
		x.c.SessionValue = sc.Info
	}
	if a, ok := m.(ext.AddressedMsg); ok {
		for _, d := range x.c.stack {
			if d.ID() == a.To {
				x.run(d.Update(x.c, a.Msg))
			}
		}
		return
	}
	x.run(x.reg.Dispatch(x.c, m))
}

// send delivers a message as if the host broadcast it.
func (x *h) send(m tea.Msg) { x.t.Helper(); x.deliver(m) }

// event delivers an engine stdout event for the main engine.
func (x *h) event(ev proto.Event) { x.send(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: ev}) }

// press routes a key like the host: keymap actions for the active contexts (offered to
// the top dialog's HandleAction, then the registered action), else HandleKey.
func (x *h) press(keys ...string) {
	x.t.Helper()
	for _, k := range keys {
		x.pressOne(k)
	}
}

func (x *h) contexts() []string {
	var out []string
	if d := x.c.top(); d != nil {
		if cs, ok := d.(ext.ContextStack); ok {
			out = append(out, cs.KeyContexts()...)
		} else {
			out = append(out, d.KeyContext())
		}
	} else {
		out = append(out, ext.ContextChat)
	}
	for k, on := range x.c.ActiveCtxs {
		if on {
			out = append(out, k)
		}
	}
	return append(out, ext.ContextGlobal)
}

func (x *h) pressOne(k string) {
	x.t.Helper()
	msg := keyMsg(k)
	stroke := msg.Keystroke()
	if stroke == "esc" {
		stroke = "escape"
	}
	for _, ctx := range x.contexts() {
		for _, b := range ext.DefaultBindings {
			if b.Context != ctx || b.Keys != stroke {
				continue
			}
			if x.action(b.Action) {
				return
			}
		}
	}
	if d := x.c.top(); d != nil {
		_, cmd := d.HandleKey(x.c, msg)
		x.run(cmd)
	}
}

// action runs an action the way the host does; it reports whether it was handled.
func (x *h) action(a ext.ActionID) bool {
	x.t.Helper()
	if d := x.c.top(); d != nil {
		if ah, ok := d.(ext.ActionHandler); ok {
			if handled, cmd := ah.HandleAction(x.c, a); handled {
				x.run(cmd)
				return true
			}
		}
	}
	var run ext.ActionFunc
	for _, act := range x.reg.Actions {
		if act.ID == a && act.Run != nil {
			run = act.Run
		}
	}
	for _, w := range x.reg.Wrapped[string(a)] {
		next := run
		if next == nil {
			next = func(ext.Ctx) (bool, tea.Cmd) { return false, nil }
		}
		run = w.(func(ext.ActionFunc) ext.ActionFunc)(next)
	}
	if run == nil {
		return false
	}
	handled, cmd := run(x.c)
	x.run(cmd)
	return handled
}

// typeText types runes into the top dialog.
func (x *h) typeText(s string) {
	x.t.Helper()
	for _, r := range s {
		_, cmd := x.c.top().HandleKey(x.c, tea.KeyPressMsg{Code: r, Text: string(r)})
		x.run(cmd)
	}
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	if mod, rest, ok := strings.Cut(s, "+"); ok && mod == "ctrl" && len(rest) == 1 {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

// find returns the delivered messages of type T.
func find[T any](x *h) []T {
	var out []T
	for _, m := range x.msgs {
		if v, ok := m.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

// view renders the top dialog at width.
func (x *h) view(width int) string {
	d := x.c.top()
	if d == nil {
		return ""
	}
	return d.View(x.c, ext.Area{Width: width}).Text
}

// replies records Reply calls of permission requests.
type replies struct{ got []proto.PermissionResult }

func (r *replies) fn() func(proto.PermissionResult) tea.Cmd {
	return func(res proto.PermissionResult) tea.Cmd {
		r.got = append(r.got, res)
		return nil
	}
}

func permMsg(engine, reqID, raw string, r *replies) ext.PermissionMsg {
	var req proto.CanUseTool
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		panic(err)
	}
	return ext.PermissionMsg{EngineID: engine, RequestID: reqID, Req: req, Reply: r.fn()}
}

func mustEqual(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

func testkitStrip(s string) string { return ansi.Strip(s) }
