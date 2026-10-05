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
		Parity: []string{"CH-05", "CH-06", "CH-07", "CH-14", "CH-15", "CH-16", "ED-16"},
		Setup: func(r ext.Registrar) error {
			r.AddComponent(ext.SlotBelowInput, newFooter(), ext.SlotOpts{Weight: 0, MaxHeight: 1})
			// Fullscreen only: the effort hint right above the prompt.
			r.AddComponent(ext.SlotAboveInput, &effortLine{s: newSessionState()}, ext.SlotOpts{Weight: 1000, MaxHeight: 2})
			ext.Subscribe(r, "chrome.engineNotices", engineNotice)
			addStories(r, footerStories())
			return nil
		},
	})
	ext.Register(ext.Feature{
		ID: "chrome.promptFrame", Order: order,
		Parity: []string{"CH-01", "CH-02", "CH-03", "CH-04", "VW-14"},
		Setup: func(r ext.Registrar) error {
			r.Wrap(EditorID, func(next ext.Component) ext.Component { return wrapFrame(next) })
			r.AddCommand(ext.Command{
				Name: "color", ArgHint: "<color|default>", Source: ext.SourceBuiltin,
				Description: "Set the prompt bar colour for this session",
				Run:         colorCommand, Complete: colorCompletions,
			})
			addStories(r, frameStories())
			return nil
		},
	})
	ext.Register(ext.Feature{
		ID: StatusLineID, Order: order,
		Parity: []string{"CH-18", "CH-19"},
		Setup: func(r ext.Registrar) error {
			// In the footer area, between the editor's menus (-100) and the mode line (0).
			r.AddComponent(ext.SlotBelowInput, newStatusLine(), ext.SlotOpts{Weight: -50})
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
		ID: SubagentsID, Order: order,
		Parity: []string{"CH-11", "CH-12", "CH-13", "CH-14", "CH-17"},
		Setup: func(r ext.Registrar) error {
			tr := newTaskTracker()
			p := newSubagentPanel(tr)
			r.AddComponent(ext.SlotAboveInput, p, ext.SlotOpts{Weight: 20})
			r.AddAction(ext.Action{
				ID: ActFooterSelect, Context: ext.ContextChat,
				Description: "Select the subagent rows below the prompt",
				Run:         p.selectFirst,
			})
			r.AddDialog(SubagentViewID, newSubagentView)
			r.AddDialog(TasksDialogID, newTasksDialogFactory(tr))
			r.AddCommand(ext.Command{
				Name: "tasks", Aliases: []string{"bashes"}, Source: ext.SourceBuiltin,
				Description: "List background tasks and their output",
				Run:         func(ctx ext.Ctx, _ string) tea.Cmd { return ctx.OpenDialog(TasksDialogID, nil) },
			})
			addStories(r, subagentStories())
			return nil
		},
	})
	ext.Register(ext.Feature{
		ID: WelcomeID, Order: order,
		Parity: []string{"CH-08", "CH-09", "CH-20"},
		Setup: func(r ext.Registrar) error {
			w := newWelcome()
			r.OnStart(WelcomeID+".start", w.start)
			ext.Subscribe(r, WelcomeID+".session", func(ctx ext.Ctx, m ext.SessionChangedMsg) tea.Cmd { return w.Update(ctx, m) })
			ext.Subscribe(r, WelcomeID+".control", func(ctx ext.Ctx, m ext.ControlResultMsg) tea.Cmd { return w.Update(ctx, m) })
			ext.Subscribe(r, WelcomeID+".engine", func(ctx ext.Ctx, m ext.EngineEventMsg) tea.Cmd { return w.Update(ctx, m) })
			ext.Subscribe(r, WelcomeID+".cleared", func(ctx ext.Ctx, m ext.ScreenClearedMsg) tea.Cmd { return w.Update(ctx, m) })
			addStories(r, welcomeStories())
			return nil
		},
	})
	ext.Register(ext.Feature{
		ID: ReleaseNotesID, Order: order,
		Parity: []string{"CH-21", "CH-22"},
		Setup: func(r ext.Registrar) error {
			rn := newReleaseNotes(terminal.OS())
			ext.Subscribe(r, ReleaseNotesID+".session", rn.onSession)
			r.AddCommand(ext.Command{
				Name: "release-notes", Source: ext.SourceBuiltin,
				Description: "Show what changed in Claude Code and mantle",
				Run:         rn.command,
			})
			return nil
		},
	})
	ext.Register(ext.Feature{
		ID: TerminalID, Order: order,
		Parity: []string{"CH-23", "CH-24", "CH-25", "CH-26", "CH-29", "CH-30"},
		Setup: func(r ext.Registrar) error {
			tc := newTerminal(terminal.OS())
			r.AddComponent(ext.SlotStatus, tc, ext.SlotOpts{Weight: 1000})
			r.AddInterceptor("chrome.activity", -1000, tc.intercept)
			// app:redraw is the host's (core); chrome adds the clear-screen action.
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
