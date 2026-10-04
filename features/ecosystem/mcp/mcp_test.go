package mcp

import (
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func readFixture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile("../../../testdata/fixtures/09/" + path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func openPanel(t *testing.T, eng *ecotest.Engine) (*ecotest.Ctx, *ecotest.Execs, ext.Dialog, *view) {
	t.Helper()
	ecotest.ResetState(t)
	ctx := ecotest.NewCtx(eng, t.TempDir())
	x := ecotest.Capture(t)
	d, _ := New(ctx, nil)
	msgs := ecotest.Open(t, ctx, d)
	_ = msgs
	return ctx, x, d, d.Root().(*view)
}

func TestListFromMCPStatus(t *testing.T) {
	eng := ecotest.NewEngine().Respond(proto.SubMCPStatus, readFixture(t, "control/mcp_status.json"))
	ecotest.ResetState(t)
	ctx := ecotest.NewCtx(eng, t.TempDir())
	d, _ := New(ctx, nil)
	msgs := ecotest.Open(t, ctx, d)
	var status *ext.MCPStatusMsg
	for _, m := range msgs {
		if s, ok := m.(ext.MCPStatusMsg); ok {
			status = &s
		}
	}
	if status == nil || status.Total != 6 || status.Connected != 2 || status.NeedsAuth != 1 || status.Failed != 1 {
		t.Fatalf("status msg = %+v", status)
	}
	s := ecotest.Screen(ctx, d, 100)
	for _, want := range []string{
		"MCP servers · 2 connected of 6",
		"Local · this project, only you", "✓ filesystem", "connected · 3 tools",
		"Project · .mcp.json", "✗ broken", "failed · spawn /usr/local/bin/broken-mcp ENOENT", "… repo-tools",
		"User · all projects", "! search", "needs sign-in", "○ quiet",
		"claude.ai connectors", "claude.ai Example Docs",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	// Sections are ordered local, project, user, claude.ai.
	if strings.Index(s, "Local ·") > strings.Index(s, "Project ·") || strings.Index(s, "User ·") > strings.Index(s, "claude.ai connectors") {
		t.Errorf("section order:\n%s", s)
	}
}

func TestActions(t *testing.T) {
	eng := ecotest.NewEngine().Respond(proto.SubMCPStatus, readFixture(t, "control/mcp_status.json"))
	ctx, x, d, v := openPanel(t, eng)

	// filesystem is selected first: disable it, reconnect it.
	ecotest.Press(t, ctx, d, "space", "r")
	var reqs []any
	for _, c := range eng.Controls {
		if c.Subtype == proto.SubMCPToggle || c.Subtype == proto.SubMCPReconnect {
			reqs = append(reqs, c.Req)
		}
	}
	want := []any{proto.MCPToggleRequest{ServerName: "filesystem", Enabled: false}, proto.MCPReconnectRequest{ServerName: "filesystem"}}
	if !reflect.DeepEqual(reqs, want) {
		t.Fatalf("requests = %#v", reqs)
	}
	// Each action refreshes the list.
	if n := strings.Count(strings.Join(eng.Subtypes(), " "), proto.SubMCPStatus); n != 3 {
		t.Errorf("mcp_status calls = %d (%v)", n, eng.Subtypes())
	}

	// stdio servers don't sign in.
	ecotest.Press(t, ctx, d, "a")
	if len(x.Cmds) != 0 || !strings.Contains(ecotest.Screen(ctx, d, 100), "doesn't sign in") {
		t.Errorf("stdio sign-in should be refused")
	}

	// Sign in to search (needs-auth): claude mcp login, then reconnect.
	v.list.Select("search")
	ecotest.Press(t, ctx, d, "a")
	if got := x.Args(); len(got) != 1 || !reflect.DeepEqual(got[0], []string{"mcp", "login", "--", "search"}) {
		t.Fatalf("login exec = %q", got)
	}
	last := eng.Controls[len(eng.Controls)-2] // reconnect, then the refresh
	if last.Req != (proto.MCPReconnectRequest{ServerName: "search"}) {
		t.Errorf("after login: %#v", last)
	}

	// claude.ai connectors can't be removed here.
	v.list.Select("claude.ai Example Docs")
	ecotest.Press(t, ctx, d, "x")
	if d.(interface{ Depth() int }).Depth() != 1 || !strings.Contains(ecotest.Screen(ctx, d, 100), "can't be removed here") {
		t.Errorf("claude.ai remove should be refused")
	}
}

func TestDetailMenu(t *testing.T) {
	eng := ecotest.NewEngine().Respond(proto.SubMCPStatus, readFixture(t, "control/mcp_status.json"))
	ctx, _, d, v := openPanel(t, eng)
	v.list.Select("quiet")
	ecotest.Press(t, ctx, d, "enter")
	s := ecotest.Screen(ctx, d, 100)
	for _, want := range []string{"MCP servers · quiet", "Status: disabled", "Type: sse", "Enable", "Sign in"} {
		if !strings.Contains(s, want) {
			t.Errorf("detail lacks %q:\n%s", want, s)
		}
	}
	ecotest.Press(t, ctx, d, "enter") // Enable
	if c := eng.Controls[len(eng.Controls)-2]; c.Req != (proto.MCPToggleRequest{ServerName: "quiet", Enabled: true}) {
		t.Errorf("enable from detail = %#v", c)
	}
}

func TestAddAndRemove(t *testing.T) {
	log := ecotest.FakeClaude(t, nil)
	eng := ecotest.NewEngine().Respond(proto.SubMCPStatus, readFixture(t, "control/mcp_status.json"))
	ctx, _, d, v := openPanel(t, eng)

	ecotest.Press(t, ctx, d, "n")
	for _, r := range "new-srv" {
		ecotest.Press(t, ctx, d, string(r))
	}
	ecotest.Press(t, ctx, d, "tab", "tab")
	for _, r := range "npx" {
		ecotest.Press(t, ctx, d, string(r))
	}
	ecotest.Press(t, ctx, d, "tab")
	d.HandlePaste(ctx, teaPaste("-y @example/new --flag"))
	ecotest.Press(t, ctx, d, "tab", "right", "ctrl+s")
	calls := ecotest.Calls(t, log)
	want := []string{"mcp", "add", "--scope=project", "--transport=stdio", "--", "new-srv", "npx", "-y", "@example/new", "--flag"}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("add argv = %q", calls)
	}
	// The engine doesn't list it after reload_plugins: offer a restart.
	subs := eng.Subtypes()
	if subs[len(subs)-2] != proto.SubReloadPlugins || subs[len(subs)-1] != proto.SubMCPStatus {
		t.Errorf("after add: %v", subs)
	}
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "needs a restart to connect new-srv") {
		t.Fatalf("restart offer missing:\n%s", s)
	}
	ecotest.Press(t, ctx, d, "y")
	if len(eng.Restarts) != 1 {
		t.Errorf("restart = %d", len(eng.Restarts))
	}

	// Remove broken (project scope) after confirming.
	d2, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d2)
	v = d2.Root().(*view)
	v.list.Select("broken")
	ecotest.Press(t, ctx, d2, "x", "y")
	calls = ecotest.Calls(t, log)
	if got := calls[len(calls)-1]; !reflect.DeepEqual(got, []string{"mcp", "remove", "--scope=project", "--", "broken"}) {
		t.Errorf("remove argv = %q", got)
	}
}

