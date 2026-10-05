package sessions

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Commands the engine runs headlessly, with mantle adding what the terminal UI shows
// around them: /goal (and the active goal), /plan, /add-dir, /cd.

func (f *feature) registerPassthrough(r ext.Registrar) {
	r.AddCommand(ext.Command{
		ID: ext.CommandID("goal"), Name: "goal", Source: ext.SourceBuiltin,
		Description: "Keep working until a condition holds (clear to stop)",
		ArgHint:     "[condition | clear]",
		Run: func(ctx ext.Ctx, args string) tea.Cmd {
			return sendCommand(ctx, ext.MainEngine, joinCommand("/goal", args))
		},
	})
	r.AddCommand(ext.Command{
		ID: ext.CommandID("plan"), Name: "plan", Source: ext.SourceBuiltin,
		Description: "Switch to plan mode, or open the session's plan",
		ArgHint:     "[open | description]",
		Run:         f.runPlan,
	})
	r.AddCommand(ext.Command{
		ID: ext.CommandID("add-dir"), Name: "add-dir", Source: ext.SourceBuiltin,
		Description: "Add a working directory",
		ArgHint:     "<path>",
		Run:         f.runAddDir,
	})
	r.AddCommand(ext.Command{
		ID: ext.CommandID("cd"), Name: "cd", Source: ext.SourceBuiltin,
		Description: "Move this session to another directory",
		ArgHint:     "<path>",
		Run:         f.runCd,
	})
	r.AddComponent(ext.SlotAboveInput, &goalLine{f: f}, ext.SlotOpts{Weight: 800, MaxHeight: 1})
}

func joinCommand(name, args string) string {
	if a := strings.TrimSpace(args); a != "" {
		return name + " " + a
	}
	return name
}

// goal is the active /goal, from active_goal events.
type goal struct {
	Condition  string `json:"condition"`
	Iterations int    `json:"iterations"`
	LastReason string `json:"last_reason"`
}

// observeGoal reads an active_goal event (the value field; older shapes used goal).
func (f *feature) observeGoal(ctx ext.Ctx, e *proto.ActiveGoal) {
	var w struct {
		Value json.RawMessage `json:"value"`
		Goal  json.RawMessage `json:"goal"`
	}
	_ = json.Unmarshal(e.Raw, &w)
	raw := w.Value
	if len(raw) == 0 {
		raw = w.Goal
	}
	if len(raw) == 0 {
		raw = e.Goal
	}
	f.goal = nil
	if len(raw) > 0 && string(raw) != "null" {
		var g goal
		if json.Unmarshal(raw, &g) == nil && g.Condition != "" {
			f.goal = &g
		}
	}
	ctx.Invalidate(goalComponentID)
}

const goalComponentID = "sessions.goal"

// goalLine shows the active goal above the prompt.
type goalLine struct{ f *feature }

func (g *goalLine) ID() string                      { return goalComponentID }
func (g *goalLine) Init(ext.Ctx) tea.Cmd            { return nil }
func (g *goalLine) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }

func (g *goalLine) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	gl := g.f.goal
	if gl == nil {
		return ext.Rendered{}
	}
	th := ctx.Theme()
	text := th.Paint(theme.Accent, "◎ ") + th.Paint(theme.Text, "Goal: "+oneLine(gl.Condition))
	if gl.Iterations > 0 {
		text += th.Paint(theme.Inactive, " · checked "+plural(gl.Iterations, "time", "times"))
	}
	return ext.Rendered{Text: fit(text, a.Width)}
}

// runPlan: no argument switches to plan mode; "open" opens the plan file in $EDITOR;
// a description switches to plan mode and sends it.
func (f *feature) runPlan(ctx ext.Ctx, args string) tea.Cmd {
	args = strings.TrimSpace(args)
	if args == "open" {
		return f.openPlan(ctx)
	}
	eng := ctx.Engine(ext.MainEngine)
	if eng == nil {
		return notice(ctx, "no-engine", "Claude is not running", ext.NoticeWarning)
	}
	cmds := []tea.Cmd{
		eng.Control(proto.SubSetPermissionMode, proto.SetPermissionModeRequest{Mode: proto.ModePlan}),
	}
	info := ctx.Session()
	info.PermissionMode = proto.ModePlan
	cmds = append(cmds, ext.Msg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: info}))
	if args != "" {
		cmds = append(cmds, eng.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text(args)}}))
	}
	return tea.Sequence(cmds...)
}

type planFileMsg struct {
	path string
	err  error
}

// openPlan finds the session's plan file (<config>/plans/<slug>.md, slug from the
// transcript) and opens it in the editor.
func (f *feature) openPlan(ctx ext.Ctx) tea.Cmd {
	sid, cwd, l := ctx.Session().SessionID, f.cwd(ctx), f.layout
	if sid == "" {
		return notice(ctx, "plan", "No plan yet in this session", ext.NoticeInfo)
	}
	return func() tea.Msg {
		path, err := planPath(l, sid, cwd)
		return planFileMsg{path: path, err: err}
	}
}

