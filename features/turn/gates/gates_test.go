package gates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// fixture is a temp HOME with a project directory, isolated from the real machine.
type fixture struct {
	t    *testing.T
	env  Env
	root string // temp root (symlink-resolved)
	proj string // <root>/work/proj
	vars map[string]string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, root: root, vars: map[string]string{}}
	home := filepath.Join(root, "home")
	f.proj = filepath.Join(root, "work", "proj")
	for _, d := range []string{home, f.proj, filepath.Join(root, "managed")} {
		mustMkdir(t, d)
	}
	f.env = Env{
		Home:       home,
		ManagedDir: filepath.Join(root, "managed"),
		Getenv:     func(k string) string { return f.vars[k] },
	}
	return f
}

func mustMkdir(t *testing.T, d string) {
	t.Helper()
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) write(path, content string) {
	f.t.Helper()
	mustMkdir(f.t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) claudeJSON(v any) {
	f.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		f.t.Fatal(err)
	}
	f.write(filepath.Join(f.env.Home, ".claude.json"), string(b))
}

func trusted(paths ...string) map[string]any {
	projects := map[string]any{}
	for _, p := range paths {
		projects[p] = map[string]any{"hasTrustDialogAccepted": true}
	}
	return map[string]any{"projects": projects}
}

func TestTrustUntrusted(t *testing.T) {
	f := newFixture(t)
	st := CheckTrust(f.env, f.proj, nil)
	if st.Trusted || !NeedsTrust(f.env, f.proj) {
		t.Fatalf("fresh folder should be untrusted: %+v", st)
	}
	if st.ProjectKey != f.proj {
		t.Fatalf("project key = %q want cwd %q", st.ProjectKey, f.proj)
	}
}

func TestTrustExactPath(t *testing.T) {
	f := newFixture(t)
	f.claudeJSON(trusted(f.proj))
	st := CheckTrust(f.env, f.proj, nil)
	if !st.Trusted || st.Source != TrustClaude || st.TrustedPath != f.proj {
		t.Fatalf("got %+v", st)
	}
}

func TestTrustFalseValueIsUntrusted(t *testing.T) {
	f := newFixture(t)
	f.claudeJSON(map[string]any{"projects": map[string]any{f.proj: map[string]any{"hasTrustDialogAccepted": false}}})
	if !NeedsTrust(f.env, f.proj) {
		t.Fatal("hasTrustDialogAccepted:false must not trust")
	}
}

func TestTrustParentOutsideRepo(t *testing.T) {
	f := newFixture(t)
	parent := filepath.Dir(f.proj)
	f.claudeJSON(trusted(parent))
	sub := filepath.Join(f.proj, "a", "b")
	mustMkdir(t, sub)
	st := CheckTrust(f.env, sub, nil)
	if !st.Trusted || st.TrustedPath != parent {
		t.Fatalf("trusted ancestor should count outside a repo: %+v", st)
	}
}

func TestTrustParentBoundedByRepoRoot(t *testing.T) {
	f := newFixture(t)
	mustMkdir(t, filepath.Join(f.proj, ".git"))
	sub := filepath.Join(f.proj, "pkg", "x")
	mustMkdir(t, sub)

	// An ancestor above the repository root doesn't count...
	f.claudeJSON(trusted(filepath.Dir(f.proj)))
	if st := CheckTrust(f.env, sub, nil); st.Trusted {
		t.Fatalf("trust above the repo root leaked in: %+v", st)
	}
	// ...but the repository root (the project key) does, from any subdirectory.
	f.claudeJSON(trusted(f.proj))
	st := CheckTrust(f.env, sub, nil)
	if !st.Trusted || st.GitRoot != f.proj || st.ProjectKey != f.proj {
		t.Fatalf("repo root trust should cover subdirs: %+v", st)
	}
	// A trusted directory between cwd and the root counts too.
	f.claudeJSON(trusted(filepath.Join(f.proj, "pkg")))
	if st := CheckTrust(f.env, sub, nil); !st.Trusted {
		t.Fatalf("intermediate trust should count: %+v", st)
	}
}