func TestCLIFallbackWithoutEngine(t *testing.T) {
	ecotest.FakeClaude(t, map[string]string{"mcp list": readFixture(t, "claudecli/mcp-list.txt")})
	ecotest.ResetState(t)
	ctx := ecotest.NewCtx(nil, t.TempDir())
	d, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d)
	s := ecotest.Screen(ctx, d, 100)
	if !strings.Contains(s, "showing claude mcp list") || !strings.Contains(s, "! plugin:demo:search") || !strings.Contains(s, "✗ broken") {
		t.Fatalf("screen = %s", s)
	}
	ecotest.Press(t, ctx, d, "space")
	if !strings.Contains(ecotest.Screen(ctx, d, 100), "Claude isn't running") {
		t.Error("toggle without engine should explain")
	}
}

func TestCommandArgs(t *testing.T) {
	ecotest.ResetState(t)
	pending = map[string]string{}
	eng := ecotest.NewEngine()
	ctx := ecotest.NewCtx(eng, "/work")
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	cmd, _ := r.Command("mcp")

	exttest.Exec(cmd.Run(ctx, ""))
	if !reflect.DeepEqual(ctx.Opened, []string{DialogID}) {
		t.Fatalf("bare /mcp opens the panel: %v", ctx.Opened)
	}
	msgs := exttest.Exec(cmd.Run(ctx, "reconnect claude.ai Example Docs"))
	if c := eng.Controls[0]; c.Req != (proto.MCPReconnectRequest{ServerName: "claude.ai Example Docs"}) {
		t.Fatalf("reconnect = %#v", c)
	}
	for _, m := range msgs {
		exttest.Exec(r.Dispatch(ctx, m))
	}
	if len(ctx.Notices) != 1 || !strings.Contains(ctx.Notices[0].Text, "reconnect claude.ai Example Docs: done") {
		t.Errorf("notices = %+v", ctx.Notices)
	}
	exttest.Exec(cmd.Run(ctx, "disable search"))
	if c := eng.Controls[1]; c.Req != (proto.MCPToggleRequest{ServerName: "search", Enabled: false}) {
		t.Errorf("disable = %#v", c)
	}
	cmd.Run(ctx, "frobnicate x")
	cmd.Run(ctx, "enable")
	if n := len(ctx.Notices); n != 3 || !strings.Contains(ctx.Notices[2].Text, "Usage") {
		t.Errorf("usage notices = %+v", ctx.Notices)
	}
	// When the engine handles /mcp headlessly, pass it through.
	ecotest.Observe(&proto.SystemInit{SlashCommands: []string{"mcp"}})
	cmd.Run(ctx, "reconnect search")
	if !reflect.DeepEqual(eng.Sent, []string{"/mcp reconnect search"}) {
		t.Errorf("sent = %q", eng.Sent)
	}
}

func TestStories(t *testing.T) {
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	for _, s := range r.Stories {
		t.Run(strings.ReplaceAll(s.ID, "/", "_"), func(t *testing.T) { testkit.RunStory(t, s) })
	}
}

func teaPaste(s string) tea.PasteMsg { return tea.PasteMsg{Content: s} }
