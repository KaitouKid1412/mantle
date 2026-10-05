package parity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseParityTable(t *testing.T) {
	rows := ParseParityTable(`# PARITY
## Views
| ID | Feature | What | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| VW-15 | Fullscreen renderer | ` + "`/tui fullscreen`" + ` | N | M3 | 12 | B | |
| SE-09 | ` + "`-r <id\\|name>`" + ` | resume | N | M1 | 06 | B | |
## Index
| ` + "`/tui`" + ` | | local-jsx | no | N | VW-15 |
`)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if r := rows[0]; r.ID != "VW-15" || r.Code != "N" || r.Plan != "12" || r.Area != "Views" {
		t.Errorf("row 0 = %+v", r)
	}
	if r := rows[1]; r.Feature != "`-r <id|name>`" || r.Plan != "06" {
		t.Errorf("escaped pipe: %+v", r)
	}
}

func TestParityStatusFromRepo(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/PARITY.md", `## Chrome
| ID | Feature | What | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| CH-01 | Frame | | N | M1 | 07 | B | |
| CH-02 | Colour | | N | M1 | 07 | B | |
| CH-03 | Width | | N | M1 | 07 | B | |
| CL-04 | Mobile | | H | M2 | 09 | B | |
`)
	write("features/chrome/chrome.go", "package chrome\nvar f = Feature{\n\tParity: []string{\"CH-01\", \"CH-02\"},\n}\nvar c = Command{Parity: \"ZZ-9\"}\n")
	write("features/chrome/chrome_test.go", "package chrome\nvar x = Feature{Parity: []string{\"CH-03\"}}\n")
	write("test/parity/scenarios/frame.scn", "name: frame\nparity: CH-01\n---\nready\ncheckpoint c\n")
	write("docs/plans/requests/12-07-chrome.md", "# 12 → 07: chrome\n\n**Status:** done by 07.\n")
	write("docs/parity-evidence/07.md", "| ID | Evidence |\n|---|---|\n| CH-03 | internal/term width tests |\n")
	st, err := BuildParityStatus(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"CH-01": "compared", "CH-02": "tagged", "CH-03": "evidenced", "CL-04": "hand-off"}
	for _, r := range st.Rows {
		if got := st.RowState(r); got != want[r.ID] {
			t.Errorf("%s = %s, want %s", r.ID, got, want[r.ID])
		}
	}
	if len(st.Unknown) != 1 || st.Unknown[0] != "ZZ-9" {
		t.Errorf("unknown = %v", st.Unknown)
	}
	if got := st.Tags["CH-01"]; len(got) != 1 || got[0] != "features/chrome/chrome.go:3" {
		t.Errorf("tag location = %v", got)
	}
	md := st.Markdown()
	for _, s := range []string{"| All rows | 4 | 1 | 1 | 1 | 0 | 0 | 1 | 0 |", "evidence: internal/term width tests", "| 07 | 3 |", "done by 07.",
		"| CH-01 | Frame | N | 07 | compared | scenarios: frame; tags: features/chrome/chrome.go:3 |", "ZZ-9"} {
		if !strings.Contains(md, s) {
			t.Errorf("report lacks %q:\n%s", s, md)
		}
	}
}
