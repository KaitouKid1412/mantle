package proto

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func readLines(t *testing.T, path string) [][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out [][]byte
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			out = append(out, line)
		}
		if err != nil {
			break
		}
	}
	return out
}

func TestSamplesDecodeAndRoundTrip(t *testing.T) {
	want := map[string]string{
		"system/init":                   "*proto.SystemInit",
		"system/hook_started":           "*proto.Hook",
		"system/hook_response":          "*proto.Hook",
		"system/session_state_changed":  "*proto.SessionStateChanged",
		"system/compact_boundary":       "*proto.CompactBoundary",
		"system/brand_new_subtype":      "*proto.Unknown",
		"stream_event/":                 "*proto.StreamEvent",
		"assistant/":                    "*proto.Assistant",
		"user/":                         "*proto.User",
		"result/success":                "*proto.Result",
		"result/error_during_execution": "*proto.Result",
		"control_request/":              "*proto.ControlRequest",
		"control_response/":             "*proto.ControlResponse",
		"control_cancel_request/":       "*proto.ControlCancelRequest",
		"keep_alive/":                   "*proto.KeepAlive",
		"totally_new_type/":             "*proto.Unknown",
	}
	for i, line := range readLines(t, "testdata/samples.jsonl") {
		ev, err := Decode(line)
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		env := ev.Env()
		if env.Mismatch != nil {
			t.Errorf("line %d (%s/%s): mismatch: %v", i+1, env.Type, env.Subtype, env.Mismatch)
		}
		key := env.Type + "/" + env.Subtype
		if w, ok := want[key]; ok {
			if got := reflect.TypeOf(ev).String(); got != w {
				t.Errorf("line %d: %s decoded as %s, want %s", i+1, key, got, w)
			}
		}
		if _, unknown := ev.(*Unknown); !unknown && strings.HasPrefix(key, "system/") && newEvent(env.Type, env.Subtype) == nil {
			t.Errorf("line %d: %s should be Unknown", i+1, key)
		}
		lost, err := Unmodelled(ev)
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		if len(lost) > 0 {
			t.Errorf("line %d (%s): round trip loses %v", i+1, key, lost)
		}
		if u, ok := ev.(*Unknown); ok {
			enc, _ := json.Marshal(u)
			if !bytes.Equal(enc, bytes.TrimSpace(line)) {
				t.Errorf("line %d: unknown not preserved verbatim", i+1)
			}
		}
	}
}

func TestDecodeSkipsNonJSON(t *testing.T) {
	for _, l := range []string{"", "   ", "[SandboxDebug] hi", "\n"} {
		if _, err := Decode([]byte(l)); !errors.Is(err, ErrNotJSON) {
			t.Errorf("%q: err = %v, want ErrNotJSON", l, err)
		}
	}
	if _, err := Decode([]byte(`{"type":`)); err == nil || errors.Is(err, ErrNotJSON) {
		t.Errorf("malformed: err = %v", err)
	}
}

func TestDecodeToleratesShapeDrift(t *testing.T) {
	// num_turns as a string and an unexpected object for result: the rest still decodes.
	ev, err := Decode([]byte(`{"type":"result","subtype":"success","num_turns":"two","is_error":false,"result":{"x":1},"total_cost_usd":1.5,"session_id":"s"}`))
	if err != nil {
		t.Fatal(err)
	}
	r, ok := ev.(*Result)
	if !ok {
		t.Fatalf("got %T", ev)
	}
	if r.Mismatch == nil {
		t.Error("expected Mismatch")
	}
	if r.TotalCostUSD != 1.5 || r.SessionID != "s" {
		t.Errorf("rest not decoded: %+v", r)
	}
}

func TestDecodeKeepsOwnCopy(t *testing.T) {
	buf := []byte(`{"type":"keep_alive"}`)
	ev, _ := Decode(buf)
	buf[2] = 'X'
	if string(ev.Env().Raw) != `{"type":"keep_alive"}` {
		t.Errorf("Raw aliases input: %s", ev.Env().Raw)
	}
}

