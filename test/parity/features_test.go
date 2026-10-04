package parity

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// argTarget echoes its arguments and an env var, then waits for input like echoTarget.
func argTarget(t *testing.T) CommandTarget {
	t.Helper()
	script := filepath.Join(t.TempDir(), "args.sh")
	body := `#!/bin/sh
printf 'args=[%s] x=%s\n' "$*" "$PARITY_X"
printf 'READY> '
while IFS= read -r line; do printf 'you said: %s\nREADY> ' "$line"; done
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return CommandTarget{TargetName: "args", Argv: []string{"/bin/sh", script}, ReadyText: "READY>"}
}

func TestEnvSettingsRestart(t *testing.T) {
	sc := mustParse(t, `name: feat
size: 60x12
args: -a
env: PARITY_X=42
settings: {"statusLine": {"type": "command", "command": "true"}}
---
ready
checkpoint first
restart -c
wait_for args=[-a -c]
checkpoint second
`)
	res := Run(context.Background(), argTarget(t), sc, RunOptions{KeepWorkspace: true, Timeout: 30 * time.Second})
	defer os.RemoveAll(res.Workspace.Root)
	if res.Err != nil {
		t.Fatalf("run: %v\n%s", res.Err, strings.Join(res.Final.Screen, "\n"))
	}
	first, _ := res.Checkpoint("first")
	if !slices.Contains(first.Frame.Screen, "args=[-a] x=42") {
		t.Errorf("first = %q", first.Frame.Screen)
	}
	second, _ := res.Checkpoint("second")
	if !slices.Contains(second.Frame.Screen, "args=[-a -c] x=42") {
		t.Errorf("after restart = %q", second.Frame.Screen)
	}
	data, err := os.ReadFile(filepath.Join(res.Workspace.ConfigDir, "settings.json"))
	if err != nil || !strings.Contains(string(data), "statusLine") {
		t.Errorf("settings = %q %v", data, err)
	}
}

func TestScriptPlaceholders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	if err := os.WriteFile(path, []byte(`{"turns":[{"reply":[{"type":"text","text":"edit {{work}}/main.go in {{config}}"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := loadScript(path, Workspace{WorkDir: "/w", ConfigDir: "/c"})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Turns[0].Reply[0].Text; got != "edit /w/main.go in /c" {
		t.Errorf("text = %q", got)
	}
}

func TestParseNewHeaders(t *testing.T) {
	if _, err := ParseScenario("env: NOEQUALS\n---\nready\n"); err == nil {
		t.Error("env without =")
	}
	sc := mustParse(t, "env: A=1\nenv: B=2\nparity: TR-01, PD-03 SE-02\n---\nrestart --resume x\n")
	if !slices.Equal(sc.Parity, []string{"TR-01", "PD-03", "SE-02"}) {
		t.Errorf("parity = %q", sc.Parity)
	}
	if !slices.Equal(sc.Env, []string{"A=1", "B=2"}) || !slices.Equal(sc.Steps[0].Args, []string{"--resume", "x"}) {
		t.Errorf("parsed = %+v", sc)
	}
}

func TestTargetOnlySteps(t *testing.T) {
	sc := mustParse(t, "---\nready\n@mantle keys enter\ncheckpoint c\n")
	if st := sc.Steps[1]; st.Only != "mantle" || st.Kind != StepKeys || st.Src != "keys enter" {
		t.Errorf("step = %+v", st)
	}
	for _, bad := range []string{"---\n@mantle checkpoint c\n", "---\n@mantle\n", "---\n@ keys enter\n"} {
		if _, err := ParseScenario(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	// The shell target skips steps meant for another target.
	sc = mustParse(t, "---\nready\n@other type never\n@args type \"mine\\r\"\nwait_for you said: mine\ncheckpoint c\n")
	res := Run(context.Background(), argTarget(t), sc, RunOptions{Timeout: 30 * time.Second})
	if res.Err != nil {
		t.Fatalf("run: %v", res.Err)
	}
	if c, _ := res.Checkpoint("c"); strings.Contains(strings.Join(c.Frame.Screen, "\n"), "never") {
		t.Errorf("ran another target's step: %q", c.Frame.Screen)
	}
}

func TestSplitArgs(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"--permission-mode default", []string{"--permission-mode", "default"}},
		{`--x "a prompt with \"quotes\"" 'it''s'`, []string{"--x", `a prompt with "quotes"`, "its"}},
		{`pre"fix"  ''`, []string{"prefix", ""}},
	} {
		got, err := splitArgs(tt.in)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("splitArgs(%q) = %q, %v", tt.in, got, err)
		}
	}
	for _, bad := range []string{`"open`, `'open`} {
		if _, err := splitArgs(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestScenarioSettingsPinRenderer(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"", `"tui": "default"`},
		{`{"statusLine":{"type":"command"}}`, `"tui": "default"`},
		{`{"tui":"fullscreen"}`, `"tui": "fullscreen"`},
	} {
		got, err := scenarioSettings(tt.in)
		if err != nil || !strings.Contains(string(got), tt.want) {
			t.Errorf("scenarioSettings(%q) = %s, %v", tt.in, got, err)
		}
	}
	if _, err := scenarioSettings("{not json"); err == nil {
		t.Error("bad JSON must fail")
	}
}
