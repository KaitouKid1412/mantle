package vim

import (
	"unicode"
	"unicode/utf8"
)

// character classes for word motions
const (
	clsBlank = iota
	clsWord
	clsPunct
	clsChip
)

func isBlank(g string) bool {
	if g == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(g)
	return unicode.IsSpace(r)
}

func classify(g string, big bool) int {
	switch {
	case isBlank(g):
		return clsBlank
	case big:
		return clsWord
	case g == ChipGrapheme:
		return clsChip
	}
	r, _ := utf8.DecodeRuneInString(g)
	if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) {
		return clsWord
	}
	return clsPunct
}

// class of the cell at p; line ends count as blank.
func (m *Machine) class(p Pos, big bool) int {
	if p.Col >= m.lineLen(p.Row) {
		return clsBlank
	}
	return classify(m.t.Grapheme(p.Row, p.Col), big)
}

// fwd steps one position forward; a line end is a position of its own.
func (m *Machine) fwd(p Pos) (Pos, bool) {
	if p.Col < m.lineLen(p.Row) {
		return Pos{p.Row, p.Col + 1}, true
	}
	if p.Row < m.last() {
		return Pos{p.Row + 1, 0}, true
	}
	return p, false
}

func (m *Machine) back(p Pos) (Pos, bool) {
	if p.Col > 0 {
		return Pos{p.Row, p.Col - 1}, true
	}
	if p.Row > 0 {
		return Pos{p.Row - 1, m.lineLen(p.Row - 1)}, true
	}
	return p, false
}

func (m *Machine) emptyLine(r int) bool { return m.lineLen(r) == 0 }

// wordStart is "w": the start of the next word; an empty line is a word.
func (m *Machine) wordStart(p Pos, big bool) Pos {
	if c := m.class(p, big); c != clsBlank {
		for p.Col < m.lineLen(p.Row) && m.class(p, big) == c {
			p.Col++
		}
	}
	for {
		if p.Col >= m.lineLen(p.Row) {
			if p.Row == m.last() {
				return p
			}
			p = Pos{p.Row + 1, 0}
			if m.emptyLine(p.Row) {
				return p
			}
			continue
		}
		if m.class(p, big) != clsBlank {
			return p
		}
		p.Col++
	}
}

// wordEnd is "e": the end of the current or next word.
func (m *Machine) wordEnd(p Pos, big bool) Pos {
	q, ok := m.fwd(p)
	if !ok {
		return p
	}
	p = q
	for m.class(p, big) == clsBlank {
		q, ok := m.fwd(p)
		if !ok {
			return p
		}
		p = q
	}
	c := m.class(p, big)
	for p.Col+1 < m.lineLen(p.Row) && m.class(Pos{p.Row, p.Col + 1}, big) == c {
		p.Col++
	}
	return p
}

// wordStartBack is "b".
func (m *Machine) wordStartBack(p Pos, big bool) Pos {
	q, ok := m.back(p)
	if !ok {
		return p
	}
	p = q
	for m.class(p, big) == clsBlank {
		if m.emptyLine(p.Row) {
			return p
		}
		q, ok := m.back(p)
		if !ok {
			return p
		}
		p = q
	}
	c := m.class(p, big)
	for p.Col > 0 && m.class(Pos{p.Row, p.Col - 1}, big) == c {
		p.Col--
	}
	return p
}

// wordEndBack is "ge": the end of the previous word.
func (m *Machine) wordEndBack(p Pos, big bool) Pos {
	if c := m.class(p, big); c != clsBlank {
		for m.class(p, big) == c {
			q, ok := m.back(p)
			if !ok {
				return q
			}
			p = q
		}
	} else {
		q, ok := m.back(p)
		if !ok {
			return p
		}
		p = q
	}
	for m.class(p, big) == clsBlank {
		if m.emptyLine(p.Row) {
			return p
		}
		q, ok := m.back(p)
		if !ok {
			return p
		}
		p = q
	}
	return p
}

type findSpec struct {
	kind string // f F t T
	ch   string
}