func TestContentStringOrBlocks(t *testing.T) {
	var c Content
	if err := json.Unmarshal([]byte(`"hi"`), &c); err != nil || !c.IsString() || c.PlainText() != "hi" {
		t.Errorf("string: %+v %v", c, err)
	}
	if b, _ := json.Marshal(c); string(b) != `"hi"` {
		t.Errorf("string marshal: %s", b)
	}
	if err := json.Unmarshal([]byte(`[{"type":"text","text":"a"},{"type":"text","text":"b"}]`), &c); err != nil || c.IsString() || c.PlainText() != "a\nb" {
		t.Errorf("blocks: %+v %v", c, err)
	}
	if err := json.Unmarshal([]byte(`{"weird":true}`), &c); err != nil || string(c.Raw) != `{"weird":true}` {
		t.Errorf("raw: %+v %v", c, err)
	}
	if b, _ := json.Marshal(c); string(b) != `{"weird":true}` {
		t.Errorf("raw marshal: %s", b)
	}
	if b, _ := json.Marshal(BlockContent()); string(b) != `[]` {
		t.Errorf("empty blocks: %s", b)
	}
}

func TestContentBlockMarshalRequiredFields(t *testing.T) {
	cases := map[string]ContentBlock{
		`{"type":"text","text":""}`:                                                          {Type: BlockText},
		`{"type":"tool_use","id":"t","name":"Bash","input":{}}`:                              {Type: BlockToolUse, ID: "t", Name: "Bash"},
		`{"type":"thinking","thinking":"x","signature":""}`:                                  {Type: BlockThinking, Thinking: "x"},
		`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}}`: Image("image/png", "AA=="),
	}
	for want, b := range cases {
		got, err := json.Marshal(b)
		if err != nil || string(got) != want {
			t.Errorf("got %s (%v), want %s", got, err, want)
		}
	}
}