func planPath(l sessions.Layout, sid, cwd string) (string, error) {
	p, err := l.FindSession(sid, cwd)
	if err != nil {
		return "", err
	}
	tr, err := sessions.Load(p)
	if err != nil {
		return "", err
	}
	slug := ""
	for _, e := range tr.Entries {
		if e.Slug != "" {
			slug = e.Slug
		}
	}
	if slug == "" {
		return "", os.ErrNotExist
	}
	path := filepath.Join(l.PlansDir(), slug+".md")
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}

func (f *feature) onPlanFile(ctx ext.Ctx, m planFileMsg) tea.Cmd {
	if m.err != nil {
		return notice(ctx, "plan", "No plan yet in this session", ext.NoticeInfo)
	}
	return tea.ExecProcess(editorCmd(m.path), func(err error) tea.Msg {
		if err != nil {
			return notifyMsg{key: "plan", text: "Editor failed: " + err.Error(), level: ext.NoticeError}
		}
		return nil
	})
}

// notifyMsg turns a Cmd result into a notice on the UI goroutine.
type notifyMsg struct {
	key, text string
	level     ext.NoticeLevel
}

func (f *feature) onNotify(ctx ext.Ctx, m notifyMsg) tea.Cmd {
	return notice(ctx, m.key, m.text, m.level)
}

// editorCmd opens path in $VISUAL or $EDITOR (vi when neither is set).
func editorCmd(path string) *exec.Cmd {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "vi"
	}
	parts := strings.Fields(ed)
	return exec.Command(parts[0], append(parts[1:], path)...)
}

// expandPath resolves ~ and relative paths against cwd.
func expandPath(p, cwd string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	if !filepath.IsAbs(p) && cwd != "" {
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p)
}

// runAddDir adds the directory through the engine's /add-dir and, where supported,
// registers it as a repository root so its CLAUDE.md and skills load.
func (f *feature) runAddDir(ctx ext.Ctx, args string) tea.Cmd {
	if strings.TrimSpace(args) == "" {
		return notice(ctx, "add-dir", "Usage: /add-dir <path>", ext.NoticeInfo)
	}
	dir := expandPath(args, f.cwd(ctx))
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return notice(ctx, "add-dir", "Not a directory: "+dir, ext.NoticeWarning)
	}
	eng := ctx.Engine(ext.MainEngine)
	if eng == nil {
		return notice(ctx, "no-engine", "Claude is not running", ext.NoticeWarning)
	}
	cmds := []tea.Cmd{eng.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text("/add-dir " + dir)}})}
	if eng.Supports(proto.SubRegisterRepoRoot) {
		cmds = append(cmds, eng.Control(proto.SubRegisterRepoRoot, proto.RegisterRepoRootRequest{
			Directory: dir, ReloadClaudeMD: true, ReloadSkills: true,
		}))
	}
	return tea.Sequence(cmds...)
}

// subSetCwd is the engine's unstable request for moving a session.
const subSetCwd = "set_cwd"

// runCd moves the session: through the engine's set_cwd when it has one, otherwise in
// Claude Code (the engine can't find a session from another project's directory, and
// mantle never writes the relocation record itself).
func (f *feature) runCd(ctx ext.Ctx, args string) tea.Cmd {
	if strings.TrimSpace(args) == "" {
		return notice(ctx, "cd", "Usage: /cd <path>", ext.NoticeInfo)
	}
	dir := expandPath(args, f.cwd(ctx))
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return notice(ctx, "cd", "Not a directory: "+dir, ext.NoticeWarning)
	}
	eng := ctx.Engine(ext.MainEngine)
	if eng != nil && eng.Supports(subSetCwd) {
		fields, _ := json.Marshal(map[string]string{"cwd": dir})
		req := proto.RawRequest{Subtype: subSetCwd, Fields: fields}
		return controlCmd(eng.Control(subSetCwd, req), func(r ext.ControlResultMsg) tea.Msg {
			if r.Err != nil {
				return notifyMsg{key: "cd", text: "Could not change directory: " + r.Err.Error(), level: ext.NoticeError}
			}
			return cwdChangedMsg{dir: dir}
		})
	}
	return ctx.OpenDialog(DialogHandoff, []string{"/cd " + dir})
}

type cwdChangedMsg struct{ dir string }

func (f *feature) onCwdChanged(ctx ext.Ctx, m cwdChangedMsg) tea.Cmd {
	info := ctx.Session()
	info.Cwd = m.dir
	return tea.Batch(
		ext.Msg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: info}),
		notice(ctx, "cd", "Now working in "+m.dir, ext.NoticeSuccess),
	)
}
