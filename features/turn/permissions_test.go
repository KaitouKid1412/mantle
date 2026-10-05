package turn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/turn/dialogs"
	"github.com/KaitouKid1412/mantle/features/turn/gates"
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

	// A request_user_dialog of a kind mantle did not declare is refused (not settled).
	var errs []error
	x.send(ext.ControlRequestMsg{EngineID: ext.MainEngine, Subtype: proto.SubRequestUserDialog, RequestID: "d1",
		Request: json.RawMessage(`{"subtype":"request_user_dialog","dialog_kind":"other","payload":{}}`),
		Reply:   func(_ any, err error) tea.Cmd { errs = append(errs, err); return nil }})
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

// The Edit preview diffs the whole file: real line numbers and three lines of context,
// like Claude Code's prompt.
func TestEditPreviewUsesFileLineNumbers(t *testing.T) {
	x := newH(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	src := "package main\n\nimport \"fmt\"\n\nfunc greet(name string) string {\n\treturn \"hello, \" + name\n}\n\nfunc main() {\n\tfmt.Println(greet(\"x\"))\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	req, _ := json.Marshal(map[string]any{
		"tool_name": "Edit", "tool_use_id": "e1",
		"input": map[string]any{"file_path": path, "old_string": "return \"hello, \" + name", "new_string": "return \"hi there, \" + name"},
	})
	x.send(permMsg(ext.MainEngine, "r1", string(req), &replies{}))
	v := testkitStrip(x.view(100))
	for _, want := range []string{"import \"fmt\"", "func main() {", "hi there"} {
		if !strings.Contains(v, want) {
			t.Fatalf("preview missing %q:\n%s", want, v)
		}
	}
	if !regexp.MustCompile(`(?m)^\s*6\s`).MatchString(v) {
		t.Fatalf("preview should number the changed line 6:\n%s", v)
	}
	if strings.Contains(v, "\t") {
		t.Fatal("tabs must be expanded")
	}
}

