package editor

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Styles are the editor's visual styles. Hosts fill them from theme tokens.
type Styles struct {
	Text         lipgloss.Style
	Placeholder  lipgloss.Style
	Ghost        lipgloss.Style
	Chip         lipgloss.Style
	ChipSelected lipgloss.Style // selected in attachment navigation
	Selection    lipgloss.Style // vim visual selection
	Cursor       lipgloss.Style // the virtual cursor cell
}

// DefaultStyles uses only attributes (faint, reverse), no colours.
func DefaultStyles() Styles {
	return Styles{
		Placeholder:  lipgloss.NewStyle().Faint(true),
		Ghost:        lipgloss.NewStyle().Faint(true),
		ChipSelected: lipgloss.NewStyle().Reverse(true),
		Selection:    lipgloss.NewStyle().Reverse(true),
		Cursor:       lipgloss.NewStyle().Reverse(true),
	}
}

// Span styles text cells [Start, End) of one logical line (see
// Editor.Decorate).
type Span struct {
	Start, End int
	Style      lipgloss.Style
}

// ChipRune stands in for a chip in the graphemes passed to Decorate.
const ChipRune = "￼"

// style classes for one render
const (
	stText = iota
	stChip
	stChipSel
	stSel
	stGhost
	stCursor
	stPlaceholder
	stDecor // first decoration span index
)

// rcell is a cell prepared for rendering.
type rcell struct {
	cell
	ghost bool
}

// View renders the editor as a string.
func (e *Editor) View() string {
	rows, _ := e.Render()
	return strings.Join(rows, "\n")
}

// Height is the number of rows Render returns.
func (e *Editor) Height() int {
	n := e.renderRowCount()
	if e.maxHeight > 0 && n > e.maxHeight {
		return e.maxHeight
	}
	return n
}

func (e *Editor) renderRowCount() int {
	if e.showPlaceholder() {
		return 1
	}
	n := 0
	for i := range e.lines {
		if i == len(e.lines)-1 && e.GhostVisible() {
			n += len(wrapCells(e.lineCells(i, true), e.width))
			continue
		}
		n += len(e.lines[i].layout(e.width))
	}
	return n
}

func (e *Editor) showPlaceholder() bool {
	return e.Empty() && e.placeholder != "" && e.ghost == ""
}

// lineCells returns line i's cells, with the ghost text appended when asked.
func (e *Editor) lineCells(i int, withGhost bool) []cell {
	cs := e.lines[i].cells
	if !withGhost {
		return cs
	}
	g := toCells(e.ghost)
	out := make([]cell, 0, len(cs)+len(g))
	return append(append(out, cs...), g...)
}

// CursorPos returns the cursor's cell position relative to the first
// rendered row, accounting for scrolling. ok is false when the editor is not
// focused.
func (e *Editor) CursorPos() (x, y int, ok bool) {
	_, c := e.render(false)
	if c == nil {
		return 0, 0, false
	}
	return c.X, c.Y, true
}

// Render returns the styled rows (each at most Width cells wide) and, when
// the editor is focused and VirtualCursor is off, the real cursor to place
// (relative to the first row). With VirtualCursor on, the cursor is drawn as
// a styled cell and no real cursor is returned; CursorPos still reports it.
func (e *Editor) Render() ([]string, *tea.Cursor) {
	rows, c := e.render(true)
	if e.VirtualCursor {
		return rows, nil
	}
	return rows, c
}

