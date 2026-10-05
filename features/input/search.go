package input

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor/history"
)

// Search scopes, cycled with ctrl+s.
const (
	scopeSession = iota
	scopeProject
	scopeAll
)

var scopeNames = []string{"this session", "this project", "all projects"}

// search is ctrl+r history search (context HistorySearch).
type search struct {
	query   string
	scope   int
	matches []history.Entry // newest first, duplicates collapsed
	idx     int
	saved   *savedDraft // the prompt before searching (restored on cancel)
}

func (s *state) openSearch(c ext.Ctx) tea.Cmd {
	if s.search != nil {
		s.search.next()
		s.invalidate(c)
		return nil
	}
	s.comp.close()
	s.help = false
	s.search = &search{scope: scopeProject, saved: s.save()}
	s.search.refresh(s)
	s.invalidate(c)
	return s.stateCmd(false)
}

func (q *search) refresh(s *state) {
	q.matches = q.matches[:0]
	needle := strings.ToLower(q.query)
	seen := map[string]bool{}
	for i := len(s.histAll) - 1; i >= 0; i-- {
		e := s.histAll[i]
		switch q.scope {
		case scopeSession:
			if e.SessionID != s.sessionID || s.sessionID == "" {
				continue
			}
		case scopeProject:
			if e.Project != s.cwd {
				continue
			}
		}
		if seen[e.Display] || !strings.Contains(strings.ToLower(e.Display), needle) {
			continue
		}
		seen[e.Display] = true
		q.matches = append(q.matches, e)
	}
	if q.idx >= len(q.matches) {
		q.idx = max(len(q.matches)-1, 0)
	}
}

func (q *search) next() {
	if q.idx+1 < len(q.matches) {
		q.idx++
	}
}

func (q *search) match() (history.Entry, bool) {
	if q.idx < len(q.matches) {
		return q.matches[q.idx], true
	}
	return history.Entry{}, false
}

func (s *state) searchKey(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	q := s.search
	switch ks := k.Keystroke(); {
	case ks == "backspace":
		if r := []rune(q.query); len(r) > 0 {
			q.query = string(r[:len(r)-1])
			q.idx = 0
			q.refresh(s)
		}
	case k.Text != "" && k.Mod&^tea.ModShift == 0:
		q.query += k.Text
		q.idx = 0
		q.refresh(s)
	default:
		// Any other key accepts the match and then acts on the prompt.
		cmd := s.acceptSearch(c)
		ok, cmd2 := s.key(c, k)
		return ok, tea.Batch(cmd, cmd2)
	}
	s.invalidate(c)
	return true, nil
}

func (s *state) searchAction(c ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	q := s.search
	switch a {
	case ext.ActHistorySearchNext, ext.ActHistorySearch:
		q.next()
	case ext.ActHistorySearchCycleScope:
		q.scope = (q.scope + 1) % len(scopeNames)
		q.idx = 0
		q.refresh(s)
	case ext.ActHistorySearchAccept:
		return true, s.acceptSearch(c)
	case ext.ActHistorySearchCancel:
		s.search = nil
		s.restore(q.saved)
		return true, s.changed(c)
	case ext.ActHistorySearchExecute:
		cmd := s.acceptSearch(c)
		_, sub := s.submitAction(c, "")
		return true, tea.Batch(cmd, sub)
	default:
		return false, nil
	}
	s.invalidate(c)
	return true, nil
}

// acceptSearch puts the match (or the original prompt) into the editor.
func (s *state) acceptSearch(c ext.Ctx) tea.Cmd {
	q := s.search
	s.search = nil
	if e, ok := q.match(); ok && q.query != "" {
		s.loadEntry(e)
	} else {
		s.restore(q.saved)
	}
	return s.changed(c)
}

// viewSearchPreview shows the current match in the prompt area with the
// query highlighted.
func (s *state) viewSearchPreview(c ext.Ctx, a ext.Area) ext.Rendered {
	t := c.Theme()
	q := s.search
	text := ""
	if e, ok := q.match(); ok && q.query != "" {
		text = e.Display
	}
	w := max(a.Width-prefixWidth, 1)
	var rows []string
	for i, l := range strings.Split(text, "\n") {
		if i >= 6 {
			rows = append(rows, t.Paint(theme.Inactive, "…"))
			break
		}
		l = highlightAll(t, ansi.Truncate(l, w, "…"), q.query)
		rows = append(rows, l)
	}
	marker := t.Paint(theme.Inactive, "❯ ")
	return ext.Rendered{Text: marker + strings.Join(rows, "\n"+strings.Repeat(" ", prefixWidth))}
}

// highlightAll paints case-insensitive occurrences of needle.
func highlightAll(t *theme.Theme, s, needle string) string {
	if needle == "" {
		return s
	}
	lower, ln := strings.ToLower(s), strings.ToLower(needle)
	if len(lower) != len(s) {
		return s // case folding changed byte lengths; skip highlighting
	}
	var b strings.Builder
	for {
		i := strings.Index(lower, ln)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(t.Paint(theme.Suggestion, s[i:i+len(ln)]))
		s, lower = s[i+len(ln):], lower[i+len(ln):]
	}
}

// view is one line where the footer was: the query, plus the scope when it
// is not this project.
func (q *search) view(t *theme.Theme, width int) []string {
	label := "search history"
	if q.scope != scopeProject {
		label += " (" + scopeNames[q.scope] + ")"
	}
	status := "  " + label + ": " + q.query
	if q.query != "" && len(q.matches) == 0 {
		status += t.Paint(theme.Inactive, "  no match")
	}
	return []string{status}
}
