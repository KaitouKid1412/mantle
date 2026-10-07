package turn

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/features/turn/gates"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestFeatureRegistration(t *testing.T) {
	r, err := exttest.Setup(ext.Feature{ID: FeatureID, Setup: func(r ext.Registrar) error { newState().setup(r); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{DialogPermission, DialogAskUserQuestion, DialogPlanApproval, DialogElicitation,
		DialogTrust, DialogMcpApproval, DialogBypassWarning, DialogAPIKey, DialogUsageLimit, DialogEngineCheck, DialogBillingNotice} {
		if r.Dialogs[id] == nil {
			t.Errorf("dialog %s not registered", id)
		}
	}
	acts := map[ext.ActionID]bool{}
	for _, a := range r.Actions {
		acts[a.ID] = true
	}
	for _, a := range []ext.ActionID{ext.ActChatCycleMode, ext.ActChatCancel, ext.ActTaskBackground, ext.ActChatKillAgents} {
		if !acts[a] {
			t.Errorf("action %s not registered", a)
		}
	}
	for _, a := range []ext.ActionID{ext.ActAppInterrupt, ext.ActAppExit} {
		if acts[a] || len(r.Wrapped[string(a)]) != 1 {
			t.Errorf("%s should wrap the core action, not replace it", a)
		}
	}
	if _, ok := r.Command("exit"); !ok {
		t.Error("/exit not registered")
	}
	if len(r.Components) != 1 || r.Components[0].Slot != ext.SlotAboveInput {
		t.Errorf("queue component: %+v", r.Components)
	}
}

func TestStoriesRender(t *testing.T) {
	x := newH(t)
	if len(x.reg.Stories) < 10 {
		t.Fatalf("stories = %d", len(x.reg.Stories))
	}
	for _, s := range x.reg.Stories {
		for _, w := range testkit.DefaultWidths {
			out, err := testkit.RenderStory(s, w)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(out) == "" {
				t.Fatalf("story %s rendered nothing at %d", s.ID, w)
			}
		}
	}
}

func TestCycleMode(t *testing.T) {
	x := newH(t)
	x.action(ext.ActChatCycleMode)
	if x.c.SessionValue.PermissionMode != proto.ModeAcceptEdits {
		t.Fatalf("mode = %q", x.c.SessionValue.PermissionMode)
	}
	x.action(ext.ActChatCycleMode)
	x.action(ext.ActChatCycleMode)
	if x.c.SessionValue.PermissionMode != proto.ModeDefault {
		t.Fatalf("plan → default without auto/bypass, got %q", x.c.SessionValue.PermissionMode)
	}
	if x.eng.count(proto.SubSetPermissionMode) != 3 {
		t.Fatalf("controls = %v", x.eng.controls)
	}
}

// Claude Code 2.1.289 goes from plan straight into auto: no first-use prompt.
func TestCycleModeIntoAutoDirectly(t *testing.T) {
	x := newH(t)
	x.st.mode(ext.MainEngine).auto = true
	x.c.SessionValue.PermissionMode = proto.ModePlan
	x.action(ext.ActChatCycleMode)
	if x.c.topID() != "" || x.c.SessionValue.PermissionMode != proto.ModeAuto {
		t.Fatalf("stack=%v mode=%q", x.c.ids, x.c.SessionValue.PermissionMode)
	}
}

func TestCycleModeFailureDisablesAuto(t *testing.T) {
	x := newH(t)
	ms := x.st.mode(ext.MainEngine)
	ms.auto = true
	x.c.SessionValue.PermissionMode = proto.ModePlan
	x.eng.errs[proto.SubSetPermissionMode] = errors.New("invalid_mode")
	x.action(ext.ActChatCycleMode)
	if ms.auto || len(x.c.Notices) == 0 {
		t.Fatalf("auto should be marked unavailable with a notice: %+v", x.c.Notices)
	}
}

// withResearch registers /research, as plan 13 does, so the research step appears in
// the cycle.
func (x *h) withResearch() {
	x.c.CommandList = append(x.c.CommandList, ext.Command{Name: "research"})
}

func (x *h) uiRequests() []ext.UIModeRequestMsg { return find[ext.UIModeRequestMsg](x) }

// MT-R1: default → research → acceptEdits. Entering research is a UI-mode request only.
func TestCycleModeThroughResearch(t *testing.T) {
	x := newH(t)
	x.withResearch()
	x.action(ext.ActChatCycleMode)
	if r := x.uiRequests(); len(r) != 1 || r[0].Mode != ext.UIModeResearch {
		t.Fatalf("ui requests = %+v", r)
	}
	if n := x.eng.count(proto.SubSetPermissionMode); n != 0 {
		t.Fatalf("entering research sent set_permission_mode: %v", x.eng.controls)
	}
	if x.st.mode(ext.MainEngine).userMode != proto.ModeDefault {
		t.Fatalf("userMode = %q", x.st.mode(ext.MainEngine).userMode)
	}
	x.send(ext.UIModeChangedMsg{Mode: ext.UIModeResearch})

	x.action(ext.ActChatCycleMode)
	if r := x.uiRequests(); len(r) != 2 || r[1].Mode != "" {
		t.Fatalf("leaving research should request mode \"\": %+v", r)
	}
	if x.c.SessionValue.PermissionMode != proto.ModeAcceptEdits {
		t.Fatalf("mode = %q", x.c.SessionValue.PermissionMode)
	}
	x.send(ext.UIModeChangedMsg{Mode: "", Prev: ext.UIModeResearch})

	x.action(ext.ActChatCycleMode) // → plan
	x.action(ext.ActChatCycleMode) // → default (manual)
	if x.c.SessionValue.PermissionMode != proto.ModeDefault || len(x.uiRequests()) != 2 {
		t.Fatalf("mode=%q ui=%+v", x.c.SessionValue.PermissionMode, x.uiRequests())
	}
	x.action(ext.ActChatCycleMode) // → research again
	if r := x.uiRequests(); len(r) != 3 || r[2].Mode != ext.UIModeResearch {
		t.Fatalf("ui requests = %+v", r)
	}
}

// Without /research (a mod removed the feature) the cycle skips the step.
func TestCycleModeWithoutResearch(t *testing.T) {
	x := newH(t)
	x.action(ext.ActChatCycleMode)
	if x.c.SessionValue.PermissionMode != proto.ModeAcceptEdits || len(x.uiRequests()) != 0 {
		t.Fatalf("mode=%q ui=%+v", x.c.SessionValue.PermissionMode, x.uiRequests())
	}
}

// shift+tab inside a dialog keeps the plain cycle, even with research available.
func TestDialogCycleSkipsResearch(t *testing.T) {
	x := newH(t)
	x.withResearch()
	x.send(permMsg(ext.MainEngine, "r1", bashRaw, &replies{}))
	x.press("shift+tab")
	if x.c.SessionValue.PermissionMode != proto.ModeAcceptEdits || len(x.uiRequests()) != 0 {
		t.Fatalf("mode=%q ui=%+v", x.c.SessionValue.PermissionMode, x.uiRequests())
	}
}

// An engine restart while research is on keeps manual, not the auto startup default.
func TestResearchRestartKeepsManual(t *testing.T) {
	x := newH(t)
	x.withResearch()
	x.c.SessionValue.Model = "claude-x"
	x.action(ext.ActChatCycleMode)
	x.send(ext.UIModeChangedMsg{Mode: ext.UIModeResearch})
	x.boot(autoModels)
	if n := x.eng.count(proto.SubSetPermissionMode); n != 0 {
		t.Fatalf("restart in research switched mode: %v", x.eng.controls)
	}
}

// Research entered without the cycle (/research, --research) still runs in manual
// mode: at startup instead of auto, and mid-session by switching.
func TestResearchEnteredElsewhereIsManual(t *testing.T) {
	x := newH(t)
	x.c.SessionValue.Model = "claude-x"
	x.send(ext.UIModeChangedMsg{Mode: ext.UIModeResearch}) // --research, before the engine
	x.boot(autoModels)
	if n := x.eng.count(proto.SubSetPermissionMode); n != 0 {
		t.Fatalf("startup in research left manual: %v", x.eng.controls)
	}

	y := newH(t)
	y.c.SessionValue.Model = "claude-x"
	y.boot(autoModels) // starts in auto
	if y.c.SessionValue.PermissionMode != proto.ModeAuto {
		t.Fatalf("mode = %q", y.c.SessionValue.PermissionMode)
	}
	y.send(ext.UIModeChangedMsg{Mode: ext.UIModeResearch}) // /research
	if got := y.eng.lastControl(proto.SubSetPermissionMode); got != (proto.SetPermissionModeRequest{Mode: proto.ModeDefault}) {
		t.Fatalf("set_permission_mode = %+v", got)
	}
}

// boot is what the engine bridge delivers when an engine starts: the attach, then the
// initialize result.
func (x *h) boot(initResp string) {
	x.t.Helper()
	x.send(ext.EngineAttachMsg{EngineID: ext.MainEngine, Engine: x.eng})
	x.send(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubInitialize, Resp: json.RawMessage(initResp)})
}

const autoModels = `{"models":[{"value":"opus","resolvedModel":"claude-x","displayName":"Opus","supportsAutoMode":true}]}`

func TestStartupModeMirrorsInteractive(t *testing.T) {
	x := newH(t)
	x.c.SessionValue.Model = "claude-x"
	// Nothing configured and auto available: start in auto and say so, once.
	x.boot(autoModels)
	if got := x.eng.lastControl(proto.SubSetPermissionMode); got != (proto.SetPermissionModeRequest{Mode: proto.ModeAuto}) {
		t.Fatalf("startup mode = %#v", got)
	}
	if len(x.c.Printed) != 1 || !strings.Contains(x.c.Printed[0], "Auto mode is now the default") {
		t.Fatalf("notice = %q", x.c.Printed)
	}
	env, _ := x.st.env()
	if gates.LoadGateStore(env).AutoNoticeAt == "" {
		t.Fatal("the notice should be recorded")
	}
	x2 := newH(t)
	x2.st.env = x.st.env // same HOME: the notice was already shown
	x2.c.SessionValue.Model = "claude-x"
	x2.boot(autoModels)
	if len(x2.c.Printed) != 0 || x2.eng.lastControl(proto.SubSetPermissionMode) == nil {
		t.Fatalf("second launch: printed=%q", x2.c.Printed)
	}
	// A user's later choice survives an engine restart, without the notice.
	x.action(ext.ActChatCycleMode) // auto → default
	x.c.SessionValue.PermissionMode = proto.ModeDefault
	x.boot(autoModels)
	if got := x.eng.lastControl(proto.SubSetPermissionMode); got != (proto.SetPermissionModeRequest{Mode: proto.ModeDefault}) {
		t.Fatalf("restart restores %#v", got)
	}
	// No auto support: stay in default, no notice.
	x3 := newH(t)
	x3.boot(`{"models":[{"value":"m","displayName":"M"}]}`)
	if x3.eng.count(proto.SubSetPermissionMode) != 0 || len(x3.c.Printed) != 0 {
		t.Fatalf("no auto: controls=%v printed=%q", x3.eng.controls, x3.c.Printed)
	}
}

func TestStartupModeHonoursSettings(t *testing.T) {
	x := newH(t)
	x.c.SettingsV = exttest.NewSettings(map[string]any{"permissions": map[string]any{"defaultMode": "acceptEdits"}})
	x.boot(`{"models":[]}`)
	if got := x.eng.lastControl(proto.SubSetPermissionMode); got != (proto.SetPermissionModeRequest{Mode: proto.ModeAcceptEdits}) {
		t.Fatalf("startup mode = %#v", got)
	}
}

// The engine's first stdin traffic must be the bridge's handshake and the user's first
// prompt: attaching sends nothing, and nothing is sent before initialize finished.
func TestAttachSendsNoControlRequest(t *testing.T) {
	x := newH(t)
	x.send(ext.EngineAttachMsg{EngineID: ext.MainEngine, Engine: x.eng})
	if len(x.eng.controls) != 0 {
		t.Fatalf("attach sent %v", x.eng.controls)
	}
	// A failed handshake still unblocks the startup mode (auto unavailable).
	x.c.SettingsV = exttest.NewSettings(map[string]any{"permissions": map[string]any{"defaultMode": "plan"}})
	x.send(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubInitialize, Err: errors.New("timeout")})
	if got := x.eng.lastControl(proto.SubSetPermissionMode); got != (proto.SetPermissionModeRequest{Mode: proto.ModePlan}) {
		t.Fatalf("startup mode after a failed handshake = %#v", got)
	}
}

