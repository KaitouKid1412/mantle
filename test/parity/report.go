package parity

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/x/ansi"
)

// Status is a checkpoint comparison outcome.
type Status string

const (
	StatusSame    Status = "same"
	StatusDiff    Status = "diff"
	StatusMissing Status = "missing" // a target never reached the checkpoint
	StatusSingle  Status = "single"  // only one target ran
	StatusAllowed Status = "allowed" // differs, but every difference is allowlisted
)

// CheckpointDiff compares one checkpoint across two targets.
type CheckpointDiff struct {
	Scenario   string
	Checkpoint string
	Status     Status
	Changed    int      // differing lines
	Left       []string // normalized frame of the first target (nil if missing)
	Right      []string // normalized frame of the second target
	Unified    string
	Note       string // why it is missing, or the allowlist reasons
}

// ScenarioRun is one scenario's results, by target.
type ScenarioRun struct {
	Scenario *Scenario
	Results  map[string]*Result
}

// Report collects runs and writes the side-by-side output.
type Report struct {
	Normalizer *Normalizer
	Targets    []string // column order: the reference target first
	Runs       []ScenarioRun
	Allow      *Allowlist
	Generated  time.Time
	Notes      []string // free-form lines for the summary (engine versions, …)
}

// Compare diffs every checkpoint of a scenario between two targets' results (b may be
// nil for a single-target run).
func Compare(n *Normalizer, sc *Scenario, a, b *Result, allow *Allowlist) []CheckpointDiff {
	var names []string
	for _, st := range sc.Steps {
		if st.Kind == StepCheckpoint {
			names = append(names, st.Text)
		}
	}
	var out []CheckpointDiff
	for _, name := range names {
		d := CheckpointDiff{Scenario: sc.Name, Checkpoint: name}
		ca, okA := a.Checkpoint(name)
		if okA {
			d.Left = n.Frame(ca.Frame, a.Workspace)
		}
		if b == nil {
			d.Status = StatusSingle
			if !okA {
				d.Status, d.Note = StatusMissing, missingNote(a)
			}
			out = append(out, d)
			continue
		}
		cb, okB := b.Checkpoint(name)
		if okB {
			d.Right = n.Frame(cb.Frame, b.Workspace)
		}
		switch {
		case !okA:
			d.Status, d.Note = StatusMissing, a.Target+": "+missingNote(a)
		case !okB:
			d.Status, d.Note = StatusMissing, b.Target+": "+missingNote(b)
		default:
			d.Changed = changedLines(d.Left, d.Right)
			if d.Changed == 0 {
				d.Status = StatusSame
				break
			}
			d.Status = StatusDiff
			d.Unified = udiff.Unified(a.Target, b.Target, joinLines(d.Left), joinLines(d.Right))
			if reasons, ok := allow.Covers(sc.Name, name, d.Left, d.Right); ok {
				d.Status, d.Note = StatusAllowed, strings.Join(reasons, "; ")
			}
		}
		out = append(out, d)
	}
	return out
}

func missingNote(r *Result) string {
	if r.Err != nil {
		return r.Err.Error()
	}
	return "checkpoint not reached"
}

// changedLines counts the lines a line diff marks as changed: a changed line counts
// once, an inserted or deleted line once, so an extra line near the top doesn't count
// everything below it.
func changedLines(a, b []string) int {
	onlyA, onlyB := diffLines(a, b)
	return max(len(onlyA), len(onlyB))
}

// diffLines returns the lines of a and of b that are not part of their longest common
// subsequence. Frames are at most a few hundred lines, so the quadratic table is fine.
func diffLines(a, b []string) (onlyA, onlyB []string) {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			onlyA, i = append(onlyA, a[i]), i+1
		default:
			onlyB, j = append(onlyB, b[j]), j+1
		}
	}
	return append(onlyA, a[i:]...), append(onlyB, b[j:]...)
}

func joinLines(l []string) string {
	if len(l) == 0 {
		return ""
	}
	return strings.Join(l, "\n") + "\n"
}