// find is f/F/t/T on the current line.
func (m *Machine) find(p Pos, f findSpec, count int, repeat bool) (Pos, bool) {
	n := m.lineLen(p.Row)
	c := p.Col
	for i := 0; i < count; i++ {
		switch f.kind {
		case "f", "t":
			start := c + 1
			if f.kind == "t" && (repeat || i > 0) {
				start = c + 2
			}
			j := -1
			for x := start; x < n; x++ {
				if m.t.Grapheme(p.Row, x) == f.ch {
					j = x
					break
				}
			}
			if j < 0 {
				return p, false
			}
			c = j
			if f.kind == "t" {
				c = j - 1
			}
		default:
			start := c - 1
			if f.kind == "T" && (repeat || i > 0) {
				start = c - 2
			}
			j := -1
			for x := start; x >= 0; x-- {
				if m.t.Grapheme(p.Row, x) == f.ch {
					j = x
					break
				}
			}
			if j < 0 {
				return p, false
			}
			c = j
			if f.kind == "T" {
				c = j + 1
			}
		}
	}
	return Pos{p.Row, c}, true
}

var bracketPairs = map[string]string{"(": ")", "[": "]", "{": "}", "<": ">"}
var closeToOpen = map[string]string{")": "(", "]": "[", "}": "{", ">": "<"}

// matchBracket is "%": from the first bracket at or after the cursor on its
// line, jump to its partner.
func (m *Machine) matchBracket(p Pos) (Pos, bool) {
	n := m.lineLen(p.Row)
	for c := p.Col; c < n; c++ {
		g := m.t.Grapheme(p.Row, c)
		if g == "<" || g == ">" {
			continue
		}
		if cl, ok := bracketPairs[g]; ok {
			return m.scanClose(Pos{p.Row, c}, g, cl)
		}
		if op, ok := closeToOpen[g]; ok {
			return m.scanOpen(Pos{p.Row, c}, op, g)
		}
	}
	return p, false
}

// scanClose finds the bracket closing the one at p.
func (m *Machine) scanClose(p Pos, open, close string) (Pos, bool) {
	depth := 0
	for q, ok := p, true; ok; q, ok = m.fwd(q) {
		if q.Col >= m.lineLen(q.Row) {
			continue
		}
		switch m.t.Grapheme(q.Row, q.Col) {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return q, true
			}
		}
	}
	return p, false
}

// scanOpen finds the bracket opening the one at p.
func (m *Machine) scanOpen(p Pos, open, close string) (Pos, bool) {
	depth := 0
	for q, ok := p, true; ok; q, ok = m.back(q) {
		if q.Col >= m.lineLen(q.Row) {
			continue
		}
		switch m.t.Grapheme(q.Row, q.Col) {
		case close:
			depth++
		case open:
			depth--
			if depth == 0 {
				return q, true
			}
		}
	}
	return p, false
}

func (m *Machine) blankLine(r int) bool {
	for c := 0; c < m.lineLen(r); c++ {
		if !isBlank(m.t.Grapheme(r, c)) {
			return false
		}
	}
	return true
}

// paragraph is "}" (dir 1) or "{" (dir -1): the next blank line past the
// current paragraph.
func (m *Machine) paragraph(p Pos, dir, count int) Pos {
	r, last := p.Row, m.last()
	for i := 0; i < count; i++ {
		for r >= 0 && r <= last && m.blankLine(r) {
			r += dir
		}
		for r >= 0 && r <= last && !m.blankLine(r) {
			r += dir
		}
		if r < 0 {
			return Pos{0, 0}
		}
		if r > last {
			return Pos{last, m.lineLen(last)}
		}
	}
	return Pos{r, 0}
}

type motionKind int

const (
	exclusive motionKind = iota
	inclusive
	linewise
)

