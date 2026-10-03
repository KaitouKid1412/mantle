// Package cli carries the command line into the running UI: it shows the parser's
// warnings, opens the resume picker for -r, and submits the positional prompt as the
// first user message once the main engine is attached (after the startup gates). When
// the installed claude is a version mantle hasn't checked, it compares it with mantle's
// tables in the background and shows one notice (runtime drift).
//
// mantle-ui records the parsed command line with internal/cli.SetCurrent before the
// host starts. Without it (tests, stories) the feature does nothing.
//
// Primary owner: plan 11 (docs/plans/11-cli-drift.md).
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	icli "github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// FeatureID is this feature's ID.
const FeatureID = "cli.startup"

func init() {
	ext.Register(ext.Feature{
		ID:     FeatureID,
		Order:  900, // after the features whose commands it uses (resume)
		Parity: []string{"CLI-03", "CLI-06", "CLI-27"},
		Setup:  setup,
	})
}

func setup(r ext.Registrar) error {
	st, ok := icli.Current()
	if !ok {
		return nil
	}
	f := &startup{st: st}
	r.OnStart(FeatureID, f.start)
	ext.Subscribe(r, FeatureID+".prompt", f.attached)
	r.OnStart(FeatureID+".drift", f.driftCheck)
	ext.Subscribe(r, FeatureID+".drift", f.driftDone)
	return nil
}

// runtimeDrift is icli.RuntimeDrift; tests replace it.
var runtimeDrift = icli.RuntimeDrift

// driftDoneMsg carries a finished runtime drift check.
type driftDoneMsg struct {
	state icli.DriftState
	ran   bool
	err   error
}

// driftCheck compares a newly installed engine with mantle's tables in the background
// (never blocking startup) when its version changed since the last check.
func (f *startup) driftCheck(ext.Ctx) tea.Cmd {
	path := icli.DriftStatePath()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		s, ran, err := runtimeDrift(ctx, path)
		return driftDoneMsg{state: s, ran: ran, err: err}
	}
}

// driftDone shows one notice when the engine adds things mantle doesn't know.
func (f *startup) driftDone(ctx ext.Ctx, m driftDoneMsg) tea.Cmd {
	if m.err != nil {
		ctx.Log().Debug("runtime drift check failed", "err", m.err)
	}
	if len(m.state.Errors) > 0 {
		ctx.Log().Debug("runtime drift check incomplete", "errors", strings.Join(m.state.Errors, "; "))
	}
	if !m.ran {
		return nil
	}
	text := m.state.Notice()
	if text == "" {
		return nil
	}
	return ctx.Notify(ext.Notice{Key: FeatureID + ".drift", Text: text, Level: ext.NoticeInfo, Source: FeatureID})
}

type startup struct {
	st   icli.Startup
	sent bool
}

// start shows the warnings and, for -r without a session, opens the resume picker.
// The picker starts the main engine itself (ext.EngineStartMsg with
// icli.Current().Spawn and Resume set).
func (f *startup) start(ctx ext.Ctx) tea.Cmd {
	var cmds []tea.Cmd
	for i, w := range f.st.Warnings {
		cmds = append(cmds, ctx.Notify(ext.Notice{
			Key: fmt.Sprintf("%s.warning.%d", FeatureID, i), Text: w,
			Level: ext.NoticeWarning, Timeout: -1, Source: FeatureID,
		}))
	}
	if len(f.st.Unknown) > 0 {
		ctx.Log().Debug("forwarding unknown flags to the engine", "flags", strings.Join(f.st.Unknown, " "))
	}
	if f.st.Picker {
		if _, ok := ctx.Command("resume"); ok {
			cmds = append(cmds, ctx.Submit(ext.Draft{Text: strings.TrimSpace("/resume " + f.st.PickerQuery), Mode: "prompt"}))
		} else {
			// No picker in this build: start a new session rather than none.
			cmds = append(cmds,
				ctx.Notify(ext.Notice{Key: FeatureID + ".picker", Text: "The resume picker isn't available; starting a new session",
					Level: ext.NoticeWarning, Source: FeatureID}),
				ext.Msg(ext.EngineStartMsg{EngineID: ext.MainEngine, Opts: f.st.Spawn}))
		}
	}
	return tea.Batch(cmds...)
}

// attached submits the positional prompt once, when the main engine first attaches.
// It goes through the prompt pipeline, as if typed, so `mantle /init` works.
func (f *startup) attached(ctx ext.Ctx, m ext.EngineAttachMsg) tea.Cmd {
	if f.sent || m.EngineID != ext.MainEngine {
		return nil
	}
	f.sent = true
	if strings.TrimSpace(f.st.Prompt) == "" {
		return nil
	}
	return ctx.Submit(ext.Draft{Text: f.st.Prompt, Mode: "prompt"})
}
