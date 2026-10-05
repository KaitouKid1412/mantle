package parity

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// ParityRow is one feature row of docs/PARITY.md.
type ParityRow struct {
	ID, Feature, Code, Milestone, Plan, Part string
	Area                                     string // the "## " section it sits in
}

// ParityStatus joins the PARITY.md rows with the evidence in the repository: Parity
// tags on registrations (code) and parity: headers of side-by-side scenarios.
type ParityStatus struct {
	Rows      []ParityRow
	Tags      map[string][]string // row ID → "file:line" of each Parity tag
	Scenarios map[string][]string // row ID → scenario names
	Unknown   []string            // IDs referenced by tags or scenarios but not in PARITY.md
	Requests  []Request           // plan 12's triage requests
	Evidence  map[string]string   // row ID → evidence for rows no registration names (docs/parity-evidence/)
}

// Request is one docs/plans/requests/12-*.md file and its Status line.
type Request struct {
	File, Title, Status string
}

var (
	rowID    = regexp.MustCompile(`^[A-Z]+-\d+$`)
	tagRe    = regexp.MustCompile(`Parity:\s*(\[\]string\{[^}]*\}|"[^"]*")`)
	idRe     = regexp.MustCompile(`\b[A-Z]{2,4}-\d+\b`)
	statusRe = regexp.MustCompile(`^\*\*Status:\*\*\s*(.*)$`)
)

// ParseParityTable reads the feature rows of PARITY.md (| ID | Feature | What | Code |
// M | Plan | Part | Notes |). Index tables further down repeat IDs in other columns
// and are skipped because their first cell is not an ID.
func ParseParityTable(text string) []ParityRow {
	var rows []ParityRow
	area := ""
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if h, ok := strings.CutPrefix(line, "## "); ok {
			area = strings.TrimSpace(h)
			continue
		}
		cells := splitRow(line)
		if len(cells) < 7 || !rowID.MatchString(cells[0]) {
			continue
		}
		rows = append(rows, ParityRow{ID: cells[0], Feature: cells[1], Code: cells[3],
			Milestone: cells[4], Plan: cells[5], Part: cells[6], Area: area})
	}
	return rows
}

// splitRow splits a markdown table row on unescaped pipes.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil
	}
	var cells []string
	var cur strings.Builder
	for i := 1; i < len(line)-1; i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line)-1 && line[i+1] == '|':
			cur.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// ScanParityTags finds Parity tags (Parity: []string{"CH-01", …} or Parity: "CL-04")
