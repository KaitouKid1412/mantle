package patch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func encode(t *testing.T, m map[string]any) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestApply(t *testing.T) {
	tests := []struct {
		name string
		in   string
		p    Patch
		want string
	}{
		{"set on empty", `{}`, SetKey(User, "opus", "model"), `{"model":"opus"}`},
		{"set nested creates", `{}`, SetKey(User, "xhigh", "modelSettings", "claude-opus-5-5", "effortLevel"),
			`{"modelSettings":{"claude-opus-5-5":{"effortLevel":"xhigh"}}}`},
		{"set keeps siblings", `{"theme":"dark","model":"sonnet"}`, SetKey(User, "opus", "model"),
			`{"model":"opus","theme":"dark"}`},
		{"delete", `{"model":"opus","theme":"dark"}`, DeleteKey(User, "model"), `{"theme":"dark"}`},
		{"delete missing nested", `{"theme":"dark"}`, DeleteKey(User, "permissions", "defaultMode"), `{"theme":"dark"}`},
		{"add unique new array", `{}`, Patch{User, []Op{{AddUnique, []string{"permissions", "allow"}, "Bash(git *)"}}},
			`{"permissions":{"allow":["Bash(git *)"]}}`},
		{"add unique dedupes", `{"permissions":{"allow":["Read"]}}`,
			Patch{User, []Op{{AddUnique, []string{"permissions", "allow"}, "Read"}}},
			`{"permissions":{"allow":["Read"]}}`},
		{"remove value", `{"permissions":{"allow":["Read","Edit","Read"]}}`,
			Patch{Local, []Op{{RemoveValue, []string{"permissions", "allow"}, "Read"}}},
			`{"permissions":{"allow":["Edit"]}}`},
		{"remove from missing", `{}`, Patch{Local, []Op{{RemoveValue, []string{"permissions", "deny"}, "Read"}}}, `{}`},
		{"int normalizes", `{"cleanupPeriodDays":30}`, SetKey(User, 45, "cleanupPeriodDays"), `{"cleanupPeriodDays":45}`},
		{"unknown keys survive", `{"futureKey":{"a":[1,2]}}`, SetKey(User, true, "verbose"),
			`{"futureKey":{"a":[1,2]},"verbose":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := decode(t, tt.in)
			before := encode(t, in)
			got, err := tt.p.Apply(in)
			if err != nil {
				t.Fatal(err)
			}
			if g := encode(t, got); g != tt.want {
				t.Errorf("got %s, want %s", g, tt.want)
			}
			if encode(t, in) != before {
				t.Errorf("input was modified")
			}
		})
	}
}

func TestApplyNilDoc(t *testing.T) {
	got, err := SetKey(User, "dark", "theme").Apply(nil)
	if err != nil || got["theme"] != "dark" {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestApplyErrors(t *testing.T) {
	if _, err := SetKey(Policy, "x", "model").Apply(nil); !errors.Is(err, ErrReadOnly) {
		t.Errorf("policy write: %v", err)
	}
	if _, err := SetKey(Flag, "x", "model").Apply(nil); !errors.Is(err, ErrReadOnly) {
		t.Errorf("flag write: %v", err)
	}
	if _, err := SetKey(User, "x").Apply(nil); err == nil {
		t.Error("empty path accepted")
	}
	doc := decode(t, `{"permissions":"oops"}`)
	if _, err := (Patch{User, []Op{{AddUnique, []string{"permissions", "allow"}, "Read"}}}).Apply(doc); err == nil {
		t.Error("non-object parent accepted")
	}
	doc = decode(t, `{"permissions":{"allow":"Read"}}`)
	if _, err := (Patch{User, []Op{{AddUnique, []string{"permissions", "allow"}, "Edit"}}}).Apply(doc); err == nil {
		t.Error("non-array target accepted")
	}
}

func TestDescribe(t *testing.T) {
	p := Patch{User, []Op{
		{Set, []string{"model"}, "opus"},
		{AddUnique, []string{"permissions", "allow"}, "Read"},
		{RemoveValue, []string{"permissions", "deny"}, "Edit"},
		{Delete, []string{"theme"}, nil},
	}}
	want := []string{
		`User: model = "opus"`,
		`User: add "Read" to permissions.allow`,
		`User: remove "Edit" from permissions.deny`,
		`User: unset theme`,
	}
	got := p.Describe()
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestPaths(t *testing.T) {
	home := t.TempDir()
	e := Env{Home: home, ProjectRoot: "/work/repo"}
	cases := map[Scope]string{
		User:    filepath.Join(home, ".claude", "settings.json"),
		Project: "/work/repo/.claude/settings.json",
		Local:   "/work/repo/.claude/settings.local.json",
	}
	for s, want := range cases {
		got, err := e.Path(s)
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", s, got, err, want)
		}
	}
	if _, err := e.Path(Policy); !errors.Is(err, ErrReadOnly) {
		t.Errorf("policy path: %v", err)
	}
	e.ConfigDir = filepath.Join(home, "alt")
	if got, _ := e.Path(User); got != filepath.Join(home, "alt", "settings.json") {
		t.Errorf("CLAUDE_CONFIG_DIR user path: %q", got)
	}
	if _, err := (Env{Home: home}).Path(Project); err == nil {
		t.Error("project path without root accepted")
	}
}

// No code path in this area may write ~/.claude.json.
func TestGlobalConfigNeverWritable(t *testing.T) {
	home := t.TempDir()
	e := Env{Home: home}
	for _, p := range []string{
		filepath.Join(home, ".claude.json"),
		filepath.Join(home, "x", "..", ".claude.json"),
		"/anywhere/.claude.json",
	} {
		if err := e.CheckWritable(p); !errors.Is(err, ErrGlobalConfig) {
			t.Errorf("%s: %v", p, err)
		}
	}
	// A symlink pointing at the global config is refused too.
	target := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "settings-link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := e.CheckWritable(link); !errors.Is(err, ErrGlobalConfig) {
		t.Errorf("symlink: %v", err)
	}
	e.ConfigDir = filepath.Join(home, "cfg")
	if err := e.CheckWritable(filepath.Join(home, "cfg", ".claude.json")); !errors.Is(err, ErrGlobalConfig) {
		t.Errorf("config dir global: %v", err)
	}
	if err := e.CheckWritable(filepath.Join(home, ".claude", "settings.json")); err != nil {
		t.Errorf("settings.json refused: %v", err)
	}
}

func TestScopeOrder(t *testing.T) {
	if !(User.Precedence() < Project.Precedence() && Project.Precedence() < Local.Precedence() &&
		Local.Precedence() < Flag.Precedence() && Flag.Precedence() < Policy.Precedence()) {
		t.Error("precedence order wrong")
	}
	if Policy.Writable() || Flag.Writable() || !User.Writable() {
		t.Error("writable wrong")
	}
}
