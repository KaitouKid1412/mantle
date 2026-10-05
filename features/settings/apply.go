package settings

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/settings/action"
	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// settingsWrittenMsg reports a settings-file write done by a Cmd.
type settingsWrittenMsg struct {
	Scope patch.Scope
	Path  string
	Err   error
}

// noEngineMsg reports effects skipped because the main engine is not running.
type noEngineMsg struct{ What string }

// apply runs effects in order. Control requests and engine commands need the main
// engine; without one they are skipped with a warning while file writes still happen,
// so a choice is never lost.
func (a *area) apply(c ext.Ctx, effects []action.Effect) tea.Cmd {
	var cmds []tea.Cmd
	eng := c.Engine("")
	env := a.env(c)
	write := a.write
	for _, e := range effects {
		switch {
		case e.Control != nil:
			if eng == nil {
				cmds = append(cmds, ext.Msg(noEngineMsg{What: e.Control.Subtype}))
				continue
			}
			cmds = append(cmds, eng.Control(e.Control.Subtype, e.Control.Payload))
		case e.Patch != nil:
			p := *e.Patch
			cmds = append(cmds, func() tea.Msg {
				path, err := write(env, p)
				return settingsWrittenMsg{Scope: p.Scope, Path: path, Err: err}
			})
		case e.Command != "":
			if eng == nil {
				cmds = append(cmds, ext.Msg(noEngineMsg{What: e.Command}))
				continue
			}
			cmds = append(cmds, eng.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text(e.Command)}}))
		case e.Mantle != nil:
			cmds = append(cmds, c.Settings().SetMantle(e.Mantle.Key, e.Mantle.Value))
		case e.Note != "":
			cmds = append(cmds, c.Notify(ext.Notice{Key: "settings.note", Text: e.Note, Source: "settings"}))
		}
	}
	return tea.Sequence(cmds...)
}

// resultLine prints a command's outcome into the transcript under its echo, in the
// transcript's result style ("  ⎿  Help closed"), as Claude Code does when a panel
// closes. Plan 04 prints the "❯ /cmd" echo above it.
func resultLine(c ext.Ctx, text string) tea.Cmd {
	return c.Print(c.Theme().Paint(theme.Inactive, "  ⎿  "+text))
}

// subscribeResults reports failed writes and skipped engine work.
func (a *area) subscribeResults(r ext.Registrar) {
	ext.Subscribe(r, "settings.write-results", func(c ext.Ctx, m settingsWrittenMsg) tea.Cmd {
		if m.Err == nil {
			return nil
		}
		return c.Notify(ext.Notice{
			Key: "settings.write", Level: ext.NoticeError, Source: "settings",
			Text: fmt.Sprintf("Could not save %s settings: %v", strings.ToLower(m.Scope.Label()), m.Err),
		})
	})
	ext.Subscribe(r, "settings.no-engine", func(c ext.Ctx, m noEngineMsg) tea.Cmd {
		return c.Notify(ext.Notice{
			Key: "settings.no-engine", Level: ext.NoticeWarning, Source: "settings",
			Text: "Claude Code is not running, so the change applies from the next session.",
		})
	})
}

// ownControls are the requests the settings panels send; their failures are reported
// here (other features report their own).
var ownControls = map[string]bool{
	proto.SubSetModel: true, proto.SubApplyFlagSettings: true, proto.SubUpdateSettings: true,
	proto.SubSetMaxThinkingTokens: true, proto.SubReloadOutputStyles: true,
}

func (a *area) controlFailed(c ext.Ctx, m ext.ControlResultMsg) tea.Cmd {
	if !ownControls[m.Subtype] {
		return nil
	}
	return c.Notify(ext.Notice{
		Key: "settings.control." + m.Subtype, Level: ext.NoticeError, Source: "settings",
		Text: fmt.Sprintf("Claude Code rejected %s: %v", m.Subtype, m.Err),
	})
}