func TestTrustLinkedWorktreeUsesMainRoot(t *testing.T) {
	f := newFixture(t)
	mainGit := filepath.Join(f.proj, ".git")
	wtGit := filepath.Join(mainGit, "worktrees", "wt1")
	mustMkdir(t, wtGit)
	f.write(filepath.Join(wtGit, "commondir"), "../..\n")
	wt := filepath.Join(f.root, "elsewhere", "wt1")
	f.write(filepath.Join(wt, ".git"), "gitdir: "+wtGit+"\n")

	f.claudeJSON(trusted(f.proj))
	st := CheckTrust(f.env, wt, nil)
	if !st.Trusted || st.ProjectKey != f.proj || st.GitRoot != wt {
		t.Fatalf("worktree should inherit main repo trust: %+v", st)
	}
}

func TestTrustMalformedClaudeJSON(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(f.env.Home, ".claude.json"), `{"projects": {`)
	st := CheckTrust(f.env, f.proj, nil)
	if st.Trusted {
		t.Fatal("malformed config must not grant trust")
	}
	if st.ConfigErr == nil || !strings.Contains(st.ConfigErr.Error(), "line 1") {
		t.Fatalf("want a located config error, got %v", st.ConfigErr)
	}
	// Wrong-typed fields are skipped one by one; the rest still works.
	f.claudeJSON(map[string]any{"projects": map[string]any{
		f.proj:       map[string]any{"hasTrustDialogAccepted": "yes"},
		"/elsewhere": "junk",
	}, "customApiKeyResponses": map[string]any{"approved": []any{1, "abc"}}})
	gc := LoadGlobalConfig(f.env)
	if gc.Err != nil || gc.trusted(f.proj) || !slices.Equal(gc.APIKeyApproved, []string{"abc"}) {
		t.Fatalf("tolerant decode failed: %+v", gc)
	}
}

func TestTrustClaudeConfigDir(t *testing.T) {
	f := newFixture(t)
	cfg := filepath.Join(f.root, "cfg")
	f.env.ClaudeConfigDir = cfg
	f.write(filepath.Join(cfg, ".claude.json"), mustJSON(t, trusted(f.proj)))
	if NeedsTrust(f.env, f.proj) {
		t.Fatal("CLAUDE_CONFIG_DIR/.claude.json should be read")
	}
	// The legacy .config.json takes precedence when it exists.
	f.write(filepath.Join(cfg, ".config.json"), `{}`)
	if !NeedsTrust(f.env, f.proj) {
		t.Fatal("legacy .config.json should win")
	}
}

func TestRecordTrustMantleStore(t *testing.T) {
	f := newFixture(t)
	claudePath := filepath.Join(f.env.Home, ".claude.json")
	f.write(claudePath, `{"numStartups":3}`)
	before, _ := os.ReadFile(claudePath)

	persisted, err := RecordTrust(f.env, f.proj)
	if err != nil || !persisted {
		t.Fatalf("RecordTrust = %v, %v", persisted, err)
	}
	st := CheckTrust(f.env, f.proj, nil)
	if !st.Trusted || st.Source != TrustMantle {
		t.Fatalf("got %+v", st)
	}
	after, _ := os.ReadFile(claudePath)
	if string(before) != string(after) {
		t.Fatal("~/.claude.json must never be written")
	}
	if _, err := os.Stat(filepath.Join(f.env.Home, ".mantle", "trust.json")); err != nil {
		t.Fatalf("trust store missing: %v", err)
	}
	if err := RevokeTrust(f.env, f.proj); err != nil {
		t.Fatal(err)
	}
	if !NeedsTrust(f.env, f.proj) {
		t.Fatal("revoked trust should need the dialog again")
	}
}

