package claudecli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const fixtureDir = "../../testdata/fixtures/09/claudecli"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// serve makes the fake claude print a fixture.
func (f *fakeClaude) serve(t *testing.T, name string) {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_STDOUT_FILE", p)
}

func TestParseMCPList(t *testing.T) {
	got := ParseMCPList(fixture(t, "mcp-list.txt"))
	want := []struct {
		name, target string
		state        MCPState
	}{
		{"claude.ai Example Docs", "https://mcp.example.test/docs", MCPConnected},
		{"filesystem", "npx -y @example/server-filesystem /tmp/work", MCPConnected},
		{"plugin:demo:search", "https://search.example.test/mcp", MCPNeedsAuth},
		{"broken", "/usr/local/bin/broken-mcp --port 9", MCPFailed},
		{"flaky", "https://flaky.example.test/mcp", MCPDegraded},
		{"repo-tools", "node ./tools/mcp.js", MCPPending},
		{"denied", "node ./denied.js", MCPRejected},
		{"quiet", "python -m quiet_server", MCPDisabled},
		{"half", "https://half.example.test", MCPNotConfigured},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d servers: %+v", len(got), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Name != w.name || g.Target != w.target || g.State != w.state {
			t.Errorf("row %d = {%q %q %v}, want {%q %q %v}", i, g.Name, g.Target, g.State, w.name, w.target, w.state)
		}
	}
	if got[0].Status != "Connected" {
		t.Errorf("status glyph not stripped: %q", got[0].Status)
	}
	if got[3].Issue != "spawn /usr/local/bin/broken-mcp ENOENT" {
		t.Errorf("issue = %q", got[3].Issue)
	}
	if n := len(ParseMCPList(fixture(t, "mcp-list-empty.txt"))); n != 0 {
		t.Errorf("empty list parsed %d servers", n)
	}
	// ANSI colour and CRLF don't matter.
	colored := "\x1b[1mx\x1b[0m: cmd - \x1b[32m✔ Connected\x1b[0m\r\n"
	if g := ParseMCPList([]byte(colored)); len(g) != 1 || g[0].Name != "x" || g[0].State != MCPConnected {
		t.Errorf("coloured row = %+v", g)
	}
}

func TestClassifyMCPStatus(t *testing.T) {
	cases := map[string]MCPState{
		"Connected":                      MCPConnected,
		"Connected · tools fetch failed": MCPDegraded,
		"Needs authentication":           MCPNeedsAuth,
		"Failed to connect":              MCPFailed,
		"Pending approval":               MCPPending,
		"Rejected (see disabledMcpjsonServers in settings)": MCPRejected,
		"Disabled for this project":                         MCPDisabled,
		"Not configured":                                    MCPNotConfigured,
		"Something new":                                     MCPUnknown,
	}
	for in, want := range cases {
		if got := ClassifyMCPStatus(in); got != want {
			t.Errorf("%q → %v, want %v", in, got, want)
		}
	}
	if MCPNeedsAuth.String() != "needs-auth" || MCPState(99).String() != "unknown" {
		t.Error("MCPState.String")
	}
}

