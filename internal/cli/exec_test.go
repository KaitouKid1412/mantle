package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeClaude writes a fake claude to a temp dir and points MANTLE_CLAUDE_BIN at it. It
// records argv (one per line) and a marker variable to $FAKE_OUT, answers --version
// and --help, and honours FAKE_SLEEP and FAKE_FAIL.
func fakeClaude(t *testing.T) (bin, out string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "claude")
	out = filepath.Join(dir, "argv.txt")
	script := `#!/bin/sh
if [ -n "$FAKE_OUT" ]; then
  printf '%s\n' "$@" > "$FAKE_OUT"
  printf 'MARK=%s\n' "$MANTLE_TEST_MARK" >> "$FAKE_OUT"
fi
case "$1" in
--version)
  [ -n "$FAKE_SLEEP" ] && sleep "$FAKE_SLEEP"
  echo "9.9.9 (Claude Code) cc=$CLAUDECODE"
  ;;
--help)
  if [ -n "$FAKE_FAIL" ]; then echo "boom" >&2; exit 3; fi
  echo "Usage: fake claude"
  ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvClaudeBin, bin)
	t.Setenv("MANTLE_HOME", filepath.Join(dir, "mantle"))
	// Generous timeouts: a loaded machine (a full go test ./...) can take seconds to
	// start even a shell script. Tests that check timeouts set their own.
	oldV, oldH := VersionTimeout, HelpTimeout
	VersionTimeout, HelpTimeout = time.Minute, time.Minute
	t.Cleanup(func() { VersionTimeout, HelpTimeout = oldV, oldH })
	return bin, out
}

func TestResolveClaude(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	look := func(name string) (string, error) {
		if name == "claude" || name == "claude-beta" {
			return "/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == EnvClaudeBin {
				return v
			}
			return ""
		}
	}
	for _, c := range []struct {
		env, want string
		err       bool
	}{
		{"", "/bin/claude", false},
		{bin, bin, false},
		{"claude-beta", "/bin/claude-beta", false},
		{"missing", "", true},
		{filepath.Join(dir, "nope"), "", true},
		{dir, "", true},
	} {
		got, err := resolveClaude(env(c.env), look)
		if (err != nil) != c.err || got != c.want {
			t.Errorf("env %q: got %q, %v", c.env, got, err)
		}
		if err != nil && !errors.Is(err, ErrClaudeNotFound) {
			t.Errorf("env %q: error %v is not ErrClaudeNotFound", c.env, err)
		}
	}
}

func TestExecClaudeArgvUnchanged(t *testing.T) {
	bin, _ := fakeClaude(t)
	var gotBin string
	var gotArgv, gotEnv []string
	old := execve
	execve = func(b string, argv, env []string) error {
		gotBin, gotArgv, gotEnv = b, argv, env
		return nil
	}
	t.Cleanup(func() { execve = old })

	argv := []string{"-p", "--model", "x", "hello world", "--", "-z"}
	if err := ExecClaude(argv); err != nil {
		t.Fatal(err)
	}
	if gotBin != bin || !slices.Equal(gotArgv, append([]string{bin}, argv...)) {
		t.Errorf("exec %q %q", gotBin, gotArgv)
	}
	if !slices.Equal(gotEnv, os.Environ()) {
		t.Error("environment changed")
	}
}

func TestExecClaudeMissing(t *testing.T) {
	t.Setenv(EnvClaudeBin, filepath.Join(t.TempDir(), "nope"))
	if err := ExecClaude(nil); !errors.Is(err, ErrClaudeNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// TestHelperDispatch is the child process of TestDispatchExecReal.
func TestHelperDispatch(t *testing.T) {
	if os.Getenv("MANTLE_CLI_HELPER") != "1" {
		t.Skip("helper process")
	}
	i := slices.Index(os.Args, "--")
	_, _, code := Dispatch(context.Background(), os.Args[i+1:], os.Stdout, os.Stderr)
	os.Exit(code)
}

// TestDispatchExecReal execs a fake claude for real and checks the argv and
// environment it received.
func TestDispatchExecReal(t *testing.T) {
	_, out := fakeClaude(t)
	for _, argv := range [][]string{
		{"mcp", "add", "--scope", "user", "x", "--", "npx", "y"},
		{"--model", "sonnet", "-p", "say hi"},
		{"--bg", "fix it"},
		{"doctor"},
	} {
		cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestHelperDispatch$", "--"}, argv...)...)
		cmd.Env = append(os.Environ(), "MANTLE_CLI_HELPER=1", "FAKE_OUT="+out, "MANTLE_TEST_MARK=kept")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%q: %v\n%s", argv, err, b)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Join(argv, "\n") + "\nMARK=kept\n"
		if string(b) != want {
			t.Errorf("%q: fake claude got\n%s\nwant\n%s", argv, b, want)
		}
	}
}

func TestDispatchModes(t *testing.T) {
	fakeClaude(t)
	ctx := context.Background()

	var stdout, stderr bytes.Buffer
	p, done, code := Dispatch(ctx, []string{"--model", "x", "hi"}, &stdout, &stderr)
	if done || code != 0 || p.Prompt != "hi" {
		t.Errorf("ui: done %v code %d prompt %q", done, code, p.Prompt)
	}

	stdout.Reset()
	_, done, code = Dispatch(ctx, []string{"--version"}, &stdout, &stderr)
	if !done || code != 0 || !strings.Contains(stdout.String(), "engine claude 9.9.9") {
		t.Errorf("version: %v %d %q", done, code, stdout.String())
	}

	stdout.Reset()
	_, done, code = Dispatch(ctx, []string{"-h"}, &stdout, &stderr)
	if !done || code != 0 || !strings.Contains(stdout.String(), "mantle options:") || !strings.Contains(stdout.String(), "Usage: fake claude") {
		t.Errorf("help: %v %d %q", done, code, stdout.String())
	}

	stderr.Reset()
	_, done, code = Dispatch(ctx, []string{"--output-format", "json"}, &stdout, &stderr)
	if !done || code != ExitUsage || !strings.Contains(stderr.String(), "only works with -p") {
		t.Errorf("error: %v %d %q", done, code, stderr.String())
	}

	stderr.Reset()
	_, done, code = Dispatch(ctx, []string{"versions"}, &stdout, &stderr)
	if !done || code != 2 || !strings.Contains(stderr.String(), "launcher") {
		t.Errorf("launcher: %v %d %q", done, code, stderr.String())
	}
}

func TestEngineVersion(t *testing.T) {
	fakeClaude(t)
	t.Setenv("CLAUDECODE", "1")
	out, err := runClaudeInfo(context.Background(), time.Minute, "--version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "cc=\n") {
		t.Errorf("CLAUDECODE reached claude: %q", out)
	}
	v, err := EngineVersion(context.Background())
	if err != nil || v != "9.9.9" {
		t.Errorf("version %q %v", v, err)
	}
}

func TestEngineVersionTimeout(t *testing.T) {
	fakeClaude(t)
	t.Setenv("FAKE_SLEEP", "5")
	old := VersionTimeout
	VersionTimeout = 200 * time.Millisecond
	t.Cleanup(func() { VersionTimeout = old })
	start := time.Now()
	_, err := EngineVersion(context.Background())
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
	if line := VersionLine(context.Background()); !strings.Contains(line, "engine claude unavailable") {
		t.Errorf("version line %q", line)
	}
}

func TestHelpWhenClaudeFails(t *testing.T) {
	fakeClaude(t)
	t.Setenv("FAKE_FAIL", "1")
	var b bytes.Buffer
	if err := WriteHelp(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "mantle options:") || !strings.Contains(b.String(), "unavailable: claude --help: boom") {
		t.Errorf("help %q", b.String())
	}
}

func TestRunClaudeInfoRefusesOtherArgs(t *testing.T) {
	fakeClaude(t)
	if _, err := runClaudeInfo(context.Background(), time.Second, "hello"); err == nil {
		t.Fatal("ran claude with a free-text argument")
	}
}

func TestBuild(t *testing.T) {
	v, id := Build()
	if v == "" || id == "" {
		t.Errorf("build %q %q", v, id)
	}
	old, oldID := Version, BuildID
	Version, BuildID = "1.2.3", "b42"
	t.Cleanup(func() { Version, BuildID = old, oldID })
	if v, id := Build(); v != "1.2.3" || id != "b42" {
		t.Errorf("ldflags build %q %q", v, id)
	}
}