func TestRecordTrustRepoSubdirStoresRoot(t *testing.T) {
	f := newFixture(t)
	mustMkdir(t, filepath.Join(f.proj, ".git"))
	sub := filepath.Join(f.proj, "src")
	mustMkdir(t, sub)
	if _, err := RecordTrust(f.env, sub); err != nil {
		t.Fatal(err)
	}
	if st := CheckTrust(f.env, f.proj, nil); !st.Trusted || st.TrustedPath != f.proj {
		t.Fatalf("trust should be recorded on the repo root: %+v", st)
	}
}

func TestRecordTrustHomeIsSessionOnly(t *testing.T) {
	f := newFixture(t)
	persisted, err := RecordTrust(f.env, f.env.Home)
	if err != nil || persisted {
		t.Fatalf("home trust must not persist: %v %v", persisted, err)
	}
	st := CheckTrust(f.env, f.env.Home, nil)
	if st.Trusted || !st.HomeDir {
		t.Fatalf("got %+v", st)
	}
}

func TestTrustSymlinkedCwd(t *testing.T) {
	f := newFixture(t)
	link := filepath.Join(f.root, "link")
	if err := os.Symlink(f.proj, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	f.claudeJSON(trusted(f.proj)) // Claude Code records the physical path
	if NeedsTrust(f.env, link) {
		t.Fatal("logical cwd should match the physical trust record")
	}
}

func TestTrustSandboxedEnv(t *testing.T) {
	f := newFixture(t)
	f.vars["CLAUDE_CODE_SANDBOXED"] = "1"
	if st := CheckTrust(f.env, f.proj, nil); !st.Trusted || st.Source != TrustEnv {
		t.Fatalf("got %+v", st)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const mcpJSON = `{"mcpServers":{
  "db": {"command":"npx","args":["db-mcp","--port",8080],"env":{"TOKEN":"x","A":"y"}},
  "docs": {"type":"http","url":"https://example.test/mcp"},
  "web": {"url":"https://example.test/sse"},
  "evil.name": {"command":"sh"}
}}`

func TestMcpApproval(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(f.proj, ".mcp.json"), mcpJSON)
	f.write(filepath.Join(f.proj, ".claude", "settings.local.json"),
		`{"enabledMcpjsonServers":["docs"],"disabledMcpjsonServers":["web"]}`)
	f.write(filepath.Join(f.env.Home, ".claude", "settings.json"), `{"enabledMcpjsonServers":["evil_name"]}`)
	layers := LoadSettings(f.env, f.proj, "")
	res := McpApproval(f.env, f.proj, layers, nil, nil)

	got := map[string]McpDecision{}
	for _, s := range res.Servers {
		got[s.Server.Name] = s.Decision
	}
	want := map[string]McpDecision{"db": McpPending, "docs": McpApproved, "web": McpRejected, "evil.name": McpApproved}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decisions = %v want %v", got, want)
	}
	db := res.Servers[0].Server
	if db.Name != "db" || db.Command != "npx" || !slices.Equal(db.Args, []string{"db-mcp", "--port", "8080"}) ||
		!slices.Equal(db.EnvKeys, []string{"A", "TOKEN"}) || db.Transport != "stdio" {
		t.Fatalf("db parsed as %+v", db)
	}
	if p := res.Pending(); len(p) != 1 || p[0].Name != "db" {
		t.Fatalf("pending = %+v", p)
	}
	if d := res.Disabled(nil); !slices.Equal(d, []string{"db", "web"}) {
		t.Fatalf("disabled without answers = %v", d)
	}
	if d := res.Disabled(map[string]bool{"db": true}); !slices.Equal(d, []string{"web"}) {
		t.Fatalf("disabled after approving db = %v", d)
	}
}

func TestMcpApprovalEnableAllAndLegacy(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(f.proj, ".mcp.json"), mcpJSON)
	f.claudeJSON(map[string]any{"projects": map[string]any{f.proj: map[string]any{
		"disabledMcpjsonServers": []string{"db"}, "enableAllProjectMcpServers": true,
	}}})
	res := McpApproval(f.env, f.proj, LoadSettings(f.env, f.proj, ""), nil, nil)
	if len(res.Pending()) != 0 {
		t.Fatalf("enableAll should approve everything not rejected: %+v", res.Servers)
	}
	if d := res.Disabled(nil); !slices.Equal(d, []string{"db"}) {
		t.Fatalf("legacy rejection lost: %v", d)
	}
}