// SideBySide renders two frames as columns, marking differing rows with "≠".
func SideBySide(leftTitle, rightTitle string, left, right []string) string {
	w := max(ansi.StringWidth(leftTitle), 20)
	for _, l := range left {
		w = max(w, ansi.StringWidth(l))
	}
	w = min(w, 220)
	pad := func(s string) string {
		if ansi.StringWidth(s) > w {
			s = ansi.Truncate(s, w, "…")
		}
		return s + strings.Repeat(" ", w-ansi.StringWidth(s))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s │ %s\n", pad(leftTitle), rightTitle)
	fmt.Fprintf(&b, "  %s─┼─%s\n", strings.Repeat("─", w), strings.Repeat("─", max(20, ansi.StringWidth(rightTitle))))
	for i := range max(len(left), len(right)) {
		var x, y string
		if i < len(left) {
			x = left[i]
		}
		if i < len(right) {
			y = right[i]
		}
		mark := "  "
		if x != y {
			mark = "≠ "
		}
		fmt.Fprintf(&b, "%s%s │ %s\n", mark, pad(x), y)
	}
	return b.String()
}

// Diffs returns every checkpoint comparison, in scenario order.
func (r *Report) Diffs() []CheckpointDiff {
	var out []CheckpointDiff
	for _, run := range r.Runs {
		a := run.Results[r.Targets[0]]
		var b *Result
		if len(r.Targets) > 1 {
			b = run.Results[r.Targets[1]]
		}
		if a == nil {
			continue
		}
		out = append(out, Compare(r.Normalizer, run.Scenario, a, b, r.Allow)...)
	}
	return out
}

// Write writes the report into dir: summary.md, and per scenario a directory with one
// file per checkpoint (side by side plus the unified diff) and each target's
// normalized frame. It returns the summary's path.
func (r *Report) Write(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	diffs := r.Diffs()
	for _, d := range diffs {
		sdir := filepath.Join(dir, sanitizeName(d.Scenario))
		if err := os.MkdirAll(sdir, 0o755); err != nil {
			return "", err
		}
		base := filepath.Join(sdir, sanitizeName(d.Checkpoint))
		var body strings.Builder
		fmt.Fprintf(&body, "%s / %s: %s", d.Scenario, d.Checkpoint, d.Status)
		if d.Changed > 0 {
			fmt.Fprintf(&body, " (%d lines)", d.Changed)
		}
		if d.Note != "" {
			fmt.Fprintf(&body, "\n%s", d.Note)
		}
		body.WriteString("\n\n")
		right := ""
		if len(r.Targets) > 1 {
			right = r.Targets[1]
		}
		if d.Status == StatusSingle {
			body.WriteString(joinLines(d.Left))
		} else {
			body.WriteString(SideBySide(r.Targets[0], right, d.Left, d.Right))
		}
		if d.Unified != "" {
			body.WriteString("\n" + d.Unified)
		}
		if err := os.WriteFile(base+".txt", []byte(body.String()), 0o644); err != nil {
			return "", err
		}
		for i, frame := range [][]string{d.Left, d.Right} {
			if frame == nil || i >= len(r.Targets) {
				continue
			}
			if err := os.WriteFile(base+"."+r.Targets[i]+".txt", []byte(joinLines(frame)), 0o644); err != nil {
				return "", err
			}
		}
	}
	// A failed run keeps the screen it failed on, for triage.
	for _, run := range r.Runs {
		for _, tg := range r.Targets {
			res := run.Results[tg]
			if res == nil || res.Err == nil {
				continue
			}
			sdir := filepath.Join(dir, sanitizeName(run.Scenario.Name))
			if err := os.MkdirAll(sdir, 0o755); err != nil {
				return "", err
			}
			body := res.Err.Error() + "\n\n" + joinLines(r.Normalizer.Frame(res.Final, res.Workspace))
			if err := os.WriteFile(filepath.Join(sdir, tg+".final.txt"), []byte(body), 0o644); err != nil {
				return "", err
			}
		}
	}
	path := filepath.Join(dir, "summary.md")
	return path, os.WriteFile(path, []byte(r.Summary(diffs)), 0o644)
}

// Summary renders the markdown summary.
func (r *Report) Summary(diffs []CheckpointDiff) string {
	var b strings.Builder
	b.WriteString("# Parity side-by-side report\n\n")
	gen := r.Generated
	if gen.IsZero() {
		gen = time.Now()
	}
	fmt.Fprintf(&b, "Generated %s. Targets: %s.\n", gen.UTC().Format(time.RFC3339), strings.Join(r.Targets, " vs "))
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "\n%s\n", n)
	}

	counts := map[Status]int{}
	for _, d := range diffs {
		counts[d.Status]++
	}
	b.WriteString("\n| Status | Checkpoints |\n|---|---|\n")
	for _, s := range []Status{StatusSame, StatusAllowed, StatusDiff, StatusMissing, StatusSingle} {
		if counts[s] > 0 {
			fmt.Fprintf(&b, "| %s | %d |\n", s, counts[s])
		}
	}

	b.WriteString("\n## Checkpoints\n\n| Scenario | Checkpoint | Status | Changed lines | Details |\n|---|---|---|---|---|\n")
	for _, d := range diffs {
		changed := ""
		if d.Changed > 0 {
			changed = fmt.Sprint(d.Changed)
		}
		link := sanitizeName(d.Scenario) + "/" + sanitizeName(d.Checkpoint) + ".txt"
		fmt.Fprintf(&b, "| %s | %s | %s | %s | [frames](%s) |\n", d.Scenario, d.Checkpoint, d.Status, changed, link)
	}

	var errs []string
	for _, run := range r.Runs {
		for _, t := range r.Targets {
			if res := run.Results[t]; res != nil && res.Err != nil {
				errs = append(errs, fmt.Sprintf("- %s / %s: %v", run.Scenario.Name, t, res.Err))
			}
			if res := run.Results[t]; res != nil && len(res.Unmatched) > 0 {
				errs = append(errs, fmt.Sprintf("- %s / %s: fakeapi turns unmatched: %s", run.Scenario.Name, t, strings.Join(res.Unmatched, "; ")))
			}
		}
	}
	if len(errs) > 0 {
		slices.Sort(errs)
		b.WriteString("\n## Run errors\n\n" + strings.Join(errs, "\n") + "\n")
	}

	var only []string
	for _, run := range r.Runs {
		for _, st := range run.Scenario.Steps {
			if st.Only != "" {
				only = append(only, fmt.Sprintf("| %s | %d | %s | `%s` |", run.Scenario.Name, st.Line, st.Only, st.Src))
			}
		}
	}
	if len(only) > 0 {
		b.WriteString("\n## Target-specific steps\n\nSteps only one target needs: each is a difference in the flow itself.\n\n" +
			"| Scenario | Line | Target | Step |\n|---|---|---|---|\n" + strings.Join(only, "\n") + "\n")
	}

	if r.Normalizer != nil {
		b.WriteString("\n## Normalization rules\n\n| Rule | What it does |\n|---|---|\n")
		for _, d := range r.Normalizer.Describe() {
			fmt.Fprintf(&b, "| %s | %s |\n", d.Name, strings.ReplaceAll(d.Description, "|", "\\|"))
		}
	}
	if r.Allow != nil && len(r.Allow.Entries) > 0 {
		b.WriteString("\n## Allowlisted differences\n\n| Scenario | Checkpoint | Pattern | Reason |\n|---|---|---|---|\n")
		for _, e := range r.Allow.Entries {
			fmt.Fprintf(&b, "| %s | %s | `%s` | %s |\n", or(e.Scenario, "*"), or(e.Checkpoint, "*"), e.Pattern, e.Reason)
		}
	}
	return b.String()
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
