package launcher

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestClassifyArgs(t *testing.T) {
	cases := []struct {
		argv []string
		kind Kind
		args []string
		safe bool
	}{
		{nil, KindUI, nil, false},
		{[]string{"fix the bug"}, KindUI, []string{"fix the bug"}, false},
		{[]string{"--model", "opus", "hello"}, KindUI, []string{"--model", "opus", "hello"}, false},
		{[]string{"versions"}, KindVersions, []string{}, false},
		{[]string{"versions", "--json"}, KindVersions, []string{"--json"}, false},
		{[]string{"rollback", "b1"}, KindRollback, []string{"b1"}, false},
		{[]string{"doctor"}, KindDoctor, []string{}, false},
		{[]string{"--safe"}, KindUI, []string{}, true},
		{[]string{"-c", "--safe", "x"}, KindUI, []string{"-c", "x"}, true},
		{[]string{"--", "--safe"}, KindUI, []string{"--", "--safe"}, false},
		{[]string{"-p", "hello"}, KindPassthrough, []string{"-p", "hello"}, false},
		{[]string{"hello", "--print"}, KindPassthrough, []string{"hello", "--print"}, false},
		{[]string{"--model", "x", "-p"}, KindPassthrough, []string{"--model", "x", "-p"}, false},
		{[]string{"-cp", "x"}, KindPassthrough, []string{"-cp", "x"}, false},
		{[]string{"-pc"}, KindPassthrough, []string{"-pc"}, false},
		{[]string{"-np"}, KindUI, []string{"-np"}, false}, // -n takes a value: name "p"
		{[]string{"-rp"}, KindUI, []string{"-rp"}, false},
		{[]string{"--", "-p"}, KindUI, []string{"--", "-p"}, false},
		{[]string{"--safe", "-p", "x"}, KindPassthrough, []string{"-p", "x"}, true},
		{[]string{"mcp", "list"}, KindPassthrough, []string{"mcp", "list"}, false},
		{[]string{"plugins"}, KindPassthrough, []string{"plugins"}, false},
		{[]string{"update"}, KindPassthrough, []string{"update"}, false},
		{[]string{"--safe", "auth", "login"}, KindPassthrough, []string{"auth", "login"}, true},
		{[]string{"mcpx"}, KindUI, []string{"mcpx"}, false},
		{[]string{"explain", "mcp"}, KindUI, []string{"explain", "mcp"}, false},
		{[]string{"--help"}, KindUI, []string{"--help"}, false},
	}
	for _, c := range cases {
		d := ClassifyArgs(c.argv)
		if d.Kind != c.kind || !slices.Equal(d.Args, c.args) || d.Safe != c.safe {
			t.Errorf("ClassifyArgs(%q) = %v %q safe=%v, want %v %q safe=%v", c.argv, d.Kind, d.Args, d.Safe, c.kind, c.args, c.safe)
		}
	}
}

func TestClassifyKeepsArgvIdentity(t *testing.T) {
	argv := []string{"-p", "x"}
	d := ClassifyArgs(argv)
	if &d.Args[0] != &argv[0] {
		t.Error("argv without --safe should be forwarded untouched")
	}
}

// TestPassthroughExecsClaude runs the launcher (this test binary in Main
// mode) and checks claude receives argv exactly.
func TestPassthroughExecsClaude(t *testing.T) {
	exe, _ := os.Executable()
	dir := t.TempDir()
	rec := filepath.Join(dir, "argv")
	fake := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > " + rec + "\necho \"MANTLE_TEST_MARK=$MANTLE_TEST_MARK\" >> " + rec + "\nexit 7\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		argv []string
		want []string
	}{
		{[]string{"-p", "two words", "--model", "x"}, []string{"-p", "two words", "--model", "x"}},
		{[]string{"mcp", "add", "--", "srv", "-p"}, []string{"mcp", "add", "--", "srv", "-p"}},
		{[]string{"--safe", "plugin", "list"}, []string{"plugin", "list"}},
	}
	for _, c := range cases {
		os.Remove(rec)
		cmd := exec.Command(exe, c.argv...)
		cmd.Env = append(os.Environ(), envFakeMain+"=1", EnvClaudeBin+"="+fake, EnvHome+"="+filepath.Join(dir, "home"), "MANTLE_TEST_MARK=kept")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 7 {
			t.Fatalf("%q: err = %v (want exit 7 from fake claude), stderr: %s", c.argv, err, stderr.String())
		}
		data, _ := os.ReadFile(rec)
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		want := append(slices.Clone(c.want), "MANTLE_TEST_MARK=kept")
		if !slices.Equal(lines, want) {
			t.Errorf("%q: claude got %q, want %q", c.argv, lines, want)
		}
	}
}

func TestPassthroughMissingClaude(t *testing.T) {
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "-p", "x")
	cmd.Env = append(os.Environ(), envFakeMain+"=1", EnvClaudeBin+"="+filepath.Join(t.TempDir(), "nope"))
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 127 {
		t.Fatalf("err = %v, out = %s", err, out)
	}
}

// TestStdlibOnly enforces that the launcher imports nothing outside the
// standard library.
func TestStdlibOnly(t *testing.T) {
	gobin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(gobin); err != nil {
		t.Skip("go tool not available")
	}
	out, err := exec.Command(gobin, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}",
		"github.com/KaitouKid1412/mantle/cmd/mantle").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	allowed := []string{
		"github.com/KaitouKid1412/mantle/cmd/mantle",
		"github.com/KaitouKid1412/mantle/internal/launcher",
	}
	for _, p := range strings.Fields(string(out)) {
		if !slices.Contains(allowed, p) {
			t.Errorf("the launcher depends on %s; it must import only the standard library", p)
		}
	}
}
