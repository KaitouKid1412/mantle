package ext

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestToolKey(t *testing.T) {
	cases := map[string]ContentKey{
		"Bash":                    "tool.Bash",
		"mcp__github__create_pr":  "tool.mcp.github.create_pr",
		"mcp__odd":                "tool.mcp__odd",
		"mcp__srv__name__with__x": "tool.mcp.srv.name__with__x",
	}
	for in, want := range cases {
		if got := ToolKey(in); got != want {
			t.Errorf("ToolKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCandidates(t *testing.T) {
	got := ContentKey("tool.mcp.gh.create").Candidates()
	want := []ContentKey{"tool.mcp.gh.create", "tool.mcp.gh.*", "tool.mcp.*", "tool.*", "default"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if !ContentKey("tool.mcp.gh.create").Match(KeyMCPTools) {
		t.Error("tool.mcp.* should match")
	}
	if ContentKey("tool.Bash").Match(KeyMCPTools) {
		t.Error("tool.Bash should not match tool.mcp.*")
	}
}

func TestDefaultBindingsUseKnownIDs(t *testing.T) {
	for _, b := range DefaultBindings {
		if !slices.Contains(Contexts, b.Context) {
			t.Errorf("unknown context %q", b.Context)
		}
		if !slices.Contains(ClaudeActions, b.Action) {
			t.Errorf("unknown action %q", b.Action)
		}
		if b.Keys == "" || strings.TrimSpace(b.Keys) != b.Keys {
			t.Errorf("bad keys %q", b.Keys)
		}
	}
	seen := map[string]bool{}
	for _, b := range DefaultBindings {
		k := b.Context + "|" + b.Keys
		if seen[k] {
			t.Errorf("duplicate default binding %s", k)
		}
		seen[k] = true
	}
}

func TestActionIDsAreFamilyQualified(t *testing.T) {
	seen := map[ActionID]bool{}
	for _, a := range ClaudeActions {
		if seen[a] {
			t.Errorf("duplicate %q", a)
		}
		seen[a] = true
		if !strings.Contains(string(a), ":") || a.IsMantle() {
			t.Errorf("bad action %q", a)
		}
	}
	for from, to := range ActionAliases {
		if !seen[from] || !seen[to] {
			t.Errorf("alias %q -> %q uses unknown IDs", from, to)
		}
	}
}

func TestRegisterRecordsOnly(t *testing.T) {
	before := len(Pending())
	called := false
	Register(Feature{ID: "test.x", Setup: func(Registrar) error { called = true; return nil }})
	t.Cleanup(func() { pending = pending[:before] })
	p := Pending()
	if len(p) != before+1 || p[len(p)-1].ID != "test.x" {
		t.Fatalf("not recorded: %v", p)
	}
	if called {
		t.Fatal("Register must not run Setup")
	}
	p[len(p)-1].ID = "mutated"
	if Pending()[before].ID != "test.x" {
		t.Fatal("Pending must return a copy")
	}
}
