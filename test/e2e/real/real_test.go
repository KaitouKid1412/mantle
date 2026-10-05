// Package real is plan 12's opt-in end-to-end check (B4): mantle with the real claude
// engine and the real API. It costs a few cents, so it never runs by default:
//
//	MANTLE_E2E_REAL=1 ANTHROPIC_API_KEY=sk-... make e2e-real
//
// It uses its own isolated CLAUDE_CONFIG_DIR and HOME (never ~/.claude), the cheapest
// model, and prints what the session cost, as mantle's /cost reports it.
package real

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/test/parity"
)

const model = "claude-haiku-4-5"

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}

// TestRealEngine drives one mantle session through an answer, a tool approval, a
// resume and /compact, then prints /cost.
func TestRealEngine(t *testing.T) {
	if os.Getenv("MANTLE_E2E_REAL") == "" {
		t.Skip("real end-to-end: set MANTLE_E2E_REAL=1 and ANTHROPIC_API_KEY (costs a few cents)")
	}
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Fatal("MANTLE_E2E_REAL=1 needs ANTHROPIC_API_KEY (the run uses an isolated config, not ~/.claude)")
	}
	url := os.Getenv("ANTHROPIC_BASE_URL")
	if url == "" {
		url = "https://api.anthropic.com"
	}
	bin, err := parity.BuildMantleUI(filepath.Join(moduleRoot(t), "test", "parity", "out", ".bin"))
	if err != nil {
		t.Fatal(err)
	}
	sc, err := parity.ParseScenario(`name: real
size: 110x34
args: --model ` + model + ` --permission-mode default
prompt: Reply with exactly the word PARITYOK and nothing else.
---
ready 60s
wait_for 120s PARITYOK
wait_gone 120s esc to interrupt
checkpoint answer
type Use the Bash tool to run: echo parity-tool-ok   Then tell me what it printed.
keys enter
wait_for 120s Do you want to proceed?
checkpoint permission
keys enter
wait_for 120s parity-tool-ok
wait_gone 120s esc to interrupt
settle 2s
checkpoint tool
restart -c
ready 60s
wait_for 60s PARITYOK
type What single word did you reply with first? Answer with just that word.
keys enter
wait_gone 120s esc to interrupt
settle 2s
checkpoint resumed
type /compact
keys enter
wait_for 180s Compacted
settle 2s
checkpoint compacted
type /cost
keys enter
settle 3s
checkpoint cost
keys esc
settle
`)
	if err != nil {
		t.Fatal(err)
	}
	res := parity.Run(context.Background(), parity.MantleTarget{Bin: bin}, sc, parity.RunOptions{
		Timeout: 15 * time.Minute,
		RealAPI: &parity.Endpoint{URL: url, Key: key},
		Logf:    t.Logf,
	})
	n := parity.DefaultNormalizer()
	for _, cp := range res.Checkpoints {
		t.Logf("── %s ──\n%s", cp.Name, strings.Join(n.Frame(cp.Frame, res.Workspace), "\n"))
	}
	if cost, ok := res.Checkpoint("cost"); ok {
		for _, l := range cost.Frame.Screen {
			if strings.Contains(l, "$") {
				t.Logf("cost: %s", strings.TrimSpace(l))
			}
		}
	}
	if res.Err != nil {
		t.Fatalf("real run: %v\n%s", res.Err, strings.Join(res.Final.Screen, "\n"))
	}
	if r, ok := res.Checkpoint("resumed"); !ok || !strings.Contains(strings.Join(r.Frame.Screen, "\n"), "PARITYOK") {
		t.Error("the resumed session lost the first answer")
	}
}
