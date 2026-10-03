package claudecli

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeClaude installs a stand-in claude on PATH that records its argv (NUL
// separated) and environment, prints $FAKE_STDOUT_FILE and $FAKE_STDERR, then
// exits with $FAKE_EXIT.
type fakeClaude struct {
	dir     string
	argvLog string
	envLog  string
}

const fakeScript = `#!/bin/sh
printf '%s\0' "$@" > "$FAKE_ARGV_LOG"
/usr/bin/env > "$FAKE_ENV_LOG"
if [ -n "$FAKE_STDIN_LOG" ]; then /bin/cat > "$FAKE_STDIN_LOG"; fi
if [ -n "$FAKE_SLEEP" ]; then /bin/sleep "$FAKE_SLEEP"; fi
if [ -n "$FAKE_STDOUT_FILE" ]; then /bin/cat "$FAKE_STDOUT_FILE"; fi
if [ -n "$FAKE_STDERR" ]; then printf '%b' "$FAKE_STDERR" >&2; fi
exit "${FAKE_EXIT:-0}"
`

func newFakeClaude(t *testing.T) *fakeClaude {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fake needs a unix shell")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(fakeScript), 0o755); err != nil {
		t.Fatal(err)
	}
	f := &fakeClaude{dir: dir, argvLog: filepath.Join(dir, "argv"), envLog: filepath.Join(dir, "env")}
	t.Setenv("PATH", dir)
	t.Setenv(EnvBinary, "")
	t.Setenv("FAKE_ARGV_LOG", f.argvLog)
	t.Setenv("FAKE_ENV_LOG", f.envLog)
	for _, k := range []string{"FAKE_STDIN_LOG", "FAKE_SLEEP", "FAKE_STDOUT_FILE", "FAKE_STDERR", "FAKE_EXIT"} {
		t.Setenv(k, "")
	}
	return f
}

func (f *fakeClaude) argv(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(f.argvLog)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
}

func (f *fakeClaude) env(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(f.envLog)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(b), "\n")
}

// sample is one valid call per subcommand with the argv the CLI must receive.
type sample struct {
	sub  Subcommand
	args []string
	want []string
}

var samples = []sample{
	{Version, nil, []string{"--version"}},
	{RootHelp, nil, []string{"--help"}},
	{MCPList, nil, []string{"mcp", "list"}},
	{MCPGet, []string{"claude.ai Gmail"}, []string{"mcp", "get", "--", "claude.ai Gmail"}},
	{MCPAdd, []string{"-s", "user", "-t", "stdio", "-e", "A=1", "-e", "B=2", "my-server", "npx", "--", "-y", "pkg"},
		[]string{"mcp", "add", "--scope=user", "--transport=stdio", "--env=A=1", "--env=B=2", "--", "my-server", "npx", "-y", "pkg"}},
	{MCPAdd, []string{"--transport=http", "--header", "Authorization: Bearer x", "sentry", "https://example.test/mcp"},
		[]string{"mcp", "add", "--transport=http", "--header=Authorization: Bearer x", "--", "sentry", "https://example.test/mcp"}},
	{MCPAddJSON, []string{"--scope=project", "srv", `{"type":"stdio","command":"x"}`},
		[]string{"mcp", "add-json", "--scope=project", "--", "srv", `{"type":"stdio","command":"x"}`}},
	{MCPRemove, []string{"-s", "local", "srv"}, []string{"mcp", "remove", "--scope=local", "--", "srv"}},
	{MCPLogout, []string{"srv"}, []string{"mcp", "logout", "--", "srv"}},
	{MCPResetProjectChoices, nil, []string{"mcp", "reset-project-choices"}},
	{PluginList, []string{"--json", "--available"}, []string{"plugin", "list", "--json", "--available"}},
	{PluginDetails, []string{"demo@market"}, []string{"plugin", "details", "--", "demo@market"}},
	{PluginInstall, []string{"--json", "-s", "project", "demo@market"},
		[]string{"plugin", "install", "--json", "--scope=project", "--", "demo@market"}},
	{PluginUninstall, []string{"--json", "--keep-data", "demo@market"},
		[]string{"plugin", "uninstall", "--json", "--keep-data", "--", "demo@market"}},
	{PluginEnable, []string{"--json", "demo@market"}, []string{"plugin", "enable", "--json", "--", "demo@market"}},
	{PluginDisable, []string{"--json", "-a"}, []string{"plugin", "disable", "--json", "--all"}},
	{PluginUpdate, []string{"--json", "--scope", "managed", "demo@market"},
		[]string{"plugin", "update", "--json", "--scope=managed", "--", "demo@market"}},
	{PluginConfigure, []string{"--json", "demo@market"}, []string{"plugin", "configure", "--json", "--", "demo@market"}},
	{PluginPrune, []string{"--dry-run"}, []string{"plugin", "prune", "--dry-run"}},
	{MarketplaceList, []string{"--json"}, []string{"plugin", "marketplace", "list", "--json"}},
	{MarketplaceAdd, []string{"--json", "--sparse", ".claude-plugin", "--sparse", "plugins", "owner/repo"},
		[]string{"plugin", "marketplace", "add", "--json", "--sparse=.claude-plugin", "--sparse=plugins", "--", "owner/repo"}},
	{MarketplaceRemove, []string{"--json", "market"}, []string{"plugin", "marketplace", "remove", "--json", "--", "market"}},
	{MarketplaceUpdate, nil, []string{"plugin", "marketplace", "update"}},
	{AuthStatus, []string{"--json"}, []string{"auth", "status", "--json"}},
	{AuthLogout, nil, []string{"auth", "logout"}},
	{AgentsList, []string{"--all", "--cwd", "/work/repo"}, []string{"agents", "--json", "--all", "--cwd=/work/repo"}},
	{Logs, []string{"002a3c85"}, []string{"logs", "002a3c85"}},
	{Stop, []string{"002a3c85"}, []string{"stop", "002a3c85"}},
	{Import, []string{"--dry-run", "codex"}, []string{"import", "--dry-run", "codex"}},
	{Import, []string{"--yes=abc123", "gemini"}, []string{"import", "--yes=abc123", "gemini"}},
	{AutoModeDefaults, []string{"--label", "Git"}, []string{"auto-mode", "defaults", "--label=Git"}},
	{AutoModeConfig, nil, []string{"auto-mode", "config"}},
}

