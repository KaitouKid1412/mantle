package transcript

import (
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// decodeLines decodes NDJSON lines into events.
func decodeLines(t testing.TB, ndjson string) []proto.Event {
	t.Helper()
	var out []proto.Event
	for _, line := range strings.Split(ndjson, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		ev, err := proto.Decode([]byte(line))
		if err != nil {
			t.Fatalf("decode %s: %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}

func newTestStore() (*Store, *time.Time) {
	now := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	return NewStore(ext.MainEngine, func() time.Time { return now }), &now
}

func apply(t testing.TB, s *Store, ndjson string) {
	t.Helper()
	for _, ev := range decodeLines(t, ndjson) {
		s.Apply(ev)
	}
}

func keys(items []*ext.Item) string {
	var parts []string
	for _, it := range items {
		parts = append(parts, string(it.Key))
	}
	return strings.Join(parts, " ")
}

const streamedText = `
{"type":"stream_event","uuid":"s1","event":{"type":"message_start","message":{"id":"msg_1","content":[]}}}
{"type":"stream_event","uuid":"s2","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}
{"type":"stream_event","uuid":"s3","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello, "}}}
{"type":"stream_event","uuid":"s4","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world."}}}
`

func TestStoreStreamsText(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, streamedText)
	items := s.Items()
	if len(items) != 1 || items[0].Key != ext.KeyAssistantText || items[0].State != ext.Streaming {
		t.Fatalf("items = %s / %+v", keys(items), items[0])
	}
	if got := items[0].Data.(*proto.ContentBlock).Text; got != "Hello, world." {
		t.Fatalf("text = %q", got)
	}
	rev := items[0].Rev
	apply(t, s, `
{"type":"stream_event","uuid":"s5","event":{"type":"content_block_stop","index":0}}
{"type":"assistant","uuid":"a1","message":{"id":"msg_1","model":"m","content":[{"type":"text","text":"Hello, world!"}]}}
`)
	if len(s.Items()) != 1 {
		t.Fatalf("assistant message duplicated the streamed item: %s", keys(s.Items()))
	}
	it := s.Items()[0]
	if it.State != ext.Done || it.Rev <= rev {
		t.Fatalf("state %v rev %d (was %d)", it.State, it.Rev, rev)
	}
	if got := it.Data.(*proto.ContentBlock).Text; got != "Hello, world!" {
		t.Fatalf("final text = %q (the assistant message is authoritative)", got)
	}
}

func TestStoreToolCallLifecycle(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_2","content":[]}}}
{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}
{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me check."}}}
{"type":"stream_event","event":{"type":"content_block_stop","index":0}}
{"type":"assistant","uuid":"a1","message":{"id":"msg_2","content":[{"type":"text","text":"Let me check."}]}}
{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"Bash","input":{}}}}
{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\":"}}}
`)
	if got := s.PartialInput("toolu_1"); got != `{"command":` {
		t.Fatalf("partial input = %q", got)
	}
	apply(t, s, `
{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"ls\"}"}}}
{"type":"stream_event","event":{"type":"content_block_stop","index":1}}
`)
	tool := s.Get("toolu_1")
	if tool == nil || tool.Key != "tool.Bash" || tool.State != ext.Running {
		t.Fatalf("tool item = %+v", tool)
	}
	if in := string(tool.Data.(*proto.ToolUse).Input); in != `{"command":"ls"}` {
		t.Fatalf("input = %s", in)
	}
	apply(t, s, `
{"type":"assistant","uuid":"a2","message":{"id":"msg_2","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}
{"type":"tool_progress","tool_use_id":"toolu_1","tool_name":"Bash","elapsed_time_seconds":2.5}
`)
	if tool.State != ext.Running {
		t.Fatalf("tool finished before its result: %v", tool.State)
	}
	if info := s.Tool("toolu_1"); info == nil || info.Elapsed != 2500*time.Millisecond {
		t.Fatalf("progress = %+v", info)
	}
	apply(t, s, `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"a\nb"}]},"tool_use_result":{"stdout":"a\nb","stderr":"","interrupted":false}}`)
	if tool.State != ext.Done || tool.Result == nil || tool.Result.Content.PlainText() != "a\nb" {
		t.Fatalf("after result: %+v", tool)
	}
	if string(tool.Result.Structured) == "" {
		t.Fatal("structured result lost")
	}
	if keys(s.Items()) != "assistant.text tool.Bash" {
		t.Fatalf("items = %s", keys(s.Items()))
	}
}

func TestStoreToolError(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/x"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"File does not exist.","is_error":true}]}}
`)
	if it := s.Get("t1"); it.State != ext.Failed {
		t.Fatalf("state = %v", it.State)
	}
}