func TestEscInterruptsOnlyWhileRunning(t *testing.T) {
	x := newH(t)
	if x.action(ext.ActChatCancel) {
		t.Fatal("esc while idle must fall through to the editor")
	}
	x.event(&proto.SystemInit{})
	if !x.c.ActiveCtxs[ext.ContextTask] {
		t.Fatal("Task context should be active while a turn runs")
	}
	x.press("esc")
	if len(x.eng.interrupts) != 1 || x.eng.interrupts[0] {
		t.Fatalf("interrupts = %v", x.eng.interrupts)
	}
	x.event(&proto.Result{})
	if x.c.ActiveCtxs[ext.ContextTask] || x.action(ext.ActChatCancel) {
		t.Fatal("the turn ended")
	}
}

// S1: an interrupt mid-text or mid-tool ends the turn with an aborted result; the turn
// state (and the Task context) must clear so esc and ctrl+c go back to idle behaviour.
func TestInterruptedTurnEnds(t *testing.T) {
	for _, reason := range []string{proto.TerminalAbortedStreaming, proto.TerminalAbortedTools} {
		x := newH(t)
		x.event(&proto.SystemInit{})
		x.press("esc")
		x.event(&proto.Result{Envelope: proto.Envelope{Subtype: proto.ResultErrorDuringExecution}, IsError: true, TerminalReason: reason})
		if x.st.isRunning(ext.MainEngine) || x.c.ActiveCtxs[ext.ContextTask] {
			t.Fatalf("%s: turn still running", reason)
		}
		if x.action(ext.ActChatCancel) {
			t.Fatalf("%s: esc after the interrupt should reach the editor", reason)
		}
	}
}