var interactiveSamples = []sample{
	{MCPAddFromDesktop, []string{"-s", "user"}, []string{"mcp", "add-from-claude-desktop", "--scope=user"}},
	{MCPLogin, []string{"--no-browser", "srv"}, []string{"mcp", "login", "--no-browser", "--", "srv"}},
	{MCPLogout, []string{"srv"}, []string{"mcp", "logout", "--", "srv"}},
	{AuthLogin, []string{"--console", "--email=a@example.test"}, []string{"auth", "login", "--console", "--email=a@example.test"}},
	{AuthLogout, nil, []string{"auth", "logout"}},
	{AgentsView, nil, []string{"agents"}},
	{Attach, []string{"002a3c85"}, []string{"attach", "002a3c85"}},
	{Import, []string{"cursor"}, []string{"import", "cursor"}},
	{Doctor, nil, []string{"doctor"}},
	{Update, nil, []string{"update"}},
}

// rootWords are the real top-level claude subcommands (and root flags) a
// Subcommand may start with. Anything else in argv[0] would be a prompt.
var rootWords = map[string]bool{
	"--version": true, "--help": true, "mcp": true, "plugin": true, "auth": true, "agents": true,
	"attach": true, "logs": true, "stop": true, "import": true, "auto-mode": true, "doctor": true, "update": true,
}

func TestEverySubcommandHasSamples(t *testing.T) {
	covered := map[Subcommand]bool{}
	for _, s := range samples {
		covered[s.sub] = true
	}
	for _, s := range interactiveSamples {
		covered[s.sub] = true
	}
	for _, sub := range Subcommands() {
		if !covered[sub] {
			t.Errorf("%v has no sample", sub)
		}
	}
}

// TestSpecTable checks the table rules that keep free text out of prompt
// position: every prefix starts with a real root word and has no spaces, and a
// subcommand without "--" accepts only strictly patterned operands.
func TestSpecTable(t *testing.T) {
	for _, sub := range Subcommands() {
		sp := sub.spec()
		if len(sp.prefix) == 0 || !rootWords[sp.prefix[0]] {
			t.Errorf("%v: prefix %q does not start with a known root word", sub, sp.prefix)
		}
		for _, w := range append(append([]string{}, sp.prefix...), sp.fixed...) {
			if w == "" || strings.ContainsAny(w, " \t\n") {
				t.Errorf("%v: prefix element %q must be one plain word", sub, w)
			}
		}
		if sp.modes == 0 {
			t.Errorf("%v: no mode", sub)
		}
		if sp.modes&modeRun != 0 && sp.timeout <= 0 {
			t.Errorf("%v: captured subcommand without a timeout", sub)
		}
		if !sp.dashdash {
			if sp.rest {
				t.Errorf("%v: rest operands need dashdash", sub)
			}
			for _, sl := range sp.slots {
				if sl.re == nil && len(sl.enum) == 0 {
					t.Errorf("%v: slot %s without dashdash must have a pattern or enum", sub, sl.name)
				}
				for _, probe := range []string{"-p", "--print", "two words", "--"} {
					if sl.check(probe) == nil {
						t.Errorf("%v: slot %s accepts %q without dashdash", sub, sl.name, probe)
					}
				}
			}
		}
	}
}

