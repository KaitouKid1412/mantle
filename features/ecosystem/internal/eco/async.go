package eco

import (
	"context"
	"encoding/json"
	"os/exec"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/editor"

	"github.com/KaitouKid1412/mantle/internal/claudecli"
	"github.com/KaitouKid1412/mantle/internal/claudecli/discovery"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// ResultMsg carries the result of an Async call back to the UI goroutine. Key
// says which call it answers ("mcp.list", "plugins.install").
type ResultMsg struct {
	Key   string
	Value any
	Err   error
}

// Async runs fn off the UI goroutine. fn must not touch Ctx or component state.
func Async(key string, fn func() (any, error)) tea.Cmd {
	return func() tea.Msg {
		v, err := fn()
		return ResultMsg{Key: key, Value: v, Err: err}
	}
}

// ExecDoneMsg reports that a process run with the terminal attached returned.
type ExecDoneMsg struct {
	Key string
	Err error
}

// ExecFunc hands a command the terminal; tests replace it to record commands.
var ExecFunc = func(cmd *exec.Cmd, done func(error) tea.Msg) tea.Cmd {
	return tea.ExecProcess(cmd, done)
}

// Exec runs cmd with the terminal attached (mantle's renderer is suspended) and
// delivers ExecDoneMsg{Key} when it returns.
func Exec(key string, cmd *exec.Cmd) tea.Cmd {
	return ExecFunc(cmd, func(err error) tea.Msg { return ExecDoneMsg{Key: key, Err: err} })
}

// ExecErr reports an error building an exec.Cmd as an ExecDoneMsg, so callers
// handle both paths in one place.
func ExecErr(key string, err error) tea.Cmd {
	return ext.Msg(ExecDoneMsg{Key: key, Err: err})
}

// EditorCmd builds the $VISUAL/$EDITOR command for path; tests replace it.
var EditorCmd = func(path string) (*exec.Cmd, error) {
	return editor.Command("mantle", path)
}

// Edit opens path in the user's editor and delivers ExecDoneMsg{Key}.
func Edit(key, path string) tea.Cmd {
	cmd, err := EditorCmd(path)
	if err != nil {
		return ExecErr(key, err)
	}
	return Exec(key, cmd)
}

// CLI returns a claudecli runner for the session: it runs in the session's cwd,
// because mcp and plugin scopes depend on the project.
func CLI(ctx ext.Ctx) *claudecli.Runner {
	r := *claudecli.Default
	if cwd := ctx.Session().Cwd; cwd != "" {
		r.Dir = cwd
	}
	return &r
}

// Background is the context for CLI calls started from the UI. Each subcommand
// has its own timeout.
func Background() context.Context { return context.Background() }

// Roots returns the discovery roots for the session, including plugins the engine
// loaded from outside the install directory (--plugin-dir).
func Roots(ctx ext.Ctx) discovery.Roots {
	cwd := ctx.Session().Cwd
	r := discovery.DefaultRoots(cwd)
	if RootsHook != nil {
		r = RootsHook(cwd)
	}
	for _, p := range State.Engine(ctx.Session().EngineID).Plugins() {
		if p.Path != "" {
			r.Plugins = append(r.Plugins, discovery.PluginRoot{Name: p.Name, Path: p.Path, Version: p.Version})
		}
	}
	return r
}

// RootsHook replaces discovery.DefaultRoots in tests.
var RootsHook func(cwd string) discovery.Roots

// Decode unmarshals a ControlResultMsg response into T.
func Decode[T any](m ext.ControlResultMsg) (T, error) {
	var v T
	if m.Err != nil {
		return v, m.Err
	}
	if len(m.Resp) == 0 {
		return v, nil
	}
	err := json.Unmarshal(m.Resp, &v)
	return v, err
}

// Control sends a control request to an engine ("" = main). ok is false when
// the engine is not running or does not support the subtype; the caller then
// falls back (usually to the CLI).
func Control(ctx ext.Ctx, engineID string, req proto.Request) (tea.Cmd, bool) {
	eng := ctx.Engine(engineID)
	if eng == nil || !eng.Supports(req.ControlSubtype()) {
		return nil, false
	}
	return eng.Control(req.ControlSubtype(), req), true
}

// SendText sends text to the engine as a user prompt: how slash commands the
// engine handles headlessly (skills, /pause-memory, /doctor the skill, …) are
// passed through. ok is false when no engine is running.
func SendText(ctx ext.Ctx, text string) (tea.Cmd, bool) {
	eng := ctx.Engine("")
	if eng == nil {
		return nil, false
	}
	return eng.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text(text)}}), true
}