func TestSessionStateDrivesRunning(t *testing.T) {
	x := newH(t)
	x.event(&proto.SessionStateChanged{State: proto.StateRunning})
	x.event(&proto.Result{}) // background work may still run
	if !x.st.isRunning(ext.MainEngine) {
		t.Fatal("with session-state events, idle ends the turn, not the result")
	}
	x.event(&proto.SessionStateChanged{State: proto.StateIdle})
	if x.st.isRunning(ext.MainEngine) {
		t.Fatal("idle should end it")
	}
}

func TestCtrlCLadder(t *testing.T) {
	x := newH(t)
	x.event(&proto.SystemInit{})
	x.press("ctrl+c")
	if len(x.eng.interrupts) != 1 || !x.eng.interrupts[0] || len(find[ext.ExitMsg](x)) != 0 {
		t.Fatalf("ctrl+c interrupts a running turn and cancels the queue: %v", x.eng.interrupts)
	}
	x.event(&proto.Result{})
	x.press("ctrl+c")
	if len(x.c.Notices) == 0 || x.c.Notices[len(x.c.Notices)-1].Text != hintCtrlC {
		t.Fatalf("first idle ctrl+c shows the hint: %+v", x.c.Notices)
	}
	x.c.ClockV.Advance(time.Second) // too slow
	x.press("ctrl+c")
	if len(find[ext.ExitMsg](x)) != 0 {
		t.Fatal("a slow second press must not exit")
	}
	x.c.ClockV.Advance(500 * time.Millisecond)
	x.press("ctrl+c")
	exits := find[ext.ExitMsg](x)
	if len(exits) != 1 || exits[0].Code != 0 {
		t.Fatalf("double press exits: %+v", exits)
	}
	if len(x.c.Printed) != 1 || !strings.Contains(x.c.Printed[0], "mantle --resume sess-1") {
		t.Fatalf("resume hint = %q", x.c.Printed)
	}
}

