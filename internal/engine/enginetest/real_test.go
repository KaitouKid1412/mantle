package enginetest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func resultFor(uuid string) func(tea.Msg) bool {
	return func(m tea.Msg) bool {
		ev, ok := m.(ext.EngineEventMsg)
		if !ok {
			return false
		}
		r, ok := ev.Event.(*proto.Result)
		return ok && (r.UserMessageUUID == uuid || contains(r.UserMessageUUIDs, uuid))
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// TestRealEngine drives the real claude through the Manager, offline: a streamed
// answer, then a Bash tool call approved through PermissionMsg.
func TestRealEngine(t *testing.T) {
	r := NewReal(t, &fakeapi.Script{Turns: []fakeapi.Turn{
		{Match: &fakeapi.Match{Contains: "say hello"}, Reply: []fakeapi.Block{{Type: "text", Text: "Hello from the real engine, streamed in pieces."}}},
		{Match: &fakeapi.Match{Contains: "make a file"}, Reply: []fakeapi.Block{
			{Type: "tool_use", Name: "Bash", Input: json.RawMessage(`{"command":"echo hi > out.txt","description":"Write out.txt"}`)}}},
		{Match: &fakeapi.Match{ToolResult: "Bash"}, Reply: []fakeapi.Block{{Type: "text", Text: "Done."}}},
	}})
	e, err := r.Manager.Start("", r.Opts())
	if err != nil {
		t.Fatal(err)
	}
	e.Send(ext.Prompt{UUID: "11111111-1111-4111-8111-111111111111", Blocks: []proto.ContentBlock{proto.Text("say hello")}})()
	res := r.Rec.WaitFor(t, resultFor("11111111-1111-4111-8111-111111111111")).(ext.EngineEventMsg).Event.(*proto.Result)
	if res.Result != "Hello from the real engine, streamed in pieces." || res.IsError {
		t.Fatalf("result: %+v", res)
	}
	var streamed string
	for _, ev := range Events(r.Rec.Msgs()) {
		if se, ok := ev.(*proto.StreamEvent); ok && se.Event.Delta != nil {
			streamed += se.Event.Delta.Text
		}
		if env := ev.Env(); env.Mismatch != nil {
			t.Errorf("%s/%s: shape drift: %v", env.Type, env.Subtype, env.Mismatch)
		}
		if lost, _ := proto.Unmodelled(ev); len(lost) > 0 {
			t.Logf("%s/%s fields not modelled: %v", ev.Env().Type, ev.Env().Subtype, lost)
		}
	}
	if streamed != res.Result {
		t.Errorf("streamed %q", streamed)
	}
	snap := e.Snapshot()
	if snap.Info.SessionID == "" || snap.Info.ClaudeVersion == "" || snap.Initialize == nil {
		t.Errorf("snapshot %+v", snap.Info)
	}

	e.Send(ext.Prompt{UUID: "22222222-2222-4222-8222-222222222222", Blocks: []proto.ContentBlock{proto.Text("make a file")}})()
	pm := r.Rec.WaitFor(t, func(m tea.Msg) bool { _, ok := m.(ext.PermissionMsg); return ok }).(ext.PermissionMsg)
	if pm.Req.ToolName != "Bash" || pm.Req.ToolUseID == "" {
		t.Fatalf("permission: %+v", pm.Req)
	}
	t.Logf("can_use_tool: title=%q display=%q suggestions=%d reasonType=%q", pm.Req.Title, pm.Req.DisplayName, len(pm.Req.PermissionSuggestions), pm.Req.DecisionReasonType)
	pm.Reply(pm.Req.Allow(nil))()
	res = r.Rec.WaitFor(t, resultFor("22222222-2222-4222-8222-222222222222")).(ext.EngineEventMsg).Event.(*proto.Result)
	if res.Result != "Done." {
		t.Fatalf("result: %+v", res)
	}
	if b, err := os.ReadFile(filepath.Join(r.Work, "out.txt")); err != nil || strings.TrimSpace(string(b)) != "hi" {
		t.Fatalf("out.txt %q %v", b, err)
	}
	var tr []proto.ToolResult
	for _, ev := range Events(r.Rec.Msgs()) {
		if u, ok := ev.(*proto.User); ok && !u.IsReplay {
			tr = append(tr, u.ToolResults()...)
		}
	}
	if len(tr) != 1 || tr[0].ToolUseID != pm.Req.ToolUseID || len(tr[0].Structured) == 0 {
		t.Errorf("tool results %+v", tr)
	}
}
