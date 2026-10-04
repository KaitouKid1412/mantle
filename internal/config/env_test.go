package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func TestReadEnvAndSyntaxOverride(t *testing.T) {
	env := map[string]string{
		"CLAUDE_CODE_DISABLE_TERMINAL_TITLE": "1",
		"CLAUDE_CODE_SYNTAX_HIGHLIGHT":       "false",
		"CLAUDE_CODE_SCROLL_SPEED":           "3",
		"CLAUDE_CODE_DISABLE_MOUSE":          "true",
		"NO_COLOR":                           "",
	}
	e := ReadEnv(func(k string) string { return env[k] })
	if !e.DisableTerminalTitle || !e.DisableMouse || e.ScrollSpeed != 3 || e.SyntaxHighlight == nil || *e.SyntaxHighlight {
		t.Fatalf("env = %+v", e)
	}
	if e.DisableAltScreen || e.NoFlicker {
		t.Fatal("unset variables must stay off")
	}

	p := testPaths(t)
	write(t, p.ScopeFile(ext.ScopeUser), `{"syntaxHighlightingDisabled": false}`)
	s := NewStore(p, "")
	s.SetEnv(e)
	if !s.UI().SyntaxHighlightingDisabled {
		t.Fatal("CLAUDE_CODE_SYNTAX_HIGHLIGHT=false must win over the setting")
	}
}

func TestManagedPlist(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("plutil is macOS-only")
	}
	if _, err := exec.LookPath(plutil); err != nil {
		t.Skip("no plutil")
	}
	p := testPaths(t)
	write(t, p.Managed, `{"permissions":{"defaultMode":"default"},"companyAnnouncements":["from json"]}`)
	plist := filepath.Join(t.TempDir(), MDMDomain+".plist")
	write(t, plist, `{"permissions":{"defaultMode":"plan"},"cleanupPeriodDays":7}`)
	if out, err := exec.Command(plutil, "-convert", "xml1", plist).CombinedOutput(); err != nil {
		t.Fatalf("plutil: %v %s", err, out)
	}
	p.ManagedPlists = []string{plist, filepath.Join(t.TempDir(), "missing.plist")}
	s := NewStore(p, "")
	pol := s.ClaudeScope(ext.ScopePolicy)
	if pol["permissions"].(map[string]any)["defaultMode"] != "plan" {
		t.Fatalf("MDM plist must win within the policy scope: %v", pol)
	}
	if pol["cleanupPeriodDays"] != 7.0 || len(s.UI().CompanyAnnouncements) != 1 {
		t.Fatalf("policy = %v", pol)
	}

	// A broken plist is reported, not fatal.
	bad := filepath.Join(t.TempDir(), "bad.plist")
	os.WriteFile(bad, []byte("not a plist"), 0o644)
	p.ManagedPlists = []string{bad}
	s = NewStore(p, "")
	for _, src := range s.ClaudeSources() {
		if src.Scope == ext.ScopePolicy && src.Err == nil {
			t.Fatal("broken plist should set the policy source's Err")
		}
	}
}
