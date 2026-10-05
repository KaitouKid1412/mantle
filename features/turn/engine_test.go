package turn

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeStub(t *testing.T, version string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "claude-stub")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho '"+version+" (Claude Code)'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// The adapter maps plan 02's CheckEngine. With MANTLE_CLAUDE_BIN set it checks the
// version only (no probe, no real claude); a version below the minimum fails.
func TestEngineAdapter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MANTLE_CLAUDE_BIN", writeStub(t, "2.1.288"))
	c, err := engineAdapter{}.Check(context.Background())
	if err != nil || !c.OK || c.Probed || c.Version != "2.1.288" {
		t.Fatalf("override check = %+v, %v", c, err)
	}
	t.Setenv("MANTLE_CLAUDE_BIN", writeStub(t, "2.1.100"))
	c, err = engineAdapter{}.Check(context.Background())
	if err != nil || c.OK || !slices.ContainsFunc(c.Failed, func(s string) bool { return strings.HasPrefix(s, "older than") }) {
		t.Fatalf("old version = %+v, %v", c, err)
	}
	t.Setenv("MANTLE_CLAUDE_BIN", filepath.Join(t.TempDir(), "missing"))
	if _, err := (engineAdapter{}).Check(context.Background()); err == nil {
		t.Fatal("a missing engine is an error")
	}
}
