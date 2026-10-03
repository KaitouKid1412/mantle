// Package ecotest has test helpers for the ecosystem panels: a scripted engine
// that answers control requests from fixtures, key routing through the default
// bindings, and a driver that feeds Cmd results back into a dialog.
package ecotest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Control is one recorded control request.
type Control struct {
	Subtype string
	Req     any
}

// Engine is a scripted ext.Engine. Responses maps a subtype to its JSON reply;
// Errors maps a subtype to an error reply. Unsupported subtypes report
// Supports false.
type Engine struct {
	ID          string
	Responses   map[string]json.RawMessage
	Errors      map[string]string
	Unsupported map[string]bool

	Controls []Control
	Sent     []string // text of prompts sent
	Restarts []ext.SpawnOpts
}

// NewEngine returns an engine with no scripted responses (every control
// succeeds with {}).
func NewEngine() *Engine {
	return &Engine{ID: ext.MainEngine, Responses: map[string]json.RawMessage{}, Errors: map[string]string{},
		Unsupported: map[string]bool{}}
}

// Respond scripts the reply for a subtype from a value or raw JSON.
func (e *Engine) Respond(subtype string, v any) *Engine {
	switch r := v.(type) {
	case json.RawMessage:
		e.Responses[subtype] = r
	case []byte:
		e.Responses[subtype] = r
	case string:
		e.Responses[subtype] = json.RawMessage(r)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		e.Responses[subtype] = b
	}
	return e
}

func (e *Engine) Send(p ext.Prompt) tea.Cmd {
	var parts []string
	for _, b := range p.Blocks {
		parts = append(parts, b.Text)
	}
	e.Sent = append(e.Sent, strings.Join(parts, ""))
	return nil
}

func (e *Engine) Interrupt(bool) tea.Cmd { return nil }

func (e *Engine) Supports(subtype string) bool { return !e.Unsupported[subtype] }

func (e *Engine) Control(subtype string, req any) tea.Cmd {
	e.Controls = append(e.Controls, Control{Subtype: subtype, Req: req})
	resp := e.Responses[subtype]
	if resp == nil {
		resp = json.RawMessage(`{}`)
	}
	var err error
	if msg, ok := e.Errors[subtype]; ok {
		err = fmt.Errorf("%s", msg)
		resp = nil
	}
	return ext.Msg(ext.ControlResultMsg{EngineID: e.ID, Subtype: subtype, Resp: resp, Err: err})
}

func (e *Engine) Restart(o ext.SpawnOpts) tea.Cmd {
	e.Restarts = append(e.Restarts, o)
	return nil
}

// Subtypes lists the recorded control subtypes in order.
func (e *Engine) Subtypes() []string {
	out := make([]string, len(e.Controls))
	for i, c := range e.Controls {
		out[i] = c.Subtype
	}
	return out
}

var _ ext.Engine = (*Engine)(nil)

// NewCtx returns an exttest.Ctx with the engine attached and a session in cwd.
func NewCtx(eng *Engine, cwd string) *exttest.Ctx {
	c := exttest.NewCtx()
	c.SessionValue = ext.SessionInfo{EngineID: ext.MainEngine, SessionID: "11111111-2222-4333-8444-555555555555",
		Cwd: cwd, Model: "claude-test", PermissionMode: "default"}
	if eng != nil {
		c.Engines[ext.MainEngine] = eng
	}
	return c
}

// Execs records commands handed the terminal. Install it with Capture.
type Execs struct {
	Cmds []*exec.Cmd
	Err  error // returned to the callback
}

// Args returns each recorded command's argv after the binary.
func (x *Execs) Args() [][]string {
	out := make([][]string, len(x.Cmds))
	for i, c := range x.Cmds {
		out[i] = c.Args[1:]
	}
	return out
}

// Capture replaces eco.ExecFunc and eco.EditorCmd for the test.
func Capture(t *testing.T) *Execs {
	t.Helper()
	x := &Execs{}
	oldExec, oldEditor := eco.ExecFunc, eco.EditorCmd
	eco.ExecFunc = func(cmd *exec.Cmd, done func(error) tea.Msg) tea.Cmd {
		x.Cmds = append(x.Cmds, cmd)
		err := x.Err
		return func() tea.Msg { return done(err) }
	}
	eco.EditorCmd = func(path string) (*exec.Cmd, error) { return exec.Command("editor", path), nil }
	t.Cleanup(func() { eco.ExecFunc, eco.EditorCmd = oldExec, oldEditor })
	return x
}

// ResetState gives the test a fresh tracker.
func ResetState(t *testing.T) {
	t.Helper()
	old := eco.State
	eco.State = eco.NewTracker()
	t.Cleanup(func() { eco.State = old })
}

// Observe feeds engine events into the tracker.
func Observe(evs ...proto.Event) {
	for _, ev := range evs {
		eco.State.Observe(ext.MainEngine, ev)
	}
}

