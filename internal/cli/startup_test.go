package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

type fakeResolver struct {
	latest   string
	sessions map[string]string // arg → id
	missing  map[string]bool   // arg → named a session that doesn't exist
}

func (f fakeResolver) Continue(cwd string) (string, error) {
	if f.latest == "" {
		return "", errors.New("none")
	}
	return f.latest, nil
}

func (f fakeResolver) Resolve(cwd, arg string) (string, string, error) {
	if f.missing[arg] {
		return "", "", errors.New("not found")
	}
	if id, ok := f.sessions[arg]; ok {
		return id, "", nil
	}
	return "", arg, nil
}

var resolver = fakeResolver{
	latest:   "11111111-1111-4111-8111-111111111111",
	sessions: map[string]string{"work": "22222222-2222-4222-8222-222222222222"},
	missing:  map[string]bool{"33333333-3333-4333-8333-333333333333": true},
}

func startup(t *testing.T, argv ...string) Startup {
	t.Helper()
	p, err := Parse(argv)
	if err != nil {
		t.Fatalf("Parse(%q): %v", argv, err)
	}
	s, err := p.Startup("/work/repo", resolver)
	if err != nil {
		t.Fatalf("Startup(%q): %v", argv, err)
	}
	return s
}

// The plan's B1 example, with the prompt before the variadic --add-dir (after it,
// claude reads the prompt as a directory; see TestStartupSwallowedPrompt).
func TestStartupModelAddDirPrompt(t *testing.T) {
	s := startup(t, "--model", "sonnet", "hello", "--add-dir", "../x")
	o := s.Spawn
	if o.Model != "sonnet" || !slices.Equal(o.AddDirs, []string{"../x"}) || len(o.ExtraArgs) != 0 || o.Cwd != "/work/repo" {
		t.Errorf("spawn %+v", o)
	}
	if s.Prompt != "hello" || s.Picker || s.MainSpawn() == nil {
		t.Errorf("startup %+v", s)
	}
	if s.Session.Model != "sonnet" || s.Session.Cwd != "/work/repo" || s.Session.EngineID != ext.MainEngine {
		t.Errorf("session %+v", s.Session)
	}
}

func TestStartupSwallowedPrompt(t *testing.T) {
	s := startup(t, "--model", "sonnet", "--add-dir", "../x", "fix the bug")
	if s.Prompt != "" || !slices.Equal(s.Spawn.AddDirs, []string{"../x", "fix the bug"}) {
		t.Errorf("startup %+v", s)
	}
	if len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], "not as the prompt") {
		t.Errorf("warnings %q", s.Warnings)
	}
}

func TestStartupExtraArgsVerbatim(t *testing.T) {
	s := startup(t, "--verbose", "-d", "api", "--settings", "a.json", "--allowed-tools", "Bash(git *)", "Edit",
		"--permission-mode", "plan", "--new-flag=1", "-cd", "--settings", "{\"x\":1}", "--chrome")
	o := s.Spawn
	want := []string{"--verbose", "-d", "api", "--allowed-tools", "Bash(git *)", "Edit", "--new-flag=1", "-d", "--chrome"}
	if !slices.Equal(o.ExtraArgs, want) {
		t.Errorf("extra args %q, want %q", o.ExtraArgs, want)
	}
	if o.Settings != `{"x":1}` || o.PermissionMode != "plan" || o.Resume != resolver.latest {
		t.Errorf("spawn %+v", o)
	}
	if !s.Verbose || !slices.Equal(s.Unknown, []string{"--new-flag=1"}) {
		t.Errorf("startup %+v", s)
	}
}