func TestMcpApprovalNearestFileWinsAndErrors(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(filepath.Dir(f.proj), ".mcp.json"), `{"mcpServers":{"db":{"command":"far"},"up":{"command":"u"}}}`)
	f.write(filepath.Join(f.proj, ".mcp.json"), `{"mcpServers":{"db":{"command":"near"}}}`)
	res := McpApproval(f.env, f.proj, nil, nil, nil)
	if len(res.Servers) != 2 || res.Servers[0].Server.Command != "near" {
		t.Fatalf("got %+v", res.Servers)
	}
	f.write(filepath.Join(f.proj, ".mcp.json"), `{oops`)
	res = McpApproval(f.env, f.proj, nil, nil, nil)
	if len(res.Errors) != 1 || len(res.Servers) != 2 || res.Servers[0].Server.Command != "far" {
		t.Fatalf("malformed near file: %+v", res)
	}
}

func TestMcpEnableAll(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(f.proj, ".mcp.json"), `{"mcpServers":{"a":{"command":"x"}}}`)
	in := Input{Env: f.env, Cwd: f.proj, AutoTrust: true}
	r := Evaluate(in)
	a := Answers{McpEnableAll: true}
	if out, _ := r.Resolve(a); out.Settings != "" {
		t.Fatalf("enable-all should disable nothing: %q", out.Settings)
	}
	if err := r.Record(a); err != nil {
		t.Fatal(err)
	}
	f.write(filepath.Join(f.proj, ".mcp.json"), `{"mcpServers":{"a":{"command":"x"},"b":{"command":"y"}}}`)
	if got := Evaluate(in).Pending(); len(got) != 0 {
		t.Fatalf("future servers should be approved: %v", got)
	}
}

func TestBypassWarning(t *testing.T) {
	f := newFixture(t)
	layers := LoadSettings(f.env, f.proj, "")
	cases := []struct {
		name  string
		flags LaunchFlags
		want  bool
	}{
		{"default mode", LaunchFlags{}, false},
		{"allow only", LaunchFlags{AllowDangerously: true}, false},
		{"dangerously skip", LaunchFlags{DangerouslySkip: true}, true},
		{"mode flag", LaunchFlags{PermissionMode: "bypassPermissions"}, true},
		{"other mode flag", LaunchFlags{PermissionMode: "plan"}, false},
	}
	for _, c := range cases {
		if got := BypassWarningNeeded(c.flags, layers, nil); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}

	// defaultMode in settings starts in bypass unless a mode flag overrides it.
	f.write(filepath.Join(f.env.Home, ".claude", "settings.json"), `{"permissions":{"defaultMode":"bypassPermissions"}}`)
	layers = LoadSettings(f.env, f.proj, "")
	if !BypassWarningNeeded(LaunchFlags{}, layers, nil) || BypassWarningNeeded(LaunchFlags{PermissionMode: "default"}, layers, nil) {
		t.Fatal("settings defaultMode handling wrong")
	}

	// A repository can't suppress the warning; the user's settings can.
	f.write(filepath.Join(f.proj, ".claude", "settings.json"), `{"skipDangerousModePermissionPrompt":true}`)
	f.write(filepath.Join(f.proj, ".claude", "settings.local.json"), `{"skipDangerousModePermissionPrompt":true}`)
	layers = LoadSettings(f.env, f.proj, "")
	if !BypassWarningNeeded(LaunchFlags{DangerouslySkip: true}, layers, nil) {
		t.Fatal("project settings must not suppress the bypass warning")
	}
	f.write(filepath.Join(f.env.Home, ".claude", "settings.json"), `{"skipDangerousModePermissionPrompt":true}`)
	layers = LoadSettings(f.env, f.proj, "")
	if BypassWarningNeeded(LaunchFlags{DangerouslySkip: true}, layers, nil) {
		t.Fatal("user setting should suppress")
	}

	// Acceptance recorded by mantle suppresses it too.
	f.write(filepath.Join(f.env.Home, ".claude", "settings.json"), `{}`)
	layers = LoadSettings(f.env, f.proj, "")
	if err := RecordBypassAccepted(f.env); err != nil {
		t.Fatal(err)
	}
	if BypassWarningNeeded(LaunchFlags{DangerouslySkip: true}, layers, LoadGateStore(f.env)) {
		t.Fatal("mantle acceptance should suppress")
	}

	// Managed policy can disable bypass entirely.
	f.write(filepath.Join(f.env.ManagedDir, "managed-settings.json"), `{"permissions":{"disableBypassPermissionsMode":"disable"}}`)
	layers = LoadSettings(f.env, f.proj, "")
	if BypassAvailable(LaunchFlags{DangerouslySkip: true}, layers) || BypassWarningNeeded(LaunchFlags{DangerouslySkip: true}, layers, nil) {
		t.Fatal("disabled bypass must be unavailable")
	}
}

