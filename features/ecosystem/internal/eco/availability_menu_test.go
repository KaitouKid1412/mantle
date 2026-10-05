package eco_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
)

// TestMenuParityGates covers what claude 2.1.289's / menu leaves out for an
// API-key account (side-by-side probe): cloud and claude.ai features, and
// /import when no other agent's config exists.
func TestMenuParityGates(t *testing.T) {
	home := t.TempDir()
	env := func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	h := eco.HiddenCommands(eco.AuthConsole, env)
	for _, n := range []string{"remote-control", "session", "cloud-plugins", "passes", "privacy-settings",
		"design-login", "daemon", "usage-credits", "import"} {
		if !h[n] {
			t.Errorf("%s should be hidden for a console account with no other agent config", n)
		}
	}
	for _, n := range []string{"mobile", "ide", "feedback", "powerup", "radio", "stickers", "statusline", "workflows", "plugin"} {
		if h[n] {
			t.Errorf("%s is in claude's menu and must stay visible", n)
		}
	}
	if h := eco.HiddenCommands(eco.AuthClaudeAI, env); h["remote-control"] || h["session"] {
		t.Errorf("claude.ai keeps the cloud commands: %v", h)
	}
	if err := os.Mkdir(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if eco.HiddenCommands(eco.AuthConsole, env)["import"] {
		t.Error("/import shows once another agent's config exists")
	}
}
