package editor

import (
	"strings"
	"time"
)

// Editor is a multi-line prompt editor: grapheme-aware buffer with soft
// wrap, readline keys, a kill ring, undo/redo, atomic chips, paste
// collapsing, ghost text and an optional vim mode.
//
// It is a widget, not a feature: it knows nothing about the engine,
// settings or the keymap. Hosts call its operations from actions and pass
// the remaining keys to HandleKey. All methods must be called from the
// Update goroutine.
type Editor struct {
	// KeyMap holds the keys HandleKey handles itself. Hosts with a keymap
	// clear the entries they route through actions instead.
	KeyMap KeyMap
	// Styles for text, chips, ghost text and the virtual cursor.
	Styles Styles
	// VirtualCursor draws the cursor as a styled cell. The real cursor
	// position is reported by Render either way (for IME placement).
	VirtualCursor bool
	// Decorate, when set, styles ranges of a line's text cells (keyword
	// highlights, spellcheck). Chip cells are passed as U+FFFC.
	Decorate func(row int, graphemes []string) []Span
	// Paste controls paste collapsing and storage.
	Paste PasteConfig
	// Now is the clock used for undo coalescing. Nil means time.Now.
	Now func() time.Time

	lines []line
	cur   Pos
	goalX int // preferred visual column for vertical moves; -1 when unset

	width, maxHeight, scroll int
	focused                  bool

	ghost       string
	placeholder string

	chipSeq int
	attach  int // selected chip index in attachment mode; -1 when off

	kill     killRing
	lastCmd  cmdKind
	yankFrom Pos

	undo, redo []snapshot
	lastEdit   editKind
	lastEditAt time.Time
	lastPos    Pos
	group      int
	groupDirty bool
	groupBase  int

	pasting  bool
	pasteBuf strings.Builder

	vim *vimState
	rev int
}

// New returns an empty, focused editor 80 cells wide with default keys and
// styles.
func New() *Editor {
	e := &Editor{
		KeyMap:        DefaultKeyMap(),
		Styles:        DefaultStyles(),
		VirtualCursor: true,
		Paste:         DefaultPasteConfig(),
		lines:         []line{{}},
		goalX:         -1,
		width:         80,
		focused:       true,
		attach:        -1,
	}
	return e
}

// Rev changes whenever the content, cursor or presentation changes. Hosts
// use it to invalidate cached renders.
func (e *Editor) Rev() int { return e.rev }

func (e *Editor) touch() { e.rev++ }

func (e *Editor) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// ---- geometry and focus ----

// SetWidth sets the wrap width in cells.
func (e *Editor) SetWidth(w int) {
	if w < 1 {
		w = 1
	}
	if w != e.width {
		e.width = w
		e.touch()
	}
}

// Width is the wrap width.
func (e *Editor) Width() int { return e.width }

// SetMaxHeight caps the rendered height in rows; 0 means unlimited. Taller
// content scrolls to keep the cursor visible.
func (e *Editor) SetMaxHeight(h int) {
	if h != e.maxHeight {
		e.maxHeight = h
		e.touch()
	}
}

// Focus and Blur control whether the cursor is shown.
func (e *Editor) Focus() { e.focused = true; e.touch() }
func (e *Editor) Blur()  { e.focused = false; e.touch() }

// Focused reports whether the editor has focus.
func (e *Editor) Focused() bool { return e.focused }

// SetPlaceholder sets the text shown while the buffer is empty.
func (e *Editor) SetPlaceholder(s string) { e.placeholder = s; e.touch() }

// ---- content ----

// SetValue replaces the content with plain text and puts the cursor at the
// end. It is undoable.
func (e *Editor) SetValue(s string) {
	e.checkpoint(editOther)
	e.lines = nil
	for _, cs := range splitLines(s) {
		e.lines = append(e.lines, line{cells: cs})
	}
	e.cur = e.endPos()
	e.afterEdit()
}

// Text is the content to submit: paste chips are expanded to their full
// text and image chips stay as their "[Image #N]" labels.
func (e *Editor) Text() string {
	var b strings.Builder
	for i, l := range e.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		for _, c := range l.cells {
			if c.chip != nil {
				b.WriteString(c.chip.Expanded())
			} else {
				b.WriteString(c.g)
			}
		}
	}
	return b.String()
}

// Display is the content with every chip shown as its label, as stored in
// history's display field.
func (e *Editor) Display() string {
	parts := make([]string, len(e.lines))
	for i, l := range e.lines {
		parts[i] = cellsText(l.cells)
	}
	return strings.Join(parts, "\n")
}

// String implements fmt.Stringer (same as Display).
func (e *Editor) String() string { return e.Display() }

// Empty reports whether the buffer has no content.
func (e *Editor) Empty() bool { return len(e.lines) == 1 && len(e.lines[0].cells) == 0 }

// LineCount is the number of logical lines.
func (e *Editor) LineCount() int { return len(e.lines) }

