package parity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeResult(target string, cps map[string][]string, err error) *Result {
	r := &Result{Target: target, Err: err}
	for name, screen := range cps {
		r.Checkpoints = append(r.Checkpoints, Checkpoint{Name: name, Frame: Frame{Screen: screen}})
	}
	return r
}

func reportScenario(t *testing.T) *Scenario {
	return mustParse(t, "name: demo\n---\nready\ncheckpoint start\ncheckpoint answer\ncheckpoint end\n")
}

func TestCompare(t *testing.T) {
	sc := reportScenario(t)
	a := fakeResult("claude", map[string][]string{
		"start":  {"> ", "? for shortcuts"},
		"answer": {"⏺ 4", "> "},
		"end":    {"bye"},
	}, nil)
	b := fakeResult("mantle", map[string][]string{
		"start":  {"> ", "? for shortcuts"},
		"answer": {"⏺ four", "> "},
	}, nil)
	diffs := Compare(DefaultNormalizer(), sc, a, b, nil)
	if len(diffs) != 3 {
		t.Fatalf("diffs = %+v", diffs)
	}
	if diffs[0].Status != StatusSame || diffs[1].Status != StatusDiff || diffs[1].Changed != 1 ||
		diffs[2].Status != StatusMissing {
		t.Errorf("statuses = %v %v(%d) %v", diffs[0].Status, diffs[1].Status, diffs[1].Changed, diffs[2].Status)
	}
	if !strings.Contains(diffs[1].Unified, "-⏺ 4") || !strings.Contains(diffs[1].Unified, "+⏺ four") {
		t.Errorf("unified = %q", diffs[1].Unified)
	}
	if !strings.HasPrefix(diffs[2].Note, "mantle:") {
		t.Errorf("missing note = %q", diffs[2].Note)
	}

	single := Compare(DefaultNormalizer(), sc, a, nil, nil)
	if single[0].Status != StatusSingle {
		t.Errorf("single = %v", single[0].Status)
	}
}

func TestPromptGap(t *testing.T) {
	rule := strings.Repeat("─", 40)
	for _, tt := range []struct {
		screen []string
		want   int
	}{
		{[]string{"✻ done", "", "", "", rule, "❯ ", rule, "  footer"}, 3},
		{[]string{"✻ done", rule, "❯\u00a0", rule}, 0},
		{[]string{"a", "", rule, "  1. Yes", rule}, -1}, // a dialog's rule, not the prompt
	} {
		if got := promptGap(tt.screen); got != tt.want {
			t.Errorf("promptGap(%q) = %d, want %d", tt.screen, got, tt.want)
		}
	}
}

func TestRepoAllowlist(t *testing.T) {
	al, err := LoadAllowlist("allowlist.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(al.Entries) == 0 {
		t.Fatal("test/parity/allowlist.txt has no entries")
	}
	// A footer line on each side is covered (mode wording, agent-view hint, effort glyph).
	left := []string{"  ⏸ manual mode on · ? for shortcuts · ← for agents          ◐ medium · /effort"}
	right := []string{"  ⏸ manual approval · ? for shortcuts                         ◑ medium · /effort"}
	if _, ok := al.Covers("plain-qa", "answered", left, right); !ok {
		t.Error("footer lines should be allowlisted")
	}
	// A real reply difference is not.
	if _, ok := al.Covers("plain-qa", "answered", []string{"⏺ Done."}, []string{"⏺ Done!"}); ok {
		t.Error("reply text must not be allowlisted")
	}
}

