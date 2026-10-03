package eco

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Row is one list entry.
type Row struct {
	Key      string // stable identity, kept selected across refreshes
	Label    string
	Detail   string // dim text after the label
	Glyph    string // status mark before the label ("●", "✓", …)
	GlyphTok theme.Token
	Section  string // header printed before the first row of each new section
	Info     bool   // not selectable (explanations, empty states)
	Value    any    // the item behind the row
}

// List is a scrolling, selectable list with optional filtering.
type List struct {
	rows     []Row
	visible  []int // indexes into rows after filtering
	cursor   int   // index into visible
	Filter   string
	Empty    string // shown when there are no rows
	Marker   string // cursor mark, default "❯"
	MinLabel int    // pad labels to this width so details line up
}

// SetRows replaces the rows, keeping the selected key when it still exists.
func (l *List) SetRows(rows []Row) {
	key := ""
	if r, ok := l.Selected(); ok {
		key = r.Key
	}
	l.rows = rows
	l.refilter()
	if key != "" {
		for i, idx := range l.visible {
			if l.rows[idx].Key == key {
				l.cursor = i
				return
			}
		}
	}
	l.clamp()
	l.skipInfo(1)
}

// Rows returns every row.
func (l *List) Rows() []Row { return l.rows }

// Len is the number of visible rows.
func (l *List) Len() int { return len(l.visible) }

// SetFilter filters rows whose label, detail or section contain every word.
func (l *List) SetFilter(f string) {
	l.Filter = f
	l.refilter()
	l.cursor = 0
	l.skipInfo(1)
}

func (l *List) refilter() {
	l.visible = l.visible[:0]
	words := strings.Fields(strings.ToLower(l.Filter))
	for i, r := range l.rows {
		if len(words) > 0 {
			if r.Info {
				continue
			}
			hay := strings.ToLower(r.Label + " " + r.Detail + " " + r.Section)
			ok := true
			for _, w := range words {
				if !strings.Contains(hay, w) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
		}
		l.visible = append(l.visible, i)
	}
}

// Selected returns the row under the cursor.
func (l *List) Selected() (Row, bool) {
	if l.cursor < 0 || l.cursor >= len(l.visible) {
		return Row{}, false
	}
	r := l.rows[l.visible[l.cursor]]
	if r.Info {
		return Row{}, false
	}
	return r, true
}

// Select moves the cursor to the row with key.
func (l *List) Select(key string) bool {
	for i, idx := range l.visible {
		if l.rows[idx].Key == key {
			l.cursor = i
			return true
		}
	}
	return false
}

// Move moves the cursor by delta selectable rows.
func (l *List) Move(delta int) {
	if len(l.visible) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	for n := 0; n < abs(delta); n++ {
		next := l.cursor + step
		for next >= 0 && next < len(l.visible) && l.rows[l.visible[next]].Info {
			next += step
		}
		if next < 0 || next >= len(l.visible) {
			break
		}
		l.cursor = next
	}
}

// Home and End jump to the first or last selectable row.
func (l *List) Home() { l.cursor = 0; l.skipInfo(1) }
func (l *List) End()  { l.cursor = len(l.visible) - 1; l.skipInfo(-1) }

func (l *List) clamp() {
	if l.cursor >= len(l.visible) {
		l.cursor = len(l.visible) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

func (l *List) skipInfo(step int) {
	for l.cursor >= 0 && l.cursor < len(l.visible) && l.rows[l.visible[l.cursor]].Info {
		l.cursor += step
	}
	l.clamp()
	if l.cursor < len(l.visible) && l.rows[l.visible[l.cursor]].Info && step > 0 {
		l.skipInfo(-1)
	}
}

// HandleAction implements the Select context's navigation actions.
func (l *List) HandleAction(a ext.ActionID, page int) bool {
	if page < 1 {
		page = 10
	}
	switch a {
	case ext.ActSelectNext:
		l.Move(1)
	case ext.ActSelectPrevious:
		l.Move(-1)
	case ext.ActSelectPageDown:
		l.Move(page)
	case ext.ActSelectPageUp:
		l.Move(-page)
	case ext.ActSelectFirst:
		l.Home()
	case ext.ActSelectLast:
		l.End()
	default:
		return false
	}
	return true
}

// Render draws the rows into width columns and at most height rows.
func (l *List) Render(th *theme.Theme, width, height int) []string {
	if len(l.visible) == 0 {
		msg := l.Empty
		if msg == "" {
			msg = "Nothing here."
		}
		if l.Filter != "" {
			msg = fmt.Sprintf("No matches for %q.", l.Filter)
		}
		return []string{th.Paint(theme.Inactive, msg)}
	}
	// Build every line with its row index, then window around the cursor.
	type line struct {
		text string
		row  int // index into visible, -1 for section headers
	}
	var lines []line
	section := ""
	marker := l.Marker
	if marker == "" {
		marker = "❯"
	}
	labelW := l.MinLabel
	for i, idx := range l.visible {
		r := l.rows[idx]
		if r.Section != "" && r.Section != section {
			if len(lines) > 0 {
				lines = append(lines, line{"", -1})
			}
			lines = append(lines, line{th.Fg(theme.Text).Bold(true).Render(r.Section), -1})
			section = r.Section
		}
		lines = append(lines, line{l.renderRow(th, r, i == l.cursor, marker, labelW, width), i})
	}
	if height <= 0 || len(lines) <= height {
		out := make([]string, len(lines))
		for i, ln := range lines {
			out[i] = ln.text
		}
		return out
	}
	cur := 0
	for i, ln := range lines {
		if ln.row == l.cursor {
			cur = i
			break
		}
	}
	room := height - 2 // leave space for the "more" lines
	if room < 1 {
		room = 1
	}
	start := cur - room/2
	if start < 0 {
		start = 0
	}
	if start+room > len(lines) {
		start = len(lines) - room
	}
	var out []string
	if start > 0 {
		out = append(out, th.Paint(theme.Inactive, fmt.Sprintf("↑ %d more", start)))
	}
	for _, ln := range lines[start : start+room] {
		out = append(out, ln.text)
	}
	if rest := len(lines) - start - room; rest > 0 {
		out = append(out, th.Paint(theme.Inactive, fmt.Sprintf("↓ %d more", rest)))
	}
	return out
}

func (l *List) renderRow(th *theme.Theme, r Row, selected bool, marker string, labelW, width int) string {
	if r.Info {
		return Truncate(th.Paint(theme.Inactive, "  "+r.Label), width)
	}
	prefix := "  "
	if selected {
		prefix = th.Paint(theme.Suggestion, marker+" ")
	}
	glyph := ""
	if r.Glyph != "" {
		tok := r.GlyphTok
		if tok == "" {
			tok = theme.Inactive
		}
		glyph = th.Paint(tok, r.Glyph) + " "
	}
	label := r.Label
	if pad := labelW - ansi.StringWidth(label); pad > 0 && r.Detail != "" {
		label += strings.Repeat(" ", pad)
	}
	if selected {
		label = th.Paint(theme.Suggestion, label)
	} else {
		label = th.Paint(theme.Text, label)
	}
	s := prefix + glyph + label
	if r.Detail != "" {
		s += "  " + th.Paint(theme.Inactive, r.Detail)
	}
	return Truncate(s, width)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
