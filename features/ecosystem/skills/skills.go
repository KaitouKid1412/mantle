// Package skills is the /skills panel. It lists the skills and custom commands
// the engine loaded (init.skills, refreshed with reload_skills) together with
// the files discovery finds, so each has a source and a path. Space cycles a
// skill's visibility (skillOverrides: on → name-only → user-invocable-only →
// off), written through ext.ClaudeSettingsWriter; e opens SKILL.md in $EDITOR.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package skills

import (
	"fmt"
	"path/filepath"
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
const FeatureID = "ecosystem.skills"

// DialogID is the /skills panel.
const DialogID = "dialog.ecosystem.skills"

func init() {
	ext.Register(ext.Feature{ID: FeatureID, Order: 380, Parity: []string{"EC-10", "EC-43"}, Setup: Setup})
}

// Setup registers /skills. /skill-doctor stays an engine command.
func Setup(r ext.Registrar) error {
	r.AddCommand(ext.Command{
		Name: "skills", Source: ext.SourceBuiltin, Description: "List skills and choose how they are offered",
		Run: func(ctx ext.Ctx, args string) tea.Cmd { return ctx.OpenDialog(DialogID, nil) },
	})
	r.AddDialog(DialogID, eco.Factory(New))
	r.AddStory(ext.Story{ID: "ecosystem.skills/list", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d, _ := New(ctx, nil)
		d.Root().(*view).setSkills(ctx, StorySkills())
		return d.View(ctx, a)
	}})
	return nil
}

// Skill is one row.
type Skill struct {
	Name, Description, ArgHint string
	Kind                       string // skill | command
	Scope                      discovery.Scope
	Plugin                     string
	Path                       string // "" for built-in skills
	Loaded                     bool   // the engine reports it
	Visibility                 discovery.Visibility
	OverrideScope              discovery.Scope // where the override is set ("" = default)
	UserInvocable              bool
}

// Merge combines the engine's skills with discovered files and overrides.
func Merge(engine []proto.SlashCommand, found []discovery.Skill, overrides map[string]discovery.SkillOverride) []Skill {
	byName := map[string]*Skill{}
	var order []string
	add := func(s Skill) *Skill {
		if cur, ok := byName[s.Name]; ok {
			return cur
		}
		byName[s.Name] = &s
		order = append(order, s.Name)
		return byName[s.Name]
	}
	for _, f := range found {
		s := add(Skill{Name: f.Name, Description: f.Description, ArgHint: f.ArgumentHint, Kind: f.Kind,
			Scope: f.Scope, Plugin: f.Plugin, Path: f.Path, UserInvocable: f.UserInvocable})
		if s.Path == "" {
			s.Path = f.Path
		}
	}
	for _, e := range engine {
		s := add(Skill{Name: e.Name, Description: e.Description, ArgHint: e.ArgumentHint, Kind: discovery.KindSkill, UserInvocable: true})
		s.Loaded = true
		if s.Description == "" {
			s.Description = e.Description
		}
	}
	out := make([]Skill, 0, len(order))
	for _, n := range order {
		s := *byName[n]
		s.Visibility = discovery.VisibilityOn
		if o, ok := overrides[s.Name]; ok {
			s.Visibility, s.OverrideScope = o.Value, o.Scope
		}
		out = append(out, s)
	}
	return out
}

var sectionOrder = map[string]int{"Project": 0, "User": 1, "Plugins": 2, "claude.ai": 3, "Managed": 4, "Built-in": 5}

func section(s Skill) string {
	switch {
	case s.Path == "":
		return "Built-in"
	case s.Scope == discovery.ScopeProject || s.Scope == discovery.ScopeLocal:
		return "Project"
	case s.Scope == discovery.ScopeUser:
		return "User"
	case s.Scope == discovery.ScopePlugin:
		return "Plugins"
	case s.Scope == discovery.ScopeClaudeAI:
		return "claude.ai"
	case s.Scope == discovery.ScopeManaged:
		return "Managed"
	}
	return "Other"
}

func visLook(v discovery.Visibility) (glyph, label string, tok theme.Token) {
	switch v {
	case discovery.VisibilityNameOnly:
		return "◐", "name only", theme.Inactive
	case discovery.VisibilityUserInvocableOnly:
		return "◑", "you only", theme.Inactive
	case discovery.VisibilityOff:
		return "○", "off", theme.Subtle
	}
	return "●", "", theme.Skill
}

// New builds the panel.
func New(ctx ext.Ctx, _ any) (*eco.Dialog, error) {
	return eco.NewDialog(DialogID, "Skills", &view{}), nil
}

