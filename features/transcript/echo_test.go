package transcript

import "testing"

func TestStoreEchoOrdering(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"conversation_reset","new_conversation_id":"n","trigger":"clear"}
{"type":"user","uuid":"e1","isReplay":true,"message":{"role":"user","content":"<command-name>/clear</command-name>"}}
{"type":"system","subtype":"compact_boundary","uuid":"c1","compact_metadata":{"trigger":"manual","pre_tokens":100,"post_tokens":10}}
{"type":"user","uuid":"e2","isReplay":true,"message":{"role":"user","content":"<local-command-stdout>Compacted </local-command-stdout>"}}
{"type":"user","uuid":"e3","isReplay":true,"message":{"role":"user","content":"<command-name>/compact</command-name>"}}
`)
	if got := keys(s.Items()); got != "user.prompt system.compact_boundary system.local_command" {
		t.Fatalf("items = %s", got)
	}
	if s.Items()[1].State.Finished() {
		t.Fatal("manual compaction finished before its turn ended")
	}
	apply(t, s, `{"type":"result","subtype":"success","uuid":"r","duration_ms":5,"is_error":false,"num_turns":0,"total_cost_usd":0}`)
	for _, it := range s.Items() {
		if !it.State.Finished() {
			t.Fatalf("%s still pending after the result", it.Key)
		}
	}
}

func TestStoreAutoCompactCommitsAtOnce(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `{"type":"system","subtype":"compact_boundary","uuid":"c1","compact_metadata":{"trigger":"auto","pre_tokens":100}}`)
	if !s.Items()[0].State.Finished() {
		t.Fatal("auto compaction waits for an echo that never comes")
	}
}

func TestStoreBackgroundAgent(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"tool_use","id":"ag","name":"Task","input":{"description":"Find TODOs"}}]}}
{"type":"system","subtype":"task_started","task_id":"k","tool_use_id":"ag","description":"Find TODOs","is_backgrounded":true}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"ag","content":"launched"}]},"tool_use_result":{"isAsync":true,"status":"async_launched"}}
{"type":"system","subtype":"task_notification","uuid":"tn","task_id":"k","status":"completed","summary":"Found 2"}
`)
	if got := keys(s.Items()); got != "tool.Task system.task_notification" {
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
