package turn

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/turn/dialogs"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

const bashRaw = `{"tool_name":"Bash","tool_use_id":"t1","input":{"command":"npm test"},
 "permission_suggestions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"npm test:*"}],"behavior":"allow","destination":"localSettings"}]}`

const readRaw = `{"tool_name":"Read","tool_use_id":"t2","input":{"file_path":"/etc/hosts"},"agent_id":"task-9"}`

func TestPermissionQueueInOrder(t *testing.T) {
	x := newH(t)
	main, builder := &replies{}, &replies{}
	x.event(&proto.TaskStarted{TaskID: "task-9", SubagentType: "code-reviewer"})
	x.send(permMsg(ext.MainEngine, "r1", bashRaw, main))
	x.send(permMsg("builder:mod-a3f", "r2", readRaw, builder))

	if x.c.topID() != DialogPermission || len(x.c.stack) != 1 {
		t.Fatalf("one inline dialog should be open, got %v", x.c.ids)
	}
	if v := x.view(80); !strings.Contains(v, "Bash command (1 of 2)") {
		t.Fatalf("counter missing:\n%s", v)
	}
	x.press("2") // Yes, and don't ask again
	if len(main.got) != 1 || main.got[0].Behavior != proto.BehaviorAllow || len(main.got[0].UpdatedPermissions) != 1 {
		t.Fatalf("main reply = %+v", main.got)
	}
	mustEqual(t, main.got[0].UpdatedPermissions[0].Rules, []proto.PermissionRule{{ToolName: "Bash", RuleContent: "npm test:*"}})

	// The builder's request follows, attributed to the builder and its subagent.
	if len(x.c.stack) != 1 {
		t.Fatalf("next request should open, stack=%v", x.c.ids)
	}
	v := x.view(80)
	if !strings.Contains(v, "Builder mod-a3f · subagent code-reviewer is asking") || strings.Contains(v, "of 2") {
		t.Fatalf("attribution/counter wrong:\n%s", v)
	}
	x.press("esc")
	if len(builder.got) != 1 || builder.got[0].Behavior != proto.BehaviorDeny || !builder.got[0].Interrupt {
		t.Fatalf("esc should deny and interrupt: %+v", builder.got)
	}
	if len(x.c.stack) != 0 || x.st.reqs.open != nil {
		t.Fatal("queue should be empty")
	}
}

func TestCancelClosesOpenDialogWithoutReply(t *testing.T) {
	x := newH(t)
	r1, r2 := &replies{}, &replies{}
	x.send(permMsg(ext.MainEngine, "r1", bashRaw, r1))
	x.send(permMsg(ext.MainEngine, "r2", readRaw, r2))
	x.send(ext.ControlCancelMsg{EngineID: ext.MainEngine, RequestID: "r1"})
	if len(r1.got) != 0 {
		t.Fatal("a cancelled request must not be answered")
	}
	if x.c.topID() != DialogPermission || !strings.Contains(x.view(80), "Read file") {
		t.Fatalf("the next request should open:\n%s", x.view(80))
	}
	// Cancelling a waiting request removes it silently.
	r3 := &replies{}
	x.send(permMsg(ext.MainEngine, "r3", bashRaw, r3))
	x.send(ext.ControlCancelMsg{EngineID: ext.MainEngine, RequestID: "r3"})
	x.press("enter")
	if len(r2.got) != 1 || len(r3.got) != 0 || len(x.c.stack) != 0 {
		t.Fatalf("r2=%v r3=%v stack=%v", r2.got, r3.got, x.c.ids)
	}
}

func TestEngineExitDropsItsRequests(t *testing.T) {
	x := newH(t)
	r := &replies{}
	x.send(permMsg("builder", "r1", bashRaw, r))
	x.send(ext.EngineExitedMsg{EngineID: "builder"})
	if len(x.c.stack) != 0 || len(r.got) != 0 {
		t.Fatal("requests of an exited engine should close unanswered")
	}
}

func TestAskUserQuestionRouting(t *testing.T) {
	x := newH(t)
	r := &replies{}
	x.send(permMsg(ext.MainEngine, "q", `{"tool_name":"AskUserQuestion","tool_use_id":"q1","input":{"questions":[
	  {"question":"Pick one","header":"Pick","multiSelect":false,"options":[{"label":"A","description":""},{"label":"B","description":""}]}]}}`, r))
	if x.c.topID() != DialogAskUserQuestion {
		t.Fatalf("opened %v", x.c.ids)
	}
	x.press("2")
	if len(r.got) != 1 || r.got[0].Behavior != proto.BehaviorAllow {
		t.Fatalf("reply = %+v", r.got)
	}
	var in struct{ Answers map[string]string }
	if err := json.Unmarshal(r.got[0].UpdatedInput, &in); err != nil || in.Answers["Pick one"] != "B" {
		t.Fatalf("answers = %s", r.got[0].UpdatedInput)
	}
	// The closed dialog reported its result.
	closed := find[ext.DialogClosedMsg](x)
	if len(closed) != 1 || closed[0].ID != DialogAskUserQuestion || closed[0].Result == nil {
		t.Fatalf("closed = %+v", closed)
	}
}