type view struct {
	eco.Base
	list   eco.List
	skills []Skill
	engine []proto.SlashCommand
	loaded bool
	files  []discovery.SettingsFile
}

func (v *view) Init(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	for _, n := range eco.State.Engine("").Skills() {
		v.engine = append(v.engine, proto.SlashCommand{Name: n})
	}
	v.rescan(ctx)
	return v.reload(ctx, d)
}

// reload asks the engine for its current skills (with descriptions).
func (v *view) reload(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	cmd, ok := eco.Control(ctx, "", proto.ReloadSkillsRequest{})
	if !ok {
		if ctx.Engine("") == nil {
			d.SetStatus(ctx, "Claude isn't running: showing skills found on disk.", theme.Inactive)
		}
		return nil
	}
	return cmd
}

// rescan re-reads files and overrides from disk.
func (v *view) rescan(ctx ext.Ctx) {
	roots := eco.Roots(ctx)
	v.files = discovery.SettingsFiles(roots)
	v.setSkills(ctx, Merge(v.engine, discovery.Skills(roots, v.files), discovery.SkillOverrides(v.files)))
}

func (v *view) setSkills(ctx ext.Ctx, skills []Skill) {
	sort.SliceStable(skills, func(i, j int) bool {
		si, sj := sectionOrder[section(skills[i])], sectionOrder[section(skills[j])]
		if si != sj {
			return si < sj
		}
		return skills[i].Name < skills[j].Name
	})
	v.skills, v.loaded = skills, true
	rows := make([]eco.Row, len(skills))
	for i, s := range skills {
		g, label, tok := visLook(s.Visibility)
		detail := s.Description
		if label != "" {
			detail = "[" + label + "] " + detail
		}
		if s.Kind == discovery.KindCommand {
			detail = "command · " + detail
		}
		rows[i] = eco.Row{Key: s.Name, Label: s.Name, Detail: detail, Glyph: g, GlyphTok: tok, Section: section(s), Value: s}
	}
	v.list.Empty = "No skills found."
	v.list.MinLabel = 16
	v.list.SetRows(rows)
}

func (v *view) Update(ctx ext.Ctx, d *eco.Dialog, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ext.ControlResultMsg:
		if m.Subtype != proto.SubReloadSkills {
			return nil
		}
		resp, err := eco.Decode[proto.ReloadSkillsResponse](m)
		if err != nil {
			eco.Fail(ctx, d, err)
			return nil
		}
		v.engine = resp.Skills
		v.rescan(ctx)
	case ext.SettingsMsg:
		for _, k := range m.Changed {
			if k == "skillOverrides" {
				v.rescan(ctx)
				return v.reload(ctx, d)
			}
		}
	case eco.ExecDoneMsg:
		if m.Key != "skills.edit" {
			return nil
		}
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
			return nil
		}
		v.rescan(ctx)
		return v.reload(ctx, d)
	}
	return nil
}

func (v *view) selected() (Skill, bool) {
	r, ok := v.list.Selected()
	if !ok {
		return Skill{}, false
	}
	return r.Value.(Skill), true
}

func (v *view) Action(ctx ext.Ctx, d *eco.Dialog, a ext.ActionID) (bool, tea.Cmd) {
	if v.list.HandleAction(a, 5) {
		return true, nil
	}
	if a == ext.ActSelectAccept {
		if s, ok := v.selected(); ok {
			d.Push(ctx, v.detail(s))
		}
		return true, nil
	}
	return false, nil
}

func (v *view) Key(ctx ext.Ctx, d *eco.Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	s, ok := v.selected()
	if !ok {
		return false, nil
	}
	switch k.String() {
	case "space":
		return true, v.cycle(ctx, d, s)
	case "e":
		return true, v.edit(ctx, d, s)
	}
	return false, nil
}

func (v *view) edit(ctx ext.Ctx, d *eco.Dialog, s Skill) tea.Cmd {
	if s.Path == "" {
		d.SetStatus(ctx, s.Name+" is built into Claude Code; there is no file to edit.", theme.Inactive)
		return nil
	}
	return eco.Edit("skills.edit", s.Path)
}

// overrideScope is where a new override is written: the scope that already
// sets one, else local settings for project skills and user settings otherwise.
func overrideScope(s Skill) string {
	switch s.OverrideScope {
	case discovery.ScopeUser:
		return ext.ScopeUser
	case discovery.ScopeProject:
		return ext.ScopeProject
	case discovery.ScopeLocal:
		return ext.ScopeLocal
	}
	if s.Scope == discovery.ScopeProject || s.Scope == discovery.ScopeLocal {
		return ext.ScopeLocal
	}
	return ext.ScopeUser
}

