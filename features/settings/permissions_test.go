package settings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/features/settings/settingsfile"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// permRig writes real files in the rig's temp home, so the panel reloads what it wrote.
func permRig(t *testing.T) *rig {
	t.Helper()
	g := newRig(t)
	env := patch.Env{Home: g.home, ProjectRoot: filepath.Join(g.home, "repo"), Managed: filepath.Join(g.home, "managed.json")}
	g.a.env = func(ext.Ctx) patch.Env { return env }
	g.a.write = func(e patch.Env, p patch.Patch) (string, error) {
		g.writes = append(g.writes, p)
		return writeFile(e, p)
	}
	write := func(path, body string) {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(g.home, ".claude", "settings.json"),
		`{"permissions":{"allow":["Bash(git status)"],"additionalDirectories":["/srv/shared"]}}`)
	write(filepath.Join(g.home, "repo", ".claude", "settings.json"), `{"permissions":{"deny":["Read(./.env)"]}}`)
	write(env.Managed, `{"permissions":{"allow":["Bash(make lint)"]},"autoMode":{"hard_deny":["Never push to main"]}}`)
	return g
}

func readScope(t *testing.T, g *rig, s patch.Scope) map[string]any {
	t.Helper()
	return settingsfile.ReadScope(g.a.env(nil), s).Doc
}

func TestPermissionsAddRule(t *testing.T) {
	g := permRig(t)
	g.command("permissions", "")
	g.mustContain(160, "[Allow]", "Add a new rule", "Bash(make lint)", "Managed", "Bash(git status)", "User")

	g.press(ext.ActSelectAccept) // "Add a new rule…"
	typeText(g, "Bash(make test")
	g.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	g.mustContain(160, "unbalanced")
	typeText(g, ")")
	g.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	g.mustContain(160, "Save Bash(make test) where?", "This project, just me")
	g.press(ext.ActSelectAccept) // local
	local := readScope(t, g, patch.Local)
	if jsonOf(local) != `{"permissions":{"allow":["Bash(make test)"]}}` {
		t.Errorf("local settings %s", jsonOf(local))
	}
	g.mustContain(160, "Bash(make test)", "Local")
	if !strings.Contains(lastNotice(g), "Added Bash(make test) to local settings") {
		t.Errorf("notice %q", lastNotice(g))
	}
}

func TestPermissionsRemoveAndReadOnly(t *testing.T) {
	g := permRig(t)
	g.command("permissions", "")
	// Managed rule: read-only.
	p := g.dialog.(*permPanel)
	for i, it := range p.items() {
		if it.raw == "Bash(make lint)" {
			p.cursor = i
		}
	}
	g.press(ext.ActSelectAccept)
	if !strings.Contains(lastNotice(g), "managed settings") || p.step != permStepBrowse {
		t.Errorf("managed: %q step %v", lastNotice(g), p.step)
	}
	for i, it := range p.items() {
		if it.raw == "Bash(git status)" {
			p.cursor = i
		}
	}
	g.press(ext.ActSelectAccept)
	g.mustContain(160, "Remove Bash(git status) from user settings?")
	g.press(ext.ActConfirmYes)
	user := readScope(t, g, patch.User)
	if jsonOf(user) != `{"permissions":{"additionalDirectories":["/srv/shared"],"allow":[]}}` {
		t.Errorf("user settings %s", jsonOf(user))
	}
}

func TestPermissionsDenyDirsAndSession(t *testing.T) {
	g := permRig(t)
	g.eng.responses[proto.SubListPermissionRules] = map[string]any{"state": map[string]any{
		"rules": []any{
			map[string]any{"behavior": "allow", "source": "session", "rule": "Bash(ls)"},
			map[string]any{"behavior": "allow", "source": "userSettings", "rule": "Bash(git status)"},
		},
		"originalCwd": "/work/repo",
	}}
	g.command("permissions", "")
	g.mustContain(160, "Bash(ls)", "session")
	g.press(ext.ActTabsNext, ext.ActTabsNext) // Deny
	g.mustContain(160, "[Deny]", "Read(./.env)", "Project")
	g.press(ext.ActTabsNext) // Workspace
	g.mustContain(160, "/work/repo", "where this session started", "/srv/shared")
	g.press(ext.ActSelectAccept)
	typeText(g, "/data/models")
	g.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	g.press(ext.ActSelectNext, ext.ActSelectAccept) // project, shared
	proj := readScope(t, g, patch.Project)
	if jsonOf(proj) != `{"permissions":{"additionalDirectories":["/data/models"],"deny":["Read(./.env)"]}}` {
		t.Errorf("project %s", jsonOf(proj))
	}
}

func TestPermissionsAutoMode(t *testing.T) {
	g := permRig(t)
	g.a.autoModeDefaults = func(context.Context) (AutoModeRules, error) {
		return AutoModeRules{SoftDeny: []string{"Deleting files outside the project"}}, nil
	}
	g.command("permissions", "")
	g.press(ext.ActTabsPrevious) // wraps to Auto mode
	g.mustContain(160, "[Auto mode]", "Never (hard deny)", "Never push to main", "Built-in ask first",
		"Deleting files outside the project")
	g.press(ext.ActSelectAccept) // Add a rule…
	g.press(ext.ActSelectAccept) // list: Allow
	typeText(g, "Running the test suite")
	g.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	g.press(ext.ActSelectNext, ext.ActSelectNext, ext.ActSelectAccept) // user
	if v := readScope(t, g, patch.User)["autoMode"]; jsonOf(v) != `{"allow":["Running the test suite"]}` {
		t.Errorf("autoMode %s", jsonOf(v))
	}

	// Without the runner the tab says why the defaults are missing.
	g2 := permRig(t)
	g2.command("permissions", "")
	g2.press(ext.ActTabsPrevious)
	g2.mustContain(160, "Built-in rules: the built-in rules can't be listed yet")
}

func TestPermissionsEscapeSteps(t *testing.T) {
	g := permRig(t)
	g.command("permissions", "")
	g.press(ext.ActSelectAccept)
	if g.dialog.(ext.ContextStack).KeyContexts() != nil {
		t.Error("typing step should take every key")
	}
	g.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if g.dialog.(*permPanel).step != permStepBrowse {
		t.Error("esc did not leave the text field")
	}
	g.press(ext.ActSelectCancel)
	if g.dialog != nil {
		t.Error("esc did not close")
	}
}