func TestStoreWithoutStreaming(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"thinking","thinking":"hmm","signature":"x"}]}}
{"type":"assistant","uuid":"a2","message":{"id":"m1","content":[{"type":"text","text":"Answer."}]}}
{"type":"assistant","uuid":"a3","message":{"id":"m1","content":[{"type":"text","text":"   "}]}}
`)
	if keys(s.Items()) != "assistant.thinking assistant.text" {
		t.Fatalf("items = %s", keys(s.Items()))
	}
	for _, it := range s.Items() {
		if it.State != ext.Done {
			t.Fatalf("%s state %v", it.Key, it.State)
		}
	}
}

func TestStoreNestsSubagentOutput(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"tool_use","id":"agent_1","name":"Agent","input":{"description":"look around","subagent_type":"Explore"}}]}}
{"type":"system","subtype":"task_started","task_id":"task1","tool_use_id":"agent_1","description":"look around"}
{"type":"assistant","uuid":"a2","parent_tool_use_id":"agent_1","message":{"id":"m2","content":[{"type":"tool_use","id":"t2","name":"Glob","input":{"pattern":"*.go"}}]}}
{"type":"user","parent_tool_use_id":"agent_1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"a.go"}]}}
{"type":"system","subtype":"task_progress","task_id":"task1","description":"look around","usage":{"total_tokens":1200,"tool_uses":1,"duration_ms":3000}}
`)
	if keys(s.Items()) != "tool.Agent" {
		t.Fatalf("top-level items = %s", keys(s.Items()))
	}
	kids := s.Children("agent_1")
	if len(kids) != 1 || kids[0].ID != "t2" || kids[0].State != ext.Done {
		t.Fatalf("children = %+v", kids)
	}
	if info := s.Tool("agent_1"); info.Task == nil || info.Task.ToolUses != 1 {
		t.Fatalf("task usage = %+v", info)
	}
	agentRev := s.Get("agent_1").Rev
	apply(t, s, `{"type":"assistant","uuid":"a3","parent_tool_use_id":"agent_1","message":{"id":"m3","content":[{"type":"text","text":"found it"}]}}`)
	if s.Get("agent_1").Rev <= agentRev {
		t.Fatal("a new child did not bump the parent's revision")
	}
}

func TestStoreSupersedes(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"text","text":"first try"}]}}
{"type":"assistant","uuid":"a2","message":{"id":"m1","content":[{"type":"text","text":"also first"}]}}
`)
	s.SetCommitted(1)
	apply(t, s, `{"type":"assistant","uuid":"a3","supersedes":["a1","a2"],"message":{"id":"m2","content":[{"type":"text","text":"second try"}]}}`)
	items := s.Items()
	if len(items) != 1 || items[0].Data.(*proto.ContentBlock).Text != "second try" {
		t.Fatalf("items = %+v", items)
	}
	if s.Committed() != 0 {
		t.Fatalf("committed = %d, want 0", s.Committed())
	}
	if notes := s.TakeNotes(); len(notes) != 1 || notes[0] != NoteReplaced {
		t.Fatalf("notes = %v", notes)
	}
}

func TestStoreInterruptedTurn(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"sleep 100"}}]}}
{"type":"result","subtype":"error_during_execution","uuid":"r1","duration_ms":5000,"is_error":true,"num_turns":1,"terminal_reason":"aborted_tools","total_cost_usd":0}
`)
	if it := s.Get("t1"); it.State != ext.Interrupted {
		t.Fatalf("tool state = %v", it.State)
	}
	if keys(s.Items()) != "tool.Bash system.result" {
		t.Fatalf("items = %s", keys(s.Items()))
	}
}