func TestCtrlDDoublePress(t *testing.T) {
	x := newH(t)
	x.press("ctrl+d")
	if len(find[ext.ExitMsg](x)) != 0 || x.c.Notices[0].Text != hintCtrlD {
		t.Fatal("first ctrl+d hints")
	}
	x.c.ClockV.Advance(300 * time.Millisecond)
	x.press("ctrl+d")
	if len(find[ext.ExitMsg](x)) != 1 {
		t.Fatal("second ctrl+d exits")
	}
}

func TestExitCommandAndVimQuit(t *testing.T) {
	x := newH(t)
	cmd, _ := x.reg.Command("exit")
	x.run(cmd.Run(x.c, ""))
	if len(find[ext.ExitMsg](x)) != 1 {
		t.Fatal("/exit exits")
	}
	stage := x.reg.Stages[0].Value.(ext.PromptStage)
	for _, text := range []string{":q", " :wq! "} {
		v, c := stage(x.c, &ext.Draft{Text: text, Mode: "prompt"})
		if v != ext.Consumed || c == nil {
			t.Fatalf("%q should exit", text)
		}
	}
	if v, _ := stage(x.c, &ext.Draft{Text: ":q is a vim command", Mode: "prompt"}); v != ext.Continue {
		t.Fatal("ordinary text continues")
	}
	if v, _ := stage(x.c, &ext.Draft{Text: ":q", Mode: "bash"}); v != ext.Continue {
		t.Fatal("bash mode continues")
	}
}

