package selfmod

import (
	"os"
	"slices"
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

// handoffArgs are the arguments a relaunch (exit 75, rollback) uses: the
// original flags that reach the engine and the UI flags, without the initial
// prompt or session selection, plus --resume <session>. -w stays, with the
// worktree name chosen at startup, so the session resumes in its worktree.
// On a parse error it returns nil and the launcher falls back to
// --resume <session>.
//
// TODO(10): use cli.Startup.RestartArgs (plan 11) once it is integrated.
func handoffArgs(argv []string, sessionID string) []string {
	if sessionID == "" {
		return nil
	}
	p, err := cli.Parse(argv)
	if err != nil {
		return nil
	}
	args := slices.Clone(p.EngineArgs)
	st, haveStartup := cli.Current()
	for _, o := range p.Flags {
		if o.Name != "-w" && o.Name != "--worktree" {
			continue
		}
		name := o.Value()
		if haveStartup && st.Worktree != "" {
			name = st.Worktree
		}
		for i := 0; i+len(o.Tokens) <= len(args); i++ {
			if slices.Equal(args[i:i+len(o.Tokens)], o.Tokens) {
				repl := []string{"--worktree"}
				if name != "" {
					repl = append(repl, name)
				}
				args = slices.Replace(args, i, i+len(o.Tokens), repl...)
				break
			}
		}
	}
	if p.Mantle.Name != "" {
		args = append(args, "--name", p.Mantle.Name)
	}
	if p.Mantle.ScreenReader {
		args = append(args, "--ax-screen-reader")
	}
	if ps := p.Mantle.PromptSuggestions; ps != nil {
		args = append(args, "--prompt-suggestions="+strconv.FormatBool(*ps))
	}
	return append(args, "--resume", sessionID)
}

func (c *controller) writeRunFile(ctx ext.Ctx) {
	if !c.run.enabled {
		return
	}
	c.run.rf.HandoffArgs = handoffArgs(c.env.args, c.run.rf.SessionID)
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
	if c.run.enabled && m.Info.SessionID != "" && m.Info.SessionID != c.run.rf.SessionID {
		c.run.rf.SessionID = m.Info.SessionID
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
