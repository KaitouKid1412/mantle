package app

import (
	"fmt"
	"reflect"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// SpawnFunc starts an engine. It runs inside a Cmd (off the UI goroutine). The
// spawner announces the engine itself with ext.EngineAttachMsg (plan 02's
// engine.Manager does, through program.Send).
type SpawnFunc func(engineID string, o ext.SpawnOpts) error

type spawnFailedMsg struct {
	engineID string
	err      error
}

// startMain spawns the main engine after OnStart hooks: through the startup gates
// when a feature subscribes to ext.SpawnGateMsg, else directly.
func (r *Root) startMain() tea.Cmd {
	if adopt := r.opts.Adopt; adopt != nil {
		log := r.log()
		return func() tea.Msg {
			if err := adopt(); err != nil {
				log.Error("engine: adopt failed; spawning instead", "err", err)
				return adoptFailedMsg{err: err}
			}
			log.Debug("engine: adopted handed-over engines")
			return nil
		}
	}
	return r.spawnMain()
}

type adoptFailedMsg struct{ err error }

// spawnMain spawns the main engine through the gates (or directly).
func (r *Root) spawnMain() tea.Cmd {
	if r.opts.MainSpawn == nil || r.opts.Spawn == nil {
		return nil
	}
	opts := *r.opts.MainSpawn
	proceed := func(o ext.SpawnOpts) tea.Cmd { return r.spawnCmd(ext.MainEngine, o) }
	if len(r.subs[reflect.TypeFor[ext.SpawnGateMsg]()]) == 0 {
		r.log().Debug("main engine: spawning (no startup gates)")
		return proceed(opts)
	}
	r.log().Debug("main engine: startup gates first")
	return ext.Msg(ext.SpawnGateMsg{
		EngineID: ext.MainEngine,
		Opts:     opts,
		Proceed:  proceed,
		Abort: func(reason string) tea.Cmd {
			return ext.Msg(ext.ExitMsg{Code: 0, Reason: reason})
		},
	})
}

func (r *Root) spawnCmd(id string, o ext.SpawnOpts) tea.Cmd {
	spawn, log := r.opts.Spawn, r.log()
	return func() tea.Msg {
		log.Debug("engine: spawn", "id", id, "resume", o.Resume)
		if err := spawn(id, o); err != nil {
			log.Error("engine: spawn failed", "id", id, "err", err)
			return spawnFailedMsg{engineID: id, err: err}
		}
		return nil
	}
}

func (r *Root) spawnFailed(m spawnFailedMsg) tea.Cmd {
	return r.addNotice(ext.Notice{
		Key:     "spawn:" + m.engineID,
		Text:    fmt.Sprintf("Could not start claude (%s): %v", m.engineID, m.err),
		Level:   ext.NoticeError,
		Timeout: -1,
		Source:  "core",
	})
}