func TestPlanApprovalReadsPlanFile(t *testing.T) {
	x := newH(t)
	path := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(path, []byte("## Plan\n1. Rename greet to hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"tool_name": "ExitPlanMode", "tool_use_id": "p1", "input": map[string]any{"planFilePath": path}})
	x.send(permMsg(ext.MainEngine, "p", string(raw), &replies{}))
	if v := testkitStrip(x.view(100)); !strings.Contains(v, "Rename greet to hello") || !strings.Contains(v, "Ready to code?") {
		t.Fatalf("plan file not shown:\n%s", v)
	}
	x.press("esc")
	r := &replies{}
	x.send(permMsg(ext.MainEngine, "q", `{"tool_name":"ExitPlanMode","tool_use_id":"p2","input":{}}`, r))
	if v := testkitStrip(x.view(100)); !strings.Contains(v, "Exit plan mode?") {
		t.Fatalf("no plan: short dialog expected:\n%s", v)
	}
	x.press("1")
	if len(r.got) != 1 || r.got[0].UpdatedPermissions[0].Mode != proto.ModeDefault {
		t.Fatalf("reply = %+v", r.got)
	}
}

const billingText = "We're changing how auto mode is billed. Details: https://code.claude.com/docs/en/auto-mode-classifier-billing"

func TestBillingNoticeDialog(t *testing.T) {
	x := newH(t)
	r := &replies{}
	x.send(permMsg(ext.MainEngine, "p", `{"tool_name":"ExitPlanMode","tool_use_id":"p1","input":{}}`, r))
	x.event(&proto.Informational{Content: billingText, Level: "warning"})
	if x.c.topID() != DialogPlanApproval {
		t.Fatalf("the notice waits behind the open prompt: %v", x.c.ids)
	}
	x.press("1")
	if x.c.topID() != DialogBillingNotice || !strings.Contains(testkitStrip(x.view(100)), "auto-mode-classifier-billing") {
		t.Fatalf("notice expected next:\n%s", x.view(100))
	}
	x.press("enter")
	if len(x.c.stack) != 0 {
		t.Fatalf("stack = %v", x.c.ids)
	}
	env, _ := x.st.env()
	if gates.LoadGateStore(env).BillingNoticeAt == "" {
		t.Fatal("acknowledgement should be recorded")
	}
	// Once per session, and quiet for a day across sessions.
	x.event(&proto.Informational{Content: billingText, Level: "warning"})
	x2 := newH(t)
	x2.st.env = x.st.env
	x2.event(&proto.Informational{Content: billingText, Level: "warning"})
	if len(x.c.stack)+len(x2.c.stack) != 0 {
		t.Fatal("acknowledged notice shown again")
	}
	x2.c.ClockV.Advance(25 * time.Hour)
	x3 := newH(t)
	x3.st.env = x.st.env
	x3.c.ClockV.Advance(25 * time.Hour)
	x3.event(&proto.Informational{Content: billingText, Level: "warning"})
	if x3.c.topID() != DialogBillingNotice {
		t.Fatal("a day later the notice comes back")
	}
	// Esc closes without acknowledging; other informational messages are ignored.
	x4 := newH(t)
	x4.event(&proto.Informational{Content: "Compacting soon", Level: "warning"})
	if len(x4.c.stack) != 0 {
		t.Fatal("unrelated informational opened a dialog")
	}
	x4.event(&proto.Informational{Content: billingText, Level: "warning"})
	x4.press("esc")
	env4, _ := x4.st.env()
	if len(x4.c.stack) != 0 || gates.LoadGateStore(env4).BillingNoticeAt != "" {
		t.Fatal("esc closes without recording")
	}
}

func billingDialogMsg(id string, got *[]string) ext.ControlRequestMsg {
	raw := `{"subtype":"request_user_dialog","dialog_kind":"auto_mode_server_fallback","payload":{` +
		`"title":"Auto mode billing is changing","paragraphs":["This session keeps the old billing.",` +
		`"Details: https://code.claude.com/docs/en/auto-mode-classifier-billing"],` +
		`"helpUrl":"https://code.claude.com/docs/en/auto-mode-classifier-billing"}}`
	return ext.ControlRequestMsg{EngineID: ext.MainEngine, Subtype: proto.SubRequestUserDialog, RequestID: id,
		Request: json.RawMessage(raw),
		Reply: func(resp any, err error) tea.Cmd {
			b, _ := json.Marshal(resp)
			*got = append(*got, string(b))
			return nil
		}}
}

func TestBillingUserDialog(t *testing.T) {
	if strings.Join(DialogKinds(), ",") != "auto_mode_server_fallback" {
		t.Fatalf("declared kinds = %v", DialogKinds())
	}
	x := newH(t)
	var got []string
	x.send(billingDialogMsg("d1", &got))
	view := testkitStrip(x.view(100))
	if x.c.topID() != DialogBillingNotice || !strings.Contains(view, "Auto mode billing is changing") ||
		!strings.Contains(view, "This session keeps the old billing.") || strings.Contains(view, "1.") {
		t.Fatalf("billing dialog:\n%s", view)
	}
	x.press("enter")
	if len(got) != 1 || got[0] != `{"behavior":"completed","result":"continue"}` || len(x.c.stack) != 0 {
		t.Fatalf("enter: %v %v", got, x.c.ids)
	}
	// The engine decides how often; mantle records nothing and the informational
	// fallback stays quiet once the dialog was shown.
	env, _ := x.st.env()
	if gates.LoadGateStore(env).BillingNoticeAt != "" {
		t.Fatal("the engine records the acknowledgement, not mantle")
	}
	x.event(&proto.Informational{Content: billingText, Level: "warning"})
	if len(x.c.stack) != 0 {
		t.Fatal("informational after the dialog opened another notice")
	}
	x.send(billingDialogMsg("d2", &got))
	x.press("esc")
	if len(got) != 2 || got[1] != `{"behavior":"completed","result":"interrupt"}` {
		t.Fatalf("esc: %v", got)
	}
	x.send(billingDialogMsg("d3", &got))
	x.press("ctrl+c")
	if len(got) != 3 || got[2] != `{"behavior":"completed","result":"interrupt"}` || len(x.c.stack) != 0 {
		t.Fatalf("ctrl+c: %v %v", got, x.c.ids)
	}
}
