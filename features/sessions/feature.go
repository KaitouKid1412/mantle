package sessions

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// FeatureID is this feature's registration ID.
const FeatureID = "sessions"

func init() {
	ext.Register(ext.Feature{
		ID:    FeatureID,
		Order: 300,
		Parity: []string{
			"SE-06", "SE-07", "SE-08", "SE-09", "SE-10", "SE-11", "SE-12", "SE-13", "SE-14",
			"SE-15", "SE-16", "SE-40", "SE-41", "SE-43",
		},
		Setup: func(r ext.Registrar) error {
			return newFeature(sessions.DefaultLayout(), sessions.DefaultCachePath()).setup(r)
		},
	})
}

// feature holds the session state. Its methods run on the UI goroutine; file reads
// and engine waits happen in Cmds.
type feature struct {
	layout sessions.Layout
	index  *sessions.Index

	engines map[string]*engineState
	titles  map[string]string // session id -> title to show for it
	cleared []string          // sessions left behind by /clear, oldest first
	ho      *handoff          // the hand-off in progress, if any
	goal    *goal             // the main session's active /goal
	away    awayState
	// branching is a /branch waiting for its fork's session id.
	branching *branching
	seq       int // for IDs of items this feature adds
	// compactOnAttach sends /compact when the main engine next attaches.
	compactOnAttach bool
	// /btw: the last exchanges, the one shown, and whether the overlay is open.
	btw     []*btwExchange
	btwSel  int
	btwSeq  int
	btwOpen bool

	// startupSpawn returns the main engine's options as parsed from the command line
	// (plan 11's cli.Current().Spawn).
	startupSpawn func() (ext.SpawnOpts, bool)

	// Seams for tests.
	now        func() time.Time
	worktrees  func(cwd string) []string
	claudePath func() (string, error)
	getwd      func() (string, error)
	// runBackground runs a claude command that returns at once (/fork).
	runBackground func(bin, cwd string, argv []string) (string, error)
	// runSide runs the one-shot claude -p that answers /btw when the engine can't.
	runSide func(bin, cwd string, argv []string) (string, error)
}

// engineState is what the feature tracks per engine.
type engineState struct {
	shown   string // session whose history is in the transcript store
	loading bool   // a history load is in flight
	state   string // proto.StateIdle | StateRunning | StateRequiresAction ("" = unknown)
	tasks   int    // running background tasks (not ambient ones)
	session string // last session id seen
	confirm map[string]time.Time
	usage   *usage
}

func newFeature(l sessions.Layout, cachePath string) *feature {
	return &feature{
		layout:  l,
		index:   sessions.NewIndex(l, cachePath),
		engines: map[string]*engineState{},
		titles:  map[string]string{},
		startupSpawn: func() (ext.SpawnOpts, bool) {
			st, ok := cli.Current()
			return st.Spawn, ok
		},
		now:           time.Now,
		worktrees:     gitWorktrees,
		claudePath:    findClaude,
		getwd:         os.Getwd,
		runBackground: runBackgroundClaude,
		runSide:       runClaudeText,
	}
}

func (f *feature) engine(id string) *engineState {
	if id == "" {
		id = ext.MainEngine
	}
	s := f.engines[id]
	if s == nil {
		s = &engineState{confirm: map[string]time.Time{}}
		f.engines[id] = s
	}
	return s
}

func (f *feature) setup(r ext.Registrar) error {
	f.registerResume(r)
	f.registerClear(r)
	f.registerHandoff(r)
	f.registerPicker(r)
	f.registerPassthrough(r)
	f.registerRename(r)
	f.registerBranch(r)
	f.registerExport(r)
	f.registerContext(r)
	f.registerUsage(r)
	f.registerRewind(r)
	f.registerDiff(r)
	f.registerSummary(r)
	f.registerBtw(r)
	ext.Subscribe(r, "sessions.plan-file", f.onPlanFile)
	ext.Subscribe(r, "sessions.notify", f.onNotify)
	ext.Subscribe(r, "sessions.cwd-changed", f.onCwdChanged)

	ext.Subscribe(r, "sessions.engine-events", f.onEngineEvent)
	ext.Subscribe(r, "sessions.session-changed", f.onSessionChanged)
	ext.Subscribe(r, "sessions.context-usage", f.onContextUsage)
	r.AddComponent(ext.SlotBelowInput, &contextWarning{f: f}, ext.SlotOpts{Weight: 900, MaxHeight: 1})
	r.OnStart("sessions.startup-history", f.startupHistory)
	return nil
}

