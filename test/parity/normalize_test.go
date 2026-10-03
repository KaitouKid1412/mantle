package parity

import (
	"slices"
	"strings"
	"testing"
)

func TestNormalizeLine(t *testing.T) {
	ws := Workspace{Root: "/r/parity-x-1", WorkDir: "/r/parity-x-1/work", ConfigDir: "/r/parity-x-1/config", HomeDir: "/r/parity-x-1/home"}
	n := DefaultNormalizer()
	tests := []struct{ in, want string }{
		{"✻ Pondering… (12s · ↑ 1.2k tokens · esc to interrupt)", "<spinner>"},
		{"  ✶ Cogitating…", "  <spinner>"},
		{"⏺ Hello from fakeapi.", "⏺ Hello from fakeapi."},
		{"· Thinking about it", "· Thinking about it"}, // no ellipsis: not a spinner
		{"cwd: /r/parity-x-1/work/src", "cwd: <work>/src"},
		{"cfg /r/parity-x-1/config/.claude.json", "cfg <config>/.claude.json"},
		{"tmp /var/folders/ab/cd/T/x.txt and /tmp/y", "tmp <tmp> and <tmp>"},
		{"session 550e8400-e29b-41d4-a716-446655440000", "session <uuid>"},
		{"tool toolu_fake_3 done", "tool <toolu> done"},
		{"at 2026-10-03T14:05:09Z", "at <date>"},
		{"at 14:05 and 2:05 PM", "at <time> and <time>"},
		{"cost $0.0123 total $12", "cost $<cost> total $<cost>"},
		{"↓ 120 tokens, 12.3k tokens", "<n> tokens, <n> tokens"},
		{"took 850ms, 12s, 1m 5s, 2h 3m, 3.2s", "took <dur>, <dur>, <dur>, <dur>, <dur>"},
		{"73% left", "<pct>% left"},
		{"\x1b[31mred\x1b[0m   ", "red"},
		{"size 80x24 stays", "size 80x24 stays"},
	}
	for _, tt := range tests {
		if got := n.Line(tt.in, ws); got != tt.want {
			t.Errorf("Line(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeFrame(t *testing.T) {
	f := Frame{Scrollback: []string{"old 1", "old 2", ""}, Screen: []string{"top", "", "bottom", "", ""}}
	n := DefaultNormalizer()
	got := n.Frame(f, Workspace{})
	want := []string{"old 1", "old 2", ScreenMarker, "top", "", "bottom"}
	if !slices.Equal(got, want) {
		t.Errorf("Frame = %q", got)
	}
	n.Scrollback = 1
	if got := n.Frame(f, Workspace{}); !slices.Equal(got[:2], []string{"old 2", ScreenMarker}) {
		t.Errorf("tail = %q", got)
	}
	n.Scrollback = 0
	if got := n.Frame(f, Workspace{}); got[0] != "top" {
		t.Errorf("no scrollback = %q", got)
	}
	n.Scrollback = -1
	if got := n.Frame(Frame{Screen: []string{"x"}}, Workspace{}); !slices.Equal(got, []string{"x"}) {
		t.Errorf("empty scrollback has no marker: %q", got)
	}
}

func TestDescribeListsEveryRule(t *testing.T) {
	n := DefaultNormalizer()
	docs := n.Describe()
	names := map[string]bool{}
	for _, d := range docs {
		if d.Description == "" {
			t.Errorf("%s has no description", d.Name)
		}
		names[d.Name] = true
	}
	for _, r := range n.Rules {
		if !names[r.Name] {
			t.Errorf("rule %s missing from Describe", r.Name)
		}
	}
	for _, want := range []string{"workspace", "strip-color", "trim", "scrollback"} {
		if !names[want] {
			t.Errorf("%s missing", want)
		}
	}
	if !strings.Contains(docs[0].Description, "<work>") {
		t.Error("workspace doc")
	}
}
