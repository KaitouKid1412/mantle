package editor

import (
	"strings"
	"time"
)

// ---- primitives ----

// insertFragment inserts f at the cursor and moves the cursor after it.
func (e *Editor) insertFragment(f fragment) {
	if len(f) == 0 {
		return
	}
	row, col := e.cur.Row, e.cur.Col
	old := e.lines[row].cells
	head := append([]cell(nil), old[:col]...)
	tail := old[col:]
	if len(f) == 1 {
		cells := append(head, f[0]...)
		e.cur.Col = len(cells)
		e.lines[row] = line{cells: append(cells, tail...)}
		return
	}
	nl := make([]line, 0, len(f))
	nl = append(nl, line{cells: append(head, f[0]...)})
	for _, mid := range f[1 : len(f)-1] {
		nl = append(nl, line{cells: append([]cell(nil), mid...)})
	}
	last := append([]cell(nil), f[len(f)-1]...)
	e.cur = Pos{row + len(f) - 1, len(last)}
	nl = append(nl, line{cells: append(last, tail...)})
	lines := make([]line, 0, len(e.lines)+len(f)-1)
	lines = append(lines, e.lines[:row]...)
	lines = append(lines, nl...)
	lines = append(lines, e.lines[row+1:]...)
	e.lines = lines
}

// copyRange returns the content between two positions.
func (e *Editor) copyRange(from, to Pos) fragment {
	from, to = order(from, to)
	if from.Row == to.Row {
		return fragment{append([]cell(nil), e.lines[from.Row].cells[from.Col:to.Col]...)}
	}
	f := fragment{append([]cell(nil), e.lines[from.Row].cells[from.Col:]...)}
	for r := from.Row + 1; r < to.Row; r++ {
		f = append(f, append([]cell(nil), e.lines[r].cells...))
	}
	return append(f, append([]cell(nil), e.lines[to.Row].cells[:to.Col]...))
}

// deleteRange removes the content between two positions and returns it. The
// cursor is not moved; callers set it.
func (e *Editor) deleteRange(from, to Pos) fragment {
	from, to = order(from, to)
	f := e.copyRange(from, to)
	head := e.lines[from.Row].cells[:from.Col]
	tail := e.lines[to.Row].cells[to.Col:]
	merged := make([]cell, 0, len(head)+len(tail))
	merged = append(append(merged, head...), tail...)
	lines := make([]line, 0, len(e.lines)-(to.Row-from.Row))
	lines = append(lines, e.lines[:from.Row]...)
	lines = append(lines, line{cells: merged})
	lines = append(lines, e.lines[to.Row+1:]...)
	e.lines = lines
	return f
}

// afterEdit finishes an edit: it clears the ghost text (the host recomputes
// it) and records where the edit ended for undo coalescing.
func (e *Editor) afterEdit() {
	e.lastPos = e.cur
	e.lastEditAt = e.now()
	e.goalX = -1
	e.ghost = ""
	e.lastCmd = cmdNone
	if e.group > 0 {
		e.groupDirty = true
	}
	e.touch()
}

// ---- undo ----

type editKind int

const (
	editNone editKind = iota
	editInsert
	editDeleteBack
	editDeleteFwd
	editOther
)

type snapshot struct {
	lines [][]cell
	cur   Pos
}

const (
	maxUndo       = 200
	coalesceAfter = time.Second
)

func (e *Editor) snap() snapshot {
	s := snapshot{lines: make([][]cell, len(e.lines)), cur: e.cur}
	for i, l := range e.lines {
		s.lines[i] = l.cells
	}
	return s
}

func (e *Editor) restore(s snapshot) {
	e.lines = make([]line, len(s.lines))
	for i, cs := range s.lines {
		e.lines[i] = line{cells: cs}
	}
	e.cur = e.clamp(s.cur)
}

// checkpoint records the state before an edit of kind k. Runs of the same
// kind of small edit (typing, backspacing) at the same spot within a second
// coalesce into one undo step.
func (e *Editor) checkpoint(k editKind) {
	if e.group > 0 {
		return
	}
	now := e.now()
	if k != editOther && k == e.lastEdit && e.cur == e.lastPos && now.Sub(e.lastEditAt) < coalesceAfter {
		return
	}
	e.pushUndo()
	e.lastEdit = k
}

func (e *Editor) pushUndo() {
	// Lines share cell slices with the snapshot, which is safe because every
	// edit builds new slices (see insertFragment and deleteRange).
	e.undo = append(e.undo, e.snap())
	if len(e.undo) > maxUndo {
		e.undo = e.undo[len(e.undo)-maxUndo:]
	}
	e.redo = nil
}

