package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Fullscreen layout (B14, for plan 12's fullscreen renderer). In Options.Layout ==
// ext.Fullscreen the frame is the alternate screen, laid out as:
//
//	┌──────────── header ─────────────┐
//	│ sidebarLeft │  live  │ sidebarR │   live: the transcript viewport (plan 12)
//	├─────────────────────────────────┤
//	│ status / aboveInput / input /   │   same bottom stack as inline
//	│ belowInput / statusLine         │
//	└─────────────────────────────────┘
//
// Every region is a lipgloss Layer whose ID is its component ID, composed on a Canvas;
// a PlaceCentered (or inline) dialog is an overlay layer centred above the frame. The
// mouse is on (cell motion): View.OnMouse hit-tests the layers and delivers the event
// to the component under the pointer as ext.AddressedMsg{To: id, Msg: MouseEvent}.

// MouseEvent is ext.MouseEvent (alias for host code).
type MouseEvent = ext.MouseEvent

// SidebarWidth is the width given to each sidebar slot at terminal width w.
func SidebarWidth(w int) int { return min(40, max(20, w/4)) }

type region struct {
	id         string
	x, y, w, h int
	lines      []string
}

func (r *Root) fullscreenView() tea.View {
	W, H := r.w, r.h
	mode := ext.Fullscreen
	dialog := r.topDialog()
	var regions []region

	// Bottom stack first: it has priority over the transcript area.
	bottomSlots := []ext.Slot{ext.SlotStatus, ext.SlotAboveInput, ext.SlotInput, ext.SlotBelowInput, ext.SlotStatusLine}
	var bottom []region
	remaining := max(1, H-1)
	var cursor *tea.Cursor
	cursorRegion := -1 // index into bottom of the focused component
	// An inline dialog (permission prompt, AskUserQuestion, pickers) takes the input's
	// place at the bottom, as in inline mode; only PlaceCentered dialogs overlay.
	inlineDialog := dialog != nil && dialog.d.Placement() == ext.PlaceInline
	for _, slot := range bottomSlots {
		if slot == ext.SlotInput && inlineDialog {
			d := dialog
			lines := r.render(d.c, func(a ext.Area) ext.Rendered { return d.d.View(r.ctx, a) }, ext.Area{Width: W, MaxHeight: remaining, Focused: true, Mode: mode})
			if len(lines) > remaining {
				lines = lines[:remaining]
			}
			remaining -= len(lines)
			if len(lines) > 0 {
				bottom = append(bottom, region{id: d.d.ID(), w: W, h: len(lines), lines: lines})
				if d.c.cursor != nil && d.c.cursor.Y < len(lines) {
					cur := *d.c.cursor
					cursor, cursorRegion = &cur, len(bottom)-1
				}
			}
			continue
		}
		for _, c := range r.comps {
			if c.m.slot != slot || r.host.Disabled(c.feature) || !r.modeOK(c) {
				continue
			}
			maxH := remaining
			if c.m.opts.MaxHeight > 0 {
				maxH = min(maxH, c.m.opts.MaxHeight)
			}
			comp := c.m.comp
			focused := dialog == nil && comp.ID() == r.focus
			lines := r.render(c, func(a ext.Area) ext.Rendered { return comp.View(r.ctx, a) }, ext.Area{Width: W, MaxHeight: maxH, Focused: focused, Mode: mode})
			if len(lines) > maxH {
				lines = lines[:maxH]
			}
			remaining -= len(lines)
			if len(lines) > 0 {
				bottom = append(bottom, region{id: comp.ID(), w: W, h: len(lines), lines: lines})
				if focused && c.cursor != nil && c.cursor.Y < len(lines) {
					cur := *c.cursor
					cursor, cursorRegion = &cur, len(bottom)-1
				}
			}
		}
	}
	bottomH := 0
	for _, b := range bottom {
		bottomH += b.h
	}

	// Header.
	y := 0
	for _, c := range r.slotComps(ext.SlotHeader) {
		comp := c.m.comp
		lines := r.render(c, func(a ext.Area) ext.Rendered { return comp.View(r.ctx, a) }, ext.Area{Width: W, MaxHeight: max(0, H-bottomH-1), Mode: mode})
		lines = lines[:min(len(lines), max(0, H-bottomH-1-y))]
		if len(lines) > 0 {
			regions = append(regions, region{id: comp.ID(), x: 0, y: y, w: W, h: len(lines), lines: lines})
			y += len(lines)
		}
	}

	// Middle band: sidebars and the live area.
	midH := max(0, H-bottomH-y)
	left, right := r.slotComps(ext.SlotSidebarL), r.slotComps(ext.SlotSidebarR)
	// A sidebar takes columns only when it renders something: a closed pane (View
	// returns "") leaves the transcript its full width.
	lx, mainW := 0, W
	if len(left) > 0 {
		sw := r.sidebarWidth(ext.SlotSidebarL)
		if rs := r.stackRegion(left, 0, y, sw, midH, mode); len(rs) > 0 {
			regions = append(regions, rs...)
			lx, mainW = sw+1, mainW-sw-1
		}
	}
	if len(right) > 0 {
		sw := r.sidebarWidth(ext.SlotSidebarR)
		if rs := r.stackRegion(right, W-sw, y, sw, midH, mode); len(rs) > 0 {
			regions = append(regions, rs...)
			mainW -= sw + 1
		}
	}
	for _, c := range r.slotComps(ext.SlotLive) {
		comp := c.m.comp
		lines := r.render(c, func(a ext.Area) ext.Rendered { return comp.View(r.ctx, a) }, ext.Area{Width: mainW, MaxHeight: midH, Mode: mode})
		if len(lines) > midH {
			lines = lines[len(lines)-midH:] // tail
		}
		regions = append(regions, region{id: comp.ID(), x: lx, y: y + midH - len(lines), w: mainW, h: len(lines), lines: lines})
	}

	// Bottom stack below the middle band.
	by := y + midH
	for i := range bottom {
		bottom[i].y = by
		if i == cursorRegion {
			cursor.Y += by
		}
		by += bottom[i].h
	}
	regions = append(regions, bottom...)

	// Compose.
	base := lipgloss.NewLayer(strings.Repeat("\n", max(0, H-1))).ID("frame")
	var layers []*lipgloss.Layer
	layers = append(layers, base)
	for _, rg := range regions {
		layers = append(layers, lipgloss.NewLayer(strings.Join(rg.lines, "\n")).ID(rg.id).X(rg.x).Y(rg.y).Z(1))
	}
	if dialog != nil && !inlineDialog {
		dw := min(W-4, 100)
		lines := r.render(dialog.c, func(a ext.Area) ext.Rendered { return dialog.d.View(r.ctx, a) }, ext.Area{Width: dw, MaxHeight: H - 2, Focused: true, Mode: mode})
		lines = lines[:min(len(lines), H-2)]
		dx, dy := (W-dw)/2, max(0, (H-len(lines))/2)
		layers = append(layers, lipgloss.NewLayer(strings.Join(lines, "\n")).ID(dialog.d.ID()).X(dx).Y(dy).Z(10))
		cursor = nil
		if dialog.c.cursor != nil && dialog.c.cursor.Y < len(lines) {
			cur := *dialog.c.cursor
			cur.X += dx
			cur.Y += dy
			cursor = &cur
		}
	}
	comp := lipgloss.NewCompositor(layers...)
	canvas := lipgloss.NewCanvas(W, H)
	canvas.Compose(comp)
	v := tea.NewView(canvas.Render())
	v.AltScreen = true
	v.Cursor = cursor
	if r.opts.NoMouse {
		return v
	}
	v.MouseMode = tea.MouseModeCellMotion
	v.OnMouse = func(m tea.MouseMsg) tea.Cmd {
		if _, wheel := m.(tea.MouseWheelMsg); wheel {
			return nil // the wheel goes through the keymap (scroll:* actions) only
		}
		mm := m.Mouse()
		hit := comp.Hit(mm.X, mm.Y)
		if hit.Empty() || hit.ID() == "frame" {
			return nil
		}
		b := hit.Bounds()
		return ext.Address(hit.ID(), MouseEvent{Msg: m, X: mm.X - b.Min.X, Y: mm.Y - b.Min.Y})
	}
	return v
}

// sidebarWidth is a sidebar's width: SidebarWidth(W) adjusted by SidebarResizeMsg,
// clamped to [12, W/2].
func (r *Root) sidebarWidth(s ext.Slot) int {
	return min(max(SidebarWidth(r.w)+r.sidebarDelta[s], 12), max(12, r.w/2))
}

// layoutRequest switches between Inline and Fullscreen at runtime.
func (r *Root) layoutRequest(m ext.LayoutRequestMsg) tea.Cmd {
	if m.Mode == r.opts.Layout || (m.Mode != ext.Inline && m.Mode != ext.Fullscreen) {
		return nil
	}
	if m.Mode == ext.Fullscreen && (r.opts.NoAltScreen || r.opts.A11y.ScreenReader) {
		return r.addNotice(ext.Notice{Key: "layout", Text: "Fullscreen is off: the alternate screen is disabled or screen-reader mode is on", Level: ext.NoticeWarning, Source: "core"})
	}
	r.opts.Layout = m.Mode
	r.invalidateAll()
	cmds := []tea.Cmd{r.broadcast(ext.LayoutChangedMsg{Mode: m.Mode})}
	if m.Mode == ext.Inline {
		// Back to native scrollback: clear and let the commit policy reprint.
		cmds = append(cmds, r.enqueueClear())
	} else {
		// Items paused behind an inline alt-screen view finish in fullscreen.
		cmds = append(cmds, r.resumePrinter())
	}
	return tea.Batch(cmds...)
}

func (r *Root) slotComps(s ext.Slot) []*comp {
	var out []*comp
	for _, c := range r.comps {
		if c.m.slot == s && !r.host.Disabled(c.feature) && r.modeOK(c) {
			out = append(out, c)
		}
	}
	return out
}

// stackRegion stacks a sidebar's components top to bottom within w×h.
func (r *Root) stackRegion(cs []*comp, x, y, w, h int, mode ext.LayoutMode) []region {
	var out []region
	used := 0
	for _, c := range cs {
		if used >= h {
			break
		}
		comp := c.m.comp
		lines := r.render(c, func(a ext.Area) ext.Rendered { return comp.View(r.ctx, a) }, ext.Area{Width: w, MaxHeight: h - used, Mode: mode})
		lines = lines[:min(len(lines), h-used)]
		if len(lines) == 0 {
			continue
		}
		out = append(out, region{id: comp.ID(), x: x, y: y + used, w: w, h: len(lines), lines: lines})
		used += len(lines)
	}
	return out
}
