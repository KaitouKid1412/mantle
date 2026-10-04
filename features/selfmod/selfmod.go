// Package selfmod registers /mantle: a builder engine edits mantle's own
// source in a private worktree, the deterministic pipeline in
// internal/selfmod vets it, and the result is installed as a new version
// that the launcher runs next time (or right away with /mantle restart).
//
// It also implements mantle-ui's side of the launcher protocol: the run
// file ~/.mantle/run/<pid>.json and the healthy marker that ends a new
// build's probation.
//
// Primary owner: plan 10 (docs/plans/10-selfmod-launcher.md).
package selfmod

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/launcher"
	sm "github.com/KaitouKid1412/mantle/internal/selfmod"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// FeatureID is the feature's ID.
const FeatureID = "selfmod"

// Setting keys (mantle settings, ~/.mantle/settings.json).
const (
	SettingConfirm   = "selfmod.confirm"
	SettingMaxBudget = "selfmod.maxBudgetUsd"
	SettingModel     = "selfmod.model"
	SettingSource    = "selfmod.source"
)

// selfmod.confirm values.
const (
	ConfirmAlways         = "always"
	ConfirmOnVisualChange = "on-visual-change"
	ConfirmNever          = "never"
)

func init() { ext.Register(Feature()) }

// Feature is the /mantle feature with the process's real environment.
func Feature() ext.Feature { return feature(nil) }

func feature(e *env) ext.Feature {
	return ext.Feature{
		ID:    FeatureID,
		Order: 900,
		Parity: []string{
			"MT-11", "MT-16", "MT-17", "MT-19", "MT-20", "MT-21", "MT-22", "MT-23", "MT-24",
			"MT-25", "MT-26", "MT-27", "MT-28", "MT-29", "MT-30", "MT-35",
		},
		Setup: func(r ext.Registrar) error {
			ee := e
			if ee == nil {
				ee = defaultEnv()
			}
			return newController(ee).setup(r)
		},
	}
}

// env is what the controller needs from the process and the machine; tests
// replace it.
type env struct {
	layout launcher.Layout
	getenv func(string) string
	pid    int
	args   []string
	cwd    string
	// workspace builds the workspace for a selfmod.source setting.
	workspace func(source string) *sm.Workspace
	// claudeSettingsPath is ~/.claude/settings.json (config proposals).
	claudeSettingsPath string
	// stories renders story diffs between two mantle-ui binaries (preview).
	stories func(ctx context.Context, before, after string) ([]StoryDiff, error)
	// exec replaces the process (instant restart); nil means syscall.Exec.
	exec func(path string, argv, env []string) error
}

func defaultEnv() *env {
	l, err := launcher.DefaultLayout()
	if err != nil {
		l = launcher.Layout{Root: filepath.Join(os.TempDir(), "mantle")}
	}
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	e := &env{
		layout:             l,
		getenv:             os.Getenv,
		pid:                os.Getpid(),
		args:               os.Args[1:],
		cwd:                cwd,
		claudeSettingsPath: filepath.Join(home, ".claude", "settings.json"),
		stories:            DiffStories,
	}
	e.workspace = func(source string) *sm.Workspace { return standardWorkspace(e.layout, source) }
	return e
}

