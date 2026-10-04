package parity

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestClaudeScenariosDeterministic runs every scenario twice against the installed
// interactive claude (with fakeapi and an isolated config: offline and free) and
// requires identical normalized frames. Opt in with MANTLE_PARITY_CLAUDE=1.
func TestClaudeScenariosDeterministic(t *testing.T) {
	if os.Getenv("MANTLE_PARITY_CLAUDE") != "1" {
		t.Skip("set MANTLE_PARITY_CLAUDE=1 to drive the installed claude")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not on PATH")
	}
	scs, err := LoadScenarios("scenarios")
	if err != nil {
		t.Fatal(err)
	}
	n := DefaultNormalizer()
	for _, sc := range scs {
		t.Run(sc.Name, func(t *testing.T) {
			var runs [2]*Result
			for i := range runs {
				runs[i] = Run(context.Background(), ClaudeTarget{}, sc, RunOptions{Timeout: 90 * time.Second})
				if runs[i].Err != nil {
					t.Fatalf("run %d: %v\n%s", i+1, runs[i].Err, strings.Join(runs[i].Final.Screen, "\n"))
				}
			}
			for _, d := range Compare(n, sc, runs[0], runs[1], nil) {
				if d.Status != StatusSame {
					t.Errorf("%s: %s\n%s", d.Checkpoint, d.Status, d.Unified)
				}
			}
		})
	}
}
