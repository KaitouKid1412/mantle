package research

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/config"
	"github.com/KaitouKid1412/mantle/internal/research"
	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/internal/sessions/normalize"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Research mode is a UI mode on top of permission mode default (MT-R1). Entering it
// switches to the fullscreen layout, turns the Research context on and scopes the
// viewport to one node; leaving undoes all three. The tree comes from the session JSONL
// and the UI state (on/off, viewed node, visit memory) from the sidecar, so both
// survive --resume and /resume (MT-R8).

// SettingQuoteOnSelect quotes a finished selection into the prompt automatically.
const SettingQuoteOnSelect = "research.quoteOnSelect"

// sessionEnv is where research finds session files and its sidecars (tests swap it).
type sessionEnv struct {
	layout sessions.Layout
	paths  func() config.Paths // MantleDir holds the sidecars
	getenv func(string) string
	getwd  func() (string, error)
}

func defaultEnv() sessionEnv {
	return sessionEnv{
		layout: sessions.DefaultLayout(),
		paths: func() config.Paths {
			p, _ := config.DefaultPaths("")
			return p
		},
		getenv: os.Getenv,
		getwd:  os.Getwd,
	}
}

// treeLoadedMsg carries a session's tree, rebuilt from its JSONL off the UI goroutine,
// and its sidecar.
type treeLoadedMsg struct {
	sid    string
	tree   *research.Tree
	side   research.Sidecar
	sideOK bool
	err    error
}

func (f *feature) setupMode(r ext.Registrar) {
	r.AddCommand(ext.Command{
		ID: ext.CommandID("research"), Name: "research", Source: ext.SourceBuiltin,
		Description: "Explore the conversation as a tree of questions",
		ArgHint:     "[on|off]",
		Run:         f.command,
		Complete: func(_ ext.Ctx, prefix string) []ext.Completion {
			var out []ext.Completion
			for _, v := range []string{"on", "off"} {
				if strings.HasPrefix(v, prefix) {
					out = append(out, ext.Completion{Value: v})
				}
			}
			return out
		},
	})
	r.AddSetting(ext.SettingSpec{Key: SettingQuoteOnSelect, Type: "bool", Default: true,
		Description: "In research mode, quote selected text into the prompt as soon as the selection is made"})

	ext.Subscribe(r, FeatureID+".uiMode", func(ctx ext.Ctx, m ext.UIModeRequestMsg) tea.Cmd {
		if m.Mode == ext.UIModeResearch {
			return f.enter(ctx)
		}
		return f.leave(ctx, "")
	})
	ext.Subscribe(r, FeatureID+".session", f.onSession)
	ext.Subscribe(r, FeatureID+".treeLoaded", f.onTreeLoaded)
	ext.Subscribe(r, FeatureID+".layout", func(ctx ext.Ctx, m ext.LayoutChangedMsg) tea.Cmd {
		// Research lives in fullscreen: switching away (/tui) ends it, and the layout
		// stays the one the user picked.
		if f.active && m.Mode != ext.Fullscreen {
			f.prevLayout = ext.Fullscreen // nothing to restore
			return f.leave(ctx, "Research mode ended: it needs the fullscreen layout")
		}
		return nil
	})
	ext.Subscribe(r, FeatureID+".engineStart", func(_ ext.Ctx, m ext.EngineStartMsg) tea.Cmd {
		if m.EngineID == ext.MainEngine && m.Opts.ForkSession && m.Opts.Resume != "" {
			f.forkFrom = m.Opts.Resume
		}
		return nil
	})
	r.OnStart(FeatureID+".env", func(ctx ext.Ctx) tea.Cmd {
		if f.env.getenv(ext.EnvResearch) == "1" {
			return f.enter(ctx)
		}
		return nil
	})
}

// command is /research [on|off]; no argument toggles.
func (f *feature) command(ctx ext.Ctx, args string) tea.Cmd {
	switch strings.ToLower(strings.TrimSpace(args)) {
	case "on":
		return f.enter(ctx)
	case "off":
		if !f.active {
			return ctx.Notify(ext.Notice{Key: "research.mode", Text: "Research mode is already off", Source: FeatureID})
		}
		return f.leave(ctx, "")
	case "":
		if f.active {
			return f.leave(ctx, "")
		}
		return f.enter(ctx)
	}
	return ctx.Notify(ext.Notice{Key: "research.mode", Text: "Usage: /research [on|off]", Level: ext.NoticeWarning, Source: FeatureID})
}