func TestStoreUserMessages(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"user","uuid":"u1","isReplay":true,"message":{"role":"user","content":"fix the bug"}}
{"type":"user","uuid":"u2","isReplay":true,"message":{"role":"user","content":"<bash-input>ls</bash-input>"}}
{"type":"user","uuid":"u3","isReplay":true,"message":{"role":"user","content":"<bash-stdout>a.go</bash-stdout><bash-stderr></bash-stderr>"}}
{"type":"user","uuid":"u4","isReplay":true,"message":{"role":"user","content":"<local-command-stdout>ok</local-command-stdout>"}}
{"type":"user","uuid":"u5","isReplay":true,"isSynthetic":true,"message":{"role":"user","content":"expanded skill text"}}
{"type":"user","uuid":"u6","message":{"role":"user","content":"not a replay"}}
`)
	if keys(s.Items()) != "user.prompt user.bash system.local_command" {
		t.Fatalf("items = %s", keys(s.Items()))
	}
	bash := s.Items()[1].Data.(*proto.User).Message.Content.PlainText()
	if !strings.Contains(bash, "<bash-stdout>a.go") {
		t.Fatalf("bash output not merged: %q", bash)
	}
}

func TestStoreTransientRetryAndReset(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"system","subtype":"api_retry","uuid":"r1","attempt":1,"max_retries":10,"retry_delay_ms":2000}
{"type":"system","subtype":"api_retry","uuid":"r2","attempt":2,"max_retries":10,"retry_delay_ms":4000}
`)
	if keys(s.Items()) != "system.api_retry" || s.Items()[0].Data.(*proto.APIRetry).Attempt != 2 {
		t.Fatalf("items = %s", keys(s.Items()))
	}
	apply(t, s, `{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"text","text":"ok"}]}}`)
	if keys(s.Items()) != "assistant.text" {
		t.Fatalf("retry item survived: %s", keys(s.Items()))
	}
	apply(t, s, `{"type":"conversation_reset","new_conversation_id":"x","trigger":"clear"}`)
	if len(s.Items()) != 0 || s.Get("txt:a1:0") != nil {
		t.Fatalf("reset left %s", keys(s.Items()))
	}
}

func TestStoreHooksAndSystemEvents(t *testing.T) {
	s, _ := newTestStore()
	apply(t, s, `
{"type":"system","subtype":"hook_started","uuid":"h1","hook_id":"hk1","hook_name":"PreToolUse:Bash","hook_event":"PreToolUse"}
{"type":"system","subtype":"hook_response","uuid":"h2","hook_id":"hk1","hook_name":"PreToolUse:Bash","hook_event":"PreToolUse","stderr":"blocked","exit_code":2,"outcome":"error"}
{"type":"system","subtype":"compact_boundary","uuid":"c1","compact_metadata":{"trigger":"manual","pre_tokens":50000,"post_tokens":8000}}
{"type":"system","subtype":"informational","uuid":"i1","content":"Heads up","level":"warning"}
{"type":"system","subtype":"session_state_changed","state":"idle"}
{"type":"system","subtype":"thinking_tokens","estimated_tokens":10}
`)
	if keys(s.Items()) != "system.hook system.compact_boundary system.informational" {
		t.Fatalf("items = %s", keys(s.Items()))
	}
	if s.Items()[0].State != ext.Failed {
		t.Fatalf("hook state = %v", s.Items()[0].State)
	}
}

func TestStoreHistory(t *testing.T) {
	s, _ := newTestStore()
	s.AppendHistory([]*ext.Item{
		{ID: "h1", Key: ext.KeyUserPrompt, Data: &proto.User{Message: proto.UserMessage{Role: "user", Content: proto.TextContent("old")}}},
		{ID: "h2", Key: "tool.Agent", Data: &proto.ToolUse{ID: "h2", Name: "Agent"}, State: ext.Running},
		{ID: "h3", ParentID: "h2", Key: "tool.Read", Data: &proto.ToolUse{ID: "h3", Name: "Read"}},
	})
	if keys(s.Items()) != "user.prompt tool.Agent" || len(s.Children("h2")) != 1 {
		t.Fatalf("items = %s children %d", keys(s.Items()), len(s.Children("h2")))
	}
	if s.Get("h2").State != ext.Done {
		t.Fatal("history items must be finished")
	}
}
