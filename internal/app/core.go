package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ui"
)

// ExitConfirmWindow is how long a second ctrl+c (or ctrl+d) has to follow the first.
const ExitConfirmWindow = 2 * time.Second

// CoreFeatures are the host's own registrations, set up before every built-in. They
// can be overridden like any built-in (Replace("app:interrupt", …)).
func CoreFeatures() []ext.Feature {
	return []ext.Feature{{
		ID:     "core.app",
		Order:  -100,
		Parity: []string{"CORE-01"},
		Setup: func(r ext.Registrar) error {
			r.AddComponent(ext.SlotAboveInput, &noticesComp{}, ext.SlotOpts{Weight: 1000, MaxHeight: 5})
			r.AddAction(ext.Action{
				ID: ext.ActAppInterrupt, Context: ext.ContextGlobal,
				Description: "Interrupt; press twice to exit",
				Run:         func(c ext.Ctx) (bool, tea.Cmd) { return true, confirmExit(c, "ctrl+c") },
			})
			r.AddAction(ext.Action{
				ID: ext.ActAppExit, Context: ext.ContextGlobal,
				Description: "Exit (press twice)",
				Run:         func(c ext.Ctx) (bool, tea.Cmd) { return true, confirmExit(c, "ctrl+d") },
			})
			r.AddAction(ext.Action{
				ID: ext.ActAppRedraw, Context: ext.ContextGlobal,
				Description: "Redraw the screen",
				Run:         func(c ext.Ctx) (bool, tea.Cmd) { return true, c.Reprint() },
			})
			return nil
		},
	}, {
		ID:     "core.widgets",
		Order:  -99,
		Parity: []string{"CORE-02"},
		Setup: func(r ext.Registrar) error {
			for _, s := range ui.Stories() {
				r.AddStory(s)
			}
			return nil
		},
	}}
}

// confirmExit exits on the second press within ExitConfirmWindow, and otherwise shows
// a hint. Features that give ctrl+c a meaning (interrupt a turn, clear the prompt)
// handle the action first through HandleAction or Wrap.
func confirmExit(c ext.Ctx, key string) tea.Cmd {
	uc, ok := c.(*uiCtx)
	if !ok {
		return ext.Msg(ext.ExitMsg{Code: 0, Reason: key})
	}
	r := uc.r
	now := r.opts.Clock.Now()
	if !r.interrupt.IsZero() && now.Sub(r.interrupt) <= ExitConfirmWindow {
		r.interrupt = time.Time{}
		return ext.Msg(ext.ExitMsg{Code: 0, Reason: key})
	}
	r.interrupt = now
	return r.addNotice(ext.Notice{Key: "core.exit", Text: "Press " + key + " again to exit", Level: ext.NoticeInfo, Timeout: ExitConfirmWindow, Source: "core"})
}

// noticesComp reads notices from the root through the ctx.
func rootOf(c ext.Ctx) *Root {
	if uc, ok := c.(*uiCtx); ok {
		return uc.r
	}
	return nil
}
