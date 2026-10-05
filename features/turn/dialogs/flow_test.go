package dialogs

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/turn/gates"
)

// key builds a key press from its keystroke name ("enter", "shift+tab", "ctrl+g", "x").
func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	}
	if mod, rest, ok := strings.Cut(s, "+"); ok && mod == "ctrl" && len(rest) == 1 {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

// press sends keys in order and returns the last effect. Keys of the form "'text'" are
// typed rune by rune.
func press(t *testing.T, m Model, keys ...string) Effect {
	t.Helper()
	var eff Effect
	for _, k := range keys {
		if strings.HasPrefix(k, "'") && strings.HasSuffix(k, "'") && len(k) >= 2 {
			for _, r := range k[1 : len(k)-1] {
				_, eff = m.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
			continue
		}
		_, eff = m.HandleKey(key(k))
	}
	return eff
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantJSON(t *testing.T, got any, want string) {
	t.Helper()
	g := toJSON(t, got)
	var a, b any
	if err := json.Unmarshal([]byte(g), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatalf("bad want JSON: %v", err)
	}
	if toJSON(t, a) != toJSON(t, b) {
		t.Fatalf("response\n got: %s\nwant: %s", g, want)
	}
}

func toolReq(t *testing.T, raw string) ToolRequest {
	t.Helper()
	var r ToolRequest
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

const bashReq = `{"tool_name":"Bash","tool_use_id":"tu1",
 "input":{"command":"npm test","description":"Run tests"},
 "permission_suggestions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"npm test:*"}],"behavior":"allow","destination":"localSettings","future":"kept"}]}`

func TestPermissionYes(t *testing.T) {
	p := NewPermission(toolReq(t, bashReq), PermissionContext{})
	if eff := press(t, p, "enter"); eff != Answered || !p.Done() {
		t.Fatalf("enter should answer, eff=%v", eff)
	}
	wantJSON(t, p.Response(), `{"behavior":"allow","updatedInput":{"command":"npm test","description":"Run tests"},
		"toolUseID":"tu1","decisionClassification":"user_temporary"}`)
}

func optionIDs(p *Permission) string {
	var ids []string
	for _, o := range p.opts.items {
		ids = append(ids, o.id)
	}
	return strings.Join(ids, ",")
}

func TestPermissionAlwaysSendsSuggestionVerbatim(t *testing.T) {
	p := NewPermission(toolReq(t, bashReq), PermissionContext{Cwd: "/w/proj"})
	if got := p.opts.items[1].label; got != "Yes, and don't ask again for `npm test` commands in proj" {
		t.Fatalf("label = %q", got)
	}
	press(t, p, "down", "enter")
	// The unknown "future" field proves the suggestion is echoed byte for byte.
	wantJSON(t, p.Response(), `{"behavior":"allow","updatedInput":{"command":"npm test","description":"Run tests"},
		"updatedPermissions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"npm test:*"}],"behavior":"allow","destination":"localSettings","future":"kept"}],
		"toolUseID":"tu1","decisionClassification":"user_permanent"}`)
}

// The parity case (claude 2.1.289): a Bash call blocked by a path check offers only
// the directory grant, and sends back only that; no accept-edits option on a Bash
// prompt.
const blockedBashReq = `{"tool_name":"Bash","tool_use_id":"tb","blocked_path":"/w/proj/marker.txt",
 "input":{"command":"touch marker.txt","description":"Create a marker file"},
 "permission_suggestions":[
  {"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"touch marker.txt"}],"behavior":"allow","destination":"localSettings"},
  {"type":"addDirectories","directories":["/w/proj"],"destination":"session"},
  {"type":"setMode","mode":"acceptEdits","destination":"session"}]}`

func TestPermissionFiltersSuggestionsLikeClaude(t *testing.T) {
	p := NewPermission(toolReq(t, blockedBashReq), PermissionContext{Cwd: "/w/proj", OfferAuto: true})
	if got := optionIDs(p); got != "yes,always,auto,no" {
		t.Fatalf("options = %s", got)
	}
	if got := p.opts.items[1].label; got != "Yes, and always allow access to /w/proj in this project" {
		t.Fatalf("label = %q", got)
	}
	press(t, p, "2")
	wantJSON(t, p.Response().UpdatedPermissions, `[{"type":"addDirectories","directories":["/w/proj"],"destination":"session"}]`)

	// Without the path check the command rule shows too, with the directory.
	req := toolReq(t, blockedBashReq)
	req.BlockedPath = ""
	p = NewPermission(req, PermissionContext{Cwd: "/w/proj"})
	if got := p.opts.items[1].label; got != "Yes, and allow /w/proj and `touch marker.txt`" {
		t.Fatalf("label = %q", got)
	}
	// Grants to shared or user settings, and deny rules, are never offered.
	req = toolReq(t, `{"tool_name":"Bash","tool_use_id":"x","input":{"command":"ls"},"permission_suggestions":[
	  {"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"ls"}],"behavior":"allow","destination":"userSettings"},
	  {"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"ls"}],"behavior":"deny","destination":"session"}]}`)
	if got := optionIDs(NewPermission(req, PermissionContext{})); got != "yes,no" {
		t.Fatalf("options = %s", got)
	}
}

func TestPermissionDigitPicks(t *testing.T) {
	p := NewPermission(toolReq(t, bashReq), PermissionContext{})
	press(t, p, "2")
	if !p.Done() || len(p.Response().UpdatedPermissions) != 1 {
		t.Fatalf("digit 2 should pick 'always': %+v", p.Response())
	}
	p = NewPermission(toolReq(t, bashReq), PermissionContext{})
	press(t, p, "9") // out of range: ignored
	if p.Done() {
		t.Fatal("out-of-range digit answered")
	}
}

func TestPermissionEscDeniesAndInterrupts(t *testing.T) {
	p := NewPermission(toolReq(t, bashReq), PermissionContext{})
	if eff := press(t, p, "esc"); eff != Answered {
		t.Fatal("esc should answer")
	}
	wantJSON(t, p.Response(), `{"behavior":"deny","message":"The user denied this tool call and stopped the turn.","interrupt":true,"toolUseID":"tu1","decisionClassification":"user_reject"}`)
}

func TestPermissionNoEndsTheTurn(t *testing.T) {
	p := NewPermission(toolReq(t, bashReq), PermissionContext{})
	press(t, p, "3") // Yes, always, No
	r := p.Response()
	if r == nil || r.Behavior != "deny" || !r.Interrupt {
		t.Fatalf("No denies and interrupts: %+v", r)
	}
}

func TestPermissionTabAddsInstructionsToNo(t *testing.T) {
	p := NewPermission(toolReq(t, bashReq), PermissionContext{})
	press(t, p, "tab")
	if p.Done() || p.stage != permStageFeedback || p.opts.selected().id != "no" {
		t.Fatal("tab should focus No and open its instructions")
	}
	press(t, p, "'use yarn'", "backspace", "'n'")
	// esc closes the field, keeping the text; tab reopens it.
	press(t, p, "esc")
	if p.Done() || p.stage != permStageOptions {
		t.Fatal("esc in the field should go back")
	}
	press(t, p, "tab", "enter")
	wantJSON(t, p.Response(), `{"behavior":"deny","message":"The user denied this tool call and said: use yarn","toolUseID":"tu1","decisionClassification":"user_reject"}`)
}

func TestPermissionDefaultToNoAndOneTime(t *testing.T) {
	req := toolReq(t, bashReq)
	req.DefaultToNo = true
	req.SuppressAlwaysAllowRule = true
	p := NewPermission(req, PermissionContext{OfferAuto: true})
	if got := optionIDs(p); got != "yes,auto,no" {
		t.Fatalf("one-time prompt options = %s", got)
	}
	if p.opts.selected().id != "no" {
		t.Fatal("default_to_no should focus No")
	}
	press(t, p, "enter")
	if r := p.Response(); r == nil || r.Behavior != "deny" {
		t.Fatal("enter on the default denies, never allows")
	}
}

func TestPermissionEditOptionsAndShiftTab(t *testing.T) {
	edit := toolReq(t, `{"tool_name":"Edit","tool_use_id":"e1",
	  "input":{"file_path":"/w/a.go","old_string":"a","new_string":"b"},
	  "permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}]}`)
	p := NewPermission(edit, PermissionContext{Cwd: "/w", OfferAuto: true})
	if got := optionIDs(p); got != "yes,always,no" {
		t.Fatalf("file prompts offer no auto switch: %s", got)
	}
	if got := p.opts.items[1].label; got != "Yes, and switch to accept edits for this session" || p.opts.items[1].hint != "(shift+tab)" {
		t.Fatalf("session option = %q %q", got, p.opts.items[1].hint)
	}
	if eff := press(t, p, "shift+tab"); eff != Answered {
		t.Fatal("shift+tab should take the session option")
	}
	wantJSON(t, p.Response(), `{"behavior":"allow","updatedInput":{"file_path":"/w/a.go","old_string":"a","new_string":"b"},
		"updatedPermissions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],
		"toolUseID":"e1","decisionClassification":"user_temporary"}`)

	p = NewPermission(toolReq(t, bashReq), PermissionContext{})
	if eff := press(t, p, "shift+tab"); eff != CycleMode || p.Done() {
		t.Fatalf("without a session grant shift+tab cycles the mode, eff=%v", eff)
	}
}

func TestPermissionOfferAuto(t *testing.T) {
	p := NewPermission(toolReq(t, bashReq), PermissionContext{OfferAuto: true})
	press(t, p, "3")
	wantJSON(t, p.Response().UpdatedPermissions, `[{"type":"setMode","mode":"auto","destination":"session"}]`)
}

func TestPermissionActions(t *testing.T) {
	p := NewPermission(toolReq(t, bashReq), PermissionContext{})
	p.Action("confirm:next")
	if _, eff := p.Action("confirm:yes"); eff != Answered || len(p.Response().UpdatedPermissions) != 1 {
		t.Fatal("confirm:next + confirm:yes should pick 'always'")
	}
	p = NewPermission(toolReq(t, bashReq), PermissionContext{})
	if _, eff := p.Action("confirm:no"); eff != Answered || p.Response().Behavior != "deny" {
		t.Fatal("confirm:no should deny")
	}
	if h, _ := p.HandleKey(key("enter")); h {
		t.Fatal("an answered dialog must ignore keys")
	}
}

func TestPermissionSanitizesInput(t *testing.T) {
	req := toolReq(t, `{"tool_name":"Bash","tool_use_id":"x","input":{"command":"echo hi\u001b[2J\u001b]0;pwn\u0007 ‮rm -rf"}}`)
	v := NewPermission(req, PermissionContext{}).View(80, PlainStyles())
	if strings.ContainsAny(v, "\x1b\x07‮") {
		t.Fatalf("control sequences leaked into the view: %q", v)
	}
	if !strings.Contains(v, "<U+202E>") {
		t.Fatal("bidi override should be shown visibly")
	}
}

func TestDenyHelpersNeverAllow(t *testing.T) {
	p := NewPermission(toolReq(t, bashReq), PermissionContext{})
	if p.Deny().Behavior != "deny" {
		t.Fatal("Deny must deny")
	}
}

const questionsReq = `{"tool_name":"AskUserQuestion","tool_use_id":"q1","input":{"questions":[
 {"question":"Which package manager?","header":"Manager","multiSelect":false,
  "options":[{"label":"npm","description":"Default"},{"label":"yarn","description":"Classic","preview":"# yarn\nfast"}]},
 {"question":"Which checks?","header":"Checks","multiSelect":true,
  "options":[{"label":"lint","description":""},{"label":"test","description":""},{"label":"build","description":""}]}
],"extra":1}}`

func TestAskQuestionMulti(t *testing.T) {
	a, err := NewAskQuestion(toolReq(t, questionsReq), PermissionContext{})
	if err != nil {
		t.Fatal(err)
	}
	press(t, a, "down", "enter") // yarn → next tab
	if a.tab != 1 {
		t.Fatalf("tab = %d", a.tab)
	}
	press(t, a, "space", "down", "down", "enter") // lint, build (enter toggles in multi)
	// Other with free text, included alongside the picks.
	press(t, a, "down", "enter", "'docs'", "enter")
	press(t, a, "down", "enter") // Review →
	if a.tab != 2 {
		t.Fatalf("should be on review, tab=%d", a.tab)
	}
	if eff := press(t, a, "enter"); eff != Answered {
		t.Fatal("submit should answer")
	}
	wantJSON(t, a.Response(), `{"behavior":"allow","updatedInput":{"extra":1,"questions":[
	 {"question":"Which package manager?","header":"Manager","multiSelect":false,
	  "options":[{"label":"npm","description":"Default"},{"label":"yarn","description":"Classic","preview":"# yarn\nfast"}]},
	 {"question":"Which checks?","header":"Checks","multiSelect":true,
	  "options":[{"label":"lint","description":""},{"label":"test","description":""},{"label":"build","description":""}]}],
	 "answers":{"Which package manager?":"yarn","Which checks?":"lint, build, docs"}},
	 "toolUseID":"q1","decisionClassification":"user_temporary"}`)
}

func TestAskQuestionSingleSubmitsImmediately(t *testing.T) {
	req := toolReq(t, `{"tool_name":"AskUserQuestion","tool_use_id":"q2","input":{"questions":[
	  {"question":"Pick","header":"P","multiSelect":false,"options":[{"label":"A","description":""},{"label":"B","description":""}]}]}}`)
	a, _ := NewAskQuestion(req, PermissionContext{})
	if eff := press(t, a, "2"); eff != Answered {
		t.Fatal("a lone single-choice question submits on selection")
	}
	if got := a.Answers()["Pick"]; got != "B" {
		t.Fatalf("answer = %q", got)
	}

	a, _ = NewAskQuestion(req, PermissionContext{})
	press(t, a, "3", "'my own'", "enter") // Other
	if got := a.Answers()["Pick"]; got != "my own" || !a.Done() {
		t.Fatalf("other answer = %q done=%v", got, a.Done())
	}
}

func TestAskQuestionReviewJumpsToUnanswered(t *testing.T) {
	a, _ := NewAskQuestion(toolReq(t, questionsReq), PermissionContext{})
	press(t, a, "tab", "tab") // skip both questions to the review tab
	if a.tab != 2 {
		t.Fatalf("tab = %d", a.tab)
	}
	press(t, a, "enter")
	if a.Done() || a.tab != 0 {
		t.Fatalf("submit with no answers should jump to question 1, tab=%d", a.tab)
	}
	press(t, a, "shift+tab") // stays at the first tab
	if a.tab != 0 {
		t.Fatal("shift+tab past the first tab")
	}
}

func TestAskQuestionEscDenies(t *testing.T) {
	a, _ := NewAskQuestion(toolReq(t, questionsReq), PermissionContext{})
	press(t, a, "esc")
	if r := a.Response(); r == nil || r.Behavior != "deny" || r.Interrupt {
		t.Fatalf("esc: %+v", r)
	}
	// Esc while typing Other only leaves the field.
	a, _ = NewAskQuestion(toolReq(t, questionsReq), PermissionContext{})
	press(t, a, "3", "'x'", "esc")
	if a.Done() || a.editing {
		t.Fatal("esc in Other should only stop editing")
	}
}

func TestAskQuestionErrors(t *testing.T) {
	if _, err := NewAskQuestion(toolReq(t, `{"tool_name":"AskUserQuestion","input":{"questions":[]}}`), PermissionContext{}); err == nil {
		t.Fatal("empty questions should fail")
	}
}

const planReq = `{"tool_name":"ExitPlanMode","tool_use_id":"p1","input":{"plan":"# Plan\n1. Do it","planFilePath":"/w/plan.md"}}`

func TestPlanApprove(t *testing.T) {
	p := NewPlanApproval(toolReq(t, planReq), PlanContext{AutoAvailable: true, BypassAvailable: true})
	var ids []string
	for _, o := range p.opts.items {
		ids = append(ids, o.id)
	}
	if strings.Join(ids, ",") != "auto,acceptEdits,bypassPermissions,default,keep" {
		t.Fatalf("options = %v", ids)
	}
	press(t, p, "2")
	wantJSON(t, p.Response(), `{"behavior":"allow","updatedInput":{"plan":"# Plan\n1. Do it","planFilePath":"/w/plan.md"},
		"updatedPermissions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],
		"toolUseID":"p1","decisionClassification":"user_temporary"}`)

	p = NewPlanApproval(toolReq(t, planReq), PlanContext{})
	press(t, p, "down", "enter") // without auto/bypass: acceptEdits, default, keep
	if p.Response().UpdatedPermissions[0].Mode != "default" {
		t.Fatalf("manual approval mode = %+v", p.Response().UpdatedPermissions)
	}
}

func TestPlanKeepPlanning(t *testing.T) {
	p := NewPlanApproval(toolReq(t, planReq), PlanContext{})
	press(t, p, "3", "'split step 1'", "enter")
	wantJSON(t, p.Response(), `{"behavior":"deny","message":"The user rejected the plan and wants to keep planning. Their feedback: split step 1",
		"toolUseID":"p1","decisionClassification":"user_reject"}`)

	p = NewPlanApproval(toolReq(t, planReq), PlanContext{})
	press(t, p, "esc")
	if r := p.Response(); r.Behavior != "deny" || r.Interrupt {
		t.Fatalf("esc keeps planning without interrupt: %+v", r)
	}
}

func TestPlanEditExternal(t *testing.T) {
	p := NewPlanApproval(toolReq(t, planReq), PlanContext{})
	if eff := press(t, p, "ctrl+g"); eff != EditExternal || p.Done() {
		t.Fatal("ctrl+g should ask the host to open the editor")
	}
	if p.EditText() != "# Plan\n1. Do it" || p.PlanFilePath() != "/w/plan.md" {
		t.Fatal("edit text / path")
	}
	p.SetEditedText("# Plan\n1. Do it better")
	press(t, p, "enter")
	wantJSON(t, p.Response().UpdatedInput, `{"plan":"# Plan\n1. Do it better","planFilePath":"/w/plan.md"}`)
}

func TestPlanScroll(t *testing.T) {
	long := strings.Repeat("line\n", 50)
	req := toolReq(t, `{"tool_name":"ExitPlanMode","tool_use_id":"p","input":{"plan":`+toJSON(t, long)+`}}`)
	p := NewPlanApproval(req, PlanContext{PlanLines: 10})
	v := p.View(60, PlainStyles())
	if !strings.Contains(v, "↓ 40 more lines") {
		t.Fatalf("missing scroll hint:\n%s", v)
	}
	press(t, p, "pgdown")
	if v := p.View(60, PlainStyles()); !strings.Contains(v, "↑ 9 more lines") {
		t.Fatalf("pgdown didn't scroll:\n%s", v)
	}
}

const formSchema = `{"type":"object","properties":{
  "name":{"type":"string","title":"Name","minLength":2},
  "age":{"type":"integer","minimum":0},
  "subscribe":{"type":"boolean","default":true},
  "color":{"type":"string","enum":["red","green"],"enumNames":["Red","Green"]}
},"required":["name","age"]}`

func TestElicitationForm(t *testing.T) {
	e := NewElicitation(ElicitationRequest{McpServerName: "srv", Message: "Who are you?", RequestedSchema: json.RawMessage(formSchema)}, PermissionContext{})
	if len(e.fields) != 4 || e.fields[0].name != "name" || e.fields[3].name != "color" {
		t.Fatalf("schema order lost: %+v", e.fields)
	}
	press(t, e, "'A'", "enter", "'x'", "tab", "space", "tab", "right", "right", "tab", "enter")
	// name too short and age not a number: submit refuses and jumps to the first error.
	if e.Done() || e.row != 0 || e.fields[0].err == "" || e.fields[1].err == "" {
		t.Fatalf("validation should block: row=%d errs=%q,%q", e.row, e.fields[0].err, e.fields[1].err)
	}
	press(t, e, "'da'", "tab", "backspace", "'42'", "down", "down", "down", "enter")
	wantJSON(t, e.Response(), `{"action":"accept","content":{"name":"Ada","age":42,"subscribe":false,"color":"green"}}`)
}

func TestElicitationDeclineCancel(t *testing.T) {
	req := ElicitationRequest{McpServerName: "srv", Message: "m", RequestedSchema: json.RawMessage(formSchema)}
	e := NewElicitation(req, PermissionContext{})
	press(t, e, "down", "down", "down", "down", "down", "enter")
	wantJSON(t, e.Response(), `{"action":"decline"}`)
	e = NewElicitation(req, PermissionContext{})
	press(t, e, "esc")
	wantJSON(t, e.Response(), `{"action":"cancel"}`)
}

func TestElicitationURL(t *testing.T) {
	e := NewElicitation(ElicitationRequest{McpServerName: "srv", Message: "Log in", Mode: "url", URL: "https://auth.test/x"}, PermissionContext{})
	if eff := press(t, e, "enter"); eff != OpenURL || e.URL() != "https://auth.test/x" {
		t.Fatalf("url mode enter: %v", eff)
	}
	wantJSON(t, e.Response(), `{"action":"accept"}`)
	e = NewElicitation(ElicitationRequest{Mode: "url", URL: "u"}, PermissionContext{})
	press(t, e, "2")
	wantJSON(t, e.Response(), `{"action":"decline"}`)
}

func TestElicitationBadSchema(t *testing.T) {
	e := NewElicitation(ElicitationRequest{McpServerName: "srv", RequestedSchema: json.RawMessage(`{"properties":[1]}`)}, PermissionContext{})
	if e.err == nil {
		t.Fatal("bad schema should be reported")
	}
	press(t, e, "enter") // only Decline/Cancel exist
	wantJSON(t, e.Response(), `{"action":"decline"}`)
}

func TestGateChoices(t *testing.T) {
	tr := NewTrust(gates.TrustReport{Dir: "/w"}, false)
	press(t, tr, "enter")
	if tr.Chosen() != ChoiceYes {
		t.Fatal("trust enter = yes")
	}
	tr = NewTrust(gates.TrustReport{Dir: "/w"}, false)
	press(t, tr, "esc")
	if tr.Chosen() != ChoiceNo {
		t.Fatal("trust esc = no")
	}
	b := NewBypassWarning()
	press(t, b, "enter")
	if b.Chosen() != ChoiceNo {
		t.Fatal("bypass defaults to No")
	}
	b = NewBypassWarning()
	press(t, b, "2")
	if b.Chosen() != ChoiceYes {
		t.Fatal("bypass 2 = accept")
	}
	k := NewAPIKeyPrompt("ABCDEFGHIJKLMNOPQRST")
	press(t, k, "enter")
	if k.Chosen() != ChoiceNo {
		t.Fatal("api key defaults to No")
	}
	am := NewAutoModePrompt()
	press(t, am, "up", "enter")
	if am.Chosen() != ChoiceYes {
		t.Fatal("auto mode up+enter = yes")
	}
}

func TestMcpApprovalDialog(t *testing.T) {
	one := []gates.McpServer{{Name: "db", Transport: "stdio", Command: "npx"}}
	m := NewMcpApproval(one)
	press(t, m, "enter")
	if !m.Approved()["db"] || !m.EnableAll() {
		t.Fatal("first option approves all future servers")
	}
	m = NewMcpApproval(one)
	press(t, m, "2")
	if !m.Approved()["db"] || m.EnableAll() {
		t.Fatal("second option approves only this server")
	}
	m = NewMcpApproval(one)
	press(t, m, "esc")
	if m.Approved()["db"] || !m.Done() {
		t.Fatal("esc approves nothing")
	}

	many := []gates.McpServer{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	m = NewMcpApproval(many)
	press(t, m, "down", "space", "enter")
	got := m.Approved()
	if !got["a"] || got["b"] || !got["c"] {
		t.Fatalf("checklist result = %v", got)
	}
}