var scopeFile = map[string]discovery.Scope{ext.ScopeUser: discovery.ScopeUser, ext.ScopeProject: discovery.ScopeProject, ext.ScopeLocal: discovery.ScopeLocal}

func (v *view) cycle(ctx ext.Ctx, d *eco.Dialog, s Skill) tea.Cmd {
	if s.OverrideScope == discovery.ScopeManaged {
		d.SetStatus(ctx, "Your administrator sets this skill's visibility.", theme.Inactive)
		return nil
	}
	w, ok := ctx.Settings().(ext.ClaudeSettingsWriter)
	if !ok {
		d.SetStatus(ctx, "mantle can't write settings in this build.", theme.Warning)
		return nil
	}
	scope := overrideScope(s)
	// Merge into that file's current map so other skills' overrides survive.
	m := map[string]any{}
	for _, f := range v.files {
		if f.Scope != scopeFile[scope] {
			continue
		}
		var cur map[string]any
		if f.Get("skillOverrides", &cur) {
			m = cur
		}
	}
	next := s.Visibility.Next()
	var value any = m
	if next == discovery.VisibilityOn {
		delete(m, s.Name)
		if len(m) == 0 {
			value = nil
		}
	} else {
		m[s.Name] = string(next)
	}
	_, label, _ := visLook(next)
	if label == "" {
		label = "shown normally"
	}
	eco.Done(ctx, d, fmt.Sprintf("%s: %s (%s)", s.Name, label, strings.TrimSuffix(scope, "Settings")))
	// Show the new value at once; the SettingsMsg that follows rescans.
	for i := range v.skills {
		if v.skills[i].Name == s.Name {
			v.skills[i].Visibility, v.skills[i].OverrideScope = next, scopeFile[scope]
		}
	}
	v.setSkills(ctx, v.skills)
	return w.SetClaude(scope, "skillOverrides", value)
}

func (v *view) detail(s Skill) eco.View {
	_, vis, _ := visLook(s.Visibility)
	if vis == "" {
		vis = "shown normally"
	}
	lines := []string{"/" + s.Name, ""}
	if s.Description != "" {
		lines = append(lines, s.Description, "")
	}
	if s.ArgHint != "" {
		lines = append(lines, "Arguments: "+s.ArgHint)
	}
	lines = append(lines, "Source: "+section(s))
	if s.Plugin != "" {
		lines = append(lines, "Plugin: "+s.Plugin)
	}
	if s.Path != "" {
		lines = append(lines, "File: "+s.Path)
	}
	lines = append(lines, "Visibility: "+vis)
	if !s.Loaded {
		lines = append(lines, "", "Claude hasn't loaded this skill in this session.")
	}
	if !s.UserInvocable {
		lines = append(lines, "Hidden from the / menu (user-invocable: false).")
	}
	tv := &eco.TextView{Lines: lines, Sub: s.Name}
	tv.HintFn = func(ext.Ctx) string { return eco.Hint("e", "edit", "esc", "back") }
	return &detailView{TextView: tv, parent: v, skill: s}
}

type detailView struct {
	*eco.TextView
	parent *view
	skill  Skill
}

func (dv *detailView) Key(ctx ext.Ctx, d *eco.Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if k.String() == "e" {
		return true, dv.parent.edit(ctx, d, dv.skill)
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
	return eco.Hint("enter", "details", "space", "cycle visibility", "e", "edit", "esc", "close")
}

// StorySkills is fixed data for the story.
func StorySkills() []Skill {
	p := filepath.Join("work", "repo", ".claude", "skills")
	return []Skill{
		{Name: "deploy", Description: "Deploy the app", Scope: discovery.ScopeProject, Kind: discovery.KindSkill,
			Path: filepath.Join(p, "deploy", "SKILL.md"), Loaded: true, Visibility: discovery.VisibilityOn, UserInvocable: true},
		{Name: "release-notes", Description: "Draft release notes", Scope: discovery.ScopeUser, Kind: discovery.KindCommand,
			Path: "~/.claude/commands/release-notes.md", Loaded: true, Visibility: discovery.VisibilityNameOnly, UserInvocable: true},
		{Name: "demo:status", Description: "(Demo) Show status", Scope: discovery.ScopePlugin, Plugin: "demo",
			Kind: discovery.KindSkill, Path: "plugins/demo/skills/status/SKILL.md", Loaded: true, Visibility: discovery.VisibilityOff, UserInvocable: true},
		{Name: "simplify", Description: "Review changed code for reuse and quality", Kind: discovery.KindSkill,
			Loaded: true, Visibility: discovery.VisibilityOn, UserInvocable: true},
	}
}
