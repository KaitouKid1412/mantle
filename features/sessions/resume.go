package sessions

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func (f *feature) registerResume(r ext.Registrar) {
	r.AddCommand(ext.Command{
		ID:          ext.CommandID("resume"),
		Name:        "resume",
		Aliases:     []string{"continue"},
		Description: "Resume a previous conversation",
		ArgHint:     "[session id, name or search]",
		Source:      ext.SourceBuiltin,
		Run:         f.runResume,
	})
	ext.Subscribe(r, "sessions.resolved", f.onResolved)
	ext.Subscribe(r, "sessions.history", f.onHistory)
}

// runResume: no argument opens the picker; an id or unique name resumes it; anything
// else opens the picker filtered by it.
func (f *feature) runResume(ctx ext.Ctx, args string) tea.Cmd {
	args = strings.TrimSpace(args)
	if args == "" {
		return ctx.OpenDialog(DialogResume, PickerArgs{})
	}
	cwd := f.cwd(ctx)
	ix, wt := f.index, f.worktrees
	return func() tea.Msg {
		res, err := ix.Resolve(cwd, args, wt(cwd)...)
		_ = ix.Save()
		return resolvedMsg{args: args, res: res, err: err}
	}
}

type resolvedMsg struct {
	args string
	res  sessions.Resolution
	err  error
}

func (f *feature) onResolved(ctx ext.Ctx, m resolvedMsg) tea.Cmd {
	switch {
	case errors.Is(m.err, sessions.ErrNotFound):
		return notice(ctx, "resume", fmt.Sprintf("No conversation found for %q", m.args), ext.NoticeWarning)
	case m.err != nil:
		return notice(ctx, "resume", "Could not look up conversations: "+m.err.Error(), ext.NoticeError)
	case m.res.Session != nil:
		return f.switchTo(ctx, *m.res.Session, switchOpts{})
	}
	return ctx.OpenDialog(DialogResume, PickerArgs{Query: m.res.Query})
}

// switchOpts are the ways of switching the main engine to a session.
type switchOpts struct {
	fork    bool   // --fork-session: continue in a new session id
	at      string // --resume-session-at: truncate history at this message uuid
	command string // the command shown in the restart guard ("/resume")
	reason  string // notice text on success ("" = none)
}

// switchTo shows meta's history and restarts the main engine on it.
func (f *feature) switchTo(ctx ext.Ctx, meta sessions.SessionMeta, o switchOpts) tea.Cmd {
	if o.command == "" {
		o.command = "/resume"
	}
	if warn, ok := f.restartGuard(ctx, ext.MainEngine, "switch:"+meta.ID, o.command); !ok {
		return warn
	}
	st := f.engine(ext.MainEngine)
	st.loading = true
	return f.loadCmd(loadReq{
		engineID: ext.MainEngine, mode: modeSwitch, id: meta.ID, path: meta.Path,
		cwd: f.cwd(ctx), title: meta.Title(), sw: o,
	})
}

// loadForDisplay prints a session's history without touching the engine (startup and
// fallback loads). A missing transcript is a fresh session: nothing to show.
func (f *feature) loadForDisplay(ctx ext.Ctx, engineID, id, cwd string) tea.Cmd {
	if cwd == "" {
		cwd = f.cwd(ctx)
	}
	f.engine(engineID).loading = true
	return f.loadCmd(loadReq{engineID: engineID, mode: modeDisplay, id: id, cwd: cwd})
}

type loadMode int

const (
	modeDisplay loadMode = iota // print after what is shown (startup)
	modeSwitch                  // replace what is shown, then restart the engine on it
	modeRefresh                 // replace what is shown (return from a hand-off)
)

type loadReq struct {
	engineID string
	mode     loadMode
	id, path string
	cwd      string
	title    string
	sw       switchOpts
}

type historyMsg struct {
	req     loadReq
	items   []*ext.Item
	title   string
	cwd     string // the session's own cwd
	foreign bool   // the session belongs to another project
	err     error
}

