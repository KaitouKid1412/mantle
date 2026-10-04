// Package memory is the /memory panel and the /pause-memory fallback. It lists
// the instruction files Claude loads (managed, user, project and local
// CLAUDE.md, rules, AGENTS.md, @imports, auto memory), opens one in $EDITOR
// (creating it if needed), toggles auto memory (autoMemoryEnabled) and asks the
// engine to reload CLAUDE.md afterwards with register_repo_root.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/internal/claudecli/discovery"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// FeatureID is this feature's ID.
const FeatureID = "ecosystem.memory"

// DialogID is the /memory panel.
const DialogID = "dialog.ecosystem.memory"

func init() {
	ext.Register(ext.Feature{ID: FeatureID, Order: 390, Parity: []string{"EC-14", "EC-15", "EC-44"}, Setup: Setup})
}

// Setup registers /memory and /pause-memory.
func Setup(r ext.Registrar) error {
	r.AddCommand(ext.Command{
		Name: "memory", Source: ext.SourceBuiltin, Description: "Edit memory files and auto memory",
		Run: func(ctx ext.Ctx, args string) tea.Cmd { return ctx.OpenDialog(DialogID, nil) },
	})
	r.AddCommand(ext.Command{
		Name: "pause-memory", Aliases: []string{"memory-pause", "toggle-memory"}, Source: ext.SourceBuiltin,
		Description: "Turn auto memory off or on", Run: pauseMemory,
	})
	r.AddDialog(DialogID, eco.Factory(New))
	r.AddStory(ext.Story{ID: "ecosystem.memory/files", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d, _ := New(ctx, nil)
		d.Root().(*view).setInfo(ctx, StoryInfo(), "/work/repo", "/home/user")
		return d.View(ctx, a)
	}})
	return nil
}

// pauseMemory passes /pause-memory to the engine when it handles it, and
// otherwise toggles autoMemoryEnabled itself.
func pauseMemory(ctx ext.Ctx, args string) tea.Cmd {
	if eco.State.Engine("").HasCommand("pause-memory") {
		line := "/pause-memory"
		if args = strings.TrimSpace(args); args != "" {
			line += " " + args
		}
		if cmd, ok := eco.SendText(ctx, line); ok {
			return cmd
		}
	}
	roots := eco.Roots(ctx)
	files := discovery.SettingsFiles(roots)
	on, _, _ := discovery.AutoMemory(roots, files)
	cmd, err := setAutoMemory(ctx, files, !on)
	if err != "" {
		return ctx.Notify(ext.Notice{Key: "memory.pause", Level: ext.NoticeWarning, Text: err})
	}
	text := "Auto memory paused: Claude won't read or write it."
	if !on {
		text = "Auto memory is on again."
	}
	return tea.Batch(cmd, ctx.Notify(ext.Notice{Key: "memory.pause", Level: ext.NoticeSuccess, Text: text, Source: FeatureID}))
}

// setAutoMemory writes autoMemoryEnabled to the scope that sets it, or local
// settings ("for this project").
func setAutoMemory(ctx ext.Ctx, files []discovery.SettingsFile, on bool) (tea.Cmd, string) {
	w, ok := ctx.Settings().(ext.ClaudeSettingsWriter)
	if !ok {
		return nil, "mantle can't write settings in this build."
	}
	_, scope := discovery.Effective(files, "autoMemoryEnabled")
	target := ext.ScopeLocal
	switch scope {
	case discovery.ScopeUser:
		target = ext.ScopeUser
	case discovery.ScopeProject:
		target = ext.ScopeProject
	case discovery.ScopeManaged:
		return nil, "Your administrator controls auto memory."
	}
	return w.SetClaude(target, "autoMemoryEnabled", on), ""
}

// New builds the panel.
func New(ctx ext.Ctx, _ any) (*eco.Dialog, error) {
	return eco.NewDialog(DialogID, "Memory", &view{}), nil
}

type view struct {
	eco.Base
	list   eco.List
	info   discovery.MemoryInfo
	files  []discovery.SettingsFile
	loaded bool
	cwd    string
	home   string
}

func (v *view) Init(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	v.rescan(ctx)
	return nil
}