// Line returns logical line i with chips as labels.
func (e *Editor) Line(i int) string { return cellsText(e.lines[i].cells) }

// Clear empties the buffer as one undoable edit (ctrl+l, double esc).
func (e *Editor) Clear() {
	if e.Empty() {
		return
	}
	e.checkpoint(editOther)
	e.lines = []line{{}}
	e.cur = Pos{}
	e.afterEdit()
}

// Reset empties the buffer and forgets undo history, the ghost text and
// attachment state (after a submit). The chip counter and kill ring stay.
func (e *Editor) Reset() {
	e.lines = []line{{}}
	e.cur = Pos{}
	e.goalX = -1
	e.undo, e.redo = nil, nil
	e.lastEdit = editNone
	e.ghost = ""
	e.attach = -1
	e.scroll = 0
	e.lastCmd = cmdNone
	if e.vim != nil {
		e.vim.reset()
	}
	e.touch()
}

// ---- cursor ----

// Cursor returns the cursor position.
func (e *Editor) Cursor() Pos { return e.cur }

// SetCursor moves the cursor, clamped to the buffer.
func (e *Editor) SetCursor(p Pos) {
	e.cur = e.clamp(p)
	e.moved()
}

func (e *Editor) clamp(p Pos) Pos {
	if p.Row < 0 {
		return Pos{}
	}
	if p.Row >= len(e.lines) {
		return e.endPos()
	}
	if p.Col < 0 {
		p.Col = 0
	}
	if n := len(e.lines[p.Row].cells); p.Col > n {
		p.Col = n
	}
	return p
}

func (e *Editor) endPos() Pos {
	r := len(e.lines) - 1
	return Pos{r, len(e.lines[r].cells)}
}

// AtStart and AtEnd report whether the cursor is at the very start or end.
func (e *Editor) AtStart() bool { return e.cur == Pos{} }
func (e *Editor) AtEnd() bool   { return e.cur == e.endPos() }

// moved records a cursor move that is not an edit: it ends undo coalescing,
// kill accumulation and the vertical goal column.
func (e *Editor) moved() {
	e.goalX = -1
	e.lastEdit = editNone
	e.lastCmd = cmdNone
	e.touch()
}

// CharForward moves right one cell, wrapping to the next line. At the very
// end it accepts the ghost text instead, if any.
func (e *Editor) CharForward() {
	if e.AtEnd() {
		e.AcceptGhost()
		return
	}
	if e.cur.Col < len(e.lines[e.cur.Row].cells) {
		e.cur.Col++
	} else {
		e.cur = Pos{e.cur.Row + 1, 0}
	}
	e.moved()
}

// CharBackward moves left one cell, wrapping to the previous line.
func (e *Editor) CharBackward() {
	switch {
	case e.cur.Col > 0:
		e.cur.Col--
	case e.cur.Row > 0:
		e.cur = Pos{e.cur.Row - 1, len(e.lines[e.cur.Row-1].cells)}
	}
	e.moved()
}

// LineStart moves to the start of the logical line (ctrl+a, home).
func (e *Editor) LineStart() { e.cur.Col = 0; e.moved() }

// LineEnd moves to the end of the logical line (ctrl+e, end).
func (e *Editor) LineEnd() { e.cur.Col = len(e.lines[e.cur.Row].cells); e.moved() }

// DocStart and DocEnd move to the start or end of the buffer.
func (e *Editor) DocStart() { e.cur = Pos{}; e.moved() }
func (e *Editor) DocEnd()   { e.cur = e.endPos(); e.moved() }

// WordForward moves to the end of the next word (alt+f, ctrl+right).
func (e *Editor) WordForward() { e.cur = e.wordEndForward(e.cur); e.moved() }

// WordBackward moves to the start of the previous word (alt+b, ctrl+left).
func (e *Editor) WordBackward() { e.cur = e.wordStartBackward(e.cur); e.moved() }

// CursorUp moves up one visual row, keeping the goal column. It reports
// false when the cursor is already on the first visual row (the host then
// recalls history).
func (e *Editor) CursorUp() bool { return e.vertical(-1) }

// CursorDown moves down one visual row. It reports false on the last row.
func (e *Editor) CursorDown() bool { return e.vertical(1) }

func (e *Editor) vertical(d int) bool {
	vr, x := e.visualPos(e.cur)
	rows := e.visualRows()
	t := vr + d
	if t < 0 || t >= len(rows) {
		return false
	}
	goal := e.goalX
	if goal < 0 {
		goal = x
	}
	e.cur = e.posAtVisual(rows, t, goal)
	e.goalX = goal
	e.lastEdit = editNone
	e.lastCmd = cmdNone
	e.touch()
	return true
}

// OnFirstRow and OnLastRow report whether the cursor is on the first or
// last visual row.
func (e *Editor) OnFirstRow() bool { vr, _ := e.visualPos(e.cur); return vr == 0 }
func (e *Editor) OnLastRow() bool {
	vr, _ := e.visualPos(e.cur)
	return vr == len(e.visualRows())-1
}