// in the non-test Go files under root's source directories.
func ScanParityTags(root string) (map[string][]string, error) {
	tags := map[string][]string{}
	for _, dir := range []string{"cmd", "features", "internal", "mods", "pkg"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			text := string(data)
			for _, m := range tagRe.FindAllStringIndex(text, -1) {
				line := strings.Count(text[:m[0]], "\n") + 1
				for _, id := range idRe.FindAllString(text[m[0]:m[1]], -1) {
					tags[id] = append(tags[id], fmt.Sprintf("%s:%d", filepath.ToSlash(rel), line))
				}
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return tags, nil
}

// LoadRequests reads plan 12's request files and their Status lines.
func LoadRequests(dir string) ([]Request, error) {
	files, err := filepath.Glob(filepath.Join(dir, "12-*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []Request
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		r := Request{File: filepath.Base(f)}
		for _, line := range strings.Split(string(data), "\n") {
			if t, ok := strings.CutPrefix(line, "# "); ok && r.Title == "" {
				r.Title = strings.TrimSpace(t)
			}
			if m := statusRe.FindStringSubmatch(line); m != nil && r.Status == "" {
				r.Status = m[1]
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// BuildParityStatus gathers everything for the report from the repository at root.
func BuildParityStatus(root string) (*ParityStatus, error) {
	data, err := os.ReadFile(filepath.Join(root, "docs", "PARITY.md"))
	if err != nil {
		return nil, err
	}
	st := &ParityStatus{Rows: ParseParityTable(string(data)), Scenarios: map[string][]string{}}
	if st.Tags, err = ScanParityTags(root); err != nil {
		return nil, err
	}
	scs, err := LoadScenarios(filepath.Join(root, "test", "parity", "scenarios"))
	if err != nil {
		return nil, err
	}
	for _, sc := range scs {
		for _, id := range sc.Parity {
			st.Scenarios[id] = append(st.Scenarios[id], sc.Name)
		}
	}
	if st.Requests, err = LoadRequests(filepath.Join(root, "docs", "plans", "requests")); err != nil {
		return nil, err
	}
	if st.Evidence, err = LoadEvidenceDir(filepath.Join(root, "docs", "parity-evidence")); err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, r := range st.Rows {
		known[r.ID] = true
	}
	seen := map[string]bool{}
	for _, m := range []map[string][]string{st.Tags, st.Scenarios} {
		for id := range m {
			if !known[id] && !seen[id] {
				seen[id] = true
				st.Unknown = append(st.Unknown, id)
			}
		}
	}
	for id := range st.Evidence {
		if !known[id] && !seen[id] {
			seen[id] = true
			st.Unknown = append(st.Unknown, id)
		}
	}
	sort.Strings(st.Unknown)
	return st, nil
}

// RowState classifies a row: "gap" (X), "hand-off" (H), "compared" (tagged and in a
// side-by-side scenario), "tagged" (registration tagged) or "untagged".
func (st *ParityStatus) RowState(r ParityRow) string {
	ev := st.Evidence[r.ID]
	switch {
	case strings.HasPrefix(r.Code, "X") || evidenceIs(ev, "gap"):
		return "gap"
	case strings.HasPrefix(r.Code, "H"):
		return "hand-off"
	case evidenceIs(ev, "not applicable"):
		return "not applicable"
	}
	tagged, scen := len(st.Tags[r.ID]) > 0, len(st.Scenarios[r.ID]) > 0
	switch {
	case (tagged || ev != "") && scen:
		return "compared"
	case tagged:
		return "tagged"
	case ev != "":
		return "evidenced"
	case scen:
		return "scenario only"
	}
	return "untagged"
}

var rowStates = []string{"compared", "tagged", "evidenced", "scenario only", "untagged", "hand-off", "gap", "not applicable"}

// evidenceIs reports evidence that classifies the row instead of proving it: "Not
// applicable: …" (a feature Claude Code offers only where mantle's engine never runs) or
// "Gap: …" (a known gap PARITY.md doesn't mark X, with the reason).
func evidenceIs(evidence, kind string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(evidence)), kind)
}

// LoadEvidenceDir reads every *.md file in dir (docs/parity-evidence/NN.md, one per
// plan) with LoadEvidence.
func LoadEvidenceDir(dir string) (map[string]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	all := map[string]string{}
	for _, f := range files {
		if strings.EqualFold(filepath.Base(f), "README.md") {
			continue // the format description and its example
		}
		ev, err := LoadEvidence(f)
		if err != nil {
			return nil, err
		}
		for id, e := range ev {
			all[id] = e
		}
	}
	return all, nil
}

// LoadEvidence reads an evidence file: table rows "| ID | evidence |" for PARITY
// rows that no registration can name (engine plumbing, libraries, launcher internals),
// each pointing at the tests or code that cover it. A missing file is no evidence.
func LoadEvidence(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	ev := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		cells := splitRow(line)
		if len(cells) >= 2 && rowID.MatchString(cells[0]) && cells[1] != "" {
			ev[cells[0]] = cells[1]
		}
	}
	return ev, nil
}

// Markdown renders the generated report.
func (st *ParityStatus) Markdown() string {
	var b strings.Builder
	b.WriteString("# Parity status (generated)\n\n")
	b.WriteString("Generated by `make parity` from docs/PARITY.md, the `Parity:` tags on registrations " +
		"and the `parity:` headers of the side-by-side scenarios in test/parity/scenarios. Do not edit by hand.\n\n")
	b.WriteString("States: **compared**, a tagged (or evidenced) row that a side-by-side scenario also exercises " +
		"against Claude Code; **tagged**, a registration names the row; **evidenced**, no registration can " +
		"name it but docs/parity-evidence/ points at the tests or code that cover it; **scenario only**, a " +
		"scenario covers it but no registration names it; **untagged**, no evidence yet; **hand-off** (H) and " +
		"**gap** (X) as PARITY.md classifies them, or evidence that starts \"Gap\"; **not applicable**, " +
		"evidence that starts \"Not applicable\" (a feature Claude Code offers only where mantle's engine " +
		"never runs).\n\n")

	count := func(rows []ParityRow) map[string]int {
		c := map[string]int{}
		for _, r := range rows {
			c[st.RowState(r)]++
		}
		return c
	}
	header := "| %s | Rows | " + strings.Join(rowStates, " | ") + " |\n"
	sep := "|---|---:|" + strings.Repeat("---:|", len(rowStates)) + "\n"
	line := func(name string, rows []ParityRow) string {
		c := count(rows)
		cells := []string{name, fmt.Sprint(len(rows))}
		for _, s := range rowStates {
			cells = append(cells, fmt.Sprint(c[s]))
		}
		return "| " + strings.Join(cells, " | ") + " |\n"
	}

	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, header, "")
	b.WriteString(sep)
	b.WriteString(line("All rows", st.Rows))

	b.WriteString("\n## By owner plan\n\n")
	fmt.Fprintf(&b, header, "Plan")
	b.WriteString(sep)
	byPlan := map[string][]ParityRow{}
	var plans []string
	for _, r := range st.Rows {
		if _, ok := byPlan[r.Plan]; !ok {
			plans = append(plans, r.Plan)
		}
		byPlan[r.Plan] = append(byPlan[r.Plan], r)
	}
	sort.Strings(plans)
	for _, p := range plans {
		b.WriteString(line(p, byPlan[p]))
	}

	b.WriteString("\n## By area\n\n")
	fmt.Fprintf(&b, header, "Area")
	b.WriteString(sep)
	byArea := map[string][]ParityRow{}
	var areas []string
	for _, r := range st.Rows {
		if _, ok := byArea[r.Area]; !ok {
			areas = append(areas, r.Area)
		}
		byArea[r.Area] = append(byArea[r.Area], r)
	}
	for _, a := range areas {
		b.WriteString(line(a, byArea[a]))
	}

	if len(st.Requests) > 0 {
		b.WriteString("\n## Plan 12 triage requests\n\n| Request | Status |\n|---|---|\n")
		for _, r := range st.Requests {
			fmt.Fprintf(&b, "| [%s](plans/requests/%s) | %s |\n", r.Title, r.File, mdCell(r.Status))
		}
	}

	if len(st.Unknown) > 0 {
		b.WriteString("\n## Unknown IDs\n\nReferenced by a tag or scenario but not a PARITY.md row: " +
			strings.Join(st.Unknown, ", ") + ".\n")
	}

	b.WriteString("\n## Rows\n\n| ID | Feature | Code | Plan | State | Evidence |\n|---|---|---|---|---|---|\n")
	for _, r := range st.Rows {
		var ev []string
		if sc := st.Scenarios[r.ID]; len(sc) > 0 {
			ev = append(ev, "scenarios: "+strings.Join(uniq(sc), ", "))
		}
		if e := st.Evidence[r.ID]; e != "" && len(st.Tags[r.ID]) == 0 {
			ev = append(ev, "evidence: "+e)
		}
		if t := st.Tags[r.ID]; len(t) > 0 {
			shown := uniq(t)
			if n := len(shown) - 2; n > 0 {
				shown = append(shown[:2:2], fmt.Sprintf("+%d", n))
			}
			ev = append(ev, "tags: "+strings.Join(shown, ", "))
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", r.ID, mdCell(r.Feature), r.Code, r.Plan,
			st.RowState(r), mdCell(strings.Join(ev, "; ")))
	}
	return b.String()
}

func mdCell(s string) string { return strings.ReplaceAll(s, "|", "\\|") }

func uniq(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return slices.Compact(out)
}
