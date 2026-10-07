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

// gutterGlyphs open a transcript line: assistant text and tool calls, tool results,
// prompts, thinking.
var gutterGlyphs = map[string]bool{"⏺": true, "⎿": true, "❯": true, "✻": true, "●": true}

func blank(c string) bool { return c == " " || c == " " }

// gutter returns how many leading cells of a line are gutter: blanks, and when a glyph
// follows them (and a blank or the end follows it), the glyph and the blanks after it.
func gutter(cs []string) (n int, glyph bool) {
	for n < len(cs) && blank(cs[n]) {
		n++
	}
	if n < len(cs) && gutterGlyphs[cs[n]] {
		g := n + 1
		for g < len(cs) && cs[g] == "" { // the right half of a wide glyph
			g++
		}
		if g == len(cs) || blank(cs[g]) {
			for n = g; n < len(cs) && blank(cs[n]); n++ {
			}
			return n, true
		}
	}
	return n, false
}

// SourceText returns the selected text as source, for quoting it: like Text, but
// without the transcript's gutters (a line's glyph and the hanging indent under it) and
// with word-wrapped lines joined with a space. blockStart returns the first line of the
// block holding a line (-1 outside every block); gutters don't carry across blocks.
func SourceText(lines LineSource, s Selection, width int, blockStart func(int) int) string {
	if s.Empty() {
		return ""
	}
	a, b := s.Range()
	type row struct {
		cs     []string
		glyph  bool // the line opens with a glyph
		start  bool // the line opens a block
		seg    string
		filled int // cells up to the last non-blank one
	}
	var rows []row
	hang := 0 // the gutter under the block's last glyph line
	first := a.Line
	if bs := blockStart(a.Line); bs >= 0 && bs < a.Line {
		first = bs
	}
	for l := first; l <= b.Line; l++ {
		cs := cells(lines(l))
		bs := blockStart(l)
		r := row{cs: cs, start: bs == l || bs < 0}
		if r.start {
			hang = 0
		}
		n, glyph := gutter(cs)
		strip := min(n, hang)
		if glyph {
			hang, strip, r.glyph = n, n, true
		}
		if l < a.Line {
			continue
		}
		from, to := strip, len(cs)
		if l == a.Line {
			from = max(from, min(a.Col, len(cs)))
		}
		if l == b.Line {
			to = min(b.Col, len(cs))
		}
		r.seg = strings.TrimRight(strings.Join(cs[min(from, len(cs)):max(from, to)], ""), "  ")
		for r.filled = len(cs); r.filled > 0 && blank(cs[r.filled-1]); r.filled-- {
		}
		rows = append(rows, r)
	}
	var out strings.Builder
	for i, r := range rows {
		seg := r.seg
		if i > 0 {
			p := rows[i-1]
			switch {
			case r.start || r.glyph || strings.TrimSpace(seg) == "" || p.seg == "":
				out.WriteByte('\n')
			case len(p.cs) >= width: // a word broken at the edge
				seg = strings.TrimLeft(seg, " ")
			case p.filled+1+firstWord(seg) > width: // the next word didn't fit
				out.WriteByte(' ')
				seg = strings.TrimLeft(seg, " ")
			default:
				out.WriteByte('\n')
			}
		}
		out.WriteString(seg)
	}
	return out.String()
}

// firstWord is the cell width of s's first word.
func firstWord(s string) int {
	s = strings.TrimLeft(s, " ")
	if i := strings.IndexAny(s, "  "); i >= 0 {
		s = s[:i]
	}
	return ansi.StringWidth(s)
}