// BeginGroup starts an undo group: every edit until the matching EndGroup
// undoes as one step (vim uses this for a whole insert session).
func (e *Editor) BeginGroup() {
	if e.group == 0 {
		e.pushUndo()
		e.groupDirty = false
		e.groupBase = len(e.undo)
	}
	e.group++
}

// EndGroup closes an undo group. A group without edits leaves no undo step.
func (e *Editor) EndGroup() {
	if e.group == 0 {
		return
	}
	e.group--
	if e.group == 0 {
		if !e.groupDirty && len(e.undo) == e.groupBase && e.groupBase > 0 {
			e.undo = e.undo[:len(e.undo)-1]
		}
		e.lastEdit = editNone
	}
}

// Undo reverts the last edit (chat:undo). It reports false when there is
// nothing to undo.
func (e *Editor) Undo() bool {
	if len(e.undo) == 0 {
		return false
	}
	e.redo = append(e.redo, e.snap())
	s := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.restore(s)
	e.lastEdit = editNone
	e.lastCmd = cmdNone
	e.goalX = -1
	e.touch()
	return true
}

// Redo reapplies the last undone edit.
func (e *Editor) Redo() bool {
	if len(e.redo) == 0 {
		return false
	}
	e.undo = append(e.undo, e.snap())
	s := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.restore(s)
	e.lastEdit = editNone
	e.lastCmd = cmdNone
	e.goalX = -1
	e.touch()
	return true
}

// CanUndo reports whether Undo would do anything.
func (e *Editor) CanUndo() bool { return len(e.undo) > 0 }

// ---- kill ring ----

type cmdKind int

const (
	cmdNone cmdKind = iota
	cmdKill
	cmdYank
)

const killRingMax = 60

type killRing struct {
	items []fragment // oldest first
	idx   int        // yank position, counted back from the newest
}

func (k *killRing) push(f fragment) {
	k.items = append(k.items, f)
	if len(k.items) > killRingMax {
		k.items = k.items[len(k.items)-killRingMax:]
	}
	k.idx = 0
}

// extend adds f to the newest entry (consecutive kills accumulate).
func (k *killRing) extend(f fragment, backward bool) {
	if len(k.items) == 0 {
		k.push(f)
		return
	}
	n := len(k.items) - 1
	if backward {
		k.items[n] = concat(f, k.items[n])
	} else {
		k.items[n] = concat(k.items[n], f)
	}
	k.idx = 0
}

func (k *killRing) current() (fragment, bool) {
	if len(k.items) == 0 {
		return nil, false
	}
	return k.items[len(k.items)-1-k.idx], true
}

func (k *killRing) rotate() (fragment, bool) {
	if len(k.items) == 0 {
		return nil, false
	}
	k.idx = (k.idx + 1) % len(k.items)
	return k.current()
}

// Killed returns the kill ring as text, newest first.
func (e *Editor) Killed() []string {
	out := make([]string, 0, len(e.kill.items))
	for i := len(e.kill.items) - 1; i >= 0; i-- {
		out = append(out, e.kill.items[i].String())
	}
	return out
}

func (e *Editor) killRange(from, to Pos, backward bool) {
	from, to = order(from, to)
	if from == to {
		return
	}
	accumulate := e.lastCmd == cmdKill
	e.checkpoint(editOther)
	f := e.deleteRange(from, to)
	e.cur = from
	if accumulate {
		e.kill.extend(f, backward)
	} else {
		e.kill.push(f)
	}
	e.afterEdit()
	e.lastCmd = cmdKill
}

// ---- editing operations ----

// InsertString inserts text at the cursor. Newlines split lines; control
// characters are dropped. Typing bursts coalesce into one undo step, broken
// at word starts.
func (e *Editor) InsertString(s string) {
	if s == "" {
		return
	}
	f := fragment(splitLines(s))
	if f.empty() && len(f) == 1 {
		return
	}
	kind := editInsert
	if len(f) > 1 {
		kind = editOther
	} else if e.lastEdit == editInsert && e.cur.Col > 0 && !f[0][0].isSpace() &&
		e.lines[e.cur.Row].cells[e.cur.Col-1].isSpace() {
		e.lastEdit = editNone // a new word starts a new undo step
	}
	e.checkpoint(kind)
	e.insertFragment(f)
	e.afterEdit()
}

// Newline inserts a line break (chat:newline, shift+enter, alt+enter).
func (e *Editor) Newline() {
	e.checkpoint(editOther)
	e.insertFragment(fragment{nil, nil})
	e.afterEdit()
}

