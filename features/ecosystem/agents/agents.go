// Package agents is the /agents panel: subagent definitions from
// ~/.claude/agents, .claude/agents and plugins, plus the engine's built-in
// agents (read-only). n creates an agent from a template in $EDITOR, e edits
// one, x deletes one. After a change the panel calls reload_plugins, whose
// reply lists the agents the engine now has; if the change isn't there it
// offers a restart. Claude Code 2.1.288 marks its own /agents as removed, so
// this is a mantle convenience kept deliberately small.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package agents

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/internal/claudecli/discovery"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// FeatureID is this feature's ID.
const FeatureID = "ecosystem.agents"

// DialogID is the /agents panel.
const DialogID = "dialog.ecosystem.agents"

var reName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func init() {
	ext.Register(ext.Feature{ID: FeatureID, Order: 400, Parity: []string{"EC-13", "EC-42"}, Setup: Setup})
}

// Setup registers /agents.
func Setup(r ext.Registrar) error {
	r.AddCommand(ext.Command{
		Name: "agents", Source: ext.SourceBuiltin, Description: "Browse and edit subagent definitions",
		Run: func(ctx ext.Ctx, args string) tea.Cmd { return ctx.OpenDialog(DialogID, nil) },
	})
	r.AddDialog(DialogID, eco.Factory(New))
	r.AddStory(ext.Story{ID: "ecosystem.agents/list", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d, _ := New(ctx, nil)
		d.Root().(*view).setAgents(ctx, StoryAgents())
		return d.View(ctx, a)
	}})
	return nil
}

// Agent is one row.
type Agent struct {
	Name, Description, Model string
	Tools                    []string
	Scope                    discovery.Scope // "" for built-in agents
	Path                     string
	Err                      error
}

func (a Agent) editable() string {
	switch a.Scope {
	case discovery.ScopeUser, discovery.ScopeProject:
		return ""
	case discovery.ScopePlugin:
		return "comes from a plugin"
	case discovery.ScopeManaged:
		return "set by your administrator"
	}
	return "built into Claude Code"
}

// Merge combines discovered agent files with the engine's agent names.
func Merge(found []discovery.Agent, engine []proto.AgentInfo) []Agent {
	seen := map[string]bool{}
	var out []Agent
	for _, f := range found {
		seen[f.Name] = true
		out = append(out, Agent{Name: f.Name, Description: f.Description, Model: f.Model, Tools: f.Tools,
			Scope: f.Scope, Path: f.Path, Err: f.Err})
	}
	for _, e := range engine {
		if !seen[e.Name] {
			seen[e.Name] = true
			out = append(out, Agent{Name: e.Name, Description: e.Description, Model: e.Model})
		}
	}
	return out
}

var sectionRank = map[string]int{"Project": 0, "User": 1, "Plugins": 2, "Managed": 3, "Built-in": 4}

func section(a Agent) string {
	switch a.Scope {
	case discovery.ScopeProject:
		return "Project"
	case discovery.ScopeUser:
		return "User"
	case discovery.ScopePlugin:
		return "Plugins"
	case discovery.ScopeManaged:
		return "Managed"
	}
	return "Built-in"
}

// New builds the panel.
func New(ctx ext.Ctx, _ any) (*eco.Dialog, error) {
	return eco.NewDialog(DialogID, "Agents", &view{}), nil
}

type view struct {
	eco.Base
	list   eco.List
	agents []Agent
	engine []proto.AgentInfo
	loaded bool
	// expect is the agent a change should add (or, with gone, remove).
	expect string
	gone   bool
}

func (v *view) Init(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	for _, n := range eco.State.Engine("").Agents() {
		v.engine = append(v.engine, proto.AgentInfo{Name: n})
	}
	v.rescan(ctx)
	return nil
}

func (v *view) rescan(ctx ext.Ctx) {
	roots := eco.Roots(ctx)
	v.setAgents(ctx, Merge(discovery.Agents(roots, discovery.SettingsFiles(roots)), v.engine))
}