func TestStartupSessions(t *testing.T) {
	for _, c := range []struct {
		name        string
		argv        []string
		resume      string
		cont        bool
		picker      bool
		query       string
		fork        bool
		sid         string
		sessionID   string
		warn        string
		noResolver  bool
		errContains string
	}{
		{name: "continue", argv: []string{"-c"}, resume: resolver.latest, sessionID: resolver.latest},
		{name: "continue without resolver", argv: []string{"-c"}, cont: true, noResolver: true},
		{name: "resume picker", argv: []string{"-r"}, picker: true},
		{name: "resume by name", argv: []string{"-r", "work"}, resume: "22222222-2222-4222-8222-222222222222", sessionID: "22222222-2222-4222-8222-222222222222"},
		{name: "resume search", argv: []string{"--resume", "auth bug"}, picker: true, query: "auth bug"},
		{name: "resume without resolver", argv: []string{"-r", "abc"}, resume: "abc", sessionID: "abc", noResolver: true},
		{name: "resume missing", argv: []string{"-r", "33333333-3333-4333-8333-333333333333"}, errContains: "no conversation found"},
		{name: "resume and continue", argv: []string{"-c", "-r", "work"}, resume: "22222222-2222-4222-8222-222222222222",
			sessionID: "22222222-2222-4222-8222-222222222222", warn: "both"},
		{name: "fork", argv: []string{"-r", "work", "--fork-session", "--session-id", "44444444-4444-4444-8444-444444444444"},
			resume: "22222222-2222-4222-8222-222222222222", fork: true, sid: "44444444-4444-4444-8444-444444444444",
			sessionID: "44444444-4444-4444-8444-444444444444"},
		{name: "fork alone", argv: []string{"--fork-session"}, fork: true, warn: "only applies"},
		{name: "new session id", argv: []string{"--session-id", "55555555-5555-4555-8555-555555555555"},
			sid: "55555555-5555-4555-8555-555555555555", sessionID: "55555555-5555-4555-8555-555555555555"},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, err := Parse(c.argv)
			if err != nil {
				t.Fatal(err)
			}
			var res SessionResolver = resolver
			if c.noResolver {
				res = nil
			}
			s, err := p.Startup("/work/repo", res)
			if c.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), c.errContains) {
					t.Fatalf("err %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			o := s.Spawn
			if o.Resume != c.resume || o.Continue != c.cont || s.Picker != c.picker || s.PickerQuery != c.query ||
				o.ForkSession != c.fork || o.SessionID != c.sid || s.Session.SessionID != c.sessionID {
				t.Errorf("spawn %+v picker %v %q session %q", o, s.Picker, s.PickerQuery, s.Session.SessionID)
			}
			if (s.MainSpawn() == nil) != c.picker {
				t.Errorf("MainSpawn nil = %v, picker %v", s.MainSpawn() == nil, c.picker)
			}
			joined := strings.Join(s.Warnings, "\n")
			if (c.warn == "") != (joined == "") || !strings.Contains(joined, c.warn) {
				t.Errorf("warnings %q, want %q", s.Warnings, c.warn)
			}
		})
	}
}

// An unnamed -w gets a name once, so engine restarts reuse the same worktree (claude
// picks a new random one on every start; verified on 2.1.288).
func TestStartupWorktreeNamedOnce(t *testing.T) {
	old := newWorktreeName
	newWorktreeName = func() string { return "brisk-comet-3f9a" }
	t.Cleanup(func() { newWorktreeName = old })

	s := startup(t, "-w", "--verbose")
	if s.Worktree != "brisk-comet-3f9a" || !slices.Equal(s.Spawn.ExtraArgs, []string{"-w", "brisk-comet-3f9a", "--verbose"}) {
		t.Errorf("unnamed: %q %q", s.Worktree, s.Spawn.ExtraArgs)
	}
	s = startup(t, "--worktree=feat", "hi")
	if s.Worktree != "feat" || !slices.Equal(s.Spawn.ExtraArgs, []string{"--worktree=feat"}) || s.Prompt != "hi" {
		t.Errorf("named: %q %q", s.Worktree, s.Spawn.ExtraArgs)
	}
	s = startup(t, "-cw")
	if !slices.Equal(s.Spawn.ExtraArgs, []string{"-w", "brisk-comet-3f9a"}) {
		t.Errorf("cluster: %q", s.Spawn.ExtraArgs)
	}
	if real := old(); !regexp.MustCompile(`^[a-z]+-[a-z]+-[0-9a-f]{4}$`).MatchString(real) {
		t.Errorf("generated name %q", real)
	}
}

func TestStartupContinueNoSession(t *testing.T) {
	p, _ := Parse([]string{"-c"})
	if _, err := p.Startup("/w", fakeResolver{}); !errors.Is(err, ErrNoSession) {
		t.Fatalf("err %v", err)
	}
}

func TestStartupConsumedFlags(t *testing.T) {
	s := startup(t, "-n", "auth work", "--ax-screen-reader", "--prompt-suggestions", "--prefill", "draft", "--safe", "--safe-mode", "hi")
	if s.Spawn.Name != "auth work" || s.Session.Title != "auth work" || !s.ScreenReader || s.PromptSuggestions == nil ||
		!*s.PromptSuggestions || s.Prefill != "draft" || !s.Safe || !s.Spawn.SafeMode || s.Prompt != "hi" {
		t.Errorf("startup %+v", s)
	}
	if !slices.Equal(s.Spawn.ExtraArgs, []string{"--safe-mode"}) {
		t.Errorf("extra args %q", s.Spawn.ExtraArgs)
	}
}