// BackslashNewline handles Enter after a trailing backslash: when the cell
// before the cursor is "\", it is replaced by a newline and true is
// returned. Terminals without shift+enter rely on this.
func (e *Editor) BackslashNewline() bool {
	c := e.cur.Col
	l := e.lines[e.cur.Row].cells
	if c == 0 || l[c-1].chip != nil || l[c-1].g != `\` {
		return false
	}
	e.checkpoint(editOther)
	e.deleteRange(Pos{e.cur.Row, c - 1}, e.cur)
	e.cur.Col--
	e.insertFragment(fragment{nil, nil})
	e.afterEdit()
	return true
}

// DeleteBackward deletes the cell (or line break) before the cursor.
func (e *Editor) DeleteBackward() {
	from, ok := e.prev(e.cur)
	if !ok {
		return
	}
	e.checkpoint(editDeleteBack)
	e.deleteRange(from, e.cur)
	e.cur = from
	e.afterEdit()
}

// DeleteForward deletes the cell (or line break) under the cursor.
func (e *Editor) DeleteForward() {
	to, ok := e.next(e.cur)
	if !ok {
		return
	}
	e.checkpoint(editDeleteFwd)
	e.deleteRange(e.cur, to)
	e.afterEdit()
}

// KillLineEnd kills to the end of the line, or the line break when already
// there (ctrl+k).
func (e *Editor) KillLineEnd() {
	end := Pos{e.cur.Row, len(e.lines[e.cur.Row].cells)}
	if e.cur == end {
		if n, ok := e.next(e.cur); ok {
			end = n
		}
	}
	e.killRange(e.cur, end, false)
}

// KillLineStart kills to the start of the line, or the preceding line break
// when already there (ctrl+u).
func (e *Editor) KillLineStart() {
	start := Pos{e.cur.Row, 0}
	if e.cur == start {
		if p, ok := e.prev(e.cur); ok {
			start = p
		}
	}
	e.killRange(start, e.cur, true)
}

// KillWordBackward kills back to the previous whitespace (ctrl+w).
func (e *Editor) KillWordBackward() {
	e.killRange(e.spaceWordStartBackward(e.cur), e.cur, true)
}

// KillWordBackwardAlnum kills back to the start of the previous word,
// stopping at punctuation (alt+backspace).
func (e *Editor) KillWordBackwardAlnum() {
	e.killRange(e.wordStartBackward(e.cur), e.cur, true)
}

// KillWordForward kills to the end of the next word (alt+d).
func (e *Editor) KillWordForward() {
	e.killRange(e.cur, e.wordEndForward(e.cur), false)
}

// Yank inserts the newest kill-ring entry (ctrl+y).
func (e *Editor) Yank() bool {
	e.kill.idx = 0
	f, ok := e.kill.current()
	if !ok {
		return false
	}
	e.checkpoint(editOther)
	e.yankFrom = e.cur
	e.insertFragment(f)
	e.afterEdit()
	e.lastCmd = cmdYank
	return true
}

// YankPop replaces the text just yanked with the next older kill-ring entry
// (alt+y). It only works right after Yank or YankPop.
func (e *Editor) YankPop() bool {
	if e.lastCmd != cmdYank {
		return false
	}
	f, ok := e.kill.rotate()
	if !ok {
		return false
	}
	e.checkpoint(editOther)
	e.deleteRange(e.yankFrom, e.cur)
	e.cur = e.yankFrom
	e.insertFragment(f)
	e.afterEdit()
	e.lastCmd = cmdYank
	return true
}

// TransposeChars swaps the two cells around the cursor.
func (e *Editor) TransposeChars() {
	l := e.lines[e.cur.Row].cells
	c := e.cur.Col
	if len(l) < 2 || c == 0 {
		return
	}
	if c == len(l) {
		c--
	}
	e.checkpoint(editOther)
	cells := append([]cell(nil), l...)
	cells[c-1], cells[c] = cells[c], cells[c-1]
	e.lines[e.cur.Row] = line{cells: cells}
	e.cur.Col = c + 1
	e.afterEdit()
}

// ---- ghost text ----

// SetGhost sets dim suggestion text shown after the cursor when it is at the
// end of the buffer (prompt suggestions, command completion). Any edit
// clears it.
func (e *Editor) SetGhost(s string) {
	s = strings.ReplaceAll(normalizeNewlines(s), "\n", " ")
	if s != e.ghost {
		e.ghost = s
		e.touch()
	}
}

// Ghost returns the current ghost text.
func (e *Editor) Ghost() string { return e.ghost }

// GhostVisible reports whether the ghost text is showing (cursor at the end).
func (e *Editor) GhostVisible() bool { return e.ghost != "" && e.AtEnd() }

// AcceptGhost inserts the ghost text when it is visible and reports whether
// it did (tab, or right arrow at the end).
func (e *Editor) AcceptGhost() bool {
	if !e.GhostVisible() {
		return false
	}
	g := e.ghost
	e.InsertString(g)
	return true
}