func (v *view) setAgents(ctx ext.Ctx, agents []Agent) {
	sort.SliceStable(agents, func(i, j int) bool {
		si, sj := sectionRank[section(agents[i])], sectionRank[section(agents[j])]
		if si != sj {
			return si < sj
		}
		return agents[i].Name < agents[j].Name
	})
	v.agents, v.loaded = agents, true
	rows := make([]eco.Row, len(agents))
	for i, a := range agents {
		detail := a.Description
		if a.Model != "" && a.Model != "inherit" {
			detail = a.Model + " · " + detail
		}
		g, tok := "●", theme.Accent
		if a.Err != nil {
			g, tok, detail = "!", theme.Warning, "can't read: "+a.Err.Error()
		}
		rows[i] = eco.Row{Key: a.Name, Label: a.Name, Detail: detail, Glyph: g, GlyphTok: tok, Section: section(a), Value: a}
	}
	v.list.Empty = "No agents. Press n to create one."
	v.list.MinLabel = 16
	v.list.SetRows(rows)
}

func (v *view) selected() (Agent, bool) {
	r, ok := v.list.Selected()
	if !ok {
		return Agent{}, false
	}
	return r.Value.(Agent), true
}

func (v *view) Update(ctx ext.Ctx, d *eco.Dialog, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case eco.ExecDoneMsg:
		if m.Key != "agents.edit" {
			return nil
		}
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
		}
		return v.reload(ctx, d)
	case ext.ControlResultMsg:
		if m.Subtype != proto.SubReloadPlugins {
			return nil
		}
		resp, err := eco.Decode[proto.ReloadPluginsResponse](m)
		if err != nil {
			eco.Fail(ctx, d, err)
			v.rescan(ctx)
			return nil
		}
		if len(resp.Agents) > 0 {
			v.engine = resp.Agents
		}
		v.rescan(ctx)
		return v.checkExpected(ctx, d)
	}
	return nil
}

// reload asks the engine to re-read agents; without one, just rescan files.
func (v *view) reload(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	if cmd, ok := eco.Control(ctx, "", proto.ReloadPluginsRequest{}); ok {
		return cmd
	}
	v.rescan(ctx)
	v.expect = ""
	return nil
}

func (v *view) checkExpected(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	if v.expect == "" {
		return nil
	}
	name, gone := v.expect, v.gone
	v.expect, v.gone = "", false
	has := false
	for _, a := range v.engine {
		has = has || a.Name == name
	}
	if has != gone {
		eco.Done(ctx, d, "Claude has the change now.")
		return nil
	}
	d.Push(ctx, &eco.ConfirmView{
		Question: []string{"Claude needs a restart to pick up the change to " + name + ".", "Restart now? The conversation is kept."},
		YesLabel: "Restart now",
		OnYes: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
			return tea.Batch(d.Close(ctx), eco.RestartEngine(ctx, "to load agent changes"))
		},
	})
	return nil
}

func (v *view) Action(ctx ext.Ctx, d *eco.Dialog, a ext.ActionID) (bool, tea.Cmd) {
	if v.list.HandleAction(a, 5) {
		return true, nil
	}
	if a == ext.ActSelectAccept {
		if ag, ok := v.selected(); ok {
			d.Push(ctx, v.detail(ag))
		}
		return true, nil
	}
	return false, nil
}

func (v *view) Key(ctx ext.Ctx, d *eco.Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "n":
		d.Push(ctx, v.scopeMenu())
		return true, nil
	case "e", "x":
		a, ok := v.selected()
		if !ok {
			return true, nil
		}
		if k.String() == "e" {
			return true, v.edit(ctx, d, a)
		}
		return true, v.confirmDelete(ctx, d, a)
	}
	return false, nil
}

func (v *view) edit(ctx ext.Ctx, d *eco.Dialog, a Agent) tea.Cmd {
	if why := a.editable(); why != "" {
		d.SetStatus(ctx, a.Name+" can't be edited here: it "+why+".", theme.Inactive)
		return nil
	}
	v.expect = ""
	return eco.Edit("agents.edit", a.Path)
}

func (v *view) confirmDelete(ctx ext.Ctx, d *eco.Dialog, a Agent) tea.Cmd {
	if why := a.editable(); why != "" {
		d.SetStatus(ctx, a.Name+" can't be deleted here: it "+why+".", theme.Inactive)
		return nil
	}
	d.Push(ctx, &eco.ConfirmView{
		Question: []string{fmt.Sprintf("Delete the %s agent?", a.Name), a.Path},
		YesLabel: "Delete", Danger: true,
		OnYes: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
			if err := os.Remove(a.Path); err != nil {
				eco.Fail(ctx, d, err)
				return nil
			}
			v.expect, v.gone = a.Name, true
			eco.Done(ctx, d, "Deleted "+a.Name+".")
			return v.reload(ctx, d)
		},
	})
	return nil
}