func (v *view) rescan(ctx ext.Ctx) {
	roots := eco.Roots(ctx)
	v.files = discovery.SettingsFiles(roots)
	info := discovery.Memory(roots, v.files)
	// The engine knows the auto-memory directory for sure (init.memory_paths).
	if dir := engineAutoDir(); dir != "" && dir != info.AutoDir {
		info.AutoDir = dir
	}
	v.setInfo(ctx, info, roots.CWD, roots.Home)
}

func engineAutoDir() string {
	in := eco.State.Engine("").Init
	if in == nil || len(in.MemoryPaths) == 0 {
		return ""
	}
	var paths map[string]any
	if json.Unmarshal(in.MemoryPaths, &paths) != nil {
		return ""
	}
	s, _ := paths["auto"].(string)
	return s
}

var scopeSection = map[discovery.Scope]string{
	discovery.ScopeManaged: "Managed", discovery.ScopeUser: "User", discovery.ScopeProject: "Project",
	discovery.ScopeLocal: "Local · only you", discovery.ScopeAuto: "Auto memory",
}

// display shortens a path relative to the project or home directory.
func display(p, cwd, home string) string {
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	if home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return p
}

type action int

const (
	actToggleAuto action = iota + 1
	actOpenAutoDir
)

func (v *view) setInfo(ctx ext.Ctx, info discovery.MemoryInfo, cwd, home string) {
	v.info, v.loaded, v.cwd, v.home = info, true, cwd, home
	var rows []eco.Row
	for _, f := range info.Files {
		if f.Scope == discovery.ScopeAuto && !info.AutoEnabled {
			continue
		}
		section := scopeSection[f.Scope]
		label := display(f.Path, cwd, home)
		var notes []string
		switch f.Kind {
		case discovery.MemoryRule:
			notes = append(notes, "rule")
			if len(f.Paths) > 0 {
				notes = append(notes, "for "+strings.Join(f.Paths, ", "))
			}
		case discovery.MemoryImport:
			notes = append(notes, "imported by "+filepath.Base(f.ImportedFrom))
		case discovery.MemoryAgentsMD:
			notes = append(notes, "AGENTS.md")
		}
		g, tok := "●", theme.Remember
		if !f.Exists {
			g, tok = "+", theme.Subtle
			notes = append(notes, "not created yet")
		}
		rows = append(rows, eco.Row{Key: f.Path, Label: label, Detail: strings.Join(notes, " · "),
			Glyph: g, GlyphTok: tok, Section: section, Value: f})
	}
	autoLabel := "Auto memory: on"
	autoDetail := "Claude saves what it learns; press enter to turn off"
	if !info.AutoEnabled {
		autoLabel, autoDetail = "Auto memory: off", "press enter to turn on"
	}
	rows = append(rows, eco.Row{Key: "auto.toggle", Label: autoLabel, Detail: autoDetail, Section: "Settings", Value: actToggleAuto})
	if info.AutoEnabled && info.AutoDir != "" {
		rows = append(rows, eco.Row{Key: "auto.dir", Label: "Open the auto-memory folder", Detail: display(info.AutoDir, cwd, home),
			Section: "Settings", Value: actOpenAutoDir})
	}
	v.list.MinLabel = 20
	v.list.SetRows(rows)
}

func (v *view) Update(ctx ext.Ctx, d *eco.Dialog, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case eco.ExecDoneMsg:
		if m.Key != "memory.edit" {
			return nil
		}
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
			return nil
		}
		v.rescan(ctx)
		return v.reload(ctx, d)
	case ext.ControlResultMsg:
		if m.Subtype != proto.SubRegisterRepoRoot {
			return nil
		}
		if m.Err != nil {
			d.SetStatus(ctx, "Saved. Claude picks it up in the next session.", theme.Inactive)
			return nil
		}
		eco.Done(ctx, d, "Saved and reloaded.")
	case ext.SettingsMsg:
		for _, k := range m.Changed {
			if k == "autoMemoryEnabled" {
				v.rescan(ctx)
			}
		}
	}
	return nil
}

// reload asks the engine to re-read CLAUDE.md files.
func (v *view) reload(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	cmd, ok := eco.Control(ctx, "", proto.RegisterRepoRootRequest{Directory: ctx.Session().Cwd, ReloadClaudeMD: true})
	if !ok {
		d.SetStatus(ctx, "Saved. Claude picks it up in the next session.", theme.Inactive)
		return nil
	}
	return cmd
}

