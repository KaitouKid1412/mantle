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
	if r.opts.MainSpawn == nil || r.opts.Spawn == nil {
		return nil
	}
	opts := *r.opts.MainSpawn
	proceed := func(o ext.SpawnOpts) tea.Cmd { return r.spawnCmd(ext.MainEngine, o) }
	if len(r.subs[reflect.TypeFor[ext.SpawnGateMsg]()]) == 0 {
		return proceed(opts)
	}
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
	spawn := r.opts.Spawn
	return func() tea.Msg {
		if err := spawn(id, o); err != nil {
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