func TestRunArgvIsSeparateElements(t *testing.T) {
	f := newFakeClaude(t)
	ctx := context.Background()
	for _, s := range samples {
		if _, _, err := Run(ctx, s.sub, s.args...); err != nil {
			t.Errorf("%v %q: %v", s.sub, s.args, err)
			continue
		}
		got := f.argv(t)
		if !reflect.DeepEqual(got, s.want) {
			t.Errorf("%v %q:\n got %q\nwant %q", s.sub, s.args, got, s.want)
		}
		checkInvariant(t, s.sub, got)
	}
}

func TestInteractiveArgv(t *testing.T) {
	newFakeClaude(t)
	for _, s := range interactiveSamples {
		cmd, err := Interactive(s.sub, s.args...)
		if err != nil {
			t.Errorf("%v %q: %v", s.sub, s.args, err)
			continue
		}
		if got := cmd.Args[1:]; !reflect.DeepEqual(got, s.want) {
			t.Errorf("%v %q:\n got %q\nwant %q", s.sub, s.args, got, s.want)
		}
		if cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil {
			t.Errorf("%v: interactive cmd must leave stdio to tea.ExecProcess", s.sub)
		}
		if cmd.SysProcAttr != nil {
			t.Errorf("%v: interactive cmd must stay in the foreground process group", s.sub)
		}
		checkInvariant(t, s.sub, cmd.Args[1:])
	}
}

// checkInvariant asserts the structural guarantee: argv is prefix, fixed flags,
// allowlisted --flags, then operands only in declared slots (after "--" unless
// strictly patterned). No element can be a stray free-text word.
func checkInvariant(t *testing.T, sub Subcommand, argv []string) {
	t.Helper()
	sp := sub.spec()
	if !rootWords[argv[0]] {
		t.Fatalf("%v: argv[0] %q is not a known root word", sub, argv[0])
	}
	head := append(append([]string{}, sp.prefix...), sp.fixed...)
	if len(argv) < len(head) || !reflect.DeepEqual(argv[:len(head)], head) {
		t.Fatalf("%v: argv %q does not start with %q", sub, argv, head)
	}
	i := len(head)
	for ; i < len(argv) && strings.HasPrefix(argv[i], "--") && argv[i] != "--"; i++ {
		name, _, _ := strings.Cut(argv[i], "=")
		if sp.findFlag(name) == nil {
			t.Fatalf("%v: flag %q is not allowlisted", sub, argv[i])
		}
	}
	operands := argv[i:]
	if len(operands) > 0 && sp.dashdash {
		if operands[0] != "--" {
			t.Fatalf("%v: operands %q must follow --", sub, operands)
		}
		operands = operands[1:]
	}
	if len(operands) > len(sp.slots) && !sp.rest {
		t.Fatalf("%v: %d operands for %d slots", sub, len(operands), len(sp.slots))
	}
	for j, op := range operands {
		if j < len(sp.slots) && sp.slots[j].check(op) != nil {
			t.Fatalf("%v: operand %q fails slot %s", sub, op, sp.slots[j].name)
		}
	}
}

