package selfmod

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/launcher"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// SettingInstantRestart turns on the fd hand-off restart: /mantle restart
// execs the new build under the same pid and hands the running claude over,
// so its background work survives. The new build must adopt the engine
// (plan 01's host with plan 11's --attach-engine-fds and plan 02's
// Manager.AdoptFile), so it is off by default; when the hand-off is not
// possible, restart falls back to exit 75.
const SettingInstantRestart = "selfmod.instantRestart"

// AttachEngineFDsFlag is the mantle-ui flag of an in-place restart: its value
// is the engine hand-off file the new process adopts.
const AttachEngineFDsFlag = "--attach-engine-fds"

// fileHandoff is implemented by plan 02's engine: it stops the engine's
// readers and writer without closing the process's pipes, keeps the fds open
// across exec, and writes the hand-off file (its own format, read back by
// Manager.AdoptFile). It fails when the engine is not idle.
type fileHandoff interface {
	HandoffToFile(path string, argv []string) error
}

// handedOffMsg reports that exec failed after the engine was handed off.
type handedOffMsg struct{ err error }

// instantRestart execs the current build in place when the setting is on and
// the main engine can be handed off. ok is false when it does not apply.
func (c *controller) instantRestart(ctx ext.Ctx) (cmd tea.Cmd, ok bool) {
	if on, _ := ctx.Settings().Mantle(SettingInstantRestart).(bool); !on {
		return nil, false
	}
	he, isHandoff := ctx.Engine(ext.MainEngine).(fileHandoff)
	if !isHandoff {
		return nil, false
	}
	x, handedOff, err := c.prepareHandoff(ctx, he)
	switch {
	case err != nil && handedOff:
		// The engine stopped its goroutines: only a full restart is left.
		ctx.Log().Warn("selfmod: instant restart", "err", err)
		return ext.Msg(ext.ExitMsg{Code: ext.ExitRestart, Reason: "/mantle restart"}), true
	case err != nil:
		ctx.Log().Warn("selfmod: instant restart", "err", err)
		return nil, false
	}
	return tea.Exec(x, func(err error) tea.Msg { return handedOffMsg{err: err} }), true
}

// prepareHandoff hands the main engine off into the hand-off file, records
// the new build in the run file, and returns the exec of the current build.
// handedOff reports whether the engine was already handed off when an error
// occurred.
func (c *controller) prepareHandoff(ctx ext.Ctx, he fileHandoff) (x *execCmd, handedOff bool, err error) {
	v, err := (launcher.Store{L: c.env.layout}).Current()
	if err != nil {
		return nil, false, err
	}
	path := c.handoffFile()
	args := append(c.relaunchArgs(), AttachEngineFDsFlag+"="+path)
	argv := append([]string{v.Binary()}, args...)
	if err := he.HandoffToFile(path, argv); err != nil {
		return nil, false, fmt.Errorf("engine hand-off: %w", err)
	}
	c.run.rf.Version = v.ID
	c.writeRunFile(ctx)
	env := withEnv(os.Environ(), map[string]string{
		launcher.EnvBuildID:   v.ID,
		launcher.EnvProbation: probationFlag(c.env.layout, v.ID),
	})
	return &execCmd{exec: c.env.exec, path: v.Binary(), argv: argv, env: env}, true, nil
}

func (c *controller) handoffFile() string {
	return strings.TrimSuffix(c.env.layout.RunFile(c.env.pid), ".json") + ".handoff.json"
}

// onHandedOff runs only when exec failed: fall back to a full restart.
func (c *controller) onHandedOff(ctx ext.Ctx, m handedOffMsg) tea.Cmd {
	os.Remove(c.handoffFile())
	ctx.Log().Warn("selfmod: instant restart failed; restarting normally", "err", m.err)
	return ext.Msg(ext.ExitMsg{Code: ext.ExitRestart, Reason: "/mantle restart"})
}

func probationFlag(l launcher.Layout, buildID string) string {
	if launcher.IsHealthy(l, buildID) {
		return ""
	}
	return "1"
}

// withEnv returns env with the given keys set ("" removes a key).
func withEnv(env []string, set map[string]string) []string {
	out := make([]string, 0, len(env)+len(set))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := set[k]; !ok {
			out = append(out, kv)
		}
	}
	for k, v := range set {
		if v != "" {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// execCmd is a tea.ExecCommand that replaces the process: Bubble Tea
// releases the terminal first (cooked mode, modes off), so the new build
// starts from a clean terminal. Run returns only if exec failed.
type execCmd struct {
	exec func(path string, argv, env []string) error
	path string
	argv []string
	env  []string
}

func (x *execCmd) Run() error {
	exec := x.exec
	if exec == nil {
		exec = syscall.Exec
	}
	if err := exec(x.path, x.argv, x.env); err != nil {
		return err
	}
	return errors.New("exec returned without replacing the process")
}

func (x *execCmd) SetStdin(io.Reader)  {}
func (x *execCmd) SetStdout(io.Writer) {}
func (x *execCmd) SetStderr(io.Writer) {}