func (v *view) Action(ctx ext.Ctx, d *eco.Dialog, a ext.ActionID) (bool, tea.Cmd) {
	if v.list.HandleAction(a, 5) {
		return true, nil
	}
	if a != ext.ActSelectAccept {
		return false, nil
	}
	r, ok := v.list.Selected()
	if !ok {
		return true, nil
	}
	switch val := r.Value.(type) {
	case discovery.MemoryFile:
		return true, v.open(ctx, d, val)
	case action:
		return true, v.do(ctx, d, val)
	}
	return true, nil
}

func (v *view) Key(ctx ext.Ctx, d *eco.Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "e":
		if r, ok := v.list.Selected(); ok {
			if f, ok := r.Value.(discovery.MemoryFile); ok {
				return true, v.open(ctx, d, f)
			}
		}
		return true, nil
	case "a":
		return true, v.do(ctx, d, actToggleAuto)
	}
	return false, nil
}

func (v *view) open(ctx ext.Ctx, d *eco.Dialog, f discovery.MemoryFile) tea.Cmd {
	if f.Scope == discovery.ScopeManaged {
		d.SetStatus(ctx, "Managed memory is set by your administrator.", theme.Inactive)
		return nil
	}
	if !f.Exists {
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
			eco.Fail(ctx, d, err)
			return nil
		}
	}
	return eco.Edit("memory.edit", f.Path)
}

func (v *view) do(ctx ext.Ctx, d *eco.Dialog, a action) tea.Cmd {
	switch a {
	case actToggleAuto:
		cmd, err := setAutoMemory(ctx, v.files, !v.info.AutoEnabled)
		if err != "" {
			d.SetStatus(ctx, err, theme.Warning)
			return nil
		}
		state := "off"
		if !v.info.AutoEnabled {
			state = "on"
		}
		eco.Done(ctx, d, "Auto memory turned "+state+".")
		v.info.AutoEnabled = !v.info.AutoEnabled
		v.setInfo(ctx, v.info, v.cwd, v.home)
		return cmd
	case actOpenAutoDir:
		if err := os.MkdirAll(v.info.AutoDir, 0o755); err != nil {
			eco.Fail(ctx, d, err)
			return nil
		}
		return eco.Edit("memory.edit", v.info.AutoDir)
	}
	return nil
}

func (v *view) Render(ctx ext.Ctx, th *theme.Theme, width, height int) []string {
	if !v.loaded {
		return nil
	}
	return v.list.Render(th, width, height)
}

func (v *view) Hints(ctx ext.Ctx) string {
	return eco.Hint("enter", "open", "a", "auto memory on/off", "esc", "close")
}

// StoryInfo is fixed data for the story.
func StoryInfo() discovery.MemoryInfo {
	return discovery.MemoryInfo{
		AutoEnabled: true,
		AutoDir:     "/home/user/.claude/projects/-work-repo/memory",
		Files: []discovery.MemoryFile{
			{Path: "/home/user/.claude/CLAUDE.md", Scope: discovery.ScopeUser, Kind: discovery.MemoryClaudeMD, Exists: true},
			{Path: "/home/user/notes/style.md", Scope: discovery.ScopeUser, Kind: discovery.MemoryImport, Exists: true,
				ImportedFrom: "/home/user/.claude/CLAUDE.md", Depth: 1},
			{Path: "/work/repo/CLAUDE.md", Scope: discovery.ScopeProject, Kind: discovery.MemoryClaudeMD, Exists: true},
			{Path: "/work/repo/.claude/rules/api.md", Scope: discovery.ScopeProject, Kind: discovery.MemoryRule, Exists: true,
				Paths: []string{"src/api/**"}},
			{Path: "/work/repo/CLAUDE.local.md", Scope: discovery.ScopeLocal, Kind: discovery.MemoryLocal},
			{Path: "/home/user/.claude/projects/-work-repo/memory/MEMORY.md", Scope: discovery.ScopeAuto, Kind: discovery.MemoryAuto, Exists: true},
		},
	}
}
