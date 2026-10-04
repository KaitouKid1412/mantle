package input

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ui/editor/history"
)

var timeZero time.Time

// historyLoadedMsg carries history.jsonl, read off the UI goroutine.
type historyLoadedMsg struct {
	entries []history.Entry
	err     error
}

// historyAppendedMsg reports a failed history write (nil error is dropped).
type historyAppendedMsg struct{ err error }

func (s *state) loadHistory() tea.Cmd {
	path := s.histPath
	return func() tea.Msg {
		es, err := history.Load(path)
		return historyLoadedMsg{entries: es, err: err}
	}
}

// setHistory installs the loaded file. Prompts submitted before it finished
// loading (already in histAll) stay newest.
func (s *state) setHistory(es []history.Entry) {
	s.histAll = append(es, s.histAll...)
	s.rebuildNav()
}

func (s *state) rebuildNav() {
	s.hist = history.NewNavigator(history.ForProject(s.histAll, s.cwd))
}

// loadEntry puts a recalled history entry into the editor. Bash-mode
// prompts are stored with a leading "!".
func (s *state) loadEntry(e history.Entry) {
	mode := modePrompt
	if strings.HasPrefix(e.Display, "!") {
		mode = modeBash
		e.Display = e.Display[1:]
	}
	s.ed.SetHistoryEntry(e, s.ed.Paste.Store)
	s.mode = mode
}

// historyEntry builds the line for the current prompt.
func (s *state) historyEntry() history.Entry {
	e := s.ed.HistoryEntry(s.cwd, s.sessionID, s.now())
	if s.mode == modeBash {
		e.Display = "!" + e.Display
	}
	return e
}

func (s *state) appendHistory(e history.Entry) tea.Cmd {
	s.histAll = append(s.histAll, e)
	s.hist.Add(e)
	if !s.cfg.writeHistory || s.histPath == "" {
		return nil
	}
	path := s.histPath
	return func() tea.Msg {
		if err := history.Append(path, e); err != nil {
			return historyAppendedMsg{err: err}
		}
		return nil
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
