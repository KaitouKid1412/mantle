package input

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor"
)

// DialogHistorySearch is ctrl+r in the fullscreen layout: a dialog with the
// matches, a preview and a search box (VW-22). Inline, ctrl+r stays the
// one-line search under the prompt.
const DialogHistorySearch = "dialog.historySearch"

// Dialog geometry.
const (
	searchListW   = 27 // the list column, left of the preview
	searchPreview = 6  // preview rows inside its box
)

// searchDialog renders and drives s.search while it is a dialog.
type searchDialog struct{ s *state }

var _ ext.Dialog = (*searchDialog)(nil)

func (d *searchDialog) ID() string                      { return DialogHistorySearch }
func (d *searchDialog) Init(ext.Ctx) tea.Cmd            { return nil }
func (d *searchDialog) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (d *searchDialog) Placement() ext.Placement        { return ext.PlaceInline }
func (d *searchDialog) KeyContext() string              { return "" } // keys are its own (see HandleKey)
func (d *searchDialog) HandlePaste(c ext.Ctx, m tea.PasteMsg) (bool, tea.Cmd) {
	if q := d.s.search; q != nil {
		q.query += editor.SanitizePaste(strings.ReplaceAll(m.Content, "\n", " "))
		q.idx = 0
		q.refresh(d.s)
		c.Invalidate(DialogHistorySearch)
	}
	return true, nil
}

// HandleAction takes the Global ctrl+r (older match) and ctrl+c (cancel).
func (d *searchDialog) HandleAction(c ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	q := d.s.search
	if q == nil {
		return false, nil
	}
	switch a {
	case ext.ActHistorySearch, ext.ActHistorySearchNext:
		q.next()
		c.Invalidate(DialogHistorySearch)
		return true, nil
	case ext.ActAppInterrupt, ext.ActHistorySearchCancel:
		return true, d.s.closeSearchDialog(c, false)
	}
	return false, nil
}

func (d *searchDialog) HandleKey(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	s := d.s
	q := s.search
	if q == nil {
		return false, nil
	}
	switch ks := k.Keystroke(); ks {
	case "esc":
		return true, s.closeSearchDialog(c, false) // back to the prompt as it was
	case "enter", "tab":
		return true, s.closeSearchDialog(c, true)
	case "up":
		q.next()
	case "down":
		if q.idx > 0 {
			q.idx--
		}
	case "ctrl+s":
		q.scope = (q.scope + 1) % len(scopeNames)
		q.idx = 0
		q.refresh(s)
	case "backspace":
		if r := []rune(q.query); len(r) > 0 {
			q.query = string(r[:len(r)-1])
			q.idx = 0
			q.refresh(s)
		}
	default:
		if k.Text == "" || k.Mod&^tea.ModShift != 0 {
			return true, nil // swallow other keys while the dialog is up
		}
		q.query += k.Text
		q.idx = 0
		q.refresh(s)
	}
	c.Invalidate(DialogHistorySearch)
	return true, nil
}

// openSearchDialog starts the fullscreen search over every project.
func (s *state) openSearchDialog(c ext.Ctx) tea.Cmd {
	s.comp.close()
	s.help = false
	s.search = &search{scope: scopeAll, saved: s.save()}
	s.search.refresh(s)
	s.searchDialog = true
	return tea.Batch(c.OpenDialog(DialogHistorySearch, nil), s.stateCmd(false))
}

// closeSearchDialog puts the match (use) or the original prompt (cancel)
// back and closes the dialog.
func (s *state) closeSearchDialog(c ext.Ctx, use bool) tea.Cmd {
	q := s.search
	s.search, s.searchDialog = nil, false
	if q != nil {
		if e, ok := q.match(); use && ok {
			s.loadEntry(e)
		} else {
			s.restore(q.saved)
		}
	}
	return tea.Batch(c.CloseDialog(DialogHistorySearch), s.changed(c))
}

func (d *searchDialog) View(c ext.Ctx, a ext.Area) ext.Rendered {
	s := d.s
	q := s.search
	if q == nil {
		return ext.Rendered{}
	}
	t := c.Theme()
	w := max(a.Width, 20)
	now := c.Clock().Now()
	dim := func(str string) string { return t.Paint(theme.Inactive, str) }

	var lines []string
	lines = append(lines, "  Search history "+dim("· "+scopeNames[q.scope]))

	// The list (newest at the bottom, next to the search box) and the
	// preview of the selected match.
	rows := searchPreview + 2
	listW := searchListW
	showPreview := w >= 60
	if !showPreview {
		listW = w
	}
	n := min(len(q.matches), rows)
	first := 0
	if q.idx >= rows {
		first = q.idx - rows + 1
	}
	list := make([]string, rows)
	for i := 0; i < n && first+i < len(q.matches); i++ {
		idx := first + i
		e := q.matches[idx]
		mark := "  "
		if idx == q.idx {
			mark = "❯ "
		}
		age := ageString(now, e.Timestamp)
		text := firstLineOf(e.Display)
		row := "  " + mark + age + strings.Repeat(" ", max(9-ansi.StringWidth(age), 2)) + text
		row = ansi.Truncate(row, listW-1, "…")
		if idx == q.idx {
			row = t.Paint(theme.Suggestion, row)
		}
		list[rows-1-i] = row
	}
	if showPreview {
		box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Color(theme.Subtle)).
			Width(w-listW-2).Height(rows).Padding(0, 1)
		inner := w - listW - 2 - 4
		var pv []string
		if e, ok := q.match(); ok {
			for _, l := range strings.Split(e.Display, "\n") {
				pv = append(pv, wordWrap(l, max(inner, 1))...)
				if len(pv) >= searchPreview {
					break
				}
			}
		}
		if len(pv) > searchPreview {
			pv = pv[:searchPreview]
		}
		boxLines := strings.Split(box.Render(strings.Join(pv, "\n")), "\n")
		for i := range list {
			left := list[i] + strings.Repeat(" ", max(listW-ansi.StringWidth(list[i]), 0))
			right := ""
			if i < len(boxLines) {
				right = boxLines[i]
			}
			list[i] = left + right
		}
	}
	lines = append(lines, list...)

	// The search box.
	sbox := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Color(theme.Subtle)).Width(w-4).Padding(0, 1)
	query := "⌕ " + q.query
	if q.query != "" && len(q.matches) == 0 {
		query += "  " + dim("no match")
	}
	for _, l := range strings.Split(sbox.Render(query), "\n") {
		lines = append(lines, "  "+l)
	}
	lines = append(lines, "  "+dim("↑/↓ choose · enter use · esc cancel · ctrl+s scope"))

	cur := tea.NewCursor(2+1+1+2+ansi.StringWidth(q.query), 1+rows+1)
	cur.Shape = tea.CursorBar
	return ext.Rendered{Text: strings.Join(lines, "\n"), Cursor: cur}
}

// ageString is how long ago a history entry was recorded.
func ageString(now time.Time, ms int64) string {
	if ms <= 0 {
		return ""
	}
	d := now.Sub(time.UnixMilli(ms))
	switch {
	case d < time.Minute:
		return strconv.Itoa(max(int(d.Seconds()), 0)) + "s ago"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	case d < 30*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d ago"
	}
	return strconv.Itoa(int(d.Hours()/24/30)) + "mo ago"
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}
