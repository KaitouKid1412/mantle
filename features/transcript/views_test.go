package transcript

import (
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

const focusTurn = `
{"type":"user","uuid":"u1","isReplay":true,"message":{"role":"user","content":"check it"}}
{"type":"assistant","uuid":"a0","message":{"id":"m","content":[{"type":"thinking","thinking":"hmm","signature":"s"}]}}
{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/x/a.go"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
{"type":"assistant","uuid":"a2","message":{"id":"m","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"a.go"}]},"tool_use_result":{"stdout":"a.go","stderr":""}}
{"type":"assistant","uuid":"a3","message":{"id":"m","content":[{"type":"text","text":"All good."}]}}
{"type":"result","subtype":"success","uuid":"r1","duration_ms":3000,"is_error":false,"num_turns":2,"total_cost_usd":0}
`

func TestFocusView(t *testing.T) {
	g := newRig(t, 61)
	g.c.SettingsV = exttest.NewSettings(map[string]any{"viewMode": "focus"})
	g.deliver(ext.SettingsMsg{})
	g.send(focusTurn)
	got := join(g.printed())
	for _, want := range []string{"> check it", "⏺ Used 2 tools (ctrl+o to see them)", "⏺ All good.", "for 3s"} {
		if !strings.Contains(got, want) {
			t.Errorf("focus view lacks %q:\n%s", want, got)
		}
	}
	for _, not := range []string{"Read(", "Bash(", "Thought"} {
		if strings.Contains(got, not) {
			t.Errorf("focus view shows %q:\n%s", not, got)
		}
	}
	if strings.Index(got, "Used 2 tools") > strings.Index(got, "All good.") {
		t.Errorf("summary after the answer:\n%s", got)
	}
}

func TestFocusLiveSummary(t *testing.T) {
	g := newRig(t, 61)
	g.engineViewFromInit("focus")
	g.send(`{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"sleep 9"}}]}}`)
	if live := join(g.live(10)); !strings.Contains(live, "Using 1 tool") || strings.Contains(live, "Bash(") {
		t.Fatalf("live = %q", live)
	}
}

func (g *rig) engineViewFromInit(mode string) {
	g.t.Helper()
	g.send(`{"type":"system","subtype":"init","uuid":"i","cwd":"/w","tools":[],"mcp_servers":[],"model":"m","permissionMode":"default","slash_commands":[],"claude_code_version":"2.1.288","output_style":"default","view_mode":"` + mode + `"}`)
}

func TestEngineViewModeReprints(t *testing.T) {
	g := newRig(t, 61)
	g.engineViewFromInit("default")
	if g.c.Reprints != 0 {
		t.Fatal("default view mode reprinted")
	}
	g.engineViewFromInit("focus")
	if g.c.Reprints != 1 || g.f.mode() != ext.Focus {
		t.Fatalf("reprints %d mode %v", g.c.Reprints, g.f.mode())
	}
	g.engineViewFromInit("default")
	if g.c.Reprints != 2 || g.f.mode() != ext.Normal {
		t.Fatalf("reprints %d mode %v", g.c.Reprints, g.f.mode())
	}
}

func TestBriefMode(t *testing.T) {
	g := newRig(t, 61)
	var toggle ext.Action
	for _, a := range g.r.Actions {
		if a.ID == ext.ActAppToggleBrief {
			toggle = a
		}
	}
	if toggle.Run == nil {
		t.Fatal("app:toggleBrief not registered")
	}
	handled, cmd := toggle.Run(g.c)
	if !handled || cmd == nil || g.f.mode() != ext.Brief || g.c.Reprints != 1 {
		t.Fatalf("toggle: handled %v mode %v reprints %d", handled, g.f.mode(), g.c.Reprints)
	}
	g.send(focusTurn)
	g.send(`
{"type":"assistant","uuid":"a9","message":{"id":"m2","content":[{"type":"tool_use","id":"t9","name":"SendUserMessage","input":{"message":"Heads **up**: done."}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t9","content":"sent"}]}}
{"type":"assistant","uuid":"a10","message":{"id":"m2","content":[{"type":"text","text":"ok"}]}}
`)
	got := join(g.printed())
	if !strings.Contains(got, "⏺ Heads up: done.") || strings.Contains(got, "Used 2 tools") || strings.Contains(got, "for 3s") {
		t.Fatalf("brief view:\n%s", got)
	}
	toggle.Run(g.c)
	if g.f.mode() != ext.Normal {
		t.Fatal("brief mode did not toggle off")
	}
}

func TestVerboseExpands(t *testing.T) {
	g := newRig(t, 61)
	g.c.SettingsV = exttest.NewSettings(map[string]any{"verbose": true})
	g.deliver(ext.SettingsMsg{})
	g.send(focusTurn)
	got := join(g.printed())
	if !strings.Contains(got, "hmm") || strings.Contains(got, "ctrl+o to expand") {
		t.Fatalf("verbose view:\n%s", got)
	}
}