func TestAutoNoticeRecorded(t *testing.T) {
	f := newFixture(t)
	if LoadGateStore(f.env).AutoNoticeAt != "" {
		t.Fatal("fresh store")
	}
	if err := RecordAutoNotice(f.env); err != nil {
		t.Fatal(err)
	}
	if LoadGateStore(f.env).AutoNoticeAt == "" {
		t.Fatal("notice should be recorded")
	}
}

func TestAPIKeyApproval(t *testing.T) {
	f := newFixture(t)
	if st := APIKeyApproval(f.env, nil, nil); st.Status != APIKeyNone {
		t.Fatalf("no key: %+v", st)
	}
	key := "sk-ant-api03-" + strings.Repeat("x", 40) + "ABCDEFGHIJKLMNOPQRST"
	f.vars["ANTHROPIC_API_KEY"] = " " + key + "\n"
	st := APIKeyApproval(f.env, LoadGlobalConfig(f.env), nil)
	if st.Status != APIKeyNew || st.Suffix != "ABCDEFGHIJKLMNOPQRST" {
		t.Fatalf("new key: %+v", st)
	}
	f.claudeJSON(map[string]any{"customApiKeyResponses": map[string]any{"rejected": []string{"ABCDEFGHIJKLMNOPQRST"}}})
	st = APIKeyApproval(f.env, LoadGlobalConfig(f.env), nil)
	if st.Status != APIKeyRejected || !slices.Equal(st.EngineEnvUnset(), []string{"ANTHROPIC_API_KEY"}) {
		t.Fatalf("claude rejection: %+v", st)
	}
	// mantle's own answer wins and is stored as a hash only.
	if err := RecordAPIKey(f.env, key, true); err != nil {
		t.Fatal(err)
	}
	st = APIKeyApproval(f.env, LoadGlobalConfig(f.env), LoadGateStore(f.env))
	if st.Status != APIKeyApproved || st.EngineEnvUnset() != nil {
		t.Fatalf("mantle approval: %+v", st)
	}
	raw, _ := os.ReadFile(f.env.GateStorePath())
	if strings.Contains(string(raw), "ABCDEFGHIJKLMNOPQRST") || strings.Contains(string(raw), "sk-ant") {
		t.Fatal("gate store must not contain the key")
	}
}

func TestSettingsProblems(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(f.env.Home, ".claude", "settings.json"), "{\n  \"a\": 1,\n  oops\n}")
	f.write(filepath.Join(f.proj, ".claude", "settings.json"), `[1,2]`)
	f.write(filepath.Join(f.proj, ".claude", "settings.local.json"), ``)
	layers := LoadSettings(f.env, f.proj, `{"x":`)
	probs := layers.Problems()
	if len(probs) != 3 {
		t.Fatalf("problems = %+v", probs)
	}
	if probs[0].Scope != ScopeUser || !strings.Contains(probs[0].Err.Error(), "line 3") {
		t.Fatalf("user problem = %v", probs[0].Err)
	}
	if !strings.Contains(probs[1].Err.Error(), "JSON object") {
		t.Fatalf("project problem = %v", probs[1].Err)
	}
	if probs[2].Scope != ScopeFlag {
		t.Fatalf("flag problem = %+v", probs[2])
	}
}