// onEngineEvent tracks engine state and reacts to session-level events.
func (f *feature) onEngineEvent(ctx ext.Ctx, m ext.EngineEventMsg) tea.Cmd {
	measure := f.observeUsage(ctx, m.EngineID, m.Event)
	return tea.Batch(measure, f.onSessionEvent(ctx, m))
}

func (f *feature) onSessionEvent(ctx ext.Ctx, m ext.EngineEventMsg) tea.Cmd {
	st := f.engine(m.EngineID)
	switch e := m.Event.(type) {
	case *proto.SessionStateChanged:
		st.state = e.State
		if e.State == proto.StateIdle {
			return f.onIdle(ctx, m.EngineID)
		}
	case *proto.BackgroundTasksChanged:
		st.tasks = 0
		for _, t := range e.Tasks {
			if !t.Ambient {
				st.tasks++
			}
		}
	case *proto.Result:
		if st.state != proto.StateRequiresAction {
			st.state = proto.StateIdle
		}
		return f.onIdle(ctx, m.EngineID)
	case *proto.ConversationReset:
		return f.onConversationReset(ctx, m.EngineID, e)
	case *proto.SessionTitleChanged:
		if sid := ctx.Session().SessionID; sid != "" && e.Title != "" {
			f.titles[sid] = e.Title
		}
	case *proto.ActiveGoal:
		if m.EngineID == ext.MainEngine {
			f.observeGoal(ctx, e)
		}
	}
	return nil
}

// onSessionChanged keeps a known title on the session info and loads history when an
// engine reports a session whose history is not shown yet (fallback for hosts that
// learn the resumed id only from the engine).
func (f *feature) onSessionChanged(ctx ext.Ctx, m ext.SessionChangedMsg) tea.Cmd {
	st := f.engine(m.EngineID)
	sid := m.Info.SessionID
	if sid == "" {
		return nil
	}
	st.session = sid
	var cmds []tea.Cmd
	if m.EngineID == ext.MainEngine {
		cmds = append(cmds, f.onBranchSession(ctx, sid))
	}
	if t := f.titles[sid]; t != "" && m.Info.Title == "" {
		info := m.Info
		info.Title = t
		cmds = append(cmds, ext.Msg(ext.SessionChangedMsg{EngineID: m.EngineID, Info: info}))
	}
	if m.EngineID == ext.MainEngine && st.shown == "" && !st.loading && storeEmpty(ctx) {
		st.shown = sid
		cmds = append(cmds, f.loadForDisplay(ctx, m.EngineID, sid, m.Info.Cwd))
	}
	return tea.Batch(cmds...)
}

func storeEmpty(ctx ext.Ctx) bool {
	t := ctx.Transcript()
	return t == nil || len(t.Items()) == 0
}

// startupHistory prints the history of the session the host resumes at startup.
func (f *feature) startupHistory(ctx ext.Ctx) tea.Cmd {
	info := ctx.Session()
	if info.SessionID == "" {
		return nil
	}
	st := f.engine(ext.MainEngine)
	st.shown, st.session = info.SessionID, info.SessionID
	return f.loadForDisplay(ctx, ext.MainEngine, info.SessionID, info.Cwd)
}

// cwd is the main session's working directory (canonical).
func (f *feature) cwd(ctx ext.Ctx) string {
	c := ctx.Session().Cwd
	if c == "" {
		c, _ = f.getwd()
	}
	if c == "" {
		return ""
	}
	return sessions.CanonicalPath(c)
}

// notice shows a short message from this feature.
func notice(ctx ext.Ctx, key, text string, level ext.NoticeLevel) tea.Cmd {
	return ctx.Notify(ext.Notice{Key: "sessions." + key, Text: text, Level: level, Source: FeatureID})
}

// optioner is implemented by engines that expose their launch options (plan 02's).
type optioner interface{ Options() ext.SpawnOpts }

