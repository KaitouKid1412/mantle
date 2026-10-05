package chrome

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// scoped adds per-scope sources to the fake settings.
type scoped struct {
	*exttest.Settings
	sources []ext.SettingsSource
}

func (s scoped) ClaudeScope(string) map[string]any   { return nil }
func (s scoped) ClaudeSources() []ext.SettingsSource { return s.sources }

func TestWelcomeOncePerSession(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.SettingsV.ClaudeM["companyAnnouncements"] = []any{"Welcome to Acme engineering"}
	w := newWelcome()
	w.version = "0.1.0"
	// What the host knew at launch: the working directory and a --model.
	ctx.SessionValue = ext.SessionInfo{EngineID: ext.MainEngine, Cwd: "/w/app", Model: "claude-sonnet-5-5"}
	resp, _ := json.Marshal(proto.InitializeResponse{Account: proto.Account{SubscriptionType: "pro", Email: "x@y.z"}})
	w.Update(ctx, ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubInitialize, Resp: resp})
	if len(ctx.Printed) != 1 {
		t.Fatalf("the banner is printed at startup, on initialize: printed = %q", ctx.Printed)
	}
	b := ansi.Strip(ctx.Printed[0])
	if !strings.Contains(b, "mantle 0.1.0") || !strings.Contains(b, "Sonnet 5.5 · Claude Pro") ||
		!strings.Contains(b, "/w/app") || strings.Contains(b, "x@y.z") {
		t.Errorf("banner = %q", b)
	}
	if len(ctx.Notices) != 1 || ctx.Notices[0].Text != "Welcome to Acme engineering" {
		t.Errorf("notices = %+v", ctx.Notices)
	}
	w.Update(ctx, ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubInitialize, Resp: resp})
	w.Update(ctx, ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{
		SessionID: "s1", Cwd: "/w/app", Model: "claude-sonnet-5-5", ClaudeVersion: "2.1.288"}})
	w.Update(ctx, ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{SessionID: "s1", Title: "x"}})
	if len(ctx.Printed) != 1 {
		t.Errorf("the first session id is the startup banner's: printed %d", len(ctx.Printed))
	}
	w.Update(ctx, ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{SessionID: "s2"}})
	if len(ctx.Printed) != 2 || len(ctx.Notices) != 1 {
		t.Errorf("new session (/clear): banner again, startup notices once (%d, %d)", len(ctx.Printed), len(ctx.Notices))
	}
	if b := ansi.Strip(ctx.Printed[1]); !strings.Contains(b, "mantle 0.1.0 · Claude Code 2.1.288") {
		t.Errorf("later banners name the engine version: %q", b)
	}
	w.Update(ctx, ext.SessionChangedMsg{EngineID: "builder", Info: ext.SessionInfo{SessionID: "b"}})
	if len(ctx.Printed) != 2 {
		t.Error("other engines get no banner")
	}
}

func TestWelcomeWithoutInitialize(t *testing.T) {
	// An engine that never answers initialize still gets a banner with its first session.
	ctx := exttest.NewCtx()
	w := newWelcome()
	w.Update(ctx, ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{SessionID: "s1"}})
	if len(ctx.Printed) != 1 {
		t.Errorf("printed = %d", len(ctx.Printed))
	}
}

func TestStartupNotices(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.SettingsV = exttest.NewSettings(nil)
	var s ext.Settings = scoped{ctx.SettingsV, []ext.SettingsSource{
		{Scope: ext.ScopeUser, Path: "/h/.claude/settings.json", Exists: true},
		{Scope: ext.ScopeProject, Path: "/w/.claude/settings.json", Exists: true, Err: errors.New("unexpected token")},
	}}
	c := &settingsCtx{Ctx: ctx, s: s}
	startupNotices(c, "s")
	if len(ctx.Notices) != 1 || !strings.Contains(ctx.Notices[0].Text, "/w/.claude/settings.json") {
		t.Errorf("notices = %+v", ctx.Notices)
	}

	mcpNotice(ctx, []proto.MCPServerInfo{{Name: "github", Status: "needs-auth"}, {Name: "fs", Status: "connected"}})
	mcpNotice(ctx, []proto.MCPServerInfo{{Name: "a", Status: "needs-auth"}, {Name: "b", Status: "needs-auth"}})
	if n := ctx.Notices; len(n) != 3 || n[1].Text != "MCP server github needs sign-in · /mcp" ||
		n[2].Text != "2 MCP servers need sign-in · /mcp" {
		t.Errorf("mcp notices = %+v", n)
	}
	if mcpNotice(ctx, nil) != nil {
		t.Error("no servers, no notice")
	}
}