func (e *Editor) render(draw bool) ([]string, *tea.Cursor) {
	width := e.width
	focused := e.focused
	virtual := focused && e.VirtualCursor

	if e.showPlaceholder() {
		var cur *tea.Cursor
		if focused {
			cur = e.cursorAt(0, 0)
		}
		if !draw {
			return nil, cur
		}
		ph := toCells(strings.ReplaceAll(normalizeNewlines(e.placeholder), "\n", " "))
		var b strings.Builder
		w := 0
		for i, c := range ph {
			if w+c.w > width {
				break
			}
			w += c.w
			if i == 0 && virtual {
				b.WriteString(e.Styles.Cursor.Render(c.g))
				continue
			}
			b.WriteString(e.Styles.Placeholder.Render(c.g))
		}
		if len(ph) == 0 && virtual {
			b.WriteString(e.Styles.Cursor.Render(" "))
		}
		return []string{b.String()}, cur
	}

	ghostOn := e.GhostVisible()
	selFrom, selTo, selOK := e.selection()
	var chipSel *Chip
	if e.attach >= 0 {
		chipSel = e.SelectedAttachment()
	}

	type vline struct {
		row   int
		cells []rcell
		spans []span
	}
	vlines := make([]vline, len(e.lines))
	total, curVR, curX := 0, 0, 0
	for i := range e.lines {
		withGhost := ghostOn && i == len(e.lines)-1
		cs := e.lines[i].cells
		var spans []span
		if withGhost {
			cs = e.lineCells(i, true)
			spans = wrapCells(cs, width)
		} else {
			spans = e.lines[i].layout(width)
		}
		rc := make([]rcell, len(cs))
		for j, c := range cs {
			rc[j] = rcell{cell: c, ghost: j >= len(e.lines[i].cells)}
		}
		vlines[i] = vline{row: i, cells: rc, spans: spans}
		if i == e.cur.Row {
			j := spanFor(spans, e.cur.Col)
			curVR = total + j
			curX = cellsWidth(cs[spans[j].start:e.cur.Col])
		}
		total += len(spans)
	}

	// Scroll window.
	top, bottom := 0, total
	if e.maxHeight > 0 && total > e.maxHeight {
		if curVR < e.scroll {
			e.scroll = curVR
		}
		if curVR >= e.scroll+e.maxHeight {
			e.scroll = curVR - e.maxHeight + 1
		}
		if e.scroll > total-e.maxHeight {
			e.scroll = total - e.maxHeight
		}
		if e.scroll < 0 {
			e.scroll = 0
		}
		top, bottom = e.scroll, e.scroll+e.maxHeight
	}

	var cur *tea.Cursor
	if focused {
		cur = e.cursorAt(curX, curVR-top)
	}
	if !draw {
		return nil, cur
	}

	styles := []lipgloss.Style{
		stText:        e.Styles.Text,
		stChip:        e.Styles.Chip,
		stChipSel:     e.Styles.ChipSelected,
		stSel:         e.Styles.Selection,
		stGhost:       e.Styles.Ghost,
		stCursor:      e.Styles.Cursor,
		stPlaceholder: e.Styles.Placeholder,
	}

	out := make([]string, 0, bottom-top)
	vr := 0
	for _, vl := range vlines {
		if vr >= bottom {
			break
		}
		if vr+len(vl.spans) <= top {
			vr += len(vl.spans)
			continue
		}
		// Decorations for this logical line.
		var decor []int // per text cell: style index or -1
		if e.Decorate != nil {
			gs := make([]string, len(e.lines[vl.row].cells))
			for j, c := range e.lines[vl.row].cells {
				if c.chip != nil {
					gs[j] = ChipRune
				} else {
					gs[j] = c.g
				}
			}
			if spans := e.Decorate(vl.row, gs); len(spans) > 0 {
				decor = make([]int, len(gs))
				for j := range decor {
					decor[j] = -1
				}
				for _, sp := range spans {
					idx := len(styles)
					styles = append(styles, sp.Style)
					for j := max(sp.Start, 0); j < sp.End && j < len(decor); j++ {
						decor[j] = idx
					}
				}
			}
		}
		for _, s := range vl.spans {
			if vr < top {
				vr++
				continue
			}
			if vr >= bottom {
				break
			}
			var b rowBuilder
			for ci := s.start; ci < s.end; ci++ {
				c := vl.cells[ci]
				st := stText
				switch {
				case c.ghost:
					st = stGhost
				case c.chip != nil && c.chip == chipSel:
					st = stChipSel
				case selOK && inRange(Pos{vl.row, ci}, selFrom, selTo):
					st = stSel
				case c.chip != nil:
					st = stChip
				case decor != nil && decor[ci] >= 0:
					st = decor[ci]
				}
				if virtual && vl.row == e.cur.Row && ci == e.cur.Col {
					st = stCursor
				}
				b.add(st, renderText(c.cell, width))
			}
			// Cursor past the last cell of the row (end of line).
			if virtual && vr == curVR && e.cur.Col >= s.end {
				b.add(stCursor, " ")
			}
			row := b.render(styles)
			if ansi.StringWidth(row) > width {
				row = ansi.Truncate(row, width, "")
			}
			out = append(out, row)
			vr++
		}
	}
	return out, cur
}

func (e *Editor) cursorAt(x, y int) *tea.Cursor {
	c := tea.NewCursor(x, y)
	c.Shape = tea.CursorBar
	if e.vim != nil && e.vim.blockCursor() {
		c.Shape = tea.CursorBlock
	}
	c.Blink = true
	return c
}

func inRange(p, from, to Pos) bool { return !p.Less(from) && p.Less(to) }

func renderText(c cell, width int) string {
	switch {
	case c.chip != nil:
		l := c.chip.Label()
		if c.w > width {
			return ansi.Truncate(l, width, "…")
		}
		return l
	case c.g == "\t":
		return strings.Repeat(" ", tabWidth)
	}
	return c.g
}

// rowBuilder groups consecutive cells of the same style into runs.
type rowBuilder struct {
	runs []run
}

type run struct {
	st   int
	text strings.Builder
}

func (b *rowBuilder) add(st int, s string) {
	if n := len(b.runs); n > 0 && b.runs[n-1].st == st {
		b.runs[n-1].text.WriteString(s)
		return
	}
	b.runs = append(b.runs, run{st: st})
	b.runs[len(b.runs)-1].text.WriteString(s)
}

func (b *rowBuilder) render(styles []lipgloss.Style) string {
	var out strings.Builder
	for i := range b.runs {
		r := &b.runs[i]
		out.WriteString(styles[r.st].Render(r.text.String()))
	}
	return out.String()
}
