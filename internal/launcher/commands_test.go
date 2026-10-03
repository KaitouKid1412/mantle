package launcher

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func newCommands(t *testing.T) (*Commands, Store, string, *bytes.Buffer, *bytes.Buffer) {
	s, bin := newStore(t)
	var out, errb bytes.Buffer
	c := &Commands{
		Layout: s.L,
		Stdout: &out,
		Stderr: &errb,
		LookPath: func(name string) (string, error) {
			return "/fake/" + name, nil
		},
		RunVersion: func(bin string, args ...string) (string, error) {
			return strings.TrimPrefix(bin, "/fake/") + " 1.0", nil
		},
	}
	return c, s, bin, &out, &errb
}

func TestVersionsCommand(t *testing.T) {
	c, s, bin, out, _ := newCommands(t)
	if code := c.Versions(nil); code != 0 || !strings.Contains(out.String(), "No builds installed") {
		t.Errorf("empty: code %d, out %q", code, out)
	}
	s.Install("b1", bin, Manifest{Created: at(1)})
	s.Install("b2", bin, Manifest{Created: at(2), Mods: []string{"demo", "spinner"}})
	s.SetCurrent("b2")
	s.SetLastGood("b1")
	MarkHealthy(s.L, "b1")
	out.Reset()
	if code := c.Versions(nil); code != 0 {
		t.Fatalf("code %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("output:\n%s", out)
	}
	if !strings.HasPrefix(lines[1], "* b2") || !strings.Contains(lines[1], "demo,spinner") || !strings.Contains(lines[1], "current, probation (0/2 failed)") {
		t.Errorf("b2 line: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "  b1") || !strings.Contains(lines[2], "last-good, healthy") {
		t.Errorf("b1 line: %q", lines[2])
	}
	out.Reset()
	if code := c.Versions([]string{"--json"}); code != 0 {
		t.Fatalf("code %d", code)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil || len(rows) != 2 || rows[0]["id"] != "b2" || rows[0]["current"] != true || rows[1]["healthy"] != true {
		t.Errorf("json = %s (%v)", out, err)
	}
	if code := c.Versions([]string{"--bogus"}); code != 1 {
		t.Errorf("bogus flag code %d", code)
	}
}

func TestRollbackCommand(t *testing.T) {
	c, s, bin, out, errb := newCommands(t)
	if code := c.Rollback(nil); code != 1 {
		t.Errorf("rollback with nothing installed: code %d", code)
	}
	s.Install("b1", bin, Manifest{Created: at(1)})
	s.Install("b2", bin, Manifest{Created: at(2)})
	s.Install("b3", bin, Manifest{Created: at(3)})
	s.SetCurrent("b3")
	s.SetLastGood("b1")
	SaveProbation(s.L, "b2", ProbationState{Failures: 2})

	if code := c.Rollback(nil); code != 0 || s.CurrentID() != "b1" {
		t.Fatalf("rollback to last-good: code %d current %s, %s", code, s.CurrentID(), errb)
	}
	if !strings.Contains(out.String(), "b3 -> b1") {
		t.Errorf("out = %q", out)
	}
	// current == last-good: go to the next older build; none is older than b1.
	if code := c.Rollback(nil); code != 1 {
		t.Errorf("no older build: code %d", code)
	}
	if code := c.Rollback([]string{"b2"}); code != 0 || s.CurrentID() != "b2" {
		t.Fatalf("rollback to b2: code %d current %s", code, s.CurrentID())
	}
	if st := LoadProbation(s.L, "b2"); st.Failures != 0 {
		t.Errorf("explicit rollback should reset the failure counter: %+v", st)
	}
	s.SetLastGood("b2")
	if code := c.Rollback(nil); code != 0 || s.CurrentID() != "b1" {
		t.Errorf("rollback from last-good to older: code %d current %s", code, s.CurrentID())
	}
	if code := c.Rollback([]string{"nope"}); code != 1 {
		t.Errorf("unknown id: code %d", code)
	}
	if code := c.Rollback([]string{"a", "b"}); code != 1 {
		t.Errorf("two args: code %d", code)
	}
}

func TestDoctorCommand(t *testing.T) {
	c, s, bin, out, _ := newCommands(t)
	t.Setenv(EnvClaudeBin, "")
	s.Install("b1", bin, Manifest{Created: at(1)})
	s.SetCurrent("b1")
	s.SetLastGood("b1")
	MarkHealthy(s.L, "b1")
	execed := false
	c.ExecClaude = func([]string) error { execed = true; return nil }
	code := c.Doctor(nil)
	text := out.String()
	for _, want := range []string{"ok    current build b1 (healthy)", "ok    last-good build b1", "ok    go 1.0", "ok    claude claude 1.0", "warn  " + s.L.Src(), "claude doctor"} {
		if !strings.Contains(text, want) {
			t.Errorf("doctor output missing %q:\n%s", want, text)
		}
	}
	if code != 0 {
		t.Errorf("code %d", code)
	}
	if execed {
		t.Error("claude doctor ran without a terminal")
	}

	out.Reset()
	c.LookPath = func(name string) (string, error) { return "", errors.New("not found") }
	if code := c.Doctor(nil); code != 1 || !strings.Contains(out.String(), "FAIL  claude not found") {
		t.Errorf("missing claude: code %d\n%s", code, out)
	}
}
