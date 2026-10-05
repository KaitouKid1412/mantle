package settings

import (
	"maps"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// visibilitySource names plan 08's overlay in ext.CommandVisibilityMsg.
const visibilitySource = "settings.availability"

// visibilityCmd tells the host which of the area's commands to hide, when that changed.
// It runs after initialize, list_models and commands_changed, so a /login that
// restarts the engine re-evaluates it.
func (a *area) visibilityCmd() tea.Cmd {
	h := a.commandAvailability()
	if a.lastVisibility != nil && maps.Equal(h, a.lastVisibility) {
		return nil
	}
	a.lastVisibility = h
	return ext.Msg(ext.CommandVisibilityMsg{Source: visibilitySource, Hidden: h})
}

// commandAvailability says which of the area's commands Claude Code would hide for the
// current account, provider or models, from what the engine reported. A command is
// only hidden on evidence: before initialize nothing is hidden.
//
// From the 2.1.288 binary, two of plan 08's commands vary:
//   - /fast is hidden when fast mode is not available, i.e. no model offers it;
//   - /advisor is hidden when the advisor feature is off, which also removes its
//     headless twin from initialize.commands.
//
// The rest are always available in an interactive session.
func (a *area) commandAvailability() map[string]bool {
	hidden := map[string]bool{"fast": false, "advisor": false}
	if len(a.engine.models) > 0 {
		fast := false
		for _, m := range a.engine.models {
			if m.SupportsFastMode {
				fast = true
			}
		}
		hidden["fast"] = !fast
	}
	if a.engine.commands != nil {
		found := false
		for _, c := range a.engine.commands {
			if c.Name == "advisor" {
				found = true
			}
		}
		hidden["advisor"] = !found
	}
	return hidden
}
