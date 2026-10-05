package input

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// TestNativeCommandRunsMidTurn: native commands (/model, /effort, /fast) run at once
// while a turn runs instead of queueing, so their control requests reach the engine
// mid-turn and apply from its next request (PARITY TC-13).
func TestNativeCommandRunsMidTurn(t *testing.T) {
	r := newRig(t, nil)
	ran := ""
	r.c.CommandList = append(r.c.CommandList, ext.Command{Name: "native", Source: ext.SourceBuiltin,
		Run: func(_ ext.Ctx, args string) tea.Cmd { ran = args; return nil }})
	r.keys("'first'", "enter")
	if !r.s.busy {
		t.Fatal("a sent prompt makes the engine busy")
	}
	r.keys("'/native opus'", "enter")
	if ran != "opus" || len(r.eng.prompts()) != 1 || len(r.s.queue) != 0 {
		t.Fatalf("ran %q, prompts %d, queued %d", ran, len(r.eng.prompts()), len(r.s.queue))
	}
}
