package fullscreen

import (
	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// paneStep is how many columns one pane:grow / pane:shrink moves a sidebar edge.
const paneStep = 4

// paneTracker remembers where the transcript sits on screen, which tells which
// sidebars exist (columns left or right of it) and which one the user clicked last.
type paneTracker struct {
	originX   int // the viewport's first screen column
	known     bool
	lastSlot  ext.Slot
	viewWidth int
}

// observe records the viewport origin from an addressed mouse event, and the sidebar
// side from raw clicks (every component sees raw mouse messages).
func (p *paneTracker) observe(msg tea.Msg, viewWidth int) {
	switch m := msg.(type) {
	case ext.MouseEvent:
		p.originX, p.known = m.Msg.Mouse().X-m.X, true
		p.viewWidth = viewWidth
	case tea.MouseClickMsg:
		if !p.known {
			return
		}
		switch {
		case m.X < p.originX-1:
			p.lastSlot = ext.SlotSidebarL
		case m.X > p.originX+p.viewWidth:
			p.lastSlot = ext.SlotSidebarR
		}
	}
}

// target picks the sidebar to resize: the last one clicked, else the right one (where
// /diff opens), else the left one, among those present on screen.
func (p *paneTracker) target(screenW int) (ext.Slot, bool) {
	if !p.known {
		return ext.SlotSidebarR, true // let the host decide; it ignores absent slots
	}
	left := p.originX > 0
	right := p.originX+p.viewWidth < screenW-1
	switch {
	case p.lastSlot == ext.SlotSidebarL && left:
		return ext.SlotSidebarL, true
	case p.lastSlot == ext.SlotSidebarR && right:
		return ext.SlotSidebarR, true
	case right:
		return ext.SlotSidebarR, true
	case left:
		return ext.SlotSidebarL, true
	}
	return "", false
}

// paneAction is pane:grow / pane:shrink in the fullscreen layout.
func (v *transcriptView) paneAction(delta int) ext.ActionFunc {
	return func(ctx ext.Ctx) (bool, tea.Cmd) {
		if !v.active(ctx) {
			return false, nil
		}
		w, _ := ctx.Size()
		slot, ok := v.panes.target(w)
		if !ok {
			return false, nil
		}
		return true, ext.Msg(ext.SidebarResizeMsg{Slot: slot, Delta: delta})
	}
}

// paneBindings put the Pane context's resize chords in the Scroll context, which is
// active throughout the fullscreen layout, so any focused sidebar (or none) can be
// resized.
var paneBindings = []ext.Binding{
	{Context: ext.ContextScroll, Keys: "ctrl+x left", Action: ext.ActPaneGrow},
	{Context: ext.ContextScroll, Keys: "ctrl+x up", Action: ext.ActPaneGrow},
	{Context: ext.ContextScroll, Keys: "ctrl+x right", Action: ext.ActPaneShrink},
	{Context: ext.ContextScroll, Keys: "ctrl+x down", Action: ext.ActPaneShrink},
}