// refusal says why research mode cannot start now, or "".
func (f *feature) refusal(ctx ext.Ctx) string {
	// The permission mode is not checked: research runs in default, and plan 05
	// switches the engine there on UIModeChangedMsg.
	if ctx.Accessibility().ScreenReader || config.ReadEnv(f.env.getenv).DisableAltScreen {
		return "Research mode needs the fullscreen layout, which is off (the alternate screen is disabled or screen-reader mode is on)"
	}
	return ""
}

// enter turns research mode on: fullscreen, the Research context, the scope, and the
// tree from the JSONL (merged into what is known live).
func (f *feature) enter(ctx ext.Ctx) tea.Cmd {
	if f.active {
		return nil
	}
	if why := f.refusal(ctx); why != "" {
		return ctx.Notify(ext.Notice{Key: "research.mode", Text: why, Level: ext.NoticeWarning, Source: FeatureID})
	}
	f.syncSession(ctx)
	f.prevLayout = ctx.Layout()
	var layout tea.Cmd
	if f.prevLayout != ext.Fullscreen {
		layout = ext.Msg(ext.LayoutRequestMsg{Mode: ext.Fullscreen})
	}
	if f.tree == nil {
		f.tree = &research.Tree{}
	}
	return tea.Batch(
		layout,
		f.activate(ctx, f.viewing),
		f.loadTree(),
		f.saveSidecar(),
		ext.Msg(ext.UIModeChangedMsg{Mode: ext.UIModeResearch}),
	)
}

// leave turns research mode off and restores the layout it replaced. A non-empty
// notice says why it ended.
func (f *feature) leave(ctx ext.Ctx, notice string) tea.Cmd {
	if !f.active {
		return nil
	}
	cmds := []tea.Cmd{f.deactivate(ctx)}
	f.selection = ""
	if f.prevLayout != ext.Fullscreen && ctx.Layout() == ext.Fullscreen {
		cmds = append(cmds, ext.Msg(ext.LayoutRequestMsg{Mode: f.prevLayout}))
	}
	cmds = append(cmds, f.saveSidecar(), ext.Msg(ext.UIModeChangedMsg{Prev: ext.UIModeResearch}))
	if notice != "" {
		cmds = append(cmds, ctx.Notify(ext.Notice{Key: "research.mode", Text: notice, Source: FeatureID}))
	}
	return tea.Batch(cmds...)
}

// syncSession picks up the main session's id and cwd when they are not known yet.
func (f *feature) syncSession(ctx ext.Ctx) {
	info := ctx.Session()
	if f.sid == "" && info.SessionID != "" {
		f.switchSession(info.SessionID, info.Cwd)
	}
	if f.cwd == "" {
		f.cwd = f.sessionCwd(info.Cwd)
	}
}

func (f *feature) sessionCwd(c string) string {
	if c == "" && f.env.getwd != nil {
		c, _ = f.env.getwd()
	}
	if c == "" {
		return ""
	}
	return sessions.CanonicalPath(c)
}

// onSession follows the main session: a new id reloads the tree and sidecar; a
// permission mode other than default (plan approval, /permissions) ends research.
func (f *feature) onSession(ctx ext.Ctx, m ext.SessionChangedMsg) tea.Cmd {
	if m.EngineID != ext.MainEngine && m.EngineID != "" {
		return nil
	}
	var cmds []tea.Cmd
	// Only a change away from default is external: entered from another mode, research
	// waits for the switch to default first.
	pm := m.Info.PermissionMode
	if f.active && pm != "" && pm != "default" && f.lastMode == "default" {
		cmds = append(cmds, f.leave(ctx, "Research mode ended: the permission mode changed to "+pm))
	}
	if pm != "" {
		f.lastMode = pm
	}
	if sid := m.Info.SessionID; sid != "" && sid != f.sid {
		cmds = append(cmds, f.saveSidecar()) // the old session's state, as it was
		f.switchSession(sid, m.Info.Cwd)
		cmds = append(cmds, f.setTree(ctx, f.tree), f.loadTree())
	}
	return tea.Batch(cmds...)
}

// switchSession forgets the previous session's tree and view state.
func (f *feature) switchSession(sid, cwd string) {
	f.sid = sid
	f.cwd = f.sessionCwd(cwd)
	f.tree = &research.Tree{}
	f.viewing = ""
	clear(f.lastChild)
	f.sideLoaded = false
	f.branching = nil
	f.confirmed = ""
	f.scopes.reset()
	f.load = loaderFor(f.env.layout, sid, f.cwd)
}

