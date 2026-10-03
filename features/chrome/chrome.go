package chrome

import (
	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// order places chrome after the core features it decorates (wraps are resolved after
// every Setup anyway).
const order = 700

func init() {
	ext.Register(ext.Feature{
		ID: FooterID, Order: order,
		Parity: []string{"CH-05", "CH-06", "CH-07", "CH-14", "ED-16"},
		Setup: func(r ext.Registrar) error {
			r.AddComponent(ext.SlotBelowInput, newFooter(), ext.SlotOpts{Weight: 0, MaxHeight: 1})
			ext.Subscribe(r, "chrome.engineNotices", engineNotice)
			addStories(r, footerStories())
			return nil
		},
	})
	ext.Register(ext.Feature{
		ID: "chrome.promptFrame", Order: order,
		Parity: []string{"CH-01", "CH-03", "CH-04"},
		Setup: func(r ext.Registrar) error {
			r.Wrap(EditorID, func(next ext.Component) ext.Component { return wrapFrame(next) })
			addStories(r, frameStories())
			return nil
		},
	})
	ext.Register(ext.Feature{
		ID: StatusLineID, Order: order,
		Parity: []string{"CH-18", "CH-19"},
		Setup: func(r ext.Registrar) error {
			r.AddComponent(ext.SlotStatusLine, newStatusLine(), ext.SlotOpts{Weight: 0})
			addStories(r, statusLineStories())
			return nil
		},
	})
	ext.Register(ext.Feature{
		ID: TodosID, Order: order,
		Parity: []string{"CH-10"},
		Setup: func(r ext.Registrar) error {
			p := newTodoPanel()
			r.AddComponent(ext.SlotAboveInput, p, ext.SlotOpts{Weight: 10})
			r.AddAction(ext.Action{
				ID: ext.ActAppToggleTodos, Context: ext.ContextGlobal,
				Description: "Show or hide the task checklist",
				Run:         p.toggle,
			})
			addStories(r, todoStories())
			return nil
		},
	})
	ext.Register(ext.Feature{
		ID: TerminalID, Order: order,
		Parity: []string{"CH-23", "CH-24", "CH-30"},
		Setup: func(r ext.Registrar) error {
			r.AddComponent(ext.SlotStatus, newTerminal(terminal.OS()), ext.SlotOpts{Weight: 1000})
			r.AddAction(ext.Action{
				ID: ext.ActAppRedraw, Context: ext.ContextGlobal,
				Description: "Redraw the screen",
				Run:         func(ctx ext.Ctx) (bool, tea.Cmd) { return true, ctx.Reprint() },
			})
			r.AddAction(ext.Action{
				ID: ext.ActChatClearScreen, Context: ext.ContextChat,
				Description: "Clear the screen",
				Run:         func(ext.Ctx) (bool, tea.Cmd) { return true, tea.ClearScreen },
			})
			return nil
		},
	})
}

func addStories(r ext.Registrar, stories []ext.Story) {
	for _, s := range stories {
		r.AddStory(s)
	}
}