// motion computes where a motion goes from p. ok is false when it fails
// (vim beeps and does nothing).
func (m *Machine) motion(c command, p Pos, withOp bool) (Pos, motionKind, bool) {
	n := max(c.count, 1)
	switch c.name {
	case "h":
		if p.Col == 0 {
			return p, exclusive, withOp
		}
		return Pos{p.Row, max(p.Col-n, 0)}, exclusive, true
	case "l":
		ln := m.lineLen(p.Row)
		limit := ln - 1
		if withOp {
			limit = ln
		}
		if p.Col >= limit {
			return p, exclusive, withOp && ln > 0
		}
		return Pos{p.Row, min(p.Col+n, limit)}, exclusive, true
	case "j", "k":
		d := n
		if c.name == "k" {
			d = -n
		}
		r := p.Row + d
		if r < 0 || r > m.last() {
			return p, linewise, false
		}
		return Pos{r, m.goalCol(r, p.Col)}, linewise, true
	case "w", "W":
		q := p
		for i := 0; i < n; i++ {
			q = m.wordStart(q, c.name == "W")
		}
		if withOp && q.Row > p.Row {
			// The last word on a line ends the operated text at the line
			// end, not at the next line's first word.
			q = Pos{q.Row - 1, m.lineLen(q.Row - 1)}
		}
		return q, exclusive, q != p || withOp
	case "b", "B":
		q := p
		for i := 0; i < n; i++ {
			q = m.wordStartBack(q, c.name == "B")
		}
		return q, exclusive, q != p
	case "e", "E":
		q := p
		for i := 0; i < n; i++ {
			q = m.wordEnd(q, c.name == "E")
		}
		return q, inclusive, true
	case "ge", "gE":
		q := p
		for i := 0; i < n; i++ {
			q = m.wordEndBack(q, c.name == "gE")
		}
		return q, inclusive, true
	case "0":
		return Pos{p.Row, 0}, exclusive, true
	case "^":
		return Pos{p.Row, m.firstNonBlank(p.Row)}, exclusive, true
	case "g_":
		r := min(p.Row+n-1, m.last())
		c := m.lineLen(r) - 1
		for c > 0 && isBlank(m.t.Grapheme(r, c)) {
			c--
		}
		return Pos{r, max(c, 0)}, inclusive, true
	case "_":
		r := min(p.Row+n-1, m.last())
		return Pos{r, m.firstNonBlank(r)}, linewise, true
	case "$":
		r := min(p.Row+n-1, m.last())
		return Pos{r, max(m.lineLen(r)-1, 0)}, inclusive, true
	case "G", "gg":
		r := m.last()
		if c.name == "gg" {
			r = 0
		}
		if c.count > 0 {
			r = min(c.count-1, m.last())
		}
		return Pos{r, m.firstNonBlank(r)}, linewise, true
	case "f", "F", "t", "T":
		f := findSpec{kind: c.name, ch: c.arg}
		m.lastFind = f
		q, ok := m.find(p, f, n, false)
		kind := inclusive
		if c.name == "F" || c.name == "T" {
			kind = exclusive
		}
		return q, kind, ok
	case ";", ",":
		f := m.lastFind
		if f.kind == "" {
			return p, exclusive, false
		}
		if c.name == "," {
			f.kind = map[string]string{"f": "F", "F": "f", "t": "T", "T": "t"}[f.kind]
		}
		q, ok := m.find(p, f, n, true)
		kind := inclusive
		if f.kind == "F" || f.kind == "T" {
			kind = exclusive
		}
		return q, kind, ok
	case "%":
		q, ok := m.matchBracket(p)
		return q, inclusive, ok
	case "}":
		return m.paragraph(p, 1, n), exclusive, true
	case "{":
		return m.paragraph(p, -1, n), exclusive, true
	}
	return p, exclusive, false
}

// goalCol picks the column on row r for vertical moves.
func (m *Machine) goalCol(r, cur int) int {
	g := m.goal
	if g < 0 {
		g = cur
		m.goal = cur
	}
	n := m.lineLen(r)
	if m.mode == Insert {
		return min(g, n)
	}
	return max(min(g, n-1), 0)
}

// ---- text objects ----

// textObject returns the half-open range of a text object at p.
func (m *Machine) textObject(name string, p Pos, count int) (from, to Pos, lw, ok bool) {
	inner := name[0] == 'i'
	obj := name[1:]
	n := max(count, 1)
	switch obj {
	case "w", "W":
		return m.wordObject(p, inner, obj == "W", n)
	case "\"", "'", "`":
		return m.quoteObject(p, inner, obj)
	case "(", ")", "b":
		return m.bracketObject(p, inner, "(", ")", n)
	case "[", "]":
		return m.bracketObject(p, inner, "[", "]", n)
	case "{", "}", "B":
		return m.bracketObject(p, inner, "{", "}", n)
	case "<", ">":
		return m.bracketObject(p, inner, "<", ">", n)
	case "p":
		return m.paragraphObject(p, inner, n)
	}
	return p, p, false, false
}

