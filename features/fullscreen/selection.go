package fullscreen

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Pos is a position in the document: a line and a cell column.
type Pos struct{ Line, Col int }

// Less orders positions.
func (p Pos) Less(q Pos) bool { return p.Line < q.Line || p.Line == q.Line && p.Col < q.Col }

// Selection is a range of document cells between an anchor and a head (the end that
// moves when extending). The range is inclusive of the anchor and exclusive of the
// head's column after normalization.
type Selection struct {
	Anchor, Head Pos
	Active       bool
}

// Range returns the ordered [start, end) of the selection.
func (s Selection) Range() (start, end Pos) {
	if s.Head.Less(s.Anchor) {
		return s.Head, s.Anchor
	}
	return s.Anchor, s.Head
}

// Empty reports whether nothing is selected.
func (s Selection) Empty() bool {
	a, b := s.Range()
	return !s.Active || a == b
}

// Contains reports whether the cell at p is selected.
func (s Selection) Contains(p Pos) bool {
	if s.Empty() {
		return false
	}
	a, b := s.Range()
	return !p.Less(a) && p.Less(b)
}

// LineSource returns a document line's styled text ("" past the end).
type LineSource func(line int) string

// cells splits a styled line into plain cells: a wide character takes two cells (the
// character, then ""), and zero-width runes (combining marks, joiners) join the cell
// before them.
func cells(styled string) []string {
	plain := ansi.Strip(styled)
	var out []string
	last := -1
	for _, r := range plain {
		s := string(r)
		w := ansi.StringWidth(s)
		if w == 0 && last >= 0 {
			out[last] += s
			continue
		}
		out = append(out, s)
		last = len(out) - 1
		for ; w > 1; w-- {
			out = append(out, "")
		}
	}
	return out
}

// WordAt returns the selection of the word under p (double-click): a run of letters,
// digits and _-./:@ (so paths and URLs select whole), or the single cell otherwise.
func WordAt(lines LineSource, p Pos) Selection {
	cs := cells(lines(p.Line))
	if p.Col < 0 || p.Col >= len(cs) {
		return Selection{Anchor: p, Head: p, Active: true}
	}
	isWord := func(c string) bool {
		if c == "" {
			return true // the right half of a wide grapheme
		}
		for _, r := range c {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-./:@~", r) {
				return true
			}
		}
		return false
	}
	if !isWord(cs[p.Col]) {
		return Selection{Anchor: p, Head: Pos{p.Line, p.Col + 1}, Active: true}
	}
	from, to := p.Col, p.Col
	for from > 0 && isWord(cs[from-1]) {
		from--
	}
	for to < len(cs) && isWord(cs[to]) {
		to++
	}
	return Selection{Anchor: Pos{p.Line, from}, Head: Pos{p.Line, to}, Active: true}
}

// LineAt selects a whole line (triple-click).
func LineAt(lines LineSource, line int) Selection {
	return Selection{Anchor: Pos{line, 0}, Head: Pos{line, len(cells(lines(line)))}, Active: true}
}

// Text returns the selected plain text. Lines that filled the viewport width are taken
// as soft-wrapped and joined to the next one without a newline; trailing spaces are
// trimmed per line.
func Text(lines LineSource, s Selection, width int) string {
	if s.Empty() {
		return ""
	}
	a, b := s.Range()
	var out strings.Builder
	for l := a.Line; l <= b.Line; l++ {
		cs := cells(lines(l))
		from, to := 0, len(cs)
		if l == a.Line {
			from = min(a.Col, len(cs))
		}
		if l == b.Line {
			to = min(b.Col, len(cs))
		}
		seg := strings.TrimRight(strings.Join(cs[from:max(from, to)], ""), " ")
		out.WriteString(seg)
		if l < b.Line && len(cs) < width {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// Highlight renders a line with the selected cells drawn by mark (for example reverse
// video). Unselected cells keep their plain text; selected lines lose their styling,
// which keeps the highlight legible on every theme.
func Highlight(styled string, line int, s Selection, mark func(string) string) string {
	if s.Empty() {
		return styled
	}
	a, b := s.Range()
	if line < a.Line || line > b.Line {
		return styled
	}
	cs := cells(styled)
	from, to := 0, len(cs)
	if line == a.Line {
		from = min(a.Col, len(cs))
	}
	if line == b.Line {
		to = min(b.Col, len(cs))
	}
	if from >= to {
		return styled
	}
	return strings.Join(cs[:from], "") + mark(strings.Join(cs[from:to], "")) + strings.Join(cs[to:], "")
}
