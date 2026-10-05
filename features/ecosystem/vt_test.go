package ecosystem_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/agents"
	"github.com/KaitouKid1412/mantle/features/ecosystem/agentview"
	"github.com/KaitouKid1412/mantle/features/ecosystem/auth"
	"github.com/KaitouKid1412/mantle/features/ecosystem/doctor"
	"github.com/KaitouKid1412/mantle/features/ecosystem/handoff"
	"github.com/KaitouKid1412/mantle/features/ecosystem/hooks"
	"github.com/KaitouKid1412/mantle/features/ecosystem/importcfg"
	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	"github.com/KaitouKid1412/mantle/features/ecosystem/mcp"
	"github.com/KaitouKid1412/mantle/features/ecosystem/memory"
	"github.com/KaitouKid1412/mantle/features/ecosystem/plugins"
	"github.com/KaitouKid1412/mantle/features/ecosystem/skills"
	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/claudecli/discovery"
	checks "github.com/KaitouKid1412/mantle/internal/claudecli/doctor"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func fixture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/fixtures/09/" + path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// world sets up everything the panels read: a fake claude on PATH, a home and
// project tree, a scripted engine and canned doctor checks.
func world(t *testing.T) (cwd string, eng *ecotest.Engine) {
	t.Helper()
	ecotest.FakeClaude(t, map[string]string{
		"--available":      fixture(t, "claudecli/plugin-list-available.json"),
		"marketplace list": fixture(t, "claudecli/marketplace-list.json"),
		"agents --json":    fixture(t, "claudecli/agents.json"),
		"auth status":      fixture(t, "claudecli/auth-status.json"),
		"--dry-run":        fixture(t, "claudecli/import-preview.txt"),
	})
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd = filepath.Join(root, "proj")
	write(t, filepath.Join(home, ".claude", "CLAUDE.md"), "User memory\n")
	// A fixed auto-memory folder keeps the temp path's slug out of the goldens.
	write(t, filepath.Join(home, ".claude", "settings.json"), `{"autoMemoryDirectory": "~/memory"}`)
	write(t, filepath.Join(home, ".claude", "agents", "notes.md"), "---\nname: notes\ndescription: Keeps notes tidy\n---\n")
	write(t, filepath.Join(cwd, ".claude", "skills", "deploy", "SKILL.md"), "---\ndescription: Deploy the app\n---\n")
	write(t, filepath.Join(cwd, ".claude", "rules", "api.md"), "---\npaths: [src/api/**]\n---\n")
	old := eco.RootsHook
	eco.RootsHook = func(string) discovery.Roots {
		return discovery.Roots{Home: home, ConfigDir: filepath.Join(home, ".claude"), CWD: cwd,
			Getenv: func(string) string { return "" }}
	}
	t.Cleanup(func() { eco.RootsHook = old })

	oldChecks := doctor.RunChecks
	doctor.RunChecks = func(context.Context, checks.Env) []checks.Check { return doctor.StoryChecks() }
	t.Cleanup(func() { doctor.RunChecks = oldChecks })

	ecotest.ResetState(t)
	ecotest.Observe(&proto.SystemInit{Agents: []string{"Explore", "general-purpose"}})
	eng = ecotest.NewEngine().
		Respond(proto.SubMCPStatus, fixture(t, "control/mcp_status.json")).
		Respond(proto.SubGetHooksListing, fixture(t, "control/hooks_listing.json")).
		Respond(proto.SubReloadSkills, fixture(t, "control/reload_skills.json"))
	return cwd, eng
}

// start runs the real host with the ecosystem features, attaches the scripted
// engine and opens dialog id once the host has it.
func start(t *testing.T, cwd string, eng *ecotest.Engine, id string, args any, w, h int) *testkit.Harness {
	t.Helper()
	features := append(ext.Pending(), ext.Feature{ID: "test.opener", Order: 900, Setup: func(r ext.Registrar) error {
		r.OnStart("attach", func(ctx ext.Ctx) tea.Cmd {
			return ext.Msg(ext.EngineAttachMsg{EngineID: ext.MainEngine, Engine: eng})
		})
		ext.Subscribe(r, "open", func(ctx ext.Ctx, m ext.EngineAttachMsg) tea.Cmd {
			return ctx.OpenDialog(id, args)
		})
		return nil
	}})
	host := app.NewHost(features, app.HostOptions{})
	root := app.New(app.Options{
		Host:              host,
		Settings:          exttest.NewSettings(nil),
		Clock:             &exttest.Clock{T: time.UnixMilli(1791029370211).Add(10 * time.Minute)},
		Session:           ext.SessionInfo{EngineID: ext.MainEngine, SessionID: "11111111-2222-4333-8444-555555555555", Cwd: cwd},
		NoBackgroundQuery: true,
	})
	return testkit.New(t, root, testkit.WithSize(w, h))
}