// --research is mantle's own: it never reaches the engine and reaches the UI process as
// ext.EnvResearch (request 13-11).
func TestStartupResearch(t *testing.T) {
	s := startup(t, "--research", "--model", "opus", "hi")
	if !s.Research || s.Prompt != "hi" {
		t.Errorf("startup %+v", s)
	}
	if slices.Contains(s.Spawn.ExtraArgs, "--research") {
		t.Errorf("extra args %q", s.Spawn.ExtraArgs)
	}
	env := map[string]string{}
	s.SetEnv(func(k, v string) error { env[k] = v; return nil })
	if env[ext.EnvResearch] != "1" || len(env) != 1 {
		t.Errorf("env %v", env)
	}
	env = map[string]string{}
	startup(t, "hi").SetEnv(func(k, v string) error { env[k] = v; return nil })
	if len(env) != 0 {
		t.Errorf("env without flags %v", env)
	}
}

func TestStartupNeedsUIMode(t *testing.T) {
	p, _ := Parse([]string{"-p", "hi"})
	if _, err := p.Startup("/w", nil); err == nil {
		t.Fatal("want an error for exec mode")
	}
}

func TestGateInputsOf(t *testing.T) {
	g := GateInputsOf(ext.SpawnOpts{PermissionMode: "plan", Settings: "a.json",
		ExtraArgs: []string{"--dangerously-skip-permissions", "--allowed-tools", "Bash", "--strict-mcp-config", "--allow-dangerously-skip-permissions"}})
	if g != (GateInputs{PermissionMode: "plan", Settings: "a.json", SkipPermissions: true, AllowSkipPermissions: true, StrictMcpConfig: true}) {
		t.Errorf("gate %+v", g)
	}
	// Flags left in ExtraArgs (by other spawners) come last on the engine's command
	// line, so they win.
	g = GateInputsOf(ext.SpawnOpts{PermissionMode: "plan", ExtraArgs: []string{"--permission-mode", "bypassPermissions", "--settings", "b.json"}})
	if g.PermissionMode != "bypassPermissions" || g.Settings != "b.json" {
		t.Errorf("gate %+v", g)
	}
	s := startup(t, "--dangerously-skip-permissions", "--permission-mode", "acceptEdits", "--settings", "{}")
	if g := GateInputsOf(s.Spawn); !g.SkipPermissions || g.PermissionMode != "acceptEdits" || g.Settings != "{}" {
		t.Errorf("from startup %+v", g)
	}
}