func TestSettingsPrecedenceAndManagedDropIns(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(f.env.Home, ".claude", "settings.json"), `{"permissions":{"defaultMode":"plan","allow":["Read"]}}`)
	f.write(filepath.Join(f.proj, ".claude", "settings.json"), `{"permissions":{"defaultMode":"acceptEdits","allow":["Bash(ls)","Read"]}}`)
	f.write(filepath.Join(f.env.ManagedDir, "managed-settings.d", "10-a.json"), `{"permissions":{"defaultMode":"default"}}`)
	f.write(filepath.Join(f.env.ManagedDir, "managed-settings.d", "20-b.json"), `{"x":1}`)
	layers := LoadSettings(f.env, f.proj, "")
	if got := layers.String("permissions.defaultMode"); got != "default" {
		t.Fatalf("policy should win, got %q", got)
	}
	if got := layers.String("permissions.defaultMode", ScopeUser, ScopeProject); got != "acceptEdits" {
		t.Fatalf("project over user, got %q", got)
	}
	if got := layers.Strings("permissions.allow"); !slices.Equal(got, []string{"Read", "Bash(ls)"}) {
		t.Fatalf("arrays should union, got %v", got)
	}
}

func TestFlagSettings(t *testing.T) {
	f := newFixture(t)
	s, err := FlagSettings("", f.proj, nil)
	if err != nil || s != "" {
		t.Fatalf("empty: %q %v", s, err)
	}
	s, err = FlagSettings(`{"model":"x","disabledMcpjsonServers":["a"]}`, f.proj,
		map[string]any{"disabledMcpjsonServers": []string{"b", "a"}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "x" || !reflect.DeepEqual(got["disabledMcpjsonServers"], []any{"a", "b"}) {
		t.Fatalf("merged = %s", s)
	}
	f.write(filepath.Join(f.proj, "s.json"), `{"y":true}`)
	s, err = FlagSettings("s.json", f.proj, map[string]any{"disabledMcpjsonServers": []string{"c"}})
	if err != nil || s != `{"disabledMcpjsonServers":["c"],"y":true}` {
		t.Fatalf("path form: %q %v", s, err)
	}
	if _, err := FlagSettings(`{bad`, f.proj, nil); err == nil {
		t.Fatal("bad user --settings should error")
	}
}

func TestTrustReport(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(f.proj, ".claude", "settings.json"), `{
	  "permissions": {"allow": ["Bash(npm test:*)"], "additionalDirectories": ["../shared"]},
	  "env": {"FOO": "1", "BAR": "2"},
	  "apiKeyHelper": "./get-key.sh",
	  "statusLine": {"type": "command", "command": "./status.sh"},
	  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "./check.sh"}]}],
	            "Stop": [{"hooks": [{"type": "http", "url": "https://hook.test"}]}]}
	}`)
	f.write(filepath.Join(f.env.Home, ".claude", "settings.json"), `{"permissions":{"allow":["Read"]},"env":{"USERVAR":"1"}}`)
	f.write(filepath.Join(f.proj, ".mcp.json"), `{"mcpServers":{"db":{"command":"x"}}}`)
	mustMkdir(t, filepath.Join(f.proj, ".claude", "commands"))
	layers := LoadSettings(f.env, f.proj, "")
	r := CollectTrustReport(f.proj, layers, McpApproval(f.env, f.proj, layers, nil, nil))

	if len(r.AllowRules) != 1 || r.AllowRules[0].Value != "Bash(npm test:*)" {
		t.Fatalf("allow rules (user scope must be excluded): %+v", r.AllowRules)
	}
	if len(r.Env) != 2 || r.Env[0].Value != "BAR" {
		t.Fatalf("env: %+v", r.Env)
	}
	if len(r.Helpers) != 2 || r.Helpers[0].Detail != "apiKeyHelper" || r.Helpers[1].Value != "./status.sh" {
		t.Fatalf("helpers: %+v", r.Helpers)
	}
	if len(r.Hooks) != 2 || r.Hooks[0].Event != "PreToolUse" || r.Hooks[0].Detail != "./check.sh" ||
		r.Hooks[1].Kind != "http" || r.Hooks[1].Detail != "https://hook.test" {
		t.Fatalf("hooks: %+v", r.Hooks)
	}
	if len(r.AdditionalDirs) != 1 || len(r.McpServers) != 1 || !slices.Equal(r.Extras, []string{filepath.Join(".claude", "commands")}) {
		t.Fatalf("report: %+v", r)
	}
	if r.Empty() {
		t.Fatal("report should not be empty")
	}
}

func TestEvaluateResolveRecord(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(f.proj, ".mcp.json"), `{"mcpServers":{"db":{"command":"x"},"ok":{"command":"y"}}}`)
	f.vars["ANTHROPIC_API_KEY"] = "sk-test-key-0123456789abcdefghij"
	in := Input{Env: f.env, Cwd: f.proj, Flags: LaunchFlags{DangerouslySkip: true, Settings: `{"model":"m"}`}}
	r := Evaluate(in)
	want := []Kind{KindTrust, KindBypass, KindMcp, KindAPIKey}
	if got := r.Pending(); !slices.Equal(got, want) {
		t.Fatalf("pending = %v want %v", got, want)
	}

	// Declining trust exits; declining bypass exits.
	if out, _ := r.Resolve(Answers{}); out.Proceed || out.Reason == "" {
		t.Fatalf("declined trust should not proceed: %+v", out)
	}
	if out, _ := r.Resolve(Answers{TrustAccepted: true}); out.Proceed {
		t.Fatal("declined bypass should not proceed")
	}

	no := false
	a := Answers{TrustAccepted: true, BypassAccepted: true, McpApproved: map[string]bool{"ok": true}, APIKeyApproved: &no}
	out, err := r.Resolve(a)
	if err != nil || !out.Proceed {
		t.Fatalf("resolve: %+v %v", out, err)
	}
	if out.Settings != `{"disabledMcpjsonServers":["db"],"model":"m"}` {
		t.Fatalf("settings = %s", out.Settings)
	}
	if !slices.Equal(out.UnsetEnv, []string{"ANTHROPIC_API_KEY"}) {
		t.Fatalf("unset env = %v", out.UnsetEnv)
	}
	if err := r.Record(a); err != nil {
		t.Fatal(err)
	}
	// Next launch: every answer is remembered, and the rejected server stays disabled.
	r2 := Evaluate(in)
	if got := r2.Pending(); len(got) != 0 {
		t.Fatalf("after record, pending = %v", got)
	}
	if out, _ := r2.Resolve(Answers{}); out.Settings != `{"disabledMcpjsonServers":["db"],"model":"m"}` {
		t.Fatalf("remembered rejection lost: %s", out.Settings)
	}
	// AutoTrust and StrictMcpConfig skip their gates.
	in2 := Input{Env: f.env, Cwd: filepath.Join(f.root, "other"), AutoTrust: true, Flags: LaunchFlags{StrictMcpConfig: true}}
	mustMkdir(t, in2.Cwd)
	f.write(filepath.Join(in2.Cwd, ".mcp.json"), `{"mcpServers":{"z":{"command":"z"}}}`)
	if got := Evaluate(in2).Pending(); len(got) != 0 {
		t.Fatalf("auto-trusted strict launch pending = %v", got)
	}
}

func TestNotices(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(f.env.Home, ".claude.json"), `nope`)
	f.write(filepath.Join(f.proj, ".claude", "settings.json"), `{`)
	f.write(filepath.Join(f.proj, ".mcp.json"), `{`)
	n := Evaluate(Input{Env: f.env, Cwd: f.proj}).Notices()
	if len(n) != 3 {
		t.Fatalf("notices = %q", n)
	}
}