// spawnOpts returns launch options for (re)starting an engine on another session:
//   - a running engine's own options with the session fields cleared, so the user's
//     flags and gate settings carry over;
//   - for a main engine that never started (`mantle -r` opened the picker), the options
//     parsed from the command line, session flags included (--fork-session,
//     --session-id);
//   - otherwise just the cwd.
//
// set then picks the session.
func (f *feature) spawnOpts(ctx ext.Ctx, engineID, cwd string, set func(*ext.SpawnOpts)) ext.SpawnOpts {
	if e := ctx.Engine(engineID); e != nil {
		if op, ok := e.(optioner); ok {
			return withSession(op.Options(), cwd, set)
		}
	}
	if engineID == ext.MainEngine && f.startupSpawn != nil {
		if o, ok := f.startupSpawn(); ok {
			o.Continue, o.Stop = false, false
			if o.Cwd == "" {
				o.Cwd = cwd
			}
			if set != nil {
				set(&o)
			}
			return o
		}
	}
	return withSession(ext.SpawnOpts{}, cwd, set)
}

func withSession(o ext.SpawnOpts, cwd string, set func(*ext.SpawnOpts)) ext.SpawnOpts {
	if o.Cwd == "" {
		o.Cwd = cwd
	}
	o.Resume, o.Continue, o.ForkSession = "", false, false
	o.ResumeSessionAt, o.ResumeDropsTurn, o.SessionID = "", false, ""
	o.Name, o.Stop = "", false
	o.ExtraArgs = stripSessionArgs(o.ExtraArgs)
	if set != nil {
		set(&o)
	}
	return o
}

// stripSessionArgs drops session-selecting flags from forwarded args (they would fight
// the restart's own).
func stripSessionArgs(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, _, hasValue := strings.Cut(a, "=")
		switch name {
		case "-c", "--continue", "--fork-session":
			continue
		case "-r", "--resume", "--session-id", "--resume-session-at", "--resume-drops-turn", "-n", "--name":
			if !hasValue && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

// startEngine (re)starts an engine through the host.
func startEngine(engineID string, o ext.SpawnOpts) tea.Cmd {
	return ext.Msg(ext.EngineStartMsg{EngineID: engineID, Opts: o})
}

// confirmRepeat implements "run it again to confirm": the first call within window
// returns false and remembers the time, the second returns true.
func (f *feature) confirmRepeat(engineID, key string, window time.Duration) bool {
	st := f.engine(engineID)
	now := f.now()
	if t, ok := st.confirm[key]; ok && now.Sub(t) <= window {
		delete(st.confirm, key)
		return true
	}
	st.confirm[key] = now
	return false
}

// restartGuard returns a warning when restarting the engine would lose work, and
// whether the caller may go ahead. Running background tasks need a repeat to confirm.
func (f *feature) restartGuard(ctx ext.Ctx, engineID, key, what string) (tea.Cmd, bool) {
	st := f.engine(engineID)
	if st.tasks == 0 {
		return nil, true
	}
	if f.confirmRepeat(engineID, key, 30*time.Second) {
		return nil, true
	}
	text := plural(st.tasks, "background task is", "background tasks are") +
		" running and will stop. Run " + what + " again to continue."
	return notice(ctx, "restart-guard", text, ext.NoticeWarning), false
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

func findClaude() (string, error) {
	if p := os.Getenv("MANTLE_CLAUDE_BIN"); p != "" {
		return p, nil
	}
	return exec.LookPath("claude")
}

// runGit runs a read-only git command in dir with hooks and fsmonitor off and a
// timeout, and returns its stdout.
func runGit(dir string, args ...string) ([]byte, error) {
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	full := append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor="}, args...)
	cmd := exec.CommandContext(c, "git", full...)
	cmd.Dir = dir
	return cmd.Output()
}

// gitWorktrees lists the worktrees of the repository at cwd (including cwd's own).
func gitWorktrees(cwd string) []string {
	if cwd == "" {
		return nil
	}
	out, err := runGit(cwd, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	var dirs []string
	for _, line := range strings.Split(string(out), "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			dirs = append(dirs, sessions.CanonicalPath(p))
		}
	}
	return dirs
}