func TestMergeSettings(t *testing.T) {
	overlay := map[string]any{"disabledMcpjsonServers": []string{"evil", "b"}}

	if got, err := MergeSettings("x.json", nil); err != nil || got != "x.json" {
		t.Errorf("no overlay: %q %v", got, err)
	}
	got, err := MergeSettings("", overlay)
	if err != nil || got != `{"disabledMcpjsonServers":["evil","b"]}` {
		t.Errorf("empty value: %q %v", got, err)
	}

	got, err = MergeSettings(`{"model":"opus","disabledMcpjsonServers":["a","b"],"env":{"X":"1"}}`,
		map[string]any{"disabledMcpjsonServers": []string{"evil", "b"}, "env": map[string]any{"Y": "2"}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatal(err)
	}
	if m["model"] != "opus" || len(m["disabledMcpjsonServers"].([]any)) != 3 || m["env"].(map[string]any)["X"] != "1" || m["env"].(map[string]any)["Y"] != "2" {
		t.Errorf("merged %s", got)
	}

	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, []byte(`{"theme":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = MergeSettings(path, overlay)
	if err != nil || !strings.Contains(got, `"theme":"dark"`) || !strings.Contains(got, "evil") {
		t.Errorf("file: %q %v", got, err)
	}

	if _, err := MergeSettings("{not json", overlay); err == nil {
		t.Error("want an error for bad JSON")
	}
	if _, err := MergeSettings(filepath.Join(t.TempDir(), "missing.json"), overlay); err == nil {
		t.Error("want an error for a missing file")
	}
}

func TestFlagSettings(t *testing.T) {
	s := startup(t, "--settings", `{"theme":"dark"}`)
	if got, err := s.FlagSettings(); err != nil || got != `{"theme":"dark"}` {
		t.Errorf("no verbose: %q %v", got, err)
	}
	s = startup(t, "--verbose", "--settings", `{"theme":"dark","verbose":false}`)
	got, err := s.FlagSettings()
	if err != nil || got != `{"theme":"dark","verbose":true}` {
		t.Errorf("verbose: %q %v", got, err)
	}
	if s.Spawn.Settings != `{"theme":"dark","verbose":false}` {
		t.Errorf("engine settings changed: %q", s.Spawn.Settings)
	}
	if got, err := startup(t, "--verbose").FlagSettings(); err != nil || got != `{"verbose":true}` {
		t.Errorf("verbose only: %q %v", got, err)
	}
}

// RestartArgs round-trips: parsing them again gives the same engine options, resuming
// the session, in the same worktree, with the UI flags kept and nothing re-submitted.
func TestRestartArgs(t *testing.T) {
	old := newWorktreeName
	newWorktreeName = func() string { return "brisk-comet-3f9a" }
	t.Cleanup(func() { newWorktreeName = old })

	s := startup(t, "fix it", "-w", "--model", "sonnet", "--add-dir", "../a", "../b", "--settings", "{}", "--ax-screen-reader",
		"--prompt-suggestions", "off", "-n", "work", "--fork-session", "--session-id", "55555555-5555-4555-8555-555555555555",
		"--prefill", "x", "--allowed-tools", "Bash", "Edit")
	args := s.RestartArgs(RestartOpts{SessionID: "66666666-6666-4666-8666-666666666666", PermissionMode: "acceptEdits"})

	p, err := Parse(args)
	if err != nil {
		t.Fatalf("%q: %v", args, err)
	}
	r, err := p.Startup("/work/repo", nil)
	if err != nil {
		t.Fatal(err)
	}
	o := r.Spawn
	if o.Resume != "66666666-6666-4666-8666-666666666666" || o.Model != "sonnet" || o.PermissionMode != "acceptEdits" ||
		!slices.Equal(o.AddDirs, []string{"../a", "../b"}) || o.Settings != "{}" || o.Name != "work" || o.ForkSession || o.SessionID != "" {
		t.Errorf("spawn %+v", o)
	}
	if !slices.Equal(o.ExtraArgs, []string{"-w", "brisk-comet-3f9a", "--allowed-tools", "Bash", "Edit"}) || r.Worktree != "brisk-comet-3f9a" {
		t.Errorf("extra args %q", o.ExtraArgs)
	}
	if !r.ScreenReader || r.PromptSuggestions == nil || *r.PromptSuggestions || r.Prompt != "" || r.Prefill != "" {
		t.Errorf("startup %+v", r)
	}
}

// The in-place restart's argv: RestartArgs plus the hand-off file. The flag is consumed,
// and Spawn still resumes the session as the host's fallback, even when the session
// index can't find it.
func TestStartupAttachEngineFDs(t *testing.T) {
	s := startup(t, "--model", "sonnet", "-n", "work")
	argv := append(s.RestartArgs(RestartOpts{SessionID: "33333333-3333-4333-8333-333333333333"}), "--attach-engine-fds=/run/42.handoff.json")
	p, err := Parse(argv)
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Startup("/work/repo", resolver) // resolver says this ID doesn't exist
	if err != nil {
		t.Fatal(err)
	}
	if r.AttachEngineFDs != "/run/42.handoff.json" || r.Spawn.Resume != "33333333-3333-4333-8333-333333333333" || r.MainSpawn() == nil {
		t.Errorf("startup %+v", r)
	}
	if slices.ContainsFunc(r.Spawn.ExtraArgs, func(a string) bool { return strings.Contains(a, "attach-engine-fds") }) {
		t.Errorf("hand-off flag forwarded: %q", r.Spawn.ExtraArgs)
	}
	// Without the hand-off a missing session is still an error.
	p, _ = Parse([]string{"-r", "33333333-3333-4333-8333-333333333333"})
	if _, err := p.Startup("/work/repo", resolver); err == nil {
		t.Error("want an error for a missing session")
	}

	// A session ID that isn't a UUID (the resolver would treat it as a search) still
	// resumes verbatim: no lookup, no picker, MainSpawn kept as the fallback.
	called := false
	lookup := ResolverFuncs{
		ContinueFunc: func(string) (string, error) { called = true; return "", nil },
		ResolveFunc:  func(_, arg string) (string, string, error) { called = true; return "", arg, nil },
	}
	p, _ = Parse([]string{"--resume=sess-not-a-uuid", "--attach-engine-fds=/run/h.json"})
	r, err = p.Startup("/work/repo", lookup)
	if err != nil || r.Picker || r.MainSpawn() == nil || r.Spawn.Resume != "sess-not-a-uuid" || called {
		t.Errorf("non-UUID hand-off: %+v err %v resolver called %v", r, err, called)
	}
}

func TestCurrent(t *testing.T) {
	ClearCurrent()
	if _, ok := Current(); ok {
		t.Fatal("current set")
	}
	SetCurrent(Startup{Prompt: "hi"})
	t.Cleanup(ClearCurrent)
	if s, ok := Current(); !ok || s.Prompt != "hi" {
		t.Errorf("current %+v %v", s, ok)
	}
}
