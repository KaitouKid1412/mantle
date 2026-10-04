package settings

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/settings/model"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

const (
	newerModelNotice = "settings.newer-model"
	// newerModelMaxAsks caps how often one pinned model is offered a successor.
	newerModelMaxAsks = 3
)

// setupNewerModel registers chat:defaultToNewerModel (ctrl+y): when the saved default
// pins an older model and a newer one of the same family is available, a notice
// offers it and the key switches to it and saves it.
func (a *area) setupNewerModel(r ext.Registrar) {
	r.AddAction(ext.Action{
		ID: ext.ActChatDefaultToNewerModel, Context: ext.ContextChat,
		Description: "Make the newer model your default",
		Run: func(c ext.Ctx) (bool, tea.Cmd) {
			if a.newerOffer == nil {
				return false, nil // nothing offered: let the key do whatever else it does
			}
			ch := model.Choice{Row: *a.newerOffer}
			a.newerOffer = nil
			_ = c.Store("settings.model").Delete(askKey(a.newerOfferedFor))
			return true, a.chooseModel(c, ch)
		},
	})
}

func askKey(from string) string { return "newerModelAsks/" + from }

// offerNewerModel shows the offer once per session for the saved model, at most
// newerModelMaxAsks times overall, and only while the action has a key.
func (a *area) offerNewerModel(c ext.Ctx) tea.Cmd {
	saved := ext.ClaudeString(c.Settings(), "model", "")
	if saved == "" || saved == a.newerOfferedFor {
		return nil
	}
	succ, ok := model.Successor(saved, a.modelRows(c))
	if !ok {
		return nil
	}
	keys := c.KeysFor(ext.ContextChat, ext.ActChatDefaultToNewerModel)
	if len(keys) == 0 {
		return nil
	}
	store := c.Store("settings.model")
	var asks int
	if _, err := store.Get(askKey(saved), &asks); err == nil && asks >= newerModelMaxAsks {
		return nil
	}
	_ = store.Set(askKey(saved), asks+1)
	a.newerOfferedFor = saved
	a.newerOffer = &succ
	return c.Notify(ext.Notice{Key: newerModelNotice, Source: "settings",
		Text: fmt.Sprintf("%s is your default model. Press %s to make %s your default.", saved, keys[0], succ.Label)})
}
