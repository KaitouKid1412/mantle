package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func truncate(s string, w int) string {
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "")
}

// render returns c's cached lines for the area, re-rendering when invalid or when the
// area changed. Lines are clipped to the width; text is split on "\n".
func (r *Root) render(c *comp, v func(ext.Area) ext.Rendered, a ext.Area) []string {
	if c.valid && c.cw == a.Width && c.ch == a.MaxHeight {
		return c.lines
	}
	var out ext.Rendered
	if !r.safe(c.feature, "View", func() { out = v(a) }) {
		c.lines, c.cursor, c.valid = nil, nil, true
		return nil
	}
	c.text, c.cursor = out.Text, out.Cursor
	c.lines = nil
	if out.Text != "" {
		c.lines = strings.Split(out.Text, "\n")
		for i, l := range c.lines {
			c.lines[i] = clip(l, a.Width)
		}
	}
	c.valid, c.cw, c.ch = true, a.Width, a.MaxHeight
	return c.lines
}

// block is one rendered component placed in the frame.
type block struct {
	c     *comp
	lines []string
	tail  bool // clipped from the top (live slot) rather than the bottom
}

// slotPriority is the order in which slots claim rows from the height budget.
var slotPriority = []ext.Slot{ext.SlotInput, ext.SlotBelowInput, ext.SlotStatus, ext.SlotStatusLine, ext.SlotAboveInput, ext.SlotLive}

// View composes the frame.
func (r *Root) View() tea.View {
	var v tea.View
	if r.quitting {
		return v
	}
	if d := r.topDialog(); d != nil && d.d.Placement() == ext.PlaceAltScreen {
		v = r.altView(d)
	} else {
		v = r.inlineView()
	}
	r.terminalState(&v)
	r.noteHeight(strings.Count(v.Content, "\n") + 1)
	return v
}

func (r *Root) altView(d *openDialog) tea.View {
	a := ext.Area{Width: r.w, MaxHeight: r.h, Focused: true, Mode: ext.AltView}
	lines := r.render(d.c, func(a ext.Area) ext.Rendered { return d.d.View(r.ctx, a) }, a)
	if len(lines) > r.h {
		lines = lines[:r.h]
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	if d.c.cursor != nil && d.c.cursor.Y < len(lines) {
		cur := *d.c.cursor
		v.Cursor = &cur
	}
	return v
}

func (r *Root) inlineView() tea.View {
	budget := r.h - 1
	if budget < 1 {
		budget = 1
	}
	mode := ext.Inline
	inlineDialog := r.topDialog() // Inline and Centered dialogs replace the input slot inline

	placed := map[ext.Slot][]block{}
	remaining := budget
	for _, slot := range slotPriority {
		if slot == ext.SlotInput && inlineDialog != nil {
			d := inlineDialog
			a := ext.Area{Width: r.w, MaxHeight: remaining, Focused: true, Mode: mode}
			lines := r.render(d.c, func(a ext.Area) ext.Rendered { return d.d.View(r.ctx, a) }, a)
			if len(lines) > remaining {
				lines = lines[:remaining]
			}
			remaining -= len(lines)
			placed[slot] = append(placed[slot], block{c: d.c, lines: lines})
			continue
		}
		for _, c := range r.comps {
			if c.m.slot != slot || r.host.Disabled(c.feature) || !r.modeOK(c) {
				continue
			}
			maxH := remaining
			if c.m.opts.MaxHeight > 0 && c.m.opts.MaxHeight < maxH {
				maxH = c.m.opts.MaxHeight
			}
			focused := inlineDialog == nil && c.m.comp.ID() == r.focus
			a := ext.Area{Width: r.w, MaxHeight: maxH, Focused: focused, Mode: mode}
			comp := c.m.comp
			lines := r.render(c, func(a ext.Area) ext.Rendered { return comp.View(r.ctx, a) }, a)
			tail := slot == ext.SlotLive
			if len(lines) > maxH {
				if tail {
					lines = lines[len(lines)-maxH:]
				} else {
					lines = lines[:maxH]
				}
			}
			remaining -= len(lines)
			placed[slot] = append(placed[slot], block{c: c, lines: lines, tail: tail})
		}
	}

	var out []string
	var cursor *tea.Cursor
	for _, slot := range ext.InlineSlots {
		for _, b := range placed[slot] {
			if b.c.cursor != nil && cursor == nil && r.hasCursor(b.c) {
				y := b.c.cursor.Y
				if b.tail {
					y -= len(b.c.lines) - len(b.lines)
				}
				if y >= 0 && y < len(b.lines) {
					cur := *b.c.cursor
					cur.Y = len(out) + y
					cursor = &cur
				}
			}
			out = append(out, b.lines...)
		}
	}
	v := tea.NewView(strings.Join(out, "\n"))
	v.Cursor = cursor
	return v
}

// hasCursor reports whether a component's cursor should be shown: only the key target
// places the real cursor.
func (r *Root) hasCursor(c *comp) bool {
	if d := r.topDialog(); d != nil {
		return d.c == c
	}
	return c.m.comp.ID() == r.focus
}

// terminalState merges TerminalStater contributions into the view.
func (r *Root) terminalState(v *tea.View) {
	for _, c := range r.comps {
		ts, ok := c.m.comp.(ext.TerminalStater)
		if !ok || r.host.Disabled(c.feature) {
			continue
		}
		var s ext.TerminalState
		if !r.safe(c.feature, c.m.comp.ID()+".TerminalState", func() { s = ts.TerminalState(r.ctx) }) {
			continue
		}
		if v.WindowTitle == "" {
			v.WindowTitle = s.WindowTitle
		}
		if v.ProgressBar == nil {
			v.ProgressBar = s.Progress
		}
		v.ReportFocus = v.ReportFocus || s.ReportFocus
	}
}

// noteHeight records a frame height; print chunking uses the maximum of recent frames
// because the renderer flushes on a ticker and may still show an older, taller frame.
func (r *Root) noteHeight(h int) {
	now := time.Now()
	keep := r.heights[:0]
	for _, x := range r.heights {
		if now.Sub(x.at) < 250*time.Millisecond {
			keep = append(keep, x)
		}
	}
	r.heights = append(keep, heightAt{h: h, at: now})
}

// liveHeight is the tallest recent frame.
func (r *Root) liveHeight() int {
	m := 0
	for _, x := range r.heights {
		m = max(m, x.h)
	}
	return m
}
