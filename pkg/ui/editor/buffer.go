package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Pos is a buffer position. Row is the logical line and Col the cell index
// within it, from 0 to the line length inclusive. A chip is one cell.
type Pos struct{ Row, Col int }

// Less reports whether p comes before q in reading order.
func (p Pos) Less(q Pos) bool {
	return p.Row < q.Row || (p.Row == q.Row && p.Col < q.Col)
}

// order returns p and q sorted in reading order.
func order(p, q Pos) (Pos, Pos) {
	if q.Less(p) {
		return q, p
	}
	return p, q
}

// tabWidth is the display width of a tab cell. Tabs are kept in the text and
// drawn as spaces so the layout never depends on terminal tab stops.
const tabWidth = 4

// cell is one cursor unit: a grapheme cluster or an atomic chip.
type cell struct {
	g    string // grapheme cluster; empty for chips
	w    int    // display width in terminal cells
	chip *Chip
}

func (c cell) isChip() bool { return c.chip != nil }

// isSpace reports whether the cell is whitespace. Chips are never whitespace.
func (c cell) isSpace() bool {
	if c.chip != nil || c.g == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(c.g)
	return unicode.IsSpace(r)
}

// isWord reports whether the cell is a word character (letters, digits, _).
func (c cell) isWord() bool {
	if c.chip != nil || c.g == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(c.g)
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

// text is the cell's content as it appears in the editor (chips as labels).
func (c cell) text() string {
	if c.chip != nil {
		return c.chip.Label()
	}
	return c.g
}

func chipCell(ch *Chip) cell {
	return cell{chip: ch, w: ansi.StringWidth(ch.Label())}
}

// toCells splits text without newlines into grapheme cells. Control
// characters other than tab are dropped; callers sanitize pasted text first.
func toCells(s string) []cell {
	if s == "" {
		return nil
	}
	out := make([]cell, 0, len(s))
	for len(s) > 0 {
		g, w := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if g == "" {
			// Invalid UTF-8: take one byte so we always make progress.
			g = s[:1]
		}
		s = s[len(g):]
		if g == "\t" {
			out = append(out, cell{g: g, w: tabWidth})
			continue
		}
		r, _ := utf8.DecodeRuneInString(g)
		if r == utf8.RuneError && len(g) == 1 {
			continue
		}
		if len(g) == 1 && (r < 0x20 || r == 0x7f) {
			continue
		}
		if r >= 0x80 && r < 0xa0 && utf8.RuneCountInString(g) == 1 {
			continue
		}
		out = append(out, cell{g: g, w: w})
	}
	return out
}

// splitLines splits text into logical lines of cells. "\r\n" and "\r" count
// as newlines.
func splitLines(s string) [][]cell {
	s = normalizeNewlines(s)
	parts := strings.Split(s, "\n")
	out := make([][]cell, len(parts))
	for i, p := range parts {
		out[i] = toCells(p)
	}
	return out
}

func normalizeNewlines(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// line is a logical line plus its cached soft-wrap layout.
type line struct {
	cells []cell
	rows  []span // layout for rowsW; recomputed when rowsW != width
	rowsW int
}

func (l *line) invalidate() { l.rowsW = 0 }

// span is one visual row of a logical line: cells [start, end) of width w.
type span struct{ start, end, w int }

// layout returns the visual rows of l at the given width, using the cache.
func (l *line) layout(width int) []span {
	if l.rowsW == width && l.rows != nil {
		return l.rows
	}
	l.rows = wrapCells(l.cells, width)
	l.rowsW = width
	return l.rows
}

// wrapCells word-wraps cells to width. Rows break after whitespace when
// possible and hard-break long words. The last row of a line always keeps
// one free column for the end-of-line cursor, so a line that exactly fills
// its last row gets an extra empty row. That keeps the height stable while
// the cursor moves.
func wrapCells(cells []cell, width int) []span {
	if width < 1 {
		width = 1
	}
	var rows []span
	start, w := 0, 0
	brk, brkW := -1, 0 // cell index after the last whitespace, and row width up to it
	for i := 0; i < len(cells); {
		cw := cells[i].w
		if w+cw > width && i > start {
			if brk > start {
				rows = append(rows, span{start, brk, brkW})
				start, w, brk = brk, w-brkW, -1
				continue // the carried-over word may still not fit
			}
			rows = append(rows, span{start, i, w})
			start, w, brk = i, 0, -1
		}
		w += cw
		if cells[i].isSpace() {
			brk, brkW = i+1, w
		}
		i++
	}
	rows = append(rows, span{start, len(cells), w})
	if last := rows[len(rows)-1]; last.w >= width && last.end > last.start {
		rows = append(rows, span{len(cells), len(cells), 0})
	}
	return rows
}

// cellsText joins cells as editor text (chips as labels).
func cellsText(cs []cell) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.text())
	}
	return b.String()
}

// cellsWidth sums display widths.
func cellsWidth(cs []cell) int {
	w := 0
	for _, c := range cs {
		w += c.w
	}
	return w
}

// fragment is a piece of buffer content spanning one or more lines; the lines
// are joined by newlines. It is the unit of the kill ring, the undo stack and
// vim registers, and it keeps chips intact.
type fragment [][]cell

func (f fragment) empty() bool { return len(f) == 0 || (len(f) == 1 && len(f[0]) == 0) }

// String renders the fragment as editor text (chips as labels).
func (f fragment) String() string {
	parts := make([]string, len(f))
	for i, l := range f {
		parts[i] = cellsText(l)
	}
	return strings.Join(parts, "\n")
}

func (f fragment) clone() fragment {
	out := make(fragment, len(f))
	for i, l := range f {
		out[i] = append([]cell(nil), l...)
	}
	return out
}

// concat joins two fragments: the last line of a continues with the first
// line of b.
func concat(a, b fragment) fragment {
	if len(a) == 0 {
		return b.clone()
	}
	if len(b) == 0 {
		return a.clone()
	}
	out := a.clone()
	last := len(out) - 1
	out[last] = append(out[last], b[0]...)
	for _, l := range b[1:] {
		out = append(out, append([]cell(nil), l...))
	}
	return out
}