// ---- word scanning ----

// at returns the cell at p; nl is true when p is a line end that is followed
// by another line, eof when p is the buffer end.
func (e *Editor) at(p Pos) (c cell, nl, eof bool) {
	l := e.lines[p.Row].cells
	if p.Col < len(l) {
		return l[p.Col], false, false
	}
	if p.Row < len(e.lines)-1 {
		return cell{}, true, false
	}
	return cell{}, false, true
}

func (e *Editor) next(p Pos) (Pos, bool) {
	if p.Col < len(e.lines[p.Row].cells) {
		return Pos{p.Row, p.Col + 1}, true
	}
	if p.Row < len(e.lines)-1 {
		return Pos{p.Row + 1, 0}, true
	}
	return p, false
}

func (e *Editor) prev(p Pos) (Pos, bool) {
	if p.Col > 0 {
		return Pos{p.Row, p.Col - 1}, true
	}
	if p.Row > 0 {
		return Pos{p.Row - 1, len(e.lines[p.Row-1].cells)}, true
	}
	return p, false
}

// isWordAt reports whether p holds a word cell; chips count as words.
func (e *Editor) isWordAt(p Pos) bool {
	c, nl, eof := e.at(p)
	return !nl && !eof && (c.isWord() || c.isChip())
}

func (e *Editor) isSpaceAt(p Pos) bool {
	c, nl, eof := e.at(p)
	return nl || (!eof && c.isSpace())
}

// wordEndForward skips non-word cells, then word cells (emacs forward-word).
// A chip is a word on its own.
func (e *Editor) wordEndForward(p Pos) Pos {
	for !e.isWordAt(p) {
		q, ok := e.next(p)
		if !ok {
			return p
		}
		p = q
	}
	if e.chipAt(p) {
		q, _ := e.next(p)
		return q
	}
	for e.isWordAt(p) && !e.chipAt(p) {
		q, ok := e.next(p)
		if !ok {
			return p
		}
		p = q
	}
	return p
}

// wordStartBackward skips non-word cells backwards, then word cells.
func (e *Editor) wordStartBackward(p Pos) Pos {
	for {
		q, ok := e.prev(p)
		if !ok {
			return p
		}
		if e.isWordAt(q) {
			break
		}
		p = q
	}
	if q, _ := e.prev(p); e.chipAt(q) {
		return q
	}
	for {
		q, ok := e.prev(p)
		if !ok || !e.isWordAt(q) || e.chipAt(q) {
			return p
		}
		p = q
	}
}

func (e *Editor) chipAt(p Pos) bool {
	c, _, _ := e.at(p)
	return c.isChip()
}

// spaceWordStartBackward skips whitespace backwards, then non-whitespace
// (unix-word-rubout, ctrl+w).
func (e *Editor) spaceWordStartBackward(p Pos) Pos {
	for {
		q, ok := e.prev(p)
		if !ok || !e.isSpaceAt(q) {
			break
		}
		p = q
	}
	for {
		q, ok := e.prev(p)
		if !ok || e.isSpaceAt(q) {
			break
		}
		p = q
	}
	return p
}

// ---- visual layout ----

// vrow is one visual row: logical row plus span.
type vrow struct {
	row int
	span
	last bool // last visual row of its logical line
}

func (e *Editor) visualRows() []vrow {
	var out []vrow
	for i := range e.lines {
		spans := e.lines[i].layout(e.width)
		for j, s := range spans {
			out = append(out, vrow{row: i, span: s, last: j == len(spans)-1})
		}
	}
	return out
}

// visualPos maps a buffer position to (visual row, x).
func (e *Editor) visualPos(p Pos) (int, int) {
	vr := 0
	for i := 0; i < p.Row; i++ {
		vr += len(e.lines[i].layout(e.width))
	}
	spans := e.lines[p.Row].layout(e.width)
	j := spanFor(spans, p.Col)
	return vr + j, cellsWidth(e.lines[p.Row].cells[spans[j].start:p.Col])
}

// spanFor returns the visual row of a line that holds column col: the first
// row ending after col, else the last row (end of line).
func spanFor(spans []span, col int) int {
	for j, s := range spans {
		if col < s.end {
			return j
		}
	}
	return len(spans) - 1
}

// posAtVisual returns the position on visual row t closest to column x.
func (e *Editor) posAtVisual(rows []vrow, t, x int) Pos {
	r := rows[t]
	cells := e.lines[r.row].cells
	acc := 0
	for i := r.start; i < r.end; i++ {
		if acc+cells[i].w > x {
			return Pos{r.row, i}
		}
		acc += cells[i].w
	}
	end := r.end
	if !r.last && end > r.start {
		end-- // the row's end belongs to the next visual row
	}
	return Pos{r.row, end}
}

// VisualLines is the number of visual rows at the current width.
func (e *Editor) VisualLines() int {
	n := 0
	for i := range e.lines {
		n += len(e.lines[i].layout(e.width))
	}
	return n
}