func TestMalformedAskUserQuestionIsRefused(t *testing.T) {
	x := newH(t)
	r := &replies{}
	x.send(permMsg(ext.MainEngine, "q", `{"tool_name":"AskUserQuestion","tool_use_id":"q1","input":{"questions":[]}}`, r))
	if len(x.c.stack) != 0 || len(r.got) != 1 || r.got[0].Behavior != proto.BehaviorDeny {
		t.Fatalf("stack=%v reply=%+v", x.c.ids, r.got)
	}
}

func TestPlanApprovalRouting(t *testing.T) {
	x := newH(t)
	x.st.mode(ext.MainEngine).auto = true
	r := &replies{}
	x.send(permMsg(ext.MainEngine, "p", `{"tool_name":"ExitPlanMode","tool_use_id":"p1","input":{"plan":"1. do it"}}`, r))
	if x.c.topID() != DialogPlanApproval || !strings.Contains(x.view(80), "Yes, and use auto mode") {
		t.Fatalf("plan dialog:\n%s", x.view(80))
	}
	x.press("2") // auto-accept edits
	if len(r.got) != 1 || r.got[0].UpdatedPermissions[0].Mode != proto.ModeAcceptEdits {
		t.Fatalf("reply = %+v", r.got)
	}
}

func TestElicitationViaControlRequest(t *testing.T) {
	x := newH(t)
	var got []any
	x.send(ext.ControlRequestMsg{
		EngineID: ext.MainEngine, Subtype: proto.SubElicitation, RequestID: "e1",
		Request: json.RawMessage(`{"mcp_server_name":"srv","message":"Name?","requested_schema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}}`),
		Reply:   func(resp any, err error) tea.Cmd { got = append(got, resp); return nil },
	})
	if x.c.topID() != DialogElicitation {
		t.Fatalf("opened %v", x.c.ids)
	}
	x.typeText("Ada")
	x.press("enter", "enter") // next field → Submit
	if len(got) != 1 {
		t.Fatalf("replies = %v", got)
	}
	b, _ := json.Marshal(got[0])
	if string(b) != `{"action":"accept","content":{"name":"Ada"}}` {
		t.Fatalf("reply = %s", b)
	}

	// request_user_dialog is refused: mantle declares no dialog kinds.
	var errs []error
	x.send(ext.ControlRequestMsg{EngineID: ext.MainEngine, Subtype: proto.SubRequestUserDialog, RequestID: "d1",
		Reply: func(_ any, err error) tea.Cmd { errs = append(errs, err); return nil }})
	if len(errs) != 1 || errs[0] == nil {
		t.Fatalf("request_user_dialog should be refused, got %v", errs)
	}
}

func TestShiftTabInsidePermissionDialogCyclesMode(t *testing.T) {
	x := newH(t)
	r := &replies{}
	x.send(permMsg(ext.MainEngine, "r1", bashRaw, r))
	x.press("shift+tab")
	if got := x.eng.lastControl(proto.SubSetPermissionMode); got != (proto.SetPermissionModeRequest{Mode: proto.ModeAcceptEdits}) {
		t.Fatalf("set_permission_mode = %#v", got)
	}
	if len(r.got) != 0 || x.c.topID() != DialogPermission {
		t.Fatal("the dialog stays open after switching mode")
	}
	if x.c.SessionValue.PermissionMode != proto.ModeAcceptEdits {
		t.Fatalf("session mode = %q", x.c.SessionValue.PermissionMode)
	}
}

func TestCtrlCInsideDialogDeniesAndInterrupts(t *testing.T) {
	x := newH(t)
	r := &replies{}
	x.send(permMsg(ext.MainEngine, "r1", bashRaw, r))
	x.press("ctrl+c")
	if len(r.got) != 1 || r.got[0].Behavior != proto.BehaviorDeny || !r.got[0].Interrupt {
		t.Fatalf("ctrl+c reply = %+v", r.got)
	}
	if len(find[ext.ExitMsg](x)) != 0 {
		t.Fatal("ctrl+c in a dialog must not exit")
	}
}

func TestOfferAutoUsesAvailability(t *testing.T) {
	x := newH(t)
	x.st.mode(ext.MainEngine).auto = true
	x.send(permMsg(ext.MainEngine, "r1", bashRaw, &replies{}))
	if !strings.Contains(x.view(100), "Yes, and switch to auto mode") {
		t.Fatalf("auto option missing:\n%s", x.view(100))
	}
}

func TestProtoConversionRoundTrip(t *testing.T) {
	var req proto.CanUseTool
	_ = json.Unmarshal([]byte(bashRaw), &req)
	tr := toToolRequest(req)
	if tr.ToolName != "Bash" || len(tr.PermissionSuggestions) != 1 || tr.ToolUseID != "t1" {
		t.Fatalf("toToolRequest = %+v", tr)
	}
	res := toProtoResult(&dialogs.PermissionResult{Behavior: "deny"})
	if res.Message == "" {
		t.Fatal("a deny must carry a message")
	}
}
