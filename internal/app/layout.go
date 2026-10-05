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
	alt := false
	if d := r.topDialog(); d != nil && d.d.Placement() == ext.PlaceAltScreen {
		v, alt = r.altView(d), true
	} else if r.opts.Layout == ext.Fullscreen {
		v, alt = r.fullscreenView(), true
	} else {
		v = r.inlineView()
	}
	r.terminalState(&v)
	h := strings.Count(v.Content, "\n") + 1
	if !alt {
		h = r.holdHeight(&v, h)
		r.frameShown = h
	} else {
		r.frameShown, r.shrink.active = 0, false
	}
	r.noteHeight(h)
	return v
}

// Shrinking inline frames. On a shorter frame Bubble Tea v2.0.10 (ultraviolet's
// TerminalRenderer) moves up to the frame's top from the cursor row it remembers, but
// first clamps that row to the new frame's last row. When the cursor sat lower than
// that, it moves up too little: the new frame is drawn lower than the old one's top and
// the old upper rows stay on screen (pinned by TestUpstreamInlineShrinkBug).
//
// The clamp is harmless when the cursor sits inside the new frame. So a shrink happens
// in two steps:
//
//  1. hold: keep the old height with the new content at the top and the freed rows
//     blank below it (on screen this already looks shrunk);
//  2. release: shrink once a held frame that shows the cursor inside the new content
//     has been flushed (a few frame intervals, then a release tick). The renderer then
//     moves up from that row exactly to the top, and erases the blank rows below.
//
// While the cursor is hidden (Bubble Tea leaves it wherever the last write ended) the
// hold lasts until it shows again or the frame grows back.
type shrinkState struct {
	active   bool
	target   int  // height to hold
	seq      int  // release token
	needTick bool // Update must schedule the release tick
	ready    bool // the release tick for seq fired
	heldH    int  // content height of the last held frame
	cursor   bool // the last held frame showed the cursor
}

type shrinkReleaseMsg struct{ seq int }

// holdHeight pads v to the held height while a shrink is pending; it returns the frame
// height actually shown.
func (r *Root) holdHeight(v *tea.View, h int) int {
	cursor := v.Cursor != nil
	switch {
	case r.shrink.active && h >= r.shrink.target:
		r.shrink.active = false // grew back: nothing to hold
	case !r.shrink.active && r.frameShown > h:
		r.shrink = shrinkState{active: true, target: r.frameShown, seq: r.shrink.seq + 1, needTick: true, heldH: h, cursor: cursor}
	case r.shrink.active && r.shrink.ready && cursor && r.shrink.cursor && h >= r.shrink.heldH:
		// The flushed held frame put the cursor on a row this frame keeps.
		r.shrink.active = false
	case r.shrink.active && (h < r.shrink.heldH || cursor && !r.shrink.cursor):
		// What was flushed may put the cursor below this frame: hold, flush this
		// frame, then try again.
		r.shrink.seq++
		r.shrink.ready, r.shrink.needTick = false, true
	}
	if !r.shrink.active || h >= r.shrink.target {
		return h
	}
	r.shrink.heldH, r.shrink.cursor = h, cursor
	v.Content += strings.Repeat("\n ", r.shrink.target-h)
	return r.shrink.target
}

// shrinkCheckMsg gives Update a turn shortly after a View, in case that View started a
// hold and the program then went quiet.
type shrinkCheckMsg struct{}

// shrinkCmd schedules the release of a held frame height (called from Update, since
// View cannot return Cmds). A hold starts in the View after some Update, so every
// Update also keeps one check tick in flight (not re-armed by the check itself): it
// schedules the release even when nothing else happens after the shrink.
func (r *Root) shrinkCmd(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	if _, check := msg.(shrinkCheckMsg); check {
		r.shrinkCheckArmed = false
	} else if !r.shrinkCheckArmed {
		r.shrinkCheckArmed = true
		cmds = append(cmds, tea.Tick(2*r.opts.FrameInterval, func(time.Time) tea.Msg { return shrinkCheckMsg{} }))
	}
	if r.shrink.needTick {
		r.shrink.needTick = false
		seq := r.shrink.seq
		cmds = append(cmds, tea.Tick(4*r.opts.FrameInterval, func(time.Time) tea.Msg { return shrinkReleaseMsg{seq: seq} }))
	}
	return tea.Batch(cmds...)
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
	// Never an empty frame: Bubble Tea treats empty content as a zero-height frame and
	// leaves the previous frame's lines on screen.
	if len(out) == 0 || (len(out) == 1 && out[0] == "") {
		out = []string{" "}
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
		if v.WindowTitle == "" && !r.opts.NoTitle {
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