func TestAllowlist(t *testing.T) {
	al, err := ParseAllowlist(`
# scenario | checkpoint | pattern | reason
* | * | manual approval | mantle names the default mode in its own words
demo | answer | ^⏺ (4|four)$ | number wording
`)
	if err != nil {
		t.Fatal(err)
	}
	if reasons, ok := al.Covers("demo", "answer", []string{"⏺ 4", "x"}, []string{"⏺ four", "x"}); !ok || reasons[0] != "number wording" {
		t.Errorf("covers = %v %v", reasons, ok)
	}
	if _, ok := al.Covers("other", "answer", []string{"⏺ 4"}, []string{"⏺ four"}); ok {
		t.Error("scenario filter")
	}
	if _, ok := al.Covers("demo", "answer", []string{"⏺ 4", "a"}, []string{"⏺ four", "b"}); ok {
		t.Error("an uncovered line must fail")
	}
	// An extra line (and a blank) on one side shifts the rest; only it needs covering.
	left := []string{"> hi", "", "⏺ answer", "footer"}
	right := []string{"> hi", "", "⏺ answer", "", "⏸ manual approval", "footer"}
	if _, ok := al.Covers("x", "y", left, right); !ok {
		t.Error("inserted lines are covered without matching the shifted lines below")
	}
	if n := changedLines(left, right); n != 2 {
		t.Errorf("changedLines = %d, want 2", n)
	}
	if _, err := ParseAllowlist("* | * | ( | x"); err == nil {
		t.Error("bad regex")
	}
	if _, err := ParseAllowlist("* | * | x | "); err == nil {
		t.Error("reason required")
	}
	if _, ok := (*Allowlist)(nil).Covers("a", "b", []string{"x"}, []string{"y"}); ok {
		t.Error("nil list covers nothing")
	}
	empty, err := LoadAllowlist(filepath.Join(t.TempDir(), "missing"))
	if err != nil || len(empty.Entries) != 0 {
		t.Error("missing file is empty")
	}
}

func TestReportWrite(t *testing.T) {
	sc := reportScenario(t)
	al, _ := ParseAllowlist("demo | end | bye | farewell differs on purpose")
	r := &Report{
		Normalizer: DefaultNormalizer(),
		Targets:    []string{"claude", "mantle"},
		Allow:      al,
		Generated:  time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
		Notes:      []string{"Engine: claude 2.1.288"},
		Runs: []ScenarioRun{{Scenario: sc, Results: map[string]*Result{
			"claude": fakeResult("claude", map[string][]string{"start": {"> "}, "answer": {"⏺ 4"}, "end": {"bye"}}, nil),
			"mantle": fakeResult("mantle", map[string][]string{"start": {"> "}, "answer": {"⏺ four"}, "end": {"bye now"}}, nil),
		}}},
	}
	dir := t.TempDir()
	path, err := r.Write(dir)
	if err != nil {
		t.Fatal(err)
	}
	sum, _ := os.ReadFile(path)
	s := string(sum)
	for _, want := range []string{
		"Targets: claude vs mantle", "Engine: claude 2.1.288",
		"| demo | start | same |", "| demo | answer | diff | 1 |  | [frames](demo/answer.txt) |",
		"| demo | end | allowed |", "## Normalization rules", "| spinner |", "farewell differs on purpose",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("summary lacks %q:\n%s", want, s)
		}
	}
	side, _ := os.ReadFile(filepath.Join(dir, "demo", "answer.txt"))
	if !strings.Contains(string(side), "≠ ⏺ 4") || !strings.Contains(string(side), "│ ⏺ four") || !strings.Contains(string(side), "+⏺ four") {
		t.Errorf("side by side:\n%s", side)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "demo", "answer.mantle.txt")); err != nil || string(b) != "⏺ four\n" {
		t.Errorf("per-target frame = %q %v", b, err)
	}
}

func TestReportRunErrors(t *testing.T) {
	sc := reportScenario(t)
	res := fakeResult("claude", nil, os.ErrDeadlineExceeded)
	res.Unmatched = []string{"turn 2: contains \"hi\""}
	r := &Report{Normalizer: DefaultNormalizer(), Targets: []string{"claude"},
		Runs: []ScenarioRun{{Scenario: sc, Results: map[string]*Result{"claude": res}}}}
	s := r.Summary(r.Diffs())
	if !strings.Contains(s, "## Run errors") || !strings.Contains(s, "fakeapi turns unmatched") || !strings.Contains(s, "| missing |") {
		t.Errorf("summary:\n%s", s)
	}
}

func TestSideBySideWideChars(t *testing.T) {
	out := SideBySide("a", "b", []string{"日本語"}, []string{"x"})
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[2], "≠ 日本語") || !strings.Contains(lines[2], " │ x") {
		t.Errorf("wide = %q", lines[2])
	}
}
