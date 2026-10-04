package selfmod

import (
	"os"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/internal/launcher"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// HealthyAfter is how long a new build must stay up (with its first frame
// drawn and the engine initialized) before it leaves probation.
const HealthyAfter = 20 * time.Second

// runState is mantle-ui's side of the launcher protocol.
type runState struct {
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
	s := &c.run
	s.enabled = true
	s.buildID = e.getenv(launcher.EnvBuildID)
	s.path = e.layout.RunFile(e.pid)
	s.rf = launcher.RunFile{
		PID: e.pid, LauncherPID: lpid, Version: s.buildID,
		Cwd: e.cwd, Argv: e.args, Started: ctx.Clock().Now().UTC(),
	}
	c.writeRunFile(ctx)
	s.needHealthy = launcher.ValidBuildID(s.buildID) && s.buildID != "dev" && !launcher.IsHealthy(e.layout, s.buildID)
	if !s.needHealthy {
		return nil
	}
	return ctx.Clock().Tick(HealthyAfter, func(time.Time) tea.Msg { return healthyTickMsg{} })
}

// handoffArgs are the arguments a relaunch (exit 75, rollback, in-place
// restart) uses: plan 11's Startup.RestartArgs, which keeps the forwarded
// flags, -w with the worktree name given at startup and the UI flags, takes
// the live session name, model and permission mode, and adds --resume. The
// initial prompt and session selection are never repeated. The startup comes
// from cmd/mantle-ui (cli.Current); without it, it is rebuilt from argv. On
// an error it returns nil and the launcher falls back to --resume <session>.
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
		Name:           live.Title,
		Model:          live.Model,
		PermissionMode: live.PermissionMode,
	})
}

// relaunchArgs are handoffArgs for this session as it is now.
func (c *controller) relaunchArgs() []string {
	live := c.mainInfo
	if live.SessionID == "" {
		live.SessionID = c.run.rf.SessionID
	}
	return handoffArgs(c.env.args, c.env.cwd, live)
}

func (c *controller) writeRunFile(ctx ext.Ctx) {
	if !c.run.enabled {
		return
	}
	c.run.rf.HandoffArgs = c.relaunchArgs()
	if err := launcher.WriteRunFile(c.run.path, c.run.rf); err != nil {
		ctx.Log().Warn("selfmod: run file", "err", err)
	}
}

func (c *controller) onSession(ctx ext.Ctx, m ext.SessionChangedMsg) tea.Cmd {
	if m.EngineID != ext.MainEngine && m.EngineID != "" {
		if b := c.byEngine(m.EngineID); b != nil && m.Info.SessionID != "" {
			b.sessionID = m.Info.SessionID
		}
		return nil
	}
	changed := m.Info.SessionID != "" && m.Info.SessionID != c.run.rf.SessionID ||
		m.Info.Model != c.mainInfo.Model || m.Info.PermissionMode != c.mainInfo.PermissionMode || m.Info.Title != c.mainInfo.Title
	c.mainInfo = m.Info
	if c.run.enabled && changed {
		if m.Info.SessionID != "" {
			c.run.rf.SessionID = m.Info.SessionID
		}
		c.writeRunFile(ctx)
	}
	return nil
}

func (c *controller) onControlResult(ctx ext.Ctx, m ext.ControlResultMsg) tea.Cmd {
	if (m.EngineID == ext.MainEngine || m.EngineID == "") && m.Subtype == "initialize" && m.Err == nil {
		c.run.engineReady = true
		c.maybeHealthy(ctx)
	}
	return nil
}

func (c *controller) onFirstFrame(ctx ext.Ctx, _ ext.FirstFrameMsg) tea.Cmd {
	c.run.firstFrame = true
	c.maybeHealthy(ctx)
	return nil
}

func (c *controller) onHealthyTick(ctx ext.Ctx, _ healthyTickMsg) tea.Cmd {
	c.run.alive = true
	c.maybeHealthy(ctx)
	return nil
}

// maybeHealthy writes the healthy marker once the first frame is drawn, the
// main engine has initialized and HealthyAfter has passed.
func (c *controller) maybeHealthy(ctx ext.Ctx) {
	s := &c.run
	if !s.needHealthy || s.marked || !s.firstFrame || !s.engineReady || !s.alive {
		return
	}
	if err := launcher.MarkHealthy(c.env.layout, s.buildID); err != nil {
		ctx.Log().Warn("selfmod: healthy marker", "err", err)
		return
	}
	s.marked = true
}

// onExit removes the run file on a clean exit, and refreshes it for a
// restart (the launcher reads the session id from it).
func (c *controller) onExit(ctx ext.Ctx, m ext.ExitMsg) tea.Cmd {
	for _, b := range c.builds {
		if b.cancel != nil {
			b.cancel()
		}
	}
	if !c.run.enabled {
		return nil
	}
	switch m.Code {
	case ext.ExitRestart:
		c.writeRunFile(ctx)
	case 0:
		os.Remove(c.run.path)
	}
	return nil
}

// restart asks the launcher to relaunch mantle (exit 75) when nothing would
// be lost: no turn, no background task, no build in flight.
func (c *controller) restart(ctx ext.Ctx) tea.Cmd {
	if !c.run.enabled {
		return c.notify(ctx, ext.NoticeWarning, "restart",
			"mantle-ui was not started by the mantle launcher; quit and run `mantle` to use the new build")
	}
	if reason := c.busyReason(); reason != "" {
		return c.notify(ctx, ext.NoticeWarning, "restart",
			"Not restarting now: "+reason+". The new build starts the next time you run mantle.")
	}
	if cmd, ok := c.instantRestart(ctx); ok {
		return cmd
	}
	c.writeRunFile(ctx)
	return ext.Msg(ext.ExitMsg{Code: ext.ExitRestart, Reason: "/mantle restart"})
}
