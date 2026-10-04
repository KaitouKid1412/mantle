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
	sc := mustParse(t, "env: A=1\nenv: B=2\n---\nrestart --resume x\n")
	if !slices.Equal(sc.Env, []string{"A=1", "B=2"}) || !slices.Equal(sc.Steps[0].Args, []string{"--resume", "x"}) {
		t.Errorf("parsed = %+v", sc)
	}
}