func TestUserToolResults(t *testing.T) {
	ev, err := Decode([]byte(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok","is_error":true}]},"parent_tool_use_id":"toolu_0","tool_use_result":{"stdout":"ok"}}`))
	if err != nil {
		t.Fatal(err)
	}
	rs := ev.(*User).ToolResults()
	if len(rs) != 1 || rs[0].ToolUseID != "toolu_1" || !rs[0].IsError || rs[0].Content.PlainText() != "ok" ||
		rs[0].ParentToolUseID != "toolu_0" || string(rs[0].Structured) != `{"stdout":"ok"}` {
		t.Errorf("got %+v", rs)
	}
}

func TestControlRequestRoundTrip(t *testing.T) {
	b, err := MarshalControlRequest("req_1", InitializeRequest{PromptSuggestions: true, SupportedDialogKinds: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"control_request","request_id":"req_1","request":{"subtype":"initialize","promptSuggestions":true,"supportedDialogKinds":["x"]}}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	b, _ = MarshalControlRequest("req_2", GetSettingsRequest{})
	if string(b) != `{"type":"control_request","request_id":"req_2","request":{"subtype":"get_settings"}}` {
		t.Errorf("empty: %s", b)
	}
	b, _ = MarshalControlRequest("req_3", MCPToggleRequest{ServerName: "x", Enabled: false})
	if !strings.Contains(string(b), `"enabled":false`) {
		t.Errorf("toggle must send enabled:false: %s", b)
	}
	b, _ = MarshalControlRequest("req_4", RawRequest{Subtype: "side_question", Fields: json.RawMessage(`{"question":"q"}`)})
	if !strings.Contains(string(b), `{"subtype":"side_question","question":"q"}`) {
		t.Errorf("raw: %s", b)
	}

	ev, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	cr := ev.(*ControlRequest)
	if cr.RequestID != "req_4" || cr.RequestSubtype() != "side_question" {
		t.Errorf("got %+v", cr)
	}
}

func TestCanUseToolDecodeAndReply(t *testing.T) {
	ev, _ := Decode([]byte(`{"type":"control_request","request_id":"cli_1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"rm x"},"tool_use_id":"toolu_5","permission_suggestions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"rm:*"}],"behavior":"allow","destination":"localSettings"}]}}`))
	v, err := ev.(*ControlRequest).DecodeRequest()
	if err != nil {
		t.Fatal(err)
	}
	c := v.(*CanUseTool)
	if c.ToolName != "Bash" || len(c.PermissionSuggestions) != 1 || c.PermissionSuggestions[0].Rules[0].RuleContent != "rm:*" {
		t.Fatalf("got %+v", c)
	}
	b, _ := MarshalControlSuccess("cli_1", c.AllowAlways(c.PermissionSuggestions...))
	want := `{"type":"control_response","response":{"subtype":"success","request_id":"cli_1","response":{"behavior":"allow","updatedPermissions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"rm:*"}],"behavior":"allow","destination":"localSettings"}],"toolUseID":"toolu_5","decisionClassification":"user_permanent"}}}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	b, _ = MarshalControlSuccess("cli_1", c.Deny("no", true))
	if !strings.Contains(string(b), `"behavior":"deny","message":"no","interrupt":true`) {
		t.Errorf("deny: %s", b)
	}
}

func TestControlResponseErrors(t *testing.T) {
	ev, _ := Decode([]byte(`{"type":"control_response","response":{"subtype":"error","request_id":"r9","error":"Unsupported control request subtype: bogus"}}`))
	body := ev.(*ControlResponse).Response
	err := body.Err()
	if err == nil || !IsUnsupported(err) {
		t.Fatalf("err = %v", err)
	}
	var v BinaryVersion
	if err := body.Decode(&v); !IsUnsupported(err) {
		t.Errorf("Decode err = %v", err)
	}
	ev, _ = Decode([]byte(`{"type":"control_response","response":{"subtype":"success","request_id":"r7","response":{"version":"2.1.288"}}}`))
	if err := ev.(*ControlResponse).Response.Decode(&v); err != nil || v.Version != "2.1.288" {
		t.Errorf("v=%+v err=%v", v, err)
	}
}

func TestUserInputLine(t *testing.T) {
	u := NewUserInput("uuid-1", Text("/compact"))
	u.Origin = &Origin{Kind: OriginHuman}
	b, err := u.MarshalLine()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"user","session_id":"","parent_tool_use_id":null,"uuid":"uuid-1","message":{"role":"user","content":"/compact"},"origin":{"kind":"human"}}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	u = NewUserInput("uuid-2", Text("look"), Image("image/png", "AA=="))
	u.Priority = PriorityNext
	b, _ = u.MarshalLine()
	if !strings.Contains(string(b), `"content":[{"type":"text","text":"look"},{"type":"image"`) || !strings.Contains(string(b), `"priority":"next"`) {
		t.Errorf("blocks: %s", b)
	}
}

func TestKnownTables(t *testing.T) {
	if len(KnownTypes()) < 15 || len(KnownSystemSubtypes()) < 25 || len(KnownRequestSubtypes()) < 38 {
		t.Errorf("tables shrank: %d %d %d", len(KnownTypes()), len(KnownSystemSubtypes()), len(KnownRequestSubtypes()))
	}
	seen := map[string]bool{}
	for _, s := range KnownRequestSubtypes() {
		if seen[s] {
			t.Errorf("duplicate %s", s)
		}
		seen[s] = true
	}
}

func TestUnmodelledReportsDroppedFields(t *testing.T) {
	ev, _ := Decode([]byte(`{"type":"assistant","message":{"id":"m","content":[{"type":"text","text":"x","brand_new":1}],"novel":{"a":1}},"zero":0,"extra":"yes","uuid":"u"}`))
	got, err := Unmodelled(ev)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"extra", "message.content[0].brand_new", "message.novel"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