func TestRejectsDangerousArgs(t *testing.T) {
	newFakeClaude(t)
	ctx := context.Background()
	cases := []struct {
		sub  Subcommand
		args []string
	}{
		{invalidSubcommand, nil},
		{numSubcommands, nil},
		{Subcommand(999), nil},
		{Version, []string{"auth login"}},
		{RootHelp, []string{"hello"}},
		{MCPList, []string{"summarize my repo"}},
		{MCPList, []string{"--", "summarize my repo"}},
		{AuthStatus, []string{"auth login"}},
		{MCPGet, nil},
		{MCPGet, []string{"a", "b"}},
		{MCPGet, []string{""}},
		{MCPGet, []string{"line\nbreak"}},
		{MCPGet, []string{"-p"}},
		{MCPGet, []string{"--print"}},
		{MCPAdd, []string{"only-name"}},
		{MCPAdd, []string{"--scope=global", "n", "c"}},
		{MCPAdd, []string{"--scope=user", "--scope=project", "n", "c"}},
		{MCPAdd, []string{"--env"}},
		{MCPAdd, []string{"--env=", "n", "c"}},
		{MCPAdd, []string{"--client-secret=xyz", "n", "c"}},
		{PluginInstall, []string{"two words"}},
		{PluginInstall, []string{"--dangerously-skip-permissions", "p@m"}},
		{PluginInstall, []string{"--model=opus", "p@m"}},
		{PluginInstall, []string{"-p", "p@m"}},
		{PluginList, []string{"--data-size"}},
		{Logs, []string{"-p"}},
		{Logs, []string{"--", "-p"}},
		{Logs, []string{"two words"}},
		{Stop, []string{"a", "b"}},
		{Import, []string{"vscode"}},
		{Import, []string{"codex", "extra"}},
		{Import, []string{"--yes="}},
		{AgentsList, []string{"--json"}}, // --json is fixed, not caller-supplied
		{AutoModeDefaults, []string{"critique"}},
		{MCPLogin, []string{"srv"}},   // interactive only
		{AuthLogin, nil},              // interactive only
		{Doctor, nil},                 // interactive only
		{PluginInstall, []string{""}}, // empty operand
	}
	for _, c := range cases {
		_, _, err := Run(ctx, c.sub, c.args...)
		var ae *ArgError
		if !errors.As(err, &ae) {
			t.Errorf("Run(%v, %q) = %v, want ArgError", c.sub, c.args, err)
		}
	}
	for _, sub := range []Subcommand{MCPList, AuthStatus, PluginInstall, invalidSubcommand} {
		if _, err := Interactive(sub, "x@y"); err == nil {
			t.Errorf("Interactive(%v) accepted a captured-only subcommand", sub)
		}
	}
	if _, err := Interactive(Attach, "--", "-p"); err == nil {
		t.Errorf("Interactive(Attach, -p) accepted a flag-like id")
	}
	if _, err := Default.Do(ctx, Call{Sub: MCPList, Stdin: []byte("prompt")}); err == nil {
		t.Errorf("stdin accepted by a subcommand that does not declare it")
	}
	if _, err := Default.Do(ctx, Call{Sub: MCPList, Env: []string{"CLAUDE_CODE_SIMPLE=1"}}); err == nil {
		t.Errorf("undeclared env accepted")
	}
}

// TestRandomArgsKeepInvariant throws random mixes of flags, dangerous words and
// separators at every subcommand: whatever buildArgv accepts must satisfy the
// structural invariant.
func TestRandomArgsKeepInvariant(t *testing.T) {
	vocab := []string{
		"--", "-", "-p", "--print", "--json", "--scope", "user", "-s", "global", "--yes", "--yes=abc",
		"--all", "-a", "--env", "A=1", "summarize my repo", "auth login", "hello", "codex", "p@m",
		"002a3c85", "--label", "Git", "--cwd", "/x", "--no-browser", "srv", "npx", "-y", "{\"a\":1}",
		"--transport=http", "--header=X: y", "--dry-run", "--available", "line\nbreak", "",
	}
	rng := rand.New(rand.NewSource(1))
	accepted := 0
	for _, sub := range Subcommands() {
		for n := 0; n < 3000; n++ {
			args := make([]string, rng.Intn(6))
			for i := range args {
				args[i] = vocab[rng.Intn(len(vocab))]
			}
			argv, err := buildArgv(sub, args)
			if err != nil {
				continue
			}
			accepted++
			checkInvariant(t, sub, argv)
		}
	}
	if accepted == 0 {
		t.Fatal("no random call was accepted; the test proves nothing")
	}
}

func TestEnvironment(t *testing.T) {
	f := newFakeClaude(t)
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("FORCE_COLOR", "3")
	if _, _, err := Run(context.Background(), AuthStatus, "--json"); err != nil {
		t.Fatal(err)
	}
	env := f.env(t)
	for _, kv := range env {
		if strings.HasPrefix(kv, "CLAUDECODE=") || strings.HasPrefix(kv, "FORCE_COLOR=") {
			t.Errorf("child saw %s", kv)
		}
	}
	if !contains(env, "NO_COLOR=1") {
		t.Errorf("captured run should set NO_COLOR=1")
	}

	cmd, err := Interactive(Doctor)
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "CLAUDECODE=") {
			t.Errorf("interactive env kept %s", kv)
		}
	}
	if !contains(cmd.Env, "FORCE_COLOR=3") {
		t.Errorf("interactive env should keep the user's colour settings")
	}
}