func TestParseMCPGet(t *testing.T) {
	d, err := ParseMCPGet(fixture(t, "mcp-get-stdio.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "filesystem" || d.Type() != "stdio" || d.State() != MCPConnected {
		t.Errorf("detail = %+v", d)
	}
	if !strings.HasPrefix(d.Scope(), "Local config") {
		t.Errorf("scope = %q", d.Scope())
	}
	env, ok := d.Field("environment")
	if !ok || !reflect.DeepEqual(env.List, []string{"LOG_LEVEL=debug", "ROOT=/tmp/work"}) {
		t.Errorf("env = %+v", env)
	}
	if len(d.Notes) != 1 || !strings.Contains(d.Notes[0], "claude mcp remove") {
		t.Errorf("notes = %q", d.Notes)
	}

	h, err := ParseMCPGet(fixture(t, "mcp-get-http.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "plugin:demo:search" || h.State() != MCPNeedsAuth {
		t.Errorf("http detail = %+v", h)
	}
	if u, _ := h.Field("URL"); u.Value != "https://search.example.test/mcp" {
		t.Errorf("URL = %q (value with a colon must survive)", u.Value)
	}
	if hd, _ := h.Field("Headers"); len(hd.List) != 1 {
		t.Errorf("headers = %+v", hd)
	}

	c, err := ParseMCPGet(fixture(t, "mcp-get-claudeai.txt"))
	if err != nil || c.Name != "claude.ai Example Docs" || c.Scope() != "claude.ai config" {
		t.Errorf("claude.ai detail = %+v, %v", c, err)
	}
	if _, err := ParseMCPGet([]byte("garbage without a header")); err == nil {
		t.Error("garbage parsed")
	}
}

func TestMCPWrappers(t *testing.T) {
	f := newFakeClaude(t)
	ctx := context.Background()

	f.serve(t, "mcp-list.txt")
	servers, err := Default.MCPList(ctx)
	if err != nil || len(servers) != 9 {
		t.Fatalf("MCPList = %d, %v", len(servers), err)
	}

	f.serve(t, "mcp-get-stdio.txt")
	if _, err := Default.MCPGet(ctx, "-p"); err != nil {
		t.Fatal(err)
	}
	if got, want := f.argv(t), []string{"mcp", "get", "--", "-p"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MCPGet argv = %q", got)
	}

	t.Setenv("FAKE_STDOUT_FILE", "")
	err = Default.MCPAdd(ctx, MCPAddOptions{
		Name: "fs", CommandOrURL: "npx", Args: []string{"-y", "@example/fs", "--root", "/tmp"},
		Scope: "user", Env: []string{"A=1"}, ClientSecret: "s3cret", CallbackPort: 8123,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantAdd := []string{"mcp", "add", "--scope=user", "--env=A=1", "--client-secret", "--callback-port=8123",
		"--", "fs", "npx", "-y", "@example/fs", "--root", "/tmp"}
	if got := f.argv(t); !reflect.DeepEqual(got, wantAdd) {
		t.Errorf("MCPAdd argv:\n got %q\nwant %q", got, wantAdd)
	}
	for _, a := range f.argv(t) {
		if strings.Contains(a, "s3cret") {
			t.Error("client secret leaked into argv")
		}
	}
	if !contains(f.env(t), "MCP_CLIENT_SECRET=s3cret") {
		t.Error("client secret not passed through the environment")
	}

	if err := Default.MCPAddJSON(ctx, "j", `{"type":"http","url":"https://x.test"}`, "project", ""); err != nil {
		t.Fatal(err)
	}
	if got := f.argv(t); !reflect.DeepEqual(got, []string{"mcp", "add-json", "--scope=project", "--", "j", `{"type":"http","url":"https://x.test"}`}) {
		t.Errorf("MCPAddJSON argv = %q", got)
	}
	if err := Default.MCPRemove(ctx, "fs", ""); err != nil {
		t.Fatal(err)
	}
	if got := f.argv(t); !reflect.DeepEqual(got, []string{"mcp", "remove", "--", "fs"}) {
		t.Errorf("MCPRemove argv = %q", got)
	}
	if err := Default.MCPLogout(ctx, "fs"); err != nil {
		t.Fatal(err)
	}
	if err := Default.MCPAdd(ctx, MCPAddOptions{Name: "x", CommandOrURL: "y", Transport: "websocket"}); err == nil {
		t.Error("invalid transport accepted")
	}

	cmd, err := Default.MCPLoginCmd("srv", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := cmd.Args[1:]; !reflect.DeepEqual(got, []string{"mcp", "login", "--no-browser", "--", "srv"}) {
		t.Errorf("MCPLoginCmd argv = %q", got)
	}
}

func TestPluginParsers(t *testing.T) {
	list, err := ParsePluginList(fixture(t, "plugin-list.json"))
	if err != nil || len(list) != 2 {
		t.Fatalf("ParsePluginList = %+v, %v", list, err)
	}
	if list[0].Name() != "demo" || list[0].Marketplace() != "example-market" || !list[0].Enabled {
		t.Errorf("plugin 0 = %+v", list[0])
	}
	if list[1].Scope != "project" || !list[1].ProjectEnabled {
		t.Errorf("plugin 1 = %+v", list[1])
	}

	inst, avail, err := ParsePluginListAvailable(fixture(t, "plugin-list-available.json"))
	if err != nil || len(inst) != 1 || len(avail) != 3 {
		t.Fatalf("available = %d/%d, %v", len(inst), len(avail), err)
	}
	if s := avail[0].Source; s.Kind != "path" || s.Path != "./plugins/demo" {
		t.Errorf("path source = %+v", s)
	}
	if s := avail[1].Source; s.Kind != "git-subdir" || s.Ref != "v2.0.0" || s.Path != "plugins/remote-tool" {
		t.Errorf("git-subdir source = %+v", s)
	}
	if s := avail[2].Source; s.Kind != "url" || s.URL == "" {
		t.Errorf("url source = %+v", s)
	}
	if avail[0].InstallCount != 1200 {
		t.Errorf("install count = %d", avail[0].InstallCount)
	}

	mk, err := ParseMarketplaceList(fixture(t, "marketplace-list.json"))
	if err != nil || len(mk) != 2 || mk[0].Repo != "example/market" || mk[1].Path == "" {
		t.Errorf("marketplaces = %+v, %v", mk, err)
	}

	r, ok := ParsePluginResult(fixture(t, "plugin-result-installed.jsonl"))
	if !ok || r.Outcome != "installed" || r.Failed() {
		t.Errorf("installed result = %+v", r)
	}
	r, ok = ParsePluginResult(fixture(t, "plugin-result-failed.jsonl"))
	if !ok || !r.Failed() || r.FailureCode != "ambiguous_marketplace" {
		t.Errorf("failed result = %+v", r)
	}
	r, ok = ParsePluginResult(fixture(t, "plugin-result-confirm.jsonl"))
	if !ok || !r.NeedsConfirmation() || r.ShownCommand.Command != "npx example-installer" {
		t.Errorf("confirm result = %+v", r)
	}
	if _, ok := ParsePluginResult([]byte("no json here\n")); ok {
		t.Error("parsed a result from plain text")
	}

	cfg, err := ParsePluginConfig(fixture(t, "plugin-configure.json"))
	if err != nil {
		t.Fatal(err)
	}
	if o, ok := cfg.Option("apiKey"); !ok || !o.Sensitive || !o.Required || o.Title != "API key" {
		t.Errorf("apiKey option = %+v", o)
	}
	if _, ok := cfg.Option("missing"); ok {
		t.Error("missing option found")
	}
}

func TestPluginWrappers(t *testing.T) {
	f := newFakeClaude(t)
	ctx := context.Background()

	f.serve(t, "plugin-list.json")
	if l, err := Default.PluginList(ctx); err != nil || len(l) != 2 {
		t.Fatalf("PluginList = %v, %v", l, err)
	}
	f.serve(t, "plugin-list-available.json")
	if _, a, err := Default.PluginListAvailable(ctx); err != nil || len(a) != 3 {
		t.Fatalf("PluginListAvailable = %v, %v", a, err)
	}
	if got := f.argv(t); !reflect.DeepEqual(got, []string{"plugin", "list", "--json", "--available"}) {
		t.Errorf("argv = %q", got)
	}

	f.serve(t, "plugin-result-installed.jsonl")
	res, err := Default.PluginInstall(ctx, PluginInstallOptions{Plugin: "demo@example-market", Scope: "project",
		Config: []string{"region=eu"}})
	if err != nil || res.Outcome != "installed" {
		t.Fatalf("PluginInstall = %+v, %v", res, err)
	}
	wantInstall := []string{"plugin", "install", "--json", "--scope=project", "--config=region=eu", "--", "demo@example-market"}
	if got := f.argv(t); !reflect.DeepEqual(got, wantInstall) {
		t.Errorf("install argv:\n got %q\nwant %q", got, wantInstall)
	}
	for _, a := range f.argv(t) {
		if a == "--yes" {
			t.Error("install must not pass a blanket --yes")
		}
	}

	f.serve(t, "plugin-result-confirm.jsonl")
	res, err = Default.PluginInstall(ctx, PluginInstallOptions{Plugin: "cmd-tool@example-market"})
	if err != nil || !res.NeedsConfirmation() {
		t.Fatalf("confirm = %+v, %v", res, err)
	}
	if _, err := Default.PluginInstall(ctx, PluginInstallOptions{Plugin: "cmd-tool@example-market",
		AcceptCommand: res.ShownCommand.SHA256}); err != nil {
		t.Fatal(err)
	}
	if got := f.argv(t); !contains(got, "--accept-command="+strings.Repeat("a", 64)) {
		t.Errorf("accept-command missing: %q", got)
	}
	if _, err := Default.PluginInstall(ctx, PluginInstallOptions{Plugin: "x@y", AcceptCommand: "not-a-sha"}); err == nil {
		t.Error("bad sha accepted")
	}

	f.serve(t, "plugin-result-failed.jsonl")
	t.Setenv("FAKE_EXIT", "1")
	res, err = Default.PluginEnable(ctx, "nope", "")
	var pe *PluginError
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("PluginEnable err = %v", err)
	}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 1 {
		t.Errorf("PluginError should wrap the ExitError, got %v", err)
	}
	if res.FailureCode != "ambiguous_marketplace" {
		t.Errorf("result = %+v", res)
	}
	t.Setenv("FAKE_EXIT", "0")

	// A failure result with exit 0 is still an error.
	if _, err := Default.PluginDisable(ctx, "nope", "user", false); !errors.As(err, &pe) {
		t.Errorf("failed outcome with exit 0: err = %v", err)
	}
	if got := f.argv(t); !reflect.DeepEqual(got, []string{"plugin", "disable", "--json", "--scope=user", "--", "nope"}) {
		t.Errorf("disable argv = %q", got)
	}

	f.serve(t, "plugin-result-installed.jsonl")
	if _, err := Default.PluginUninstall(ctx, "demo@example-market", "", true, true); err != nil {
		t.Fatal(err)
	}
	if got := f.argv(t); !reflect.DeepEqual(got, []string{"plugin", "uninstall", "--json", "--keep-data", "--prune", "--yes", "--", "demo@example-market"}) {
		t.Errorf("uninstall argv = %q", got)
	}
	if _, err := Default.PluginUpdate(ctx, "demo@example-market", "managed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Default.MarketplaceAdd(ctx, "example/market", "user", false, []string{"plugins"}); err != nil {
		t.Fatal(err)
	}
	if got := f.argv(t); !reflect.DeepEqual(got, []string{"plugin", "marketplace", "add", "--json", "--scope=user", "--sparse=plugins", "--", "example/market"}) {
		t.Errorf("marketplace add argv = %q", got)
	}
	if _, err := Default.MarketplaceRemove(ctx, "example-market", ""); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_STDOUT_FILE", "")
	if _, err := Default.MarketplaceUpdate(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got := f.argv(t); !reflect.DeepEqual(got, []string{"plugin", "marketplace", "update"}) {
		t.Errorf("marketplace update argv = %q", got)
	}

	f.serve(t, "marketplace-list.json")
	if m, err := Default.MarketplaceList(ctx); err != nil || len(m) != 2 {
		t.Errorf("MarketplaceList = %v, %v", m, err)
	}

	f.serve(t, "plugin-configure.json")
	stdinLog := filepath.Join(f.dir, "stdin")
	t.Setenv("FAKE_STDIN_LOG", stdinLog)
	if _, err := Default.PluginConfigureSet(ctx, "demo@example-market", map[string]string{"region": "us"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(stdinLog); string(b) != `{"region":"us"}` {
		t.Errorf("configure stdin = %q", b)
	}
	if got := f.argv(t); !reflect.DeepEqual(got, []string{"plugin", "configure", "--json", "--values-stdin", "--", "demo@example-market"}) {
		t.Errorf("configure argv = %q", got)
	}
	if _, err := Default.PluginConfigureSet(ctx, "d@m", map[string]string{"k": "two\nlines"}); err == nil {
		t.Error("multi-line value accepted")
	}
	if c, err := Default.PluginConfigure(ctx, "demo@example-market"); err != nil || c.DisplayName != "Demo" {
		t.Errorf("PluginConfigure = %+v, %v", c, err)
	}
}

func TestMiscWrappers(t *testing.T) {
	f := newFakeClaude(t)
	ctx := context.Background()

	f.serve(t, "version.txt")
	if v, err := Default.Version(ctx); err != nil || v != "2.1.288" {
		t.Errorf("Version = %q, %v", v, err)
	}

	f.serve(t, "auth-status.json")
	a, err := Default.AuthStatus(ctx)
	if err != nil || !a.LoggedIn || a.SubscriptionType != "max" || a.Email != "user@example.test" {
		t.Errorf("AuthStatus = %+v, %v", a, err)
	}
	f.serve(t, "auth-status-loggedout.json")
	t.Setenv("FAKE_EXIT", "1")
	if a, err := Default.AuthStatus(ctx); err != nil || a.LoggedIn {
		t.Errorf("logged out = %+v, %v", a, err)
	}
	t.Setenv("FAKE_STDOUT_FILE", "")
	if _, err := Default.AuthStatus(ctx); err == nil {
		t.Error("exit 1 with no JSON should be an error")
	}
	t.Setenv("FAKE_EXIT", "0")

	f.serve(t, "agents.json")
	s, err := Default.Agents(ctx, true, "/home/user/work")
	if err != nil || len(s) != 2 {
		t.Fatalf("Agents = %+v, %v", s, err)
	}
	if got := f.argv(t); !reflect.DeepEqual(got, []string{"agents", "--json", "--all", "--cwd=/home/user/work"}) {
		t.Errorf("agents argv = %q", got)
	}
	if s[1].ID != "2a3c85ff" || s[1].State != "working" || s[1].StartedAt().IsZero() || s[0].ID != "" {
		t.Errorf("sessions = %+v", s)
	}
	if _, err := Default.Logs(ctx, "2a3c85ff"); err != nil {
		t.Fatal(err)
	}
	if err := Default.StopSession(ctx, "2a3c85ff"); err != nil {
		t.Fatal(err)
	}
	if got := f.argv(t); !reflect.DeepEqual(got, []string{"stop", "2a3c85ff"}) {
		t.Errorf("stop argv = %q", got)
	}
	if err := Default.StopSession(ctx, "--all"); err == nil {
		t.Error("flag-like id accepted")
	}

	f.serve(t, "import-preview.txt")
	p, err := Default.ImportDryRun(ctx, "codex")
	if err != nil || p.Digest != "d1g3st-42" {
		t.Errorf("ImportDryRun = %+v, %v", p, err)
	}
	if got := f.argv(t); !reflect.DeepEqual(got, []string{"import", "--dry-run", "codex"}) {
		t.Errorf("import argv = %q", got)
	}
	if _, err := Default.ImportApply(ctx, "codex", p.Digest); err != nil {
		t.Fatal(err)
	}
	if got := f.argv(t); !reflect.DeepEqual(got, []string{"import", "--yes=d1g3st-42", "codex"}) {
		t.Errorf("import apply argv = %q", got)
	}
	if _, err := Default.ImportApply(ctx, "codex", ""); err == nil {
		t.Error("apply without digest accepted")
	}
	if p := ParseImportPreview(fixture(t, "import-none.txt")); p.Digest != "" {
		t.Errorf("none preview digest = %q", p.Digest)
	}

	f.serve(t, "auto-mode.json")
	rules, err := Default.AutoModeDefaults(ctx, "Git")
	if err != nil || len(rules.SoftDeny) != 2 || len(rules.HardDeny) != 1 || len(rules.Environment) != 1 {
		t.Errorf("AutoModeDefaults = %+v, %v", rules, err)
	}
	if _, err := Default.AutoModeConfig(ctx); err != nil {
		t.Fatal(err)
	}

	for name, mk := range map[string]func() ([]string, error){
		"login": func() ([]string, error) {
			c, err := Default.AuthLoginCmd(AuthSSO, "")
			if err != nil {
				return nil, err
			}
			return c.Args[1:], nil
		},
		"attach": func() ([]string, error) {
			c, err := Default.AttachCmd("2a3c85ff")
			if err != nil {
				return nil, err
			}
			return c.Args[1:], nil
		},
		"doctor": func() ([]string, error) {
			c, err := Default.DoctorCmd()
			if err != nil {
				return nil, err
			}
			return c.Args[1:], nil
		},
	} {
		argv, err := mk()
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		want := map[string][]string{"login": {"auth", "login", "--sso"}, "attach": {"attach", "2a3c85ff"}, "doctor": {"doctor"}}[name]
		if !reflect.DeepEqual(argv, want) {
			t.Errorf("%s argv = %q, want %q", name, argv, want)
		}
	}
	if _, err := Default.AuthLoginCmd("magic", ""); err == nil {
		t.Error("unknown auth method accepted")
	}
}

func TestVersionHelpers(t *testing.T) {
	if _, err := ParseVersion([]byte("no version")); err == nil {
		t.Error("garbage version parsed")
	}
	cases := []struct {
		a, b string
		want int
	}{
		{"2.1.288", "2.1.288", 0},
		{"2.1.288", "2.1.290", -1},
		{"2.10.0", "2.9.9", 1},
		{"2.1", "2.1.0", 0},
		{"v2.1.300-beta", "2.1.299", 1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
