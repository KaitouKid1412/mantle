package input

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor"
)

// Spellcheck timing.
const (
	spellDebounce = 300 * time.Millisecond
	spellTimeout  = 5 * time.Second
)

// spellCheckers are tried in order for checker "" / "auto".
var spellCheckers = []string{"aspell", "hunspell", "ispell"}

// spellRunner returns the misspelled words among words, using checker.
type spellRunner func(ctx context.Context, checker string, words []string) ([]string, error)

// spell is the spellcheck state (setting spellcheck {enabled, checker,
// color}).
type spell struct {
	enabled bool
	checker string
	color   string

	known  map[string]bool // checked words: true = misspelled
	asked  map[string]bool // words sent and not answered yet
	seq    int
	run    spellRunner
	failed bool // the checker is missing or failed; stop trying
}

type spellTickMsg struct{ seq int }

type spellResultMsg struct {
	words, bad []string
	err        error
}

func newSpell() spell {
	return spell{known: map[string]bool{}, asked: map[string]bool{}, run: runSpellChecker}
}

func (sp *spell) configure(st ext.Settings) {
	sp.enabled, sp.checker, sp.color = false, "", ""
	v, ok := st.Claude("spellcheck")
	if !ok {
		return
	}
	switch m := v.(type) {
	case bool:
		sp.enabled = m
	case map[string]any:
		sp.enabled, _ = m["enabled"].(bool)
		sp.checker, _ = m["checker"].(string)
		sp.color, _ = m["color"].(string)
	}
}

// changed schedules a check of new words after a pause in typing.
func (sp *spell) changed(c ext.Ctx, s *state) tea.Cmd {
	if !sp.enabled || sp.failed {
		return nil
	}
	sp.seq++
	seq := sp.seq
	return c.Clock().Tick(spellDebounce, func(time.Time) tea.Msg { return spellTickMsg{seq: seq} })
}

func (sp *spell) tick(s *state, m spellTickMsg) tea.Cmd {
	if m.seq != sp.seq || !sp.enabled || sp.failed {
		return nil
	}
	var words []string
	for _, w := range promptWords(s.ed) {
		if _, done := sp.known[w]; done || sp.asked[w] {
			continue
		}
		sp.asked[w] = true
		words = append(words, w)
	}
	if len(words) == 0 {
		return nil
	}
	run, checker := sp.run, sp.checker
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), spellTimeout)
		defer cancel()
		bad, err := run(ctx, checker, words)
		return spellResultMsg{words: words, bad: bad, err: err}
	}
}

func (sp *spell) result(c ext.Ctx, s *state, m spellResultMsg) tea.Cmd {
	for _, w := range m.words {
		delete(sp.asked, w)
	}
	if m.err != nil {
		sp.failed = true
		return c.Notify(ext.Notice{Key: "input.spellcheck", Text: "Spellcheck is off: " + m.err.Error(), Level: ext.NoticeWarning, Source: FeatureID})
	}
	bad := map[string]bool{}
	for _, b := range m.bad {
		bad[b] = true
	}
	for _, w := range m.words {
		sp.known[w] = bad[w]
	}
	s.invalidate(c)
	return nil
}

// spans underlines misspelled words in a line.
func (sp *spell) spans(t *theme.Theme, gs []string) []editor.Span {
	if !sp.enabled || len(sp.known) == 0 {
		return nil
	}
	st := lipgloss.NewStyle().Underline(true)
	switch {
	case sp.color != "":
		st = st.Foreground(lipgloss.Color(sp.color))
	case t != nil:
		st = st.Foreground(t.Color(theme.Error))
	}
	var out []editor.Span
	for _, w := range wordRuns(gs) {
		if sp.known[w.text] {
			out = append(out, editor.Span{Start: w.start, End: w.end, Style: st})
		}
	}
	return out
}

type wordRun struct {
	text       string
	start, end int
}

// wordRuns finds plain words (letters and inner apostrophes) in a line,
// skipping parts of paths, mentions, commands, identifiers and numbers.
func wordRuns(gs []string) []wordRun {
	letter := func(g string) bool {
		for _, r := range g {
			return unicode.IsLetter(r)
		}
		return false
	}
	var out []wordRun
	for i := 0; i < len(gs); {
		if !letter(gs[i]) {
			i++
			continue
		}
		j := i
		for j < len(gs) && (letter(gs[j]) || (gs[j] == "'" && j+1 < len(gs) && letter(gs[j+1]))) {
			j++
		}
		skip := (i > 0 && strings.ContainsAny(gs[i-1], "/@\\_-.:#$0123456789")) ||
			(j < len(gs) && strings.ContainsAny(gs[j], "/\\_.0123456789(") && !(gs[j] == "." && (j+1 == len(gs) || gs[j+1] == " ")))
		if w := strings.Join(gs[i:j], ""); !skip && len([]rune(w)) > 1 {
			out = append(out, wordRun{text: w, start: i, end: j})
		}
		i = j
	}
	return out
}

// promptWords lists the distinct plain words in the prompt.
func promptWords(ed *editor.Editor) []string {
	seen := map[string]bool{}
	var out []string
	for r := 0; r < ed.LineCount(); r++ {
		for _, w := range wordRuns(graphemes(ed.Line(r))) {
			if !seen[w.text] {
				seen[w.text] = true
				out = append(out, w.text)
			}
		}
	}
	return out
}

func graphemes(s string) []string {
	var out []string
	for _, r := range s {
		out = append(out, string(r))
	}
	return out
}

// runSpellChecker pipes words to aspell/hunspell/ispell in "list" mode and
// returns the ones it reports.
func runSpellChecker(ctx context.Context, checker string, words []string) ([]string, error) {
	name := checker
	if name == "" || name == "auto" {
		for _, c := range spellCheckers {
			if _, err := exec.LookPath(c); err == nil {
				name = c
				break
			}
		}
		if name == "" || name == "auto" {
			return nil, errNoChecker
		}
	}
	var args []string
	switch name {
	case "aspell":
		args = []string{"list"}
	default:
		args = []string{"-l"}
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(strings.Join(words, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var bad []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if w := strings.TrimSpace(sc.Text()); w != "" {
			bad = append(bad, w)
		}
	}
	return bad, nil
}

type spellErr string

func (e spellErr) Error() string { return string(e) }

const errNoChecker = spellErr("no spellchecker found (install aspell, hunspell or ispell)")
