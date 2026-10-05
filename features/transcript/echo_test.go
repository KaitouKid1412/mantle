package transcript

import "testing"

func TestStoreEchoOrdering(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"user","uuid":"q1","isReplay":true,"message":{"role":"user","content":"earlier question"}}
{"type":"conversation_reset","new_conversation_id":"n","trigger":"clear"}
{"type":"user","uuid":"e1","isReplay":true,"message":{"role":"user","content":"<command-name>/clear</command-name>"}}
{"type":"user","uuid":"q2","isReplay":true,"message":{"role":"user","content":"before compaction"}}
{"type":"system","subtype":"compact_boundary","uuid":"c1","compact_metadata":{"trigger":"manual","pre_tokens":100,"post_tokens":10}}
{"type":"user","uuid":"s1","message":{"role":"user","content":"This session continues from a summary."}}
{"type":"user","uuid":"e2","isReplay":true,"message":{"role":"user","content":"<local-command-stdout>Compacted </local-command-stdout>"}}
{"type":"user","uuid":"e3","isReplay":true,"message":{"role":"user","content":"<command-name>/compact</command-name>"}}
`)
	// Like claude: the boundary, then the /compact echo, then its output; the
	// summary is kept for ctrl+o.
	if got := keys(s.Items()); got != "user.prompt system.compact_boundary system.compact_summary user.prompt system.local_command" {
		t.Fatalf("items = %s", got)
	}
	if s.Base() != 1 {
		t.Fatalf("base = %d, want the boundary (1)", s.Base())
	}
	apply(t, s, `{"type":"result","subtype":"success","uuid":"r","duration_ms":5,"is_error":false,"num_turns":0,"total_cost_usd":0}`)
	for _, it := range s.Items() {
		if !it.State.Finished() {
			t.Fatalf("%s still pending after the result", it.Key)
		}
	}
}

func TestStoreAutoCompactKeepsTheScreen(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"user","uuid":"q1","isReplay":true,"message":{"role":"user","content":"long task"}}
{"type":"system","subtype":"compact_boundary","uuid":"c1","compact_metadata":{"trigger":"auto","pre_tokens":100}}`)
	if s.Base() != 0 || !s.Items()[1].State.Finished() {
		t.Fatal("auto compaction must not start the screen over")
	}
}

func TestStoreBackgroundAgent(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"tool_use","id":"ag","name":"Task","input":{"description":"Find TODOs"}}]}}
{"type":"system","subtype":"task_started","task_id":"k","tool_use_id":"ag","description":"Find TODOs","is_backgrounded":true}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"ag","content":"launched"}]},"tool_use_result":{"isAsync":true,"status":"async_launched"}}
{"type":"system","subtype":"task_notification","uuid":"tn","task_id":"k","status":"completed","summary":"Found 2","usage":{"total_tokens":10,"tool_uses":1,"duration_ms":2000}}
{"type":"assistant","uuid":"a2","message":{"id":"m","content":[{"type":"text","text":"It runs in the background."}]}}
`)
	// The finished row waits for the turn to end.
	if got := keys(s.Items()); got != "tool.Task assistant.text" {
		t.Fatalf("items = %s", got)
	}
	apply(t, s, `{"type":"result","subtype":"success","uuid":"r1","duration_ms":5,"is_error":false,"num_turns":2,"total_cost_usd":0}`)
	if s.WaitingAgents("r1") != 1 {
		t.Fatalf("waiting agents = %d, want 1", s.WaitingAgents("r1"))
	}
	apply(t, s, `{"type":"assistant","uuid":"a3","message":{"id":"m2","content":[{"type":"text","text":"OK."}]}}`)
	if got := keys(s.Items()); got != "tool.Task assistant.text system.result system.task_notification assistant.text" {
		t.Fatalf("items = %s", got)
	}
	if !s.Tool("ag").Async || s.TaskTool("k") != "ag" {
		t.Fatalf("info = %+v", s.Tool("ag"))
	}
}

func TestStoreDuplicateUUIDs(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"user","uuid":"00000000","isReplay":true,"message":{"role":"user","content":"one"}}
{"type":"user","uuid":"00000000","isReplay":true,"message":{"role":"user","content":"two"}}
`)
	items := s.Items()
	if len(items) != 2 || items[0].ID == items[1].ID || s.Get(items[1].ID) != items[1] {
		t.Fatalf("items = %+v", items)
	}
}
