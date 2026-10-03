package chrome

import (
	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/osc"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// TerminalID is the zero-height component that owns the window title, the OSC 9;4
// progress bar and focus reporting.
const TerminalID = "chrome.terminal"

type terminalComp struct {
	s        sessionState
	progress ProgressTracker

	env             terminal.Env
	titleDisabled   bool // CLAUDE_CODE_DISABLE_TERMINAL_TITLE
	titleFromRename bool // terminalTitleFromRename
	progressOn      bool // terminalProgressBarEnabled and a terminal that renders it
}

func newTerminal(env terminal.Env) *terminalComp {
	return &terminalComp{s: newSessionState(), env: env}
}

func (c *terminalComp) ID() string { return TerminalID }

func (c *terminalComp) Init(ctx ext.Ctx) tea.Cmd {
	c.readSettings(ctx)
	return nil
}

func (c *terminalComp) readSettings(ctx ext.Ctx) {
	s := ctx.Settings()
	c.titleDisabled = TitleDisabled(c.env)
	c.titleFromRename = ext.ClaudeBool(s, "terminalTitleFromRename", true)
	c.progressOn = ext.ClaudeBool(s, "terminalProgressBarEnabled", true) && osc.ProgressSupported(c.env)
}

func (c *terminalComp) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	c.s.observe(msg)
	switch m := msg.(type) {
	case ext.SettingsMsg:
		c.readSettings(ctx)
	case ext.EngineEventMsg:
		if !isMain(m.EngineID) {
			return nil
		}
		switch e := m.Event.(type) {
		case *proto.SessionStateChanged:
			c.progress.OnState(e.State)
		case *proto.Result:
			c.progress.OnResult(e.IsError && !e.Interrupted())
		}
	case ext.EditorStateMsg:
		if !m.Empty {
			c.progress.Clear() // the user moved on from a failed turn
		}
	}
	return nil
}

// View draws nothing; the component only contributes terminal state.
func (c *terminalComp) View(ext.Ctx, ext.Area) ext.Rendered { return ext.Rendered{} }

func (c *terminalComp) TerminalState(ext.Ctx) ext.TerminalState {
	var ts ext.TerminalState
	if !c.titleDisabled {
		ts.WindowTitle = WindowTitle(TitleInput{
			SessionName: c.s.Title,
			AITitle:     c.s.AITitle,
			Cwd:         c.s.Cwd,
			FromRename:  c.titleFromRename,
		})
	}
	if st := c.progress.State(c.progressOn); st != osc.ProgressNone {
		ts.Progress = tea.NewProgressBar(teaProgress(st), 0)
	}
	return ts
}

func teaProgress(s osc.ProgressState) tea.ProgressBarState {
	switch s {
	case osc.ProgressNormal:
		return tea.ProgressBarDefault
	case osc.ProgressError:
		return tea.ProgressBarError
	case osc.ProgressIndeterminate:
		return tea.ProgressBarIndeterminate
	case osc.ProgressPause:
		return tea.ProgressBarWarning
	}
	return tea.ProgressBarNone
}

var _ ext.TerminalStater = (*terminalComp)(nil)
