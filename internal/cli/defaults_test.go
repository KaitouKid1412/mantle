package cli

import "testing"

func TestDefaultsFor(t *testing.T) {
	for _, c := range []struct{ version, tui string }{
		{"2.1.288", "default"},
		{"2.1.100", "default"},
		{"1.9.999", "default"},
		{"2.1.289", "fullscreen"},
		{"2.1.290", "fullscreen"},
		{"2.2.0", "fullscreen"},
		{"3.0.0", "fullscreen"},
		{"2.1.289 (Claude Code)", "fullscreen"},
		{"2.1.288-beta.1", "default"},
		{"", "fullscreen"},
		{"nonsense", "fullscreen"},
	} {
		if got := DefaultsFor(c.version).TUI; got != c.tui {
			t.Errorf("DefaultsFor(%q).TUI = %q, want %q", c.version, got, c.tui)
		}
	}
}

func TestDefaultsTableOrdered(t *testing.T) {
	for i := 1; i < len(DefaultsTable); i++ {
		a, okA := parseVersion(DefaultsTable[i-1].Since)
		b, okB := parseVersion(DefaultsTable[i].Since)
		if !okA || !okB || compareVersions(a, b) >= 0 {
			t.Errorf("entries %d and %d out of order", i-1, i)
		}
	}
	for _, e := range DefaultsTable {
		if e.Defaults.TUI != "default" && e.Defaults.TUI != "fullscreen" {
			t.Errorf("%s: tui %q", e.Since, e.Defaults.TUI)
		}
	}
}
