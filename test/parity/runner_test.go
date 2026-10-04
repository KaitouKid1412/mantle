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

// echoTarget is a tiny line-echoing program: a prompt, a reply per line, "big" prints
// 60 numbered lines (to fill the scrollback), "size" prints the terminal size.
func echoTarget(t *testing.T) CommandTarget {
	t.Helper()
	script := filepath.Join(t.TempDir(), "echo.sh")
	body := `#!/bin/sh
printf 'READY> '
while IFS= read -r line; do
  case "$line" in
    big) i=1; while [ $i -le 60 ]; do printf 'row %02d\n' $i; i=$((i+1)); done ;;
    size) stty size ;;
    *) printf 'you said: %s\n' "$line" ;;
  esac
  printf 'READY> '
done
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return CommandTarget{TargetName: "echo", Argv: []string{"/bin/sh", script}, ReadyText: "READY>"}
}

func mustParse(t *testing.T, text string) *Scenario {
	t.Helper()
	sc, err := ParseScenario(text)
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

const echoScenario = `name: echo
size: 60x12
---
ready
type hello parity
keys enter
wait_for you said: hello parity
checkpoint greeted
type big
keys enter
wait_for row 60
checkpoint big
resize 80x10
type size
keys enter
wait_for 10 80
checkpoint resized
`

func TestRunnerCapturesCheckpoints(t *testing.T) {
	res := Run(context.Background(), echoTarget(t), mustParse(t, echoScenario), RunOptions{Timeout: 30 * time.Second})
	if res.Err != nil {
		t.Fatalf("run failed: %v\n%s", res.Err, strings.Join(res.Final.Screen, "\n"))
	}
	if len(res.Checkpoints) != 3 {
		t.Fatalf("checkpoints = %d", len(res.Checkpoints))
	}
	g, _ := res.Checkpoint("greeted")
	if !slices.Contains(g.Frame.Screen, "you said: hello parity") {
		t.Errorf("greeted screen = %q", g.Frame.Screen)
	}
	b, _ := res.Checkpoint("big")
	if len(b.Frame.Screen) != 12 || !slices.Contains(b.Frame.Scrollback, "row 01") {
		t.Errorf("big: screen %d lines, scrollback %q", len(b.Frame.Screen), b.Frame.Scrollback)
	}
	r, _ := res.Checkpoint("resized")
	if !slices.Contains(r.Frame.Screen, "10 80") || len(r.Frame.Screen) != 10 {
		t.Errorf("resized = %q", r.Frame.Screen)
	}
	if res.Width != 80 || res.Height != 10 {
		t.Errorf("size = %dx%d", res.Width, res.Height)
	}
	if _, err := os.Stat(res.Workspace.Root); !os.IsNotExist(err) {
		t.Error("workspace should be removed")
	}
}

func TestRunnerFailingStep(t *testing.T) {
	sc := mustParse(t, "size: 40x8\n---\nready\ncheckpoint first\nwait_for 300ms never shows\ncheckpoint never\n")
	res := Run(context.Background(), echoTarget(t), sc, RunOptions{})
	if res.Err == nil || res.FailedLine != 5 {
		t.Fatalf("err = %v line %d", res.Err, res.FailedLine)
	}
	if len(res.Checkpoints) != 1 || !strings.Contains(strings.Join(res.Final.Screen, "\n"), "READY>") {
		t.Errorf("partial result: %d checkpoints, final %q", len(res.Checkpoints), res.Final.Screen)
	}
}

func TestRunnerPasteAndFixtures(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "fx", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fx", "src", "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	scn := filepath.Join(dir, "p.scn")
	text := "files: fx\n---\nready\npaste \"pasted words\"\nkeys enter\nwait_for you said: pasted words\ncheckpoint pasted\n"
	if err := os.WriteFile(scn, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	sc, err := LoadScenario(scn)
	if err != nil {
		t.Fatal(err)
	}
	var sawFixture bool
	tg := echoTarget(t)
	res := Run(context.Background(), tg, sc, RunOptions{KeepWorkspace: true})
	defer os.RemoveAll(res.Workspace.Root)
	if res.Err != nil {
		t.Fatalf("run: %v", res.Err)
	}
	if _, err := os.Stat(filepath.Join(res.Workspace.WorkDir, "src", "a.txt")); err == nil {
		sawFixture = true
	}
	if !sawFixture {
		t.Error("fixtures not copied")
	}
	if _, err := os.Stat(filepath.Join(res.Workspace.ConfigDir, ".claude.json")); err != nil {
		t.Error("config not seeded")
	}
}

// TestRunnerDeterministic is the harness self-test: the same scenario twice against
// the same target gives identical normalized frames.
func TestRunnerDeterministic(t *testing.T) {
	sc := mustParse(t, echoScenario)
	a := Run(context.Background(), echoTarget(t), sc, RunOptions{})
	b := Run(context.Background(), echoTarget(t), sc, RunOptions{})
	if a.Err != nil || b.Err != nil {
		t.Fatalf("errors: %v / %v", a.Err, b.Err)
	}
	n := DefaultNormalizer()
	for _, ca := range a.Checkpoints {
		cb, ok := b.Checkpoint(ca.Name)
		if !ok {
			t.Fatalf("missing %s", ca.Name)
		}
		fa, fb := n.Frame(ca.Frame, a.Workspace), n.Frame(cb.Frame, b.Workspace)
		if strings.Join(fa, "\n") != strings.Join(fb, "\n") {
			t.Errorf("%s differs:\n%s\n---\n%s", ca.Name, strings.Join(fa, "\n"), strings.Join(fb, "\n"))
		}
	}
}