// standardWorkspace returns the workspace for a selfmod.source value: ""
// means ~/.mantle/src; a path means dev mode on that repository.
func standardWorkspace(l launcher.Layout, source string) *sm.Workspace {
	var ws *sm.Workspace
	if source == "" {
		ws = sm.NewWorkspace(l)
	} else {
		ws = sm.NewDevWorkspace(l, expandHome(source), "main")
	}
	ws.Pipeline.Smoke = sm.DefaultSmokeConfig()
	return ws
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

// controller holds /mantle's state. It lives on the UI goroutine; blocking
// work (git, the pipeline, builds) runs in Cmds that report back with
// messages.
type controller struct {
	env    *env
	builds map[string]*build
	order  []string // request ids, oldest first

	// Main engine activity, for "restart now".
	mainState string
	bgTasks   int

	run runState

	ticking  bool
	sawState bool
	mainInfo ext.SessionInfo // the main engine's latest session info
}

func newController(e *env) *controller {
	return &controller{env: e, builds: map[string]*build{}}
}

func (c *controller) setup(r ext.Registrar) error {
	r.AddSetting(ext.SettingSpec{Key: SettingConfirm, Type: "enum", Default: ConfirmOnVisualChange,
		Options:     []string{ConfirmAlways, ConfirmOnVisualChange, ConfirmNever},
		Description: "When /mantle asks before installing a build: always, only when a story's look changes, or never"})
	r.AddSetting(ext.SettingSpec{Key: SettingMaxBudget, Type: "json", Default: sm.DefaultMaxBudgetUSD,
		Description: "Spending cap in USD for one /mantle builder session"})
	r.AddSetting(ext.SettingSpec{Key: SettingModel, Type: "string", Default: "",
		Description: "Model for the /mantle builder (empty: the session's model)"})
	r.AddSetting(ext.SettingSpec{Key: SettingSource, Type: "string", Default: "",
		Description: "Repository /mantle builds from (empty: ~/.mantle/src; a path enables dev mode)"})
	r.AddSetting(ext.SettingSpec{Key: SettingInstantRestart, Type: "bool", Default: false,
		Description: "/mantle restart hands the running claude to the new build instead of restarting it (needs a build with engine hand-off)"})

	r.AddCommand(ext.Command{
		Name:        "mantle",
		Description: "Change mantle itself: /mantle <request>, or list, show, undo, rollback, retry, status, restart",
		ArgHint:     "<request> | list | show <id> | undo <id> | rollback | retry [id] | status | restart",
		Source:      ext.SourceBuiltin,
		Run:         c.runCommand,
		Complete:    c.complete,
	})
	r.AddRenderer(KeyBuild, RenderBuild)
	r.AddComponent(ext.SlotAboveInput, &progressComp{c: c}, ext.SlotOpts{Weight: 400, MaxHeight: 6})
	r.AddDialog(PreviewDialogID, c.newPreviewDialog)
	addStories(r)

	ext.Subscribe(r, "selfmod.attach", c.onAttach)
	ext.Subscribe(r, "selfmod.event", c.onEvent)
	ext.Subscribe(r, "selfmod.exited", c.onExited)
	ext.Subscribe(r, "selfmod.session", c.onSession)
	ext.Subscribe(r, "selfmod.control", c.onControlResult)
	ext.Subscribe(r, "selfmod.firstframe", c.onFirstFrame)
	ext.Subscribe(r, "selfmod.exit", c.onExit)
	ext.Subscribe(r, "selfmod.dialog", c.onDialogClosed)
	ext.Subscribe(r, "selfmod.started", c.onStarted)
	ext.Subscribe(r, "selfmod.vetted", c.onVetted)
	ext.Subscribe(r, "selfmod.previewed", c.onPreviewed)
	ext.Subscribe(r, "selfmod.promoted", c.onPromoted)
	ext.Subscribe(r, "selfmod.continued", c.onContinued)
	ext.Subscribe(r, "selfmod.update", c.onUpdateStep)
	ext.Subscribe(r, "selfmod.print", c.onPrint)
	ext.Subscribe(r, "selfmod.tick", c.onTick)
	ext.Subscribe(r, "selfmod.healthytick", c.onHealthyTick)
	ext.Subscribe(r, "selfmod.handedoff", c.onHandedOff)
	r.OnStart("selfmod.runfile", c.onStart)
	return nil
}

// workspace returns the workspace selected by the selfmod.source setting.
func (c *controller) workspace(ctx ext.Ctx) *sm.Workspace {
	source, _ := ctx.Settings().Mantle(SettingSource).(string)
	return c.env.workspace(strings.TrimSpace(source))
}

func (c *controller) activeBuilds() []*build {
	var out []*build
	for _, id := range c.order {
		if b := c.builds[id]; b != nil && activePhase(b.phase) {
			out = append(out, b)
		}
	}
	return out
}

func (c *controller) byEngine(engineID string) *build {
	id, ok := strings.CutPrefix(engineID, sm.BuilderEnginePrefix)
	if !ok {
		return nil
	}
	return c.builds[id]
}

func (c *controller) add(b *build) {
	if _, ok := c.builds[b.req.ID]; !ok {
		c.order = append(c.order, b.req.ID)
	}
	c.builds[b.req.ID] = b
}

// changed re-renders the progress component and keeps its clock ticking
// while builds run.
func (c *controller) changed(ctx ext.Ctx) tea.Cmd {
	ctx.Invalidate(ComponentID)
	return c.tick(ctx)
}

type tickMsg struct{}

func (c *controller) tick(ctx ext.Ctx) tea.Cmd {
	if len(c.activeBuilds()) == 0 || c.ticking {
		return nil
	}
	c.ticking = true
	return ctx.Clock().Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (c *controller) onTick(ctx ext.Ctx, _ tickMsg) tea.Cmd {
	c.ticking = false
	ctx.Invalidate(ComponentID)
	return c.tick(ctx)
}

// printMsg carries command output from a Cmd to the UI goroutine.
type printMsg struct {
	text   string
	notice *ext.Notice
}

func (c *controller) onPrint(ctx ext.Ctx, m printMsg) tea.Cmd {
	var cmds []tea.Cmd
	if m.text != "" {
		cmds = append(cmds, ctx.Print(m.text))
	}
	if m.notice != nil {
		cmds = append(cmds, ctx.Notify(*m.notice))
	}
	return tea.Batch(cmds...)
}

func notice(level ext.NoticeLevel, key, text string) *ext.Notice {
	return &ext.Notice{Key: "selfmod." + key, Text: text, Level: level, Source: FeatureID}
}

func (c *controller) notify(ctx ext.Ctx, level ext.NoticeLevel, key, text string) tea.Cmd {
	return ctx.Notify(*notice(level, key, text))
}

// Main engine activity: idle means no turn and no background work.

func (c *controller) trackMain(ev proto.Event) {
	switch e := ev.(type) {
	case *proto.SessionStateChanged:
		c.mainState, c.sawState = e.State, true
	case *proto.BackgroundTasksChanged:
		n := 0
		for _, t := range e.Tasks {
			if !t.Ambient {
				n++
			}
		}
		c.bgTasks = n
	case *proto.Result:
		// Without session_state_changed events, a result ends the turn.
		if !c.sawState {
			c.mainState = proto.StateIdle
		}
	case *proto.Assistant:
		if !c.sawState {
			c.mainState = proto.StateRunning
		}
	}
}

// busyReason explains why restarting now would lose work ("" when idle).
func (c *controller) busyReason() string {
	switch {
	case c.mainState == proto.StateRunning || c.mainState == proto.StateRequiresAction:
		return "a turn is running"
	case c.bgTasks > 0:
		return "background tasks are running"
	case slices.ContainsFunc(c.activeBuilds(), func(b *build) bool { return b.phase != PhaseReady }):
		return "a /mantle build is running"
	}
	return ""
}