// loaderFor normalizes branches of session sid for off-branch nodes.
func loaderFor(l sessions.Layout, sid, cwd string) Loader {
	return func(leaf string) ([]*ext.Item, error) {
		p, err := l.FindSession(sid, cwd)
		if err != nil {
			return nil, err
		}
		tr, err := sessions.Load(p)
		if err != nil {
			return nil, err
		}
		dir := filepath.Dir(p)
		return normalize.Normalize(tr, normalize.Options{
			EngineID:       ext.MainEngine,
			Leaf:           leaf,
			SubagentsDir:   sessions.SubagentsDir(dir, sid),
			ToolResultsDir: sessions.ToolResultsDir(dir, sid),
		}), nil
	}
}

// loadTree rebuilds the session's tree from its JSONL and reads the sidecar (copied
// from the fork's source first, for a fork), off the UI goroutine.
func (f *feature) loadTree() tea.Cmd {
	sid, cwd, l, from := f.sid, f.cwd, f.env.layout, f.forkFrom
	if sid == "" {
		return nil
	}
	paths := f.env.paths
	if from != "" && from != sid {
		f.forkFrom = ""
	} else {
		from = ""
	}
	return func() tea.Msg {
		m := treeLoadedMsg{sid: sid}
		p := paths()
		if from != "" {
			if _, ok := research.LoadSidecar(p, sid); !ok {
				_ = research.CopySidecar(p, from, sid)
			}
		}
		m.side, m.sideOK = research.LoadSidecar(p, sid)
		path, err := l.FindSession(sid, cwd)
		if err != nil {
			m.err = err // not written yet: a new session
			return m
		}
		tr, err := sessions.Load(path)
		if err != nil {
			m.err = err
			return m
		}
		m.tree = research.Build(tr)
		return m
	}
}

// onTreeLoaded merges a rebuilt tree into the live one (the JSONL is authoritative,
// live-only nodes and the live engine leaf are kept) and, the first time for a session,
// applies its sidecar: visit memory, the viewed node, and research mode itself.
func (f *feature) onTreeLoaded(ctx ext.Ctx, m treeLoadedMsg) tea.Cmd {
	if m.sid != f.sid {
		return nil
	}
	if f.tree == nil {
		f.tree = &research.Tree{}
	}
	fresh := m.tree
	if fresh == nil {
		fresh = &research.Tree{}
	}
	if f.busy {
		// The running turn's prompt is in the file already but not its answer.
		if l := f.tree.Leaf(); l != nil && l.State == research.Running {
			if n := fresh.Node(l.ID); n != nil {
				n.State, n.LeafUUID = research.Running, ""
			}
		}
	}
	tree := f.tree.Merge(fresh)
	var cmds []tea.Cmd
	if !f.sideLoaded {
		f.sideLoaded = true
		if m.sideOK {
			for k, v := range m.side.LastChild {
				f.lastChild[k] = v
			}
			if tree.Node(m.side.Viewing) != nil {
				f.viewing = m.side.Viewing
			}
			if m.side.Active && !f.active {
				f.tree = tree
				return f.enter(ctx)
			}
		}
	}
	cmds = append(cmds, f.setTree(ctx, tree))
	if f.active {
		// Show the restored node (setTree keeps the view; navigate records the path).
		if n := f.node(); n != nil {
			cmds = append(cmds, f.navigate(ctx, n.ID))
		}
	}
	return tea.Batch(cmds...)
}

// saveSidecar writes the UI state of the current session, off the UI goroutine. It
// does nothing until the session's sidecar was read.
func (f *feature) saveSidecar() tea.Cmd {
	if f.sid == "" || !f.sideLoaded {
		return nil
	}
	s := research.Sidecar{Active: f.active, Viewing: f.viewing, LastChild: make(map[string]string, len(f.lastChild))}
	for k, v := range f.lastChild {
		s.LastChild[k] = v
	}
	sid, paths := f.sid, f.env.paths
	return func() tea.Msg {
		_ = research.SaveSidecar(paths(), sid, s)
		return nil
	}
}

// engineState follows the main engine's turns for the send plan.
func (f *feature) engineState(ev proto.Event) {
	switch e := ev.(type) {
	case *proto.SessionStateChanged:
		f.busy = e.State != proto.StateIdle
	case *proto.Result:
		f.busy = false
	}
}
