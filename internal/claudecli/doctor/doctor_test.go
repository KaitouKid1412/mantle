package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/claudecli"
)

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// fakeClaudeBin writes a claude stand-in that prints version.
func fakeClaudeBin(t *testing.T, dir, version string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fake needs a unix shell")
	}
	p := filepath.Join(dir, "claude")
	write(t, p, "#!/bin/sh\necho '"+version+" (Claude Code)'\n", 0o755)
	return p
}

func testEnv(t *testing.T) (Env, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd := filepath.Join(home, "proj")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	env := Env{
		Home: home, ConfigDir: filepath.Join(home, ".claude"), MantleDir: filepath.Join(home, ".mantle"),
		CWD: cwd, ManagedDir: filepath.Join(root, "managed"),
		Getenv:   func(string) string { return "" },
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Claude:   &claudecli.Runner{Bin: fakeClaudeBin(t, root, "2.1.288")},
		DiskFree: func(string) (uint64, error) { return 50 << 30, nil },
	}
	return env, root
}

func find(checks []Check, id string) Check {
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	return Check{}
}

func TestRunHealthyMachine(t *testing.T) {
	env, root := testEnv(t)
	env.LookPath = func(name string) (string, error) { return "/usr/local/go/bin/" + name, nil }
	env.GoVersion = func(context.Context, string) (string, error) { return "go1.27.1", nil }
	env.Getenv = func(k string) string {
		return map[string]string{"TERM_PROGRAM": "ghostty", "TERM": "xterm-ghostty"}[k]
	}
	write(t, filepath.Join(env.ConfigDir, "settings.json"), `{"model": "opus"}`, 0o644)
	write(t, filepath.Join(env.Home, ".claude.json"), `{"projects": {"`+env.CWD+`": {"hasTrustDialogAccepted": true}}}`, 0o644)
	build := filepath.Join(env.MantleDir, "versions", "b1")
	write(t, filepath.Join(build, "mantle-ui"), "#!/bin/sh\n", 0o755)
	if err := os.Symlink(build, filepath.Join(env.MantleDir, "current")); err != nil {
		t.Fatal(err)
	}
	_ = root

	checks := Run(context.Background(), env)
	if len(checks) != 6 {
		t.Fatalf("got %d checks", len(checks))
	}
	for _, c := range checks {
		if c.Status != OK {
			t.Errorf("%s: %v %s (%+v)", c.ID, c.Status, c.Detail, c.Items)
		}
	}
	if Worst(checks) != OK {
		t.Errorf("worst = %v", Worst(checks))
	}
	if d := find(checks, "claude").Detail; !strings.HasPrefix(d, "2.1.288") {
		t.Errorf("claude detail = %q", d)
	}
}

func TestCheckClaude(t *testing.T) {
	env, root := testEnv(t)
	ctx := context.Background()

	env.Claude = &claudecli.Runner{Bin: fakeClaudeBin(t, filepath.Join(root, "old"), "2.1.200")}
	if c := CheckClaude(ctx, env); c.Status != Fail || !strings.Contains(c.Detail, "older") {
		t.Errorf("old claude = %+v", c)
	}

	env.Claude = &claudecli.Runner{Bin: filepath.Join(root, "missing", "claude")}
	if c := CheckClaude(ctx, env); c.Status != Fail {
		t.Errorf("missing binary = %+v", c)
	}

	env.Claude = &claudecli.Runner{Bin: fakeClaudeBin(t, filepath.Join(root, "new"), "2.1.290")}
	state := filepath.Join(env.MantleDir, "state", "engines.json")
	cases := []struct {
		json string
		want Status
	}{
		{`{"passed": ["2.1.290"]}`, OK},
		{`{"versions": {"2.1.290": {"status": "pass"}}}`, OK},
		{`{"verified": {"2.1.288": {}}}`, Warn},                            // not probed yet
		{`{"failed": ["2.1.290"]}`, Fail},                                  // failed the probe
		{`{"passed": ["2.1.290"], "pinned": "/x/versions/2.1.288"}`, Warn}, // pinned
		{`not json`, Warn},
	}
	for _, c := range cases {
		write(t, state, c.json, 0o644)
		if got := CheckClaude(ctx, env); got.Status != c.want {
			t.Errorf("state %s: %v %s %+v", c.json, got.Status, got.Detail, got.Items)
		}
	}
}