func TestBackgroundAndKillAgents(t *testing.T) {
	x := newH(t)
	if x.action(ext.ActTaskBackground) {
		t.Fatal("ctrl+b while idle does nothing")
	}
	x.event(&proto.SystemInit{})
	x.press("ctrl+b")
	if x.eng.count(proto.SubBackgroundTasks) != 1 {
		t.Fatalf("controls = %v", x.eng.controls)
	}
	x.action(ext.ActChatKillAgents)
	if !strings.Contains(x.c.Notices[len(x.c.Notices)-1].Text, "No background agents") {
		t.Fatal("nothing to stop")
	}
	x.event(&proto.BackgroundTasksChanged{Tasks: []proto.BackgroundTask{
		{TaskID: "a1", TaskType: "local_agent"}, {TaskID: "b1", TaskType: "local_bash"}, {TaskID: "a2"},
	}})
	x.event(&proto.TaskNotification{TaskID: "a2", Status: "completed"})
	x.action(ext.ActChatKillAgents)
	if x.eng.count(proto.SubStopTask) != 1 || x.eng.lastControl(proto.SubStopTask) != (proto.StopTaskRequest{TaskID: "a1"}) {
		t.Fatalf("stop_task calls = %v %v", x.eng.controls, x.eng.requests)
	}
}

func TestPermissionDeniedNotice(t *testing.T) {
	x := newH(t)
	x.event(&proto.PermissionDenied{ToolName: "Bash", ToolUseID: "t9", Message: "blocked by rule Bash(rm:*)"})
	if len(x.c.Notices) != 1 || x.c.Notices[0].Text != "Bash was denied: blocked by rule Bash(rm:*)" {
		t.Fatalf("notices = %+v", x.c.Notices)
	}
}