// Drive runs cmd and delivers every resulting message to d.Update, repeatedly,
// until nothing more comes back. It returns every message seen.
func Drive(t *testing.T, ctx ext.Ctx, d ext.Component, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	var seen []tea.Msg
	queue := exttest.Exec(cmd)
	for i := 0; len(queue) > 0; i++ {
		if i > 200 {
			t.Fatal("Drive: too many messages (loop?)")
		}
		msg := queue[0]
		queue = queue[1:]
		seen = append(seen, msg)
		queue = append(queue, exttest.Exec(d.Update(ctx, msg))...)
	}
	return seen
}

// Key builds a key press from keybindings.json syntax ("enter", "j", "ctrl+s").
func Key(s string) tea.KeyPressMsg {
	var mod tea.KeyMod
	for {
		switch {
		case strings.HasPrefix(s, "ctrl+") && len(s) > 5:
			mod |= tea.ModCtrl
			s = s[5:]
			continue
		case strings.HasPrefix(s, "shift+") && len(s) > 6:
			mod |= tea.ModShift
			s = s[6:]
			continue
		case strings.HasPrefix(s, "alt+") && len(s) > 4:
			mod |= tea.ModAlt
			s = s[4:]
			continue
		}
		break
	}
	named := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "escape": tea.KeyEscape, "tab": tea.KeyTab,
		"space": tea.KeySpace, "backspace": tea.KeyBackspace, "up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight, "home": tea.KeyHome, "end": tea.KeyEnd,
		"pageup": tea.KeyPgUp, "pagedown": tea.KeyPgDown, "delete": tea.KeyDelete,
	}
	if r, ok := named[s]; ok {
		k := tea.KeyPressMsg{Code: r, Mod: mod}
		if r == tea.KeySpace && mod == 0 {
			k.Text = " "
		}
		return k
	}
	r := []rune(s)[0]
	k := tea.KeyPressMsg{Code: r, Mod: mod}
	if mod == 0 || mod == tea.ModShift {
		k.Text = s
	}
	return k
}

// Press routes keys like the host: the dialog's contexts (then Global) are
// looked up in the default bindings; a bound action goes to HandleAction, and
// anything unhandled to HandleKey. Resulting Cmds are driven.
func Press(t *testing.T, ctx *exttest.Ctx, d ext.Dialog, keys ...string) []tea.Msg {
	t.Helper()
	var seen []tea.Msg
	for _, key := range keys {
		k := Key(key)
		name := k.String()
		contexts := []string{d.KeyContext()}
		if cs, ok := d.(ext.ContextStack); ok {
			contexts = cs.KeyContexts()
		}
		contexts = append(contexts, ext.ContextGlobal)
		var cmd tea.Cmd
		handled := false
		if ah, ok := d.(ext.ActionHandler); ok {
		lookup:
			for _, c := range contexts {
				for _, b := range ctx.Bindings {
					if b.Context == c && (b.Keys == name || b.Keys == key) {
						handled, cmd = ah.HandleAction(ctx, b.Action)
						break lookup
					}
				}
			}
		}
		if !handled {
			_, cmd = d.HandleKey(ctx, k)
		}
		seen = append(seen, Drive(t, ctx, d, cmd)...)
	}
	return seen
}

// Screen renders a dialog at width (no height limit) as plain text.
func Screen(ctx ext.Ctx, d ext.Component, width int) string {
	return stripANSI(d.View(ctx, ext.Area{Width: width}).Text)
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// FakeClaude installs a claude stand-in on PATH that records argv (NUL
// separated) to the returned file and prints out for any subcommand whose
// argv contains a key of outputs (first match), else nothing.
func FakeClaude(t *testing.T, outputs map[string]string) (argvLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fake needs a unix shell")
	}
	dir := t.TempDir()
	argvLog = filepath.Join(dir, "argv")
	var cases strings.Builder
	for k, v := range outputs {
		f := filepath.Join(dir, "out-"+fmt.Sprint(len(v))+"-"+sanitize(k))
		if err := os.WriteFile(f, []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&cases, "  *%q*) /bin/cat %q; exit 0;;\n", k, f)
	}
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\0' \"$a\"; done >> \"" + argvLog + "\"\nprintf '\\n' >> \"" + argvLog + "\"\n" +
		"all=\"$*\"\ncase \"$all\" in\n" + cases.String() + "esac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("MANTLE_CLAUDE_BIN", "")
	return argvLog
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, strings.ToLower(s))
}

// Calls reads the argv log written by FakeClaude: one []string per call.
func Calls(t *testing.T, argvLog string) [][]string {
	t.Helper()
	b, err := os.ReadFile(argvLog)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		if line == "" {
			continue
		}
		out = append(out, strings.Split(strings.TrimSuffix(line, "\x00"), "\x00"))
	}
	return out
}