func TestPanelsVT(t *testing.T) {
	panels := []struct {
		name, id string
		args     any
		ready    string // text shown once the panel has loaded
	}{
		{"mcp", mcp.DialogID, nil, "filesystem"},
		{"plugins", plugins.DialogID, plugins.TabDiscover, "remote-tool"},
		{"skills", skills.DialogID, nil, "simplify"},
		{"hooks", hooks.DialogID, nil, "PreToolUse"},
		{"agents", agents.DialogID, nil, "general-purpose"},
		{"memory", memory.DialogID, nil, "Auto memory"},
		{"doctor", doctor.DialogID, nil, "Workspace trust"},
		{"login", auth.LoginDialogID, nil, "Signed in as user@example.test"},
		{"logout", auth.LogoutDialogID, nil, "Sign out of Claude Code?"},
		{"import", importcfg.DialogID, "codex", "Import these"},
		{"upgrade", handoff.UpgradeDialogID, nil, "Update Claude Code"},
		{"agentview", agentview.DialogID, nil, "repo-task"},
	}
	for _, p := range panels {
		for _, w := range []int{60, 100, 160} {
			t.Run(fmt.Sprintf("%s/w%d", p.name, w), func(t *testing.T) {
				cwd, eng := world(t)
				hs := start(t, cwd, eng, p.id, p.args, w, 40)
				hs.WaitForText(p.ready, 10*time.Second)
				time.Sleep(150 * time.Millisecond) // let follow-up refreshes land
				screen := hs.Screen()
				for i, l := range strings.Split(screen, "\n") {
					if n := len([]rune(l)); n > w {
						t.Errorf("line %d is %d cells wide (> %d): %q", i, n, w, l)
					}
				}
				testkit.RequireGolden(t, strings.ReplaceAll(screen, cwd, "/work/proj"))
			})
		}
	}
}

// TestMCPInteractionVT drives /mcp through the real keymap: navigate, open a
// server's page, go back, toggle, close. Nothing from the dialog may leak into
// the terminal's scrollback.
func TestMCPInteractionVT(t *testing.T) {
	cwd, eng := world(t)
	hs := start(t, cwd, eng, mcp.DialogID, nil, 100, 40)
	hs.WaitForText("filesystem", 10*time.Second)
	hs.Send("down", "enter") // second row: broken (Project)
	hs.WaitForText("Error: spawn /usr/local/bin/broken-mcp ENOENT", 5*time.Second)
	hs.Send("escape")
	hs.WaitFor(func(s string) bool {
		return strings.Contains(s, "enter details") && !strings.Contains(s, "Status: failed")
	}, 5*time.Second)
	hs.Send("space") // disable broken
	hs.WaitFor(func(string) bool {
		for _, c := range eng.Controls {
			if c.Req == (proto.MCPToggleRequest{ServerName: "broken", Enabled: false}) {
				return true
			}
		}
		return false
	}, 5*time.Second)
	hs.Send("escape")
	// Closing leaves one result line (request 12-09) and nothing of the panel.
	hs.WaitFor(func(s string) bool {
		return strings.Contains(s, "⎿  MCP servers closed") && !strings.Contains(s, "filesystem")
	}, 5*time.Second)
	for _, l := range append(hs.Scrollback(), strings.Split(hs.Screen(), "\n")...) {
		if strings.Contains(l, "filesystem") || strings.Contains(l, "enter details") {
			t.Fatalf("dialog lines leaked:\n%s\n---\n%s", strings.Join(hs.Scrollback(), "\n"), hs.Screen())
		}
	}
}
