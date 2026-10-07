// Package launch is mantle-ui's side of the launcher protocol: the run file
// ~/.mantle/run/<pid>.json (session id, cwd and the arguments a relaunch uses after
// an exit-75 restart) and the healthy marker that ends a new build's probation.
//
// Primary owner: plan 10 (docs/plans/10-selfmod-launcher.md).
package launch

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/internal/launcher"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// FeatureID is the feature's ID.
const FeatureID = "launch"

// HealthyAfter is how long a new build must stay up (with its first frame
// drawn and the engine initialized) before it leaves probation.
const HealthyAfter = 20 * time.Second

func init() { ext.Register(Feature()) }

// Feature is the launcher-protocol feature with the process's real environment.
func Feature() ext.Feature { return feature(nil) }

func feature(e *env) ext.Feature {
	return ext.Feature{
		ID:     FeatureID,
		Order:  900,
		Parity: []string{"MT-11"},
		Setup: func(r ext.Registrar) error {
			ee := e
			if ee == nil {
				ee = defaultEnv()
			}
			c := &controller{env: ee}
			ext.Subscribe(r, "launch.session", c.onSession)
			ext.Subscribe(r, "launch.control", c.onControlResult)
			ext.Subscribe(r, "launch.firstframe", c.onFirstFrame)
			ext.Subscribe(r, "launch.exit", c.onExit)
			ext.Subscribe(r, "launch.healthytick", c.onHealthyTick)
			r.OnStart("launch.runfile", c.onStart)
			return nil
		},
	}
}

// env is what the controller needs from the process; tests replace it.
type env struct {
	layout launcher.Layout
	getenv func(string) string
	pid    int
	args   []string
	cwd    string
}

func defaultEnv() *env {
	l, err := launcher.DefaultLayout()
	if err != nil {
		l = launcher.Layout{Root: filepath.Join(os.TempDir(), "mantle")}
	}
	cwd, _ := os.Getwd()
	return &env{layout: l, getenv: os.Getenv, pid: os.Getpid(), args: os.Args[1:], cwd: cwd}
}

type controller struct {
	env      *env
	mainInfo ext.SessionInfo // the main engine's latest session info

	enabled bool // started by the launcher
	path    string
	rf      launcher.RunFile

	buildID     string
	needHealthy bool
	firstFrame  bool
	engineReady bool
	alive       bool
	marked      bool
}

type healthyTickMsg struct{}

// onStart writes run/<pid>.json when the launcher started us, and starts
// the probation clock.
func (c *controller) onStart(ctx ext.Ctx) tea.Cmd {
	e := c.env
	lpid, _ := strconv.Atoi(e.getenv(launcher.EnvLauncherPID))
	if lpid == 0 {
		return nil
	}
	c.enabled = true
	c.buildID = e.getenv(launcher.EnvBuildID)
	c.path = e.layout.RunFile(e.pid)
	c.rf = launcher.RunFile{
		PID: e.pid, LauncherPID: lpid, Version: c.buildID,
		Cwd: e.cwd, Argv: e.args, Started: ctx.Clock().Now().UTC(),
	}
	c.writeRunFile(ctx)
	c.needHealthy = launcher.ValidBuildID(c.buildID) && c.buildID != "dev" && !launcher.IsHealthy(e.layout, c.buildID)
	if !c.needHealthy {
		return nil
	}
	return ctx.Clock().Tick(HealthyAfter, func(time.Time) tea.Msg { return healthyTickMsg{} })
}

// handoffArgs are the arguments a relaunch (exit 75, rollback) uses: plan 11's
// Startup.RestartArgs, which keeps the forwarded flags, -w with the worktree name
// given at startup and the UI flags, takes the live model and permission mode, and
// adds --resume. The initial prompt and session selection are never repeated. The
// live title is not passed as --name: it can be the first prompt or a generated
// summary, and a name set with /rename is stored in the session, so --resume brings
// it back. The startup comes from cmd/mantle-ui (cli.Current); without it, it is
// rebuilt from argv. On an error it returns nil and the launcher falls back to
// --resume <session>.
func handoffArgs(argv []string, cwd string, live ext.SessionInfo) []string {
	if live.SessionID == "" {
		return nil
	}
	st, ok := cli.Current()
	if !ok {
		p, err := cli.Parse(argv)
		if err != nil {
			return nil
		}
		if st, err = p.Startup(cwd, nil); err != nil {
			return nil
		}
	}
	return st.RestartArgs(cli.RestartOpts{
		SessionID:      live.SessionID,
		Model:          live.Model,
		PermissionMode: live.PermissionMode,
	})
}

func (c *controller) writeRunFile(ctx ext.Ctx) {
	if !c.enabled {
		return
	}
	live := c.mainInfo
	if live.SessionID == "" {
		live.SessionID = c.rf.SessionID
	}
	c.rf.HandoffArgs = handoffArgs(c.env.args, c.env.cwd, live)
	if err := launcher.WriteRunFile(c.path, c.rf); err != nil {
		ctx.Log().Warn("launch: run file", "err", err)
	}
}

func (c *controller) onSession(ctx ext.Ctx, m ext.SessionChangedMsg) tea.Cmd {
	if m.EngineID != ext.MainEngine && m.EngineID != "" {
		return nil
	}
	changed := m.Info.SessionID != "" && m.Info.SessionID != c.rf.SessionID ||
		m.Info.Model != c.mainInfo.Model || m.Info.PermissionMode != c.mainInfo.PermissionMode || m.Info.Title != c.mainInfo.Title
	c.mainInfo = m.Info
	if c.enabled && changed {
		if m.Info.SessionID != "" {
			c.rf.SessionID = m.Info.SessionID
		}
		c.writeRunFile(ctx)
	}
	return nil
}

func (c *controller) onControlResult(ctx ext.Ctx, m ext.ControlResultMsg) tea.Cmd {
	if (m.EngineID == ext.MainEngine || m.EngineID == "") && m.Subtype == "initialize" && m.Err == nil {
		c.engineReady = true
		c.maybeHealthy(ctx)
	}
	return nil
}

func (c *controller) onFirstFrame(ctx ext.Ctx, _ ext.FirstFrameMsg) tea.Cmd {
	c.firstFrame = true
	c.maybeHealthy(ctx)
	return nil
}

func (c *controller) onHealthyTick(ctx ext.Ctx, _ healthyTickMsg) tea.Cmd {
	c.alive = true
	c.maybeHealthy(ctx)
	return nil
}

// maybeHealthy writes the healthy marker once the first frame is drawn, the
// main engine has initialized and HealthyAfter has passed.
func (c *controller) maybeHealthy(ctx ext.Ctx) {
	if !c.needHealthy || c.marked || !c.firstFrame || !c.engineReady || !c.alive {
		return
	}
	if err := launcher.MarkHealthy(c.env.layout, c.buildID); err != nil {
		ctx.Log().Warn("launch: healthy marker", "err", err)
		return
	}
	c.marked = true
}

// onExit removes the run file on a clean exit, and refreshes it for a
// restart (the launcher reads the session id from it).
func (c *controller) onExit(ctx ext.Ctx, m ext.ExitMsg) tea.Cmd {
	if !c.enabled {
		return nil
	}
	switch m.Code {
	case ext.ExitRestart:
		c.writeRunFile(ctx)
	case 0:
		os.Remove(c.path)
	}
	return nil
}