func (v *view) scopeMenu() eco.View {
	pick := func(scope discovery.Scope) func(ext.Ctx, *eco.Dialog) tea.Cmd {
		return func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
			d.Pop(ctx)
			d.Push(ctx, v.nameForm(scope))
			return nil
		}
	}
	return eco.NewMenu("New agent", []string{"Where should the agent live?"},
		eco.MenuItem{Label: "This project", Detail: ".claude/agents, shared through the repo", Run: pick(discovery.ScopeProject)},
		eco.MenuItem{Label: "Your user", Detail: "~/.claude/agents, every project", Run: pick(discovery.ScopeUser)},
	)
}

func (v *view) nameForm(scope discovery.Scope) eco.View {
	f := &eco.FormView{Sub: "New agent", Fields: []eco.Field{
		{Label: "Name", Help: "lowercase letters, digits and dashes, e.g. code-reviewer"},
	}}
	f.OnSubmit = func(ctx ext.Ctx, d *eco.Dialog, vals []string) tea.Cmd {
		name := vals[0]
		if !reName.MatchString(name) {
			f.Err = "Use lowercase letters, digits and dashes."
			return nil
		}
		path := discovery.AgentPath(eco.Roots(ctx), scope, name)
		if _, err := os.Stat(path); err == nil {
			f.Err = "An agent with that name already exists: " + path
			return nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			f.Err = err.Error()
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			f.Err = err.Error()
			return nil
		}
		if err := os.WriteFile(path, []byte(discovery.AgentTemplate(name)), 0o644); err != nil {
			f.Err = err.Error()
			return nil
		}
		d.Pop(ctx)
		v.expect, v.gone = name, false
		v.rescan(ctx)
		v.list.Select(name)
		return eco.Edit("agents.edit", path)
	}
	return f
}

func (v *view) detail(a Agent) eco.View {
	lines := []string{a.Name, ""}
	if a.Description != "" {
		lines = append(lines, a.Description, "")
	}
	if a.Model != "" {
		lines = append(lines, "Model: "+a.Model)
	}
	tools := "all tools"
	if len(a.Tools) > 0 {
		tools = strings.Join(a.Tools, ", ")
	}
	lines = append(lines, "Tools: "+tools, "Source: "+section(a))
	if a.Path != "" {
		lines = append(lines, "File: "+a.Path)
	}
	if a.Err != nil {
		lines = append(lines, "", "Problem: "+a.Err.Error())
	}
	tv := &eco.TextView{Lines: lines, Sub: a.Name}
	if a.editable() == "" {
		tv.HintFn = func(ext.Ctx) string { return eco.Hint("e", "edit", "x", "delete", "esc", "back") }
	}
	return &detailView{TextView: tv, parent: v, agent: a}
}

type detailView struct {
	*eco.TextView
	parent *view
	agent  Agent
}

func (dv *detailView) Key(ctx ext.Ctx, d *eco.Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "e":
		return true, dv.parent.edit(ctx, d, dv.agent)
	case "x":
		d.Pop(ctx)
		return true, dv.parent.confirmDelete(ctx, d, dv.agent)
	}
	return false, nil
}

func (v *view) Render(ctx ext.Ctx, th *theme.Theme, width, height int) []string {
	if !v.loaded {
		return nil
	}
	return v.list.Render(th, width, height)
}

func (v *view) Hints(ctx ext.Ctx) string {
	return eco.Hint("enter", "details", "n", "new", "e", "edit", "x", "delete", "esc", "close")
}

// StoryAgents is fixed data for the story.
func StoryAgents() []Agent {
	return []Agent{
		{Name: "code-reviewer", Description: "Reviews diffs for bugs and style", Model: "sonnet", Tools: []string{"Read", "Grep"},
			Scope: discovery.ScopeProject, Path: ".claude/agents/code-reviewer.md"},
		{Name: "notes", Description: "Keeps my notes tidy", Scope: discovery.ScopeUser, Path: "~/.claude/agents/notes.md"},
		{Name: "demo:reviewer", Description: "Reviews with the demo plugin", Scope: discovery.ScopePlugin, Path: "plugins/demo/agents/reviewer.md"},
		{Name: "Explore", Description: "Read-only search agent"},
		{Name: "general-purpose", Description: "Researches and runs multi-step tasks"},
	}
}
