package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Column is a Table column. Width 0 = fit content; the last flexible column takes the
// remaining width.
type Column struct {
	Title string
	Width int
	Right bool // right-align (numbers)
}

// Table renders rows in aligned columns with a dim header. With Selectable it keeps a
// highlighted row and handles select:* actions like Select.
type Table struct {
	IDValue    string
	Columns    []Column
	Rows       [][]string
	Selectable bool
	OnAccept   func(ext.Ctx, int) tea.Cmd
	OnCancel   func(ext.Ctx) tea.Cmd

	cursor int
	offset int
}

var _ ext.Focusable = (*Table)(nil)
var _ ext.ActionHandler = (*Table)(nil)

func (t *Table) ID() string                      { return t.IDValue }
func (t *Table) Init(ext.Ctx) tea.Cmd            { return nil }
func (t *Table) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (t *Table) KeyContext() string              { return ext.ContextSelect }
func (t *Table) HandleKey(ext.Ctx, tea.KeyPressMsg) (bool, tea.Cmd) {
	return false, nil
}
func (t *Table) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return false, nil }

// Cursor returns the highlighted row.
func (t *Table) Cursor() int { return t.cursor }

// HandleAction moves the highlight when Selectable.
func (t *Table) HandleAction(c ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	if !t.Selectable || len(t.Rows) == 0 {
		return false, nil
	}
	move := func(to int) (bool, tea.Cmd) {
		t.cursor = min(max(to, 0), len(t.Rows)-1)
		c.Invalidate(t.IDValue)
		return true, nil
	}
	switch a {
	case ext.ActSelectNext:
		return move(t.cursor + 1)
	case ext.ActSelectPrevious:
		return move(t.cursor - 1)
	case ext.ActSelectPageDown:
		return move(t.cursor + 10)
	case ext.ActSelectPageUp:
		return move(t.cursor - 10)
	case ext.ActSelectFirst:
		return move(0)
	case ext.ActSelectLast:
		return move(len(t.Rows) - 1)
	case ext.ActSelectAccept:
		if t.OnAccept != nil {
			return true, t.OnAccept(c, t.cursor)
		}
	case ext.ActSelectCancel:
		if t.OnCancel != nil {
			return true, t.OnCancel(c)
		}
	}
	return false, nil
}

// widths computes column widths that fit width.
func (t *Table) widths(width int) []int {
	ws := make([]int, len(t.Columns))
	flex := -1
	for i, col := range t.Columns {
		if col.Width > 0 {
			ws[i] = col.Width
			continue
		}
		flex = i
		ws[i] = ansi.StringWidth(col.Title)
		for _, r := range t.Rows {
			if i < len(r) {
				ws[i] = max(ws[i], ansi.StringWidth(r[i]))
			}
		}
	}
	prefix := 0
	if t.Selectable {
		prefix = 2
	}
	total := prefix + 2*max(0, len(ws)-1)
	for _, w := range ws {
		total += w
	}
	if total > width {
		// Shrink the last flexible column (or the last column) to fit.
		i := flex
		if i < 0 {
			i = len(ws) - 1
		}
		if i >= 0 {
			ws[i] = max(1, ws[i]-(total-width))
		}
	}
	return ws
}

func (t *Table) cell(s string, w int, right bool) string {
	s = ansi.Truncate(s, w, "…")
	pad := strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
	if right {
		return pad + s
	}
	return s + pad
}

// View renders the header and rows.
func (t *Table) View(c ext.Ctx, a ext.Area) ext.Rendered {
	th := c.Theme()
	ws := t.widths(a.Width)
	prefix := ""
	if t.Selectable {
		prefix = "  "
	}
	var head []string
	for i, col := range t.Columns {
		head = append(head, t.cell(col.Title, ws[i], col.Right))
	}
	lines := []string{th.Paint(theme.Inactive, strings.TrimRight(prefix+strings.Join(head, "  "), " "))}
	rows := len(t.Rows)
	if a.MaxHeight > 0 {
		rows = min(rows, max(1, a.MaxHeight-1))
	}
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+rows {
		t.offset = t.cursor - rows + 1
	}
	for ri := t.offset; ri < min(len(t.Rows), t.offset+rows); ri++ {
		var cells []string
		for i, col := range t.Columns {
			v := ""
			if i < len(t.Rows[ri]) {
				v = t.Rows[ri][i]
			}
			cells = append(cells, t.cell(v, ws[i], col.Right))
		}
		line := strings.TrimRight(strings.Join(cells, "  "), " ")
		if t.Selectable {
			if ri == t.cursor {
				line = th.Paint(theme.Suggestion, "❯ "+line)
			} else {
				line = "  " + line
			}
		}
		lines = append(lines, ansi.Truncate(line, a.Width, ""))
	}
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}