// loadCmd reads and normalizes a transcript off the UI goroutine.
func (f *feature) loadCmd(req loadReq) tea.Cmd {
	l, wt := f.layout, f.worktrees
	return func() tea.Msg {
		m := historyMsg{req: req}
		var extra []string
		if req.cwd != "" {
			extra = wt(req.cwd)
		}
		path := req.path
		if path == "" {
			p, err := l.FindSession(req.id, req.cwd, extra...)
			if err != nil {
				m.err = err
				return m
			}
			path = p
		}
		tr, err := sessions.Load(path)
		if err != nil {
			m.err = err
			return m
		}
		m.items = Normalize(tr, NormalizeOptions{
			EngineID:     req.engineID,
			SubagentsDir: sessions.SubagentsDir(filepath.Dir(path), sessions.SessionIDFromPath(path)),
		})
		m.title = tr.Title()
		for _, e := range tr.Entries {
			if e.Cwd != "" && !e.IsSidechain {
				m.cwd = e.Cwd
				break
			}
		}
		if tr.Meta.RelocatedCwd != "" {
			m.cwd = tr.Meta.RelocatedCwd
		}
		if req.mode == modeSwitch && req.cwd != "" {
			m.foreign = !inProject(l, filepath.Dir(path), append([]string{req.cwd}, extra...))
		}
		return m
	}
}

// inProject reports whether dir is a project dir of one of cwds.
func inProject(l sessions.Layout, dir string, cwds []string) bool {
	for _, c := range cwds {
		if slices.Contains(l.ProjectDirs(c), dir) || l.ProjectDir(c) == dir {
			return true
		}
	}
	return false
}

func (f *feature) onHistory(ctx ext.Ctx, m historyMsg) tea.Cmd {
	req := m.req
	st := f.engine(req.engineID)
	st.loading = false
	if m.err != nil {
		if req.mode == modeDisplay && errors.Is(m.err, sessions.ErrNotFound) {
			return nil
		}
		return notice(ctx, "history", "Could not read the conversation: "+m.err.Error(), ext.NoticeError)
	}
	title := m.title
	if req.title != "" {
		title = req.title
	}
	if title != "" {
		f.titles[req.id] = title
	}

	switch req.mode {
	case modeDisplay:
		cmds := []tea.Cmd{ext.Msg(ext.TranscriptHistoryMsg{EngineID: req.engineID, Items: m.items})}
		if title != "" && req.engineID == ext.MainEngine {
			info := ctx.Session()
			if info.SessionID == req.id && info.Title == "" {
				info.Title = title
				cmds = append(cmds, ext.Msg(ext.SessionChangedMsg{EngineID: req.engineID, Info: info}))
			}
		}
		return tea.Sequence(cmds...)

	case modeRefresh:
		st.shown = req.id
		return tea.Sequence(
			ext.Msg(ext.TranscriptHistoryMsg{EngineID: req.engineID, Items: m.items, Reset: true}),
			ctx.Reprint(),
		)
	}

	// modeSwitch
	if m.foreign {
		where := m.cwd
		if where == "" {
			where = "its project directory"
		}
		return notice(ctx, "resume",
			fmt.Sprintf("That conversation belongs to %s. To resume it: cd %s && mantle --resume %s", where, shellQuote(where), req.id),
			ext.NoticeWarning)
	}
	sw := req.sw
	opts := f.spawnOpts(ctx, req.engineID, req.cwd, func(o *ext.SpawnOpts) {
		o.Resume = req.id
		if sw.fork {
			o.ForkSession = true
		}
		if sw.at != "" {
			o.ResumeSessionAt = sw.at
		}
	})
	sw.fork = opts.ForkSession
	info := ctx.Session()
	info.EngineID = req.engineID
	if sw.fork {
		info.SessionID = ""
		st.shown = "fork:" + req.id
	} else {
		info.SessionID = req.id
		st.shown = req.id
		st.session = req.id
	}
	info.Title = title
	if sw.fork {
		info.Title = ""
	}
	cmds := []tea.Cmd{
		ext.Msg(ext.TranscriptHistoryMsg{EngineID: req.engineID, Items: m.items, Reset: true}),
		ctx.Reprint(),
		ext.Msg(ext.SessionChangedMsg{EngineID: req.engineID, Info: info}),
		startEngine(req.engineID, opts),
	}
	if sw.reason != "" {
		cmds = append(cmds, notice(ctx, "switch", sw.reason, ext.NoticeSuccess))
	}
	return tea.Sequence(cmds...)
}

// shellQuote quotes s for a POSIX shell when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+@%:,", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