func (m *Machine) wordObject(p Pos, inner, big bool, n int) (Pos, Pos, bool, bool) {
	ln := m.lineLen(p.Row)
	if ln == 0 {
		return p, p, false, false
	}
	if p.Col >= ln {
		p.Col = ln - 1
	}
	runEnd := func(c int) int {
		cls := m.class(Pos{p.Row, c}, big)
		for c < ln && m.class(Pos{p.Row, c}, big) == cls {
			c++
		}
		return c
	}
	start := p.Col
	cls := m.class(p, big)
	for start > 0 && m.class(Pos{p.Row, start - 1}, big) == cls {
		start--
	}
	end := p.Col
	for i := 0; i < n && end < ln; i++ {
		end = runEnd(end)
		if !inner {
			if cls != clsBlank && end < ln && m.class(Pos{p.Row, end}, big) == clsBlank {
				end = runEnd(end)
			} else if cls == clsBlank && end < ln {
				end = runEnd(end)
			}
		} else if i+1 < n && end < ln {
			// inner with a count also counts the whitespace runs
			end = runEnd(end)
			i++
		}
	}
	if !inner && cls != clsBlank && (end == ln || m.class(Pos{p.Row, end - 1}, big) != clsBlank) {
		// No trailing whitespace: take the leading whitespace instead.
		for start > 0 && m.class(Pos{p.Row, start - 1}, big) == clsBlank {
			start--
		}
	}
	return Pos{p.Row, start}, Pos{p.Row, end}, false, true
}

func (m *Machine) quoteObject(p Pos, inner bool, q string) (Pos, Pos, bool, bool) {
	ln := m.lineLen(p.Row)
	var qs []int
	for c := 0; c < ln; c++ {
		if m.t.Grapheme(p.Row, c) == q && (c == 0 || m.t.Grapheme(p.Row, c-1) != `\`) {
			qs = append(qs, c)
		}
	}
	for i := 0; i+1 < len(qs); i += 2 {
		a, b := qs[i], qs[i+1]
		if p.Col <= b {
			if inner {
				return Pos{p.Row, a + 1}, Pos{p.Row, b}, false, true
			}
			end := b + 1
			for end < ln && isBlank(m.t.Grapheme(p.Row, end)) {
				end++
			}
			return Pos{p.Row, a}, Pos{p.Row, end}, false, true
		}
	}
	return p, p, false, false
}

func (m *Machine) bracketObject(p Pos, inner bool, open, close string, n int) (Pos, Pos, bool, bool) {
	// Find the n-th enclosing open bracket.
	var o Pos
	found := false
	depth := 0
	q, ok := p, true
	if p.Col < m.lineLen(p.Row) && m.t.Grapheme(p.Row, p.Col) == close {
		q, ok = m.back(p)
		depth = 0
	}
	for ; ok; q, ok = m.back(q) {
		if q.Col >= m.lineLen(q.Row) {
			continue
		}
		switch m.t.Grapheme(q.Row, q.Col) {
		case close:
			depth++
		case open:
			if depth == 0 {
				n--
				if n == 0 {
					o, found = q, true
				}
			} else {
				depth--
			}
		}
		if found {
			break
		}
	}
	if !found {
		return p, p, false, false
	}
	cl, ok := m.scanClose(o, open, close)
	if !ok {
		return p, p, false, false
	}
	if inner {
		from := Pos{o.Row, o.Col + 1}
		if from.Col >= m.lineLen(from.Row) && from.Row < cl.Row {
			from = Pos{from.Row + 1, 0}
		}
		return from, cl, false, true
	}
	return o, Pos{cl.Row, cl.Col + 1}, false, true
}

func (m *Machine) paragraphObject(p Pos, inner bool, n int) (Pos, Pos, bool, bool) {
	blank := m.blankLine(p.Row)
	r1, r2 := p.Row, p.Row
	for r1 > 0 && m.blankLine(r1-1) == blank {
		r1--
	}
	for r2 < m.last() && m.blankLine(r2+1) == blank {
		r2++
	}
	if !inner {
		if r2 < m.last() {
			r2++
			for r2 < m.last() && m.blankLine(r2+1) != blank {
				r2++
			}
		}
	}
	for i := 1; i < n && r2 < m.last(); i++ {
		b := m.blankLine(r2 + 1)
		r2++
		for r2 < m.last() && m.blankLine(r2+1) == b {
			r2++
		}
	}
	return Pos{r1, 0}, Pos{r2, m.lineLen(r2)}, true, true
}