func TestQueueDisplay(t *testing.T) {
	x := newH(t)
	view := func() string {
		return x.reg.Components[0].Component.View(x.c, ext.Area{Width: 40}).Text
	}
	if view() != "" {
		t.Fatal("empty queue renders nothing")
	}
	x.send(ext.QueuedPromptsMsg{EngineID: ext.MainEngine, Prompts: []ext.QueuedPrompt{
		{UUID: "u1", Text: "first"}, {UUID: "u2", Text: "second\nline"}, {UUID: "u3", Text: "c"}, {UUID: "u4", Text: "d"},
	}})
	v := testkitStrip(view())
	for _, want := range []string{"› first", "› second⏎ line", "… +1 more", "↑ to edit queued messages"} {
		if !strings.Contains(v, want) {
			t.Fatalf("queue view missing %q:\n%s", want, v)
		}
	}
	wide := testkitStrip(x.reg.Components[0].Component.View(x.c, ext.Area{Width: 80}).Text)
	if !strings.Contains(wide, "↑ to edit queued messages · ctrl+x ctrl+s to send them now") {
		t.Fatalf("queue hint:\n%s", wide)
	}
	x.event(&proto.CommandLifecycle{CommandUUID: "u1", State: proto.LifecycleStarted})
	if strings.Contains(testkitStrip(view()), "first") {
		t.Fatal("started prompts leave the queue")
	}
}

func TestUsageLimitWaitAndContinue(t *testing.T) {
	x := newH(t)
	reset := exttest.Epoch.Add(2 * time.Hour).Unix()
	ev := &proto.RateLimitEvent{RateLimitInfo: proto.RateLimitInfo{Status: "rejected", ResetsAt: json.RawMessage(itoa(int(reset)))}}
	x.event(ev)
	if x.c.topID() != DialogUsageLimit {
		t.Fatalf("limit dialog expected, got %v", x.c.ids)
	}
	x.press("enter") // wait
	// exttest's clock fires ticks immediately, so the continue prompt goes out now.
	if len(x.eng.sent) != 1 || x.eng.sent[0].Blocks[0].Text != continuePrompt {
		t.Fatalf("sent = %+v", x.eng.sent)
	}
	x.event(ev) // the same limit again: no second dialog
	if x.c.topID() != "" {
		t.Fatal("duplicate limit events are ignored")
	}
}

func TestUsageLimitAutoContinueAndCancel(t *testing.T) {
	x := newH(t)
	x.c.SettingsV = exttest.NewSettings(map[string]any{"autoContinueAtUsageLimit": true})
	// Start the wait by hand so the tick is not delivered.
	x.run(x.st.startLimitWait(x.c, ext.MainEngine, exttest.Epoch.Add(time.Hour)))
	if len(x.eng.sent) != 1 {
		t.Fatalf("tick should have continued: %+v", x.eng.sent)
	}
	x.st.startLimitWait(x.c, ext.MainEngine, exttest.Epoch.Add(time.Hour)) // tick not run
	if !x.action(ext.ActChatCancel) || x.st.limit != nil {
		t.Fatal("esc cancels the wait")
	}
	x.send(limitTickMsg{gen: x.st.limitGen})
	if len(x.eng.sent) != 1 {
		t.Fatal("a cancelled wait must not continue")
	}
	rl := &proto.RateLimitEvent{RateLimitInfo: proto.RateLimitInfo{Status: "rejected", ResetsAt: json.RawMessage(`"` + exttest.Epoch.Add(3*time.Hour).Format(time.RFC3339) + `"`)}}
	x.event(rl)
	if x.c.topID() != "" || len(x.eng.sent) != 2 {
		t.Fatalf("autoContinueAtUsageLimit waits without asking: stack=%v sent=%d", x.c.ids, len(x.eng.sent))
	}
}

func TestParseResetsAt(t *testing.T) {
	cases := map[string]int64{
		`1767366245`:             1767366245,
		`1767366245000`:          1767366245,
		`"2026-01-02T15:04:05Z"`: 1767366245,
		`"1767366245"`:           1767366245,
	}
	for in, want := range cases {
		got, ok := parseResetsAt(json.RawMessage(in))
		if !ok || got.Unix() != want {
			t.Errorf("parseResetsAt(%s) = %v %v", in, got, ok)
		}
	}
	if _, ok := parseResetsAt(json.RawMessage(`null`)); ok {
		t.Error("null is not a time")
	}
}
