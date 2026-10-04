package sessions

import (
	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// In fullscreen, /diff is a right sidebar rather than a dialog (Claude Code shows it as
// a side panel from 110 columns, and opens it on its own from 144 once the session
// has changes). The panel draws the same viewer as dialog.diff; the host places it
// (plan 12) and narrows the transcript.

const (
	DiffPanelID = "sessions.diffPanel"

	diffPanelMinWidth  = 110
	diffPanelAutoWidth = 144
)

type diffPanel struct {
	f         *feature
	open      bool
	dismissed bool // closed by the user: don't open on its own again
	prevFocus string
	v         *diffViewer
}

func (f *feature) registerDiffPanel(r ext.Registrar) {
	f.diffPanel = &diffPanel{f: f}
	r.AddComponent(ext.SlotSidebarR, f.diffPanel, ext.SlotOpts{Modes: []ext.LayoutMode{ext.Fullscreen}})
}

// runDiff opens the panel in a wide fullscreen layout, the dialog otherwise.
func (f *feature) runDiff(ctx ext.Ctx, args string) tea.Cmd {
	if w, _ := ctx.Size(); ctx.Layout() == ext.Fullscreen && w >= diffPanelMinWidth && f.diffPanel != nil {
		return f.diffPanel.show(ctx, true)
	}
	return ctx.OpenDialog(DialogDiff, nil)
}

// show opens (or refreshes) the panel, focusing it when asked.
func (p *diffPanel) show(ctx ext.Ctx, focus bool) tea.Cmd {
	var cmds []tea.Cmd
	if !p.open {
		p.v = newDiffViewer(p.f, ctx)
		p.v.id = DiffPanelID
		p.open = true
		cmds = append(cmds, p.v.loadGit())
	}
	p.dismissed = false
	if focus && ctx.Focused() != DiffPanelID {
		p.prevFocus = ctx.Focused()
		cmds = append(cmds, ctx.Focus(DiffPanelID))
	}
	ctx.Invalidate(DiffPanelID)
	return tea.Batch(cmds...)
}

func (p *diffPanel) close(ctx ext.Ctx) tea.Cmd {
	p.open, p.dismissed = false, true
	ctx.Invalidate(DiffPanelID)
	if ctx.Focused() == DiffPanelID && p.prevFocus != "" {
		return ctx.Focus(p.prevFocus)
	}
	return nil
}

// afterTurn refreshes an open panel, or opens it on its own in a very wide
// fullscreen layout once a turn has edited files.
func (p *diffPanel) afterTurn(ctx ext.Ctx) tea.Cmd {
	w, _ := ctx.Size()
	if ctx.Layout() != ext.Fullscreen {
		return nil
	}
	if p.open {
		p.v.sources = append(p.v.sources[:1], turnSources(ctx.Transcript())...)
		ctx.Invalidate(DiffPanelID)
		return p.v.loadGit()
	}
	if w >= diffPanelAutoWidth && !p.dismissed && len(turnSources(ctx.Transcript())) > 0 {
		return p.show(ctx, false)
	}
	return nil
}

func (p *diffPanel) ID() string           { return DiffPanelID }
func (p *diffPanel) Init(ext.Ctx) tea.Cmd { return nil }
func (p *diffPanel) KeyContext() string   { return ext.ContextDiffPanel }

// KeyContexts adds the DiffDialog keys (sources, files, scrolling) to the panel's own.
func (p *diffPanel) KeyContexts() []string {
	return []string{ext.ContextDiffPanel, ext.ContextDiffDialog}
}

func (p *diffPanel) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	if !p.open || p.v == nil {
		return nil
	}
	switch m := msg.(type) {
	case gitDiffMsg:
		return p.v.Update(ctx, m)
	case ext.MouseEvent:
		if wm, ok := m.Msg.(tea.MouseWheelMsg); ok {
			switch wm.Mouse().Button {
			case tea.MouseWheelDown:
				p.v.scroll += 3
			case tea.MouseWheelUp:
				p.v.scroll = max(p.v.scroll-3, 0)
			}
			ctx.Invalidate(DiffPanelID)
		}
	}
	return nil
}

func (p *diffPanel) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return false, nil }

func (p *diffPanel) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	if !p.open {
		return false, nil
	}
	switch a {
	case ext.ActDiffDismiss, ext.ActDiffBack, ext.ActSelectCancel:
		return true, p.close(ctx)
	}
	return p.v.HandleAction(ctx, a)
}

func (p *diffPanel) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if !p.open {
		return false, nil
	}
	switch k.String() {
	case "esc", "q":
		return true, p.close(ctx)
	}
	return p.v.HandleKey(ctx, k)
}

func (p *diffPanel) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	if !p.open || p.v == nil {
		return ext.Rendered{}
	}
	return p.v.View(ctx, a)
}

// onTurnEnd is called on each main-engine result.
func (f *feature) diffPanelTurnEnd(ctx ext.Ctx, ev proto.Event) tea.Cmd {
	if _, ok := ev.(*proto.Result); !ok || f.diffPanel == nil {
		return nil
	}
	return f.diffPanel.afterTurn(ctx)
}
