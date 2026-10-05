package transcript

import (
	"strings"
	"testing"
)

func TestManualCompactStartsOver(t *testing.T) {
	g := newRig(t, 61)
	g.send(`
{"type":"user","uuid":"q1","isReplay":true,"message":{"role":"user","content":"Say something"}}
{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"text","text":"An answer."}]}}
{"type":"result","subtype":"success","uuid":"r1","duration_ms":1500,"is_error":false,"num_turns":1,"total_cost_usd":0}
{"type":"system","subtype":"compact_boundary","uuid":"c1","compact_metadata":{"trigger":"manual","pre_tokens":18200,"post_tokens":900}}
{"type":"user","uuid":"s1","message":{"role":"user","content":"Summary of the conversation so far."}}
{"type":"user","uuid":"e1","isReplay":true,"message":{"role":"user","content":"<local-command-stdout>Compacted </local-command-stdout>"}}
{"type":"user","uuid":"e2","isReplay":true,"message":{"role":"user","content":"<command-name>/compact</command-name>"}}
{"type":"result","subtype":"success","uuid":"r2","duration_ms":20,"is_error":false,"num_turns":0,"total_cost_usd":0}
`)
	want := "\n✻ Conversation compacted (ctrl+o for history)\n\n❯ /compact\n  ⎿  Compacted (ctrl+o for the summary)"
	if got := join(g.printed()); got != want {
		t.Fatalf("after /compact:\n%q\nwant\n%q", got, want)
	}
	// ctrl+o still has the whole history, summary included.
	v := g.viewer()
	viewText(g, v)
	all := strings.Join(v.plain, "\n")
	for _, s := range []string{"Say something", "An answer.", "Summary of the earlier conversation", "Summary of the conversation so far."} {
		if !strings.Contains(all, s) {
			t.Errorf("viewer lacks %q", s)
		}
	}
}

func TestBackgroundAgentRows(t *testing.T) {
	g := newRig(t, 81)
	g.send(`
{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"tool_use","id":"ag","name":"Agent","input":{"description":"Find greet callers","subagent_type":"general-purpose"}}]}}
{"type":"system","subtype":"task_started","task_id":"k","tool_use_id":"ag","description":"Find greet callers","is_backgrounded":true}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"ag","content":"launched"}]},"tool_use_result":{"isAsync":true,"status":"async_launched"}}
{"type":"system","subtype":"task_notification","uuid":"tn","task_id":"k","status":"completed","summary":"The only caller of greet is main().","usage":{"total_tokens":10,"tool_uses":1,"duration_ms":3000}}
{"type":"assistant","uuid":"a2","message":{"id":"m","content":[{"type":"text","text":"The subagent found one caller: main()."}]}}
{"type":"result","subtype":"success","uuid":"r1","duration_ms":4000,"is_error":false,"num_turns":2,"total_cost_usd":0}
{"type":"system","subtype":"init","uuid":"i2","cwd":"/w","tools":[],"mcp_servers":[],"model":"m","permissionMode":"default","slash_commands":[],"claude_code_version":"2.1.289","output_style":"default"}
{"type":"assistant","uuid":"a3","message":{"id":"m2","content":[{"type":"text","text":"OK."}]}}
{"type":"result","subtype":"success","uuid":"r2","duration_ms":2000,"is_error":false,"num_turns":1,"total_cost_usd":0}
`)
	got := join(g.printed())
	order := []string{
		"⏺ Agent(Find greet callers)\n  ⎿  Backgrounded agent (ctrl+o to expand)",
		"⏺ The subagent found one caller: main().",
		"✻ Waiting for 1 background agent to finish",
		"⏺ Agent \"Find greet callers\" finished · 3s",
		"⏺ OK.",
		" for 2s · done ",
	}
	at := 0
	for _, s := range order {
		i := strings.Index(got[at:], s)
		if i < 0 {
			t.Fatalf("%q missing or out of order in:\n%s", s, got)
		}
		at += i + len(s)
	}
	if strings.Contains(got, "The only caller of greet") {
		t.Fatalf("the finished row has a body:\n%s", got)
	}
}
