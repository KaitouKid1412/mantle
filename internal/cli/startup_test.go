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