func TestCheckGo(t *testing.T) {
	env, _ := testEnv(t)
	ctx := context.Background()
	if c := CheckGo(ctx, env); c.Status != Warn || !strings.Contains(c.Detail, "not found") {
		t.Errorf("no go = %+v", c)
	}
	env.LookPath = func(string) (string, error) { return "/go/bin/go", nil }
	env.GoVersion = func(context.Context, string) (string, error) { return "go1.25.3", nil }
	if c := CheckGo(ctx, env); c.Status != Warn || !strings.Contains(c.Detail, "older") {
		t.Errorf("old go = %+v", c)
	}
	env.GoVersion = func(context.Context, string) (string, error) { return "go1.28rc1", nil }
	if c := CheckGo(ctx, env); c.Status != OK {
		t.Errorf("new go = %+v", c)
	}
	env.GoVersion = func(context.Context, string) (string, error) { return "", errors.New("boom") }
	if c := CheckGo(ctx, env); c.Status != Warn {
		t.Errorf("broken go = %+v", c)
	}
}

func TestCheckSettings(t *testing.T) {
	env, _ := testEnv(t)
	ctx := context.Background()
	if c := CheckSettings(ctx, env); c.Status != OK || c.Detail != "no settings files" {
		t.Errorf("none = %+v", c)
	}
	write(t, filepath.Join(env.ConfigDir, "settings.json"), `{"hooks": {}}`, 0o644)
	write(t, filepath.Join(env.CWD, ".claude", "settings.local.json"), "{\n  \"model\": \"opus\",\n}\n", 0o644)
	write(t, filepath.Join(env.ConfigDir, "keybindings.json"), `[]`, 0o644)
	write(t, filepath.Join(env.MantleDir, "settings.json"), `{"selfmod": {}}`, 0o644)
	write(t, filepath.Join(env.ManagedDir, "managed-settings.json"), `{}`, 0o644)
	c := CheckSettings(ctx, env)
	if c.Status != Warn || len(c.Items) != 5 || !strings.Contains(c.Detail, "2 of 5") {
		t.Fatalf("settings = %+v", c)
	}
	var local, keys Check
	for _, it := range c.Items {
		switch {
		case strings.HasSuffix(it.Detail, "settings.local.json") || strings.Contains(it.Detail, "settings.local.json:"):
			local = it
		case strings.Contains(it.Detail, "keybindings.json"):
			keys = it
		}
	}
	if local.Status != Warn || !strings.Contains(local.Detail, "line 3") || !strings.Contains(local.Fix, "ignores") {
		t.Errorf("local = %+v", local)
	}
	if keys.Status != Warn || !strings.Contains(keys.Detail, "object") {
		t.Errorf("keybindings = %+v", keys)
	}
}

func TestCheckMantleHome(t *testing.T) {
	env, _ := testEnv(t)
	ctx := context.Background()
	if c := CheckMantleHome(ctx, env); c.Status != Skip {
		t.Errorf("no ~/.mantle = %+v", c)
	}
	if err := os.MkdirAll(env.MantleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if c := CheckMantleHome(ctx, env); c.Status != Warn {
		t.Errorf("no current = %+v", c)
	}
	if err := os.Symlink(filepath.Join(env.MantleDir, "versions", "gone"), filepath.Join(env.MantleDir, "current")); err != nil {
		t.Fatal(err)
	}
	if c := CheckMantleHome(ctx, env); c.Status != Fail || !strings.Contains(c.Fix, "rollback") {
		t.Errorf("broken current = %+v", c)
	}
	good := filepath.Join(env.MantleDir, "versions", "good")
	write(t, filepath.Join(good, "mantle-ui"), "#!/bin/sh\n", 0o755)
	os.Remove(filepath.Join(env.MantleDir, "current"))
	if err := os.Symlink(good, filepath.Join(env.MantleDir, "current")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(good, filepath.Join(env.MantleDir, "last-good")); err != nil {
		t.Fatal(err)
	}
	if c := CheckMantleHome(ctx, env); c.Status != OK {
		t.Errorf("healthy = %+v", c)
	}
	env.DiskFree = func(string) (uint64, error) { return 500 << 20, nil }
	if c := CheckMantleHome(ctx, env); c.Status != Warn {
		t.Errorf("low disk = %+v", c)
	}
	env.DiskFree = func(string) (uint64, error) { return 10 << 20, nil }
	if c := CheckMantleHome(ctx, env); c.Status != Fail {
		t.Errorf("very low disk = %+v", c)
	}
}

func TestCheckTrust(t *testing.T) {
	env, _ := testEnv(t)
	ctx := context.Background()
	if c := CheckTrust(ctx, env); c.Status != Warn {
		t.Errorf("untrusted = %+v", c)
	}
	// A trusted parent counts.
	write(t, filepath.Join(env.Home, ".claude.json"), `{"projects": {"`+env.Home+`": {"hasTrustDialogAccepted": true}}}`, 0o644)
	if c := CheckTrust(ctx, env); c.Status != OK || !strings.Contains(c.Detail, "Claude Code") {
		t.Errorf("parent trusted = %+v", c)
	}
	os.Remove(filepath.Join(env.Home, ".claude.json"))
	for _, doc := range []string{
		`["` + env.CWD + `"]`,
		`{"` + env.CWD + `": true}`,
		`{"trusted": ["` + env.CWD + `"]}`,
		`{"projects": {"` + env.CWD + `": {"accepted": true}}}`,
	} {
		write(t, filepath.Join(env.MantleDir, "trust.json"), doc, 0o644)
		if c := CheckTrust(ctx, env); c.Status != OK || !strings.Contains(c.Detail, "mantle") {
			t.Errorf("mantle trust %s = %+v", doc, c)
		}
	}
	write(t, filepath.Join(env.MantleDir, "trust.json"), `{"`+env.CWD+`": false}`, 0o644)
	if c := CheckTrust(ctx, env); c.Status != Warn {
		t.Errorf("explicitly untrusted = %+v", c)
	}
	// CLAUDE_CONFIG_DIR moves .claude.json.
	cfg := filepath.Join(env.Home, "alt")
	write(t, filepath.Join(cfg, ".claude.json"), `{"projects": {"`+env.CWD+`": {"hasTrustDialogAccepted": true}}}`, 0o644)
	env.Getenv = func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return cfg
		}
		return ""
	}
	if c := CheckTrust(ctx, env); c.Status != OK {
		t.Errorf("CLAUDE_CONFIG_DIR trust = %+v", c)
	}
}