func TestDeclaredEnvAndStdin(t *testing.T) {
	f := newFakeClaude(t)
	stdinLog := filepath.Join(f.dir, "stdin")
	t.Setenv("FAKE_STDIN_LOG", stdinLog)
	ctx := context.Background()
	_, err := Default.Do(ctx, Call{Sub: PluginConfigure, Args: []string{"--json", "--values-stdin", "p@m"},
		Stdin: []byte(`{"k":"v"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(stdinLog); string(b) != `{"k":"v"}` {
		t.Errorf("stdin = %q", b)
	}
	// Without declared stdin the child reads /dev/null.
	if _, _, err := Run(ctx, MCPList); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(stdinLog); len(b) != 0 {
		t.Errorf("stdin should be empty, got %q", b)
	}
	_, err = Default.Do(ctx, Call{Sub: MCPAdd, Args: []string{"--client-secret", "n", "https://x.test"},
		Env: []string{"MCP_CLIENT_SECRET=s3cret"}})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(f.env(t), "MCP_CLIENT_SECRET=s3cret") {
		t.Errorf("declared env not passed")
	}
}

func TestExitError(t *testing.T) {
	newFakeClaude(t)
	t.Setenv("FAKE_EXIT", "1")
	t.Setenv("FAKE_STDERR", `\033[31mNo MCP server named "x".\033[0m\nmore`)
	_, stderr, err := Run(context.Background(), MCPGet, "x")
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v, want ExitError", err)
	}
	if ee.Code != 1 {
		t.Errorf("code = %d", ee.Code)
	}
	if want := `claude mcp get: No MCP server named "x".`; ee.Error() != want {
		t.Errorf("Error() = %q, want %q", ee.Error(), want)
	}
	if !bytes.Contains(stderr, []byte("\x1b[31m")) {
		t.Errorf("raw stderr should be returned unchanged")
	}
}

func TestTimeoutKillsProcessGroup(t *testing.T) {
	newFakeClaude(t)
	t.Setenv("FAKE_SLEEP", "30")
	r := &Runner{Timeout: 200 * time.Millisecond}
	start := time.Now()
	_, _, err := r.Run(context.Background(), MCPList)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("run took %v; the sleeping child was not killed", d)
	}
}

func TestStdoutCapped(t *testing.T) {
	f := newFakeClaude(t)
	big := filepath.Join(f.dir, "big")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), 10000), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_STDOUT_FILE", big)
	r := &Runner{MaxOutput: 100}
	out, _, err := r.Run(context.Background(), MCPList)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 100 {
		t.Errorf("len(out) = %d, want 100", len(out))
	}
}

func TestResolveBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	look := func(name string) (string, error) {
		if name == "claude" || name == "claude-pinned" {
			return "/path/" + name, nil
		}
		return "", errors.New("not found")
	}
	if got, _ := resolveBinary(getenv, look); got != "/path/claude" {
		t.Errorf("PATH lookup = %q", got)
	}
	env[EnvBinary] = bin
	if got, _ := resolveBinary(getenv, look); got != bin {
		t.Errorf("explicit path = %q", got)
	}
	env[EnvBinary] = "claude-pinned"
	if got, _ := resolveBinary(getenv, look); got != "/path/claude-pinned" {
		t.Errorf("explicit name = %q", got)
	}
	env[EnvBinary] = filepath.Join(dir, "missing")
	if _, err := resolveBinary(getenv, look); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing explicit path: err = %v", err)
	}
	env[EnvBinary] = ""
	noLook := func(string) (string, error) { return "", errors.New("nope") }
	if _, err := resolveBinary(getenv, noLook); !errors.Is(err, ErrNotFound) {
		t.Errorf("no binary: err = %v", err)
	}
}

func TestHelpUsesRealPath(t *testing.T) {
	f := newFakeClaude(t)
	if _, err := Default.Help(context.Background(), MarketplaceList); err != nil {
		t.Fatal(err)
	}
	if got, want := f.argv(t), []string{"plugin", "marketplace", "list", "--help"}; !reflect.DeepEqual(got, want) {
		t.Errorf("help argv = %q, want %q", got, want)
	}
	if _, err := Default.Help(context.Background(), AgentsList); err != nil {
		t.Fatal(err)
	}
	if got, want := f.argv(t), []string{"agents", "--help"}; !reflect.DeepEqual(got, want) {
		t.Errorf("help argv = %q, want %q", got, want)
	}
}
