package transcript

import (
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestWaitingRowWhilePromptOpen(t *testing.T) {
	g := newRig(t, 61)
	g.send(`{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"rm -rf x"}}]}}`)
	g.deliver(ext.PermissionMsg{EngineID: ext.MainEngine, RequestID: "r1", Req: proto.CanUseTool{ToolName: "Bash", ToolUseID: "t1"}})
	if live := join(g.live(10)); !strings.Contains(live, "Bash(rm -rf x)\n  ⎿  Waiting…") {
		t.Fatalf("live = %q", live)
	}
	// Answered: the engine runs again.
	g.send(`{"type":"system","subtype":"session_state_changed","state":"running"}`)
	if live := join(g.live(10)); strings.Contains(live, "Waiting…") {
		t.Fatalf("still waiting after the answer: %q", live)
	}
	// A cancelled request clears its mark too.
	g.deliver(ext.PermissionMsg{EngineID: ext.MainEngine, RequestID: "r2", Req: proto.CanUseTool{ToolName: "Bash", ToolUseID: "t1"}})
	g.deliver(ext.ControlCancelMsg{EngineID: ext.MainEngine, RequestID: "r2"})
	if g.f.store.Waiting("t1") {
		t.Fatal("cancelled prompt still marked")
	}
}

func TestExitPlanModeRows(t *testing.T) {
	g := newRig(t, 61)
	g.send(`{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"tool_use","id":"p1","name":"ExitPlanMode","input":{"plan":"1. Test\n2. Fix"}}]}}`)
	g.deliver(ext.PermissionMsg{EngineID: ext.MainEngine, RequestID: "r1", Req: proto.CanUseTool{ToolName: "ExitPlanMode", ToolUseID: "p1"}})
	if live := join(g.live(10)); !strings.Contains(live, "Plan ready for review\n  ⎿  Waiting…") || strings.Contains(live, "1. Test") {
		t.Fatalf("live = %q", live)
	}
	g.send(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"p1","content":"approved"}]},"tool_use_result":{"plan":null,"filePath":"/w/plans/p.md"}}`)
	got := join(g.printed())
	if !strings.Contains(got, "⏺ Exited plan mode") || strings.Contains(got, "Waiting") || strings.Contains(got, "1. Test") {
		t.Fatalf("printed = %q", got)
	}
	// The plan and its file show at full detail.
	it := g.f.store.Get("p1")
	full := join(g.f.renderExitPlan(ext.RenderCtx{Width: 60, Mode: ext.FullTranscript}, it).Lines)
	if !strings.Contains(full, "1. Test") || !strings.Contains(full, "/w/plans/p.md") {
		t.Fatalf("full = %q", full)
	}
}