func TestCheckTerminal(t *testing.T) {
	env, _ := testEnv(t)
	ctx := context.Background()
	set := func(vars map[string]string) { env.Getenv = func(k string) string { return vars[k] } }
	status := func(c Check, title string) Status {
		for _, it := range c.Items {
			if it.Title == title {
				return it.Status
			}
		}
		t.Fatalf("no item %q", title)
		return Skip
	}

	set(map[string]string{"TERM_PROGRAM": "Apple_Terminal", "TERM": "xterm-256color"})
	c := CheckTerminal(ctx, env)
	if c.Status != Warn || status(c, "Kitty keyboard protocol") != Warn || status(c, "Truecolor") != Warn {
		t.Errorf("Terminal.app = %+v", c)
	}

	set(map[string]string{"TERM": "xterm-kitty", "KITTY_WINDOW_ID": "1"})
	if c := CheckTerminal(ctx, env); c.Status != OK {
		t.Errorf("kitty = %+v", c)
	}

	set(map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.5.4"})
	if c := CheckTerminal(ctx, env); status(c, "Kitty keyboard protocol") != OK {
		t.Errorf("iTerm2 3.5 = %+v", c)
	}
	set(map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.4.0"})
	if c := CheckTerminal(ctx, env); status(c, "Kitty keyboard protocol") != Skip {
		t.Errorf("iTerm2 3.4 = %+v", c)
	}

	set(map[string]string{"TERM_PROGRAM": "ghostty", "TMUX": "/tmp/tmux,1,0"})
	c = CheckTerminal(ctx, env)
	if status(c, "Clipboard (OSC 52)") != Skip || !strings.HasPrefix(c.Detail, "tmux") {
		t.Errorf("tmux = %+v", c)
	}

	// Detected capabilities override the guess.
	no, yesv := false, true
	set(map[string]string{"TERM_PROGRAM": "ghostty"})
	env.Term = TermCaps{KittyKeyboard: &no, Truecolor: &yesv}
	if c := CheckTerminal(ctx, env); status(c, "Kitty keyboard protocol") != Warn {
		t.Errorf("detected override = %+v", c)
	}

	env.Term = TermCaps{}
	set(map[string]string{})
	if c := CheckTerminal(ctx, env); c.Status != OK || c.Detail != "unknown terminal" {
		t.Errorf("unknown terminal = %+v", c)
	}
}

func TestGuardedRecoversPanics(t *testing.T) {
	c := guarded(context.Background(), Env{}, "x", "X", func(context.Context, Env) Check { panic("boom") })
	if c.Status != Fail || !strings.Contains(c.Detail, "boom") || c.ID != "x" {
		t.Errorf("guarded = %+v", c)
	}
	if Status(9).String() != "skip" || Warn.String() != "warn" {
		t.Error("Status.String")
	}
	if humanBytes(1536) != "1.5 KiB" || humanBytes(10) != "10 B" {
		t.Errorf("humanBytes = %s %s", humanBytes(1536), humanBytes(10))
	}
}