// settingsCtx overrides Settings on a fake Ctx.
type settingsCtx struct {
	*exttest.Ctx
	s ext.Settings
}

func (c *settingsCtx) Settings() ext.Settings { return c.s }

func TestPickAnnouncement(t *testing.T) {
	s := exttest.NewSettings(map[string]any{"companyAnnouncements": []any{"a", map[string]any{"text": "b"}, 3}})
	seen := map[string]bool{}
	for _, id := range []string{"1", "2", "3", "4", "5", "6", "7", "8"} {
		seen[pickAnnouncement(s, id)] = true
	}
	if !seen["a"] || !seen["b"] || len(seen) != 2 {
		t.Errorf("picks = %v", seen)
	}
	if pickAnnouncement(exttest.NewSettings(nil), "x") != "" {
		t.Error("none configured")
	}
}

const testChangelog = "# Changelog\n\n## 2.1.290\n- Future\n\n## 2.1.289\n- Newest fix\n- Another\n\n## 2.1.288\n- Old\n"

func newTestNotes() *releaseNotes {
	rn := newReleaseNotes(terminal.Map{"CLAUDE_CONFIG_DIR": "/cfg"})
	rn.read = func(p string) ([]byte, error) {
		if p != "/cfg/cache/changelog.md" {
			return nil, errors.New("not found: " + p)
		}
		return []byte(testChangelog), nil
	}
	return rn
}

func TestReleaseNotesOnUpgrade(t *testing.T) {
	ctx := exttest.NewCtx()
	rn := newTestNotes()
	rn.onSession(ctx, ext.SessionChangedMsg{Info: ext.SessionInfo{ClaudeVersion: "2.1.288"}})
	if len(ctx.Printed) != 0 {
		t.Fatal("the first run only records the version")
	}
	var seen string
	ctx.Store(ReleaseNotesID).Get(seenClaudeKey, &seen)
	if seen != "2.1.288" {
		t.Fatalf("seen = %q", seen)
	}

	rn2 := newTestNotes()
	rn2.onSession(ctx, ext.SessionChangedMsg{Info: ext.SessionInfo{ClaudeVersion: "2.1.289"}})
	if len(ctx.Printed) != 1 {
		t.Fatalf("upgrade should print what's new: %q", ctx.Printed)
	}
	out := ansi.Strip(ctx.Printed[0])
	if !strings.Contains(out, "Claude Code 2.1.289") || !strings.Contains(out, "• Newest fix") ||
		strings.Contains(out, "2.1.290") || strings.Contains(out, "Old") {
		t.Errorf("what's new = %q", out)
	}
	rn2.onSession(ctx, ext.SessionChangedMsg{Info: ext.SessionInfo{ClaudeVersion: "2.1.289"}})
	if len(ctx.Printed) != 1 {
		t.Error("only once per process")
	}
}

func TestReleaseNotesCommand(t *testing.T) {
	ctx := exttest.NewCtx()
	rn := newTestNotes()
	rn.command(ctx, "")
	if len(ctx.Printed) != 1 {
		t.Fatalf("printed = %q", ctx.Printed)
	}
	out := ansi.Strip(ctx.Printed[0])
	if !strings.Contains(out, "Claude Code 2.1.290") || !strings.Contains(out, "mantle 0.1.0") {
		t.Errorf("notes = %q", out)
	}
	// Second time: nothing new, so the latest few are shown again.
	rn.command(ctx, "")
	if out := ansi.Strip(ctx.Printed[1]); !strings.Contains(out, "2.1.290") {
		t.Errorf("repeat = %q", out)
	}

	empty := newReleaseNotes(terminal.Map{"CLAUDE_CONFIG_DIR": "/none"})
	empty.read = func(string) ([]byte, error) { return nil, errors.New("missing") }
	empty.mantle = ""
	empty.command(ctx, "")
	if len(ctx.Notices) != 1 {
		t.Error("no notes: a notice")
	}
}
