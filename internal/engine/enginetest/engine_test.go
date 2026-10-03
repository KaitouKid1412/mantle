package enginetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func setup(t *testing.T, scripts ...*enginefake.Script) (*engine.Manager, *Spawner, *Recorder) {
	t.Helper()
	sp := &Spawner{Scripts: scripts}
	rec := NewRecorder()
	m := engine.NewManager(rec.Send)
	m.Spawner, m.RunDir, m.Binary = sp, "-", "claude"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.Close(ctx)
	})
	return m, sp, rec
}

func isExit(m tea.Msg) bool { _, ok := m.(ext.EngineExitedMsg); return ok }

func isResult(m tea.Msg) bool {
	ev, ok := m.(ext.EngineEventMsg)
	if !ok {
		return false
	}
	_, ok = ev.Event.(*proto.Result)
	return ok
}

func isInitialized(m tea.Msg) bool {
	r, ok := m.(ext.ControlResultMsg)
	return ok && r.Subtype == proto.SubInitialize
}

const turn = `
{"expect": {"type":"user","uuid":"u-1","origin":{"kind":"human"},"message":{"role":"user","content":"hi"}}}
{"emit": %s}
{"emit": {"type":"stream_event","event":{"type":"message_start","message":{"id":"m1","content":[]}},"uuid":"s0","session_id":"sess-1"}}
{"emit": {"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}},"uuid":"s1","session_id":"sess-1"}}
{"emit": {"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}},"uuid":"s2","session_id":"sess-1"}}
{"emit": {"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"Hello"}]},"uuid":"a1","session_id":"sess-1"}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"Hello","session_id":"sess-1","user_message_uuid":"${uuid}"}}
`

func TestTurnFlowsInOrder(t *testing.T) {
	script := enginefake.MustParse(fmt.Sprintf(turn, enginefake.InitEvent("sess-1")))
	script.Rules = append(script.Rules, enginefake.InitializeRule(nil))
	m, sp, rec := setup(t, script)
	m.CoalesceInterval = time.Hour // deltas only flush on the next non-delta

	e, err := m.Start("", ext.SpawnOpts{Cwd: t.TempDir(), Model: "opus", Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if msg := e.Send(ext.Prompt{UUID: "u-1", Blocks: []proto.ContentBlock{proto.Text("hi")}})(); msg != nil {
		t.Fatalf("send: %v", msg)
	}
	rec.WaitFor(t, isResult)
	rec.WaitFor(t, isInitialized)

	msgs := rec.Msgs()
	if _, ok := msgs[0].(ext.EngineAttachMsg); !ok {
		t.Errorf("first message %T, want EngineAttachMsg", msgs[0])
	}
	var kinds []string
	for _, ev := range Events(msgs) {
		k := ev.Env().Type + "/" + ev.Env().Subtype
		if se, ok := ev.(*proto.StreamEvent); ok {
			k = se.Event.Type
			if se.Event.Delta != nil {
				k += ":" + se.Event.Delta.Text
			}
		}
		kinds = append(kinds, k)
	}
	want := "system/init message_start content_block_delta:Hello assistant/ result/success"
	if strings.Join(kinds, " ") != want {
		t.Errorf("events:\n got %s\nwant %s", strings.Join(kinds, " "), want)
	}
	snap := e.Snapshot()
	if snap.Info.SessionID != "sess-1" || snap.Info.ClaudeVersion != "2.1.288" || snap.Initialize == nil || len(snap.Commands) != 2 {
		t.Errorf("snapshot: %+v", snap.Info)
	}
	if !e.Supports(proto.SubRewindFiles) || !e.Supports("interrupt_receipt_v1") || !e.Supports(engine.SubRewindConversation) || e.Supports("nope") {
		t.Error("capabilities")
	}
	spec := sp.Specs()[0]
	args := strings.Join(spec.Args, " ")
	for _, w := range []string{"--output-format stream-json", "--permission-prompt-tool stdio", "--replay-user-messages", "-n demo", "--model opus"} {
		if !strings.Contains(args, w) {
			t.Errorf("args missing %q: %s", w, args)
		}
	}
	if strings.Contains(args, "--system-prompt") {
		t.Error("must never pass --system-prompt")
	}
}

func TestControlCorrelationAndUnsupported(t *testing.T) {
	script := enginefake.New(
		enginefake.InitializeRule(nil),
		enginefake.On(json.RawMessage(`{"type":"control_request","request":{"subtype":"get_binary_version"}}`), map[string]string{"version": "2.1.288"}),
		enginefake.Step{On: json.RawMessage(`{"type":"control_request","request":{"subtype":"get_workspace_diff"}}`), RespondError: "Unsupported control request subtype: get_workspace_diff"},
	)
	m, _, rec := setup(t, script)
	e, err := m.Start("", ext.SpawnOpts{})
	if err != nil {
		t.Fatal(err)
	}
	rec.WaitFor(t, isInitialized)

	res := e.Control(proto.SubGetBinaryVersion, proto.GetBinaryVersionRequest{})().(ext.ControlResultMsg)
	var v proto.BinaryVersion
	if res.Err != nil || json.Unmarshal(res.Resp, &v) != nil || v.Version != "2.1.288" || !strings.HasPrefix(res.RequestID, "req_") {
		t.Fatalf("got %+v", res)
	}
	if !e.Supports(engine.SubGetWorkspaceDiff) {
		t.Fatal("unstable subtype should start supported on 2.1.288")
	}
	res = e.Control(engine.SubGetWorkspaceDiff, nil)().(ext.ControlResultMsg)
	if !proto.IsUnsupported(res.Err) || e.Supports(engine.SubGetWorkspaceDiff) {
		t.Errorf("unsupported not recorded: %+v", res)
	}
}

func TestPermissionRoundTripAndCancel(t *testing.T) {
	script := enginefake.MustParse(`
{"expect": {"type":"user"}}
{"request": {"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"},"tool_use_id":"toolu_1"}, "id": "cli_1"}
{"expect": {"type":"control_response","response":{"subtype":"success","request_id":"cli_1","response":{"behavior":"allow","toolUseID":"toolu_1"}}}}
{"request": {"subtype":"can_use_tool","tool_name":"Edit","input":{},"tool_use_id":"toolu_2"}, "id": "cli_2"}
{"emit": {"type":"control_cancel_request","request_id":"cli_2"}}
{"request": {"subtype":"hook_callback","callback_id":"x","input":{}}, "id": "cli_3"}
{"expect": {"type":"control_response","response":{"subtype":"success","request_id":"cli_3","response":{}}}}
{"request": {"subtype":"elicitation","mcp_server_name":"srv","message":"Name?"}, "id": "cli_4"}
{"expect": {"type":"control_response","response":{"subtype":"success","request_id":"cli_4","response":{"action":"decline"}}}}
{"request": {"subtype":"oauth_token_refresh"}, "id": "cli_5"}
{"expect": {"type":"control_response","response":{"subtype":"error","request_id":"cli_5"}}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"done"}}
`)
	script.Rules = append(script.Rules, enginefake.InitializeRule(nil))
	m, sp, rec := setup(t, script)
	e, _ := m.Start("", ext.SpawnOpts{})
	e.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text("go")}})()

	pm := rec.WaitFor(t, func(m tea.Msg) bool { p, ok := m.(ext.PermissionMsg); return ok && p.RequestID == "cli_1" }).(ext.PermissionMsg)
	if pm.Req.ToolName != "Bash" || pm.EngineID != ext.MainEngine {
		t.Fatalf("permission: %+v", pm)
	}
	pm.Reply(proto.PermissionResult{Behavior: proto.BehaviorAllow})()
	pm.Reply(proto.PermissionResult{Behavior: proto.BehaviorDeny, Message: "late"})() // dropped: exactly once

	rec.WaitFor(t, func(m tea.Msg) bool { c, ok := m.(ext.ControlCancelMsg); return ok && c.RequestID == "cli_2" })
	cm := rec.WaitFor(t, func(m tea.Msg) bool { c, ok := m.(ext.ControlRequestMsg); return ok && c.RequestID == "cli_4" }).(ext.ControlRequestMsg)
	cm.Reply(proto.ElicitationResult{Action: "decline"}, nil)()
	rec.WaitFor(t, isResult)

	for _, msg := range rec.Msgs() {
		if p, ok := msg.(ext.PermissionMsg); ok && p.RequestID == "cli_2" {
			// The dialog may have opened before the cancel; replying now must be a no-op.
			p.Reply(proto.PermissionResult{Behavior: proto.BehaviorAllow})()
		}
		if c, ok := msg.(ext.ControlRequestMsg); ok && c.Subtype == proto.SubHookCallback {
			t.Error("hook_callback should be answered by the engine, not forwarded")
		}
	}
	e.Stop(context.Background())
	code, err := sp.Procs()[0].Wait()
	if err != nil || code != 0 {
		t.Errorf("script: code=%d err=%v", code, err)
	}
	for _, l := range sp.Procs()[0].Received() {
		if strings.Contains(string(l), `"late"`) || strings.Contains(string(l), `"cli_2"`) {
			t.Errorf("reply must not be sent: %s", l)
		}
	}
}

func TestUnexpectedExitAndRestart(t *testing.T) {
	crash := enginefake.New(enginefake.InitializeRule(nil), enginefake.Step{Stderr: "boom: something broke\n"}, enginefake.Delay(50), enginefake.Exit(1))
	second := enginefake.New(enginefake.InitializeRule(nil))
	m, sp, rec := setup(t, crash, second)
	e, _ := m.Start("", ext.SpawnOpts{Cwd: "/w"})
	ex := rec.WaitFor(t, isExit).(ext.EngineExitedMsg)
	if ex.Err == nil || !strings.Contains(ex.Stderr, "boom") {
		t.Fatalf("exit: %+v", ex)
	}
	if e.Running() {
		t.Error("still running")
	}
	if res := e.Control(proto.SubGetSettings, nil)().(ext.ControlResultMsg); !errors.Is(res.Err, engine.ErrNotRunning) {
		t.Errorf("control on dead engine: %v", res.Err)
	}
	if msg := e.Restart(ext.SpawnOpts{Cwd: "/w", Resume: "sess-9", ForkSession: true})(); msg != nil {
		t.Fatalf("restart: %v", msg)
	}
	if !e.Running() {
		t.Fatal("not running after restart")
	}
	args := strings.Join(sp.Specs()[1].Args, " ")
	if !strings.Contains(args, "--resume=sess-9 --fork-session") {
		t.Errorf("restart args: %s", args)
	}
	n := len(rec.Msgs())
	e.Stop(context.Background())
	ex2 := rec.WaitFor(t, func(m tea.Msg) bool {
		x, ok := m.(ext.EngineExitedMsg)
		return ok && len(rec.Msgs()) > n && x != ex
	}).(ext.EngineExitedMsg)
	if ex2.Err != nil {
		t.Errorf("requested stop should exit cleanly: %v", ex2.Err)
	}
}

func TestSessionTracking(t *testing.T) {
	script := enginefake.MustParse(`
{"expect": {"type":"user"}}
{"emit": {"type":"system","subtype":"status","status":null,"permissionMode":"plan","uuid":"x1","session_id":"s1"}}
{"emit": {"type":"system","subtype":"session_title_changed","title":"Fix bug","uuid":"x2","session_id":"s1"}}
{"emit": {"type":"conversation_reset","new_conversation_id":"s2","trigger":"clear","uuid":"x3","session_id":"s1"}}
{"emit": {"type":"system","subtype":"session_state_changed","state":"idle","uuid":"x4","session_id":"s2"}}
`)
	script.Rules = append(script.Rules,
		enginefake.InitializeRule(nil),
		enginefake.On(json.RawMessage(`{"type":"control_request","request":{"subtype":"set_model"}}`), nil),
	)
	m, _, rec := setup(t, script)
	e, _ := m.Start("", ext.SpawnOpts{Resume: "s1"})
	e.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text("/clear")}})()
	rec.WaitFor(t, func(m tea.Msg) bool { s, ok := m.(ext.SessionChangedMsg); return ok && s.Info.SessionID == "s2" })
	if res := e.Control(proto.SubSetModel, proto.SetModelRequest{Model: "sonnet"})().(ext.ControlResultMsg); res.Err != nil {
		t.Fatal(res.Err)
	}
	last := rec.WaitFor(t, func(m tea.Msg) bool { s, ok := m.(ext.SessionChangedMsg); return ok && s.Info.Model == "sonnet" }).(ext.SessionChangedMsg)
	if last.Info.PermissionMode != "plan" || last.Info.Title != "" || last.Info.SessionID != "s2" {
		t.Errorf("info: %+v", last.Info)
	}
	if e.Snapshot().State != proto.StateIdle {
		t.Errorf("state %q", e.Snapshot().State)
	}
}

func TestSpawnFailure(t *testing.T) {
	m, sp, _ := setup(t, enginefake.New())
	sp.Err = errors.New("no such file")
	if _, err := m.Start("", ext.SpawnOpts{}); err == nil {
		t.Fatal("want error")
	}
	if msg := m.StartCmd("btw", ext.SpawnOpts{})(); msg.(ext.EngineExitedMsg).EngineID != "btw" {
		t.Errorf("got %+v", msg)
	}
}

func TestMultipleEngines(t *testing.T) {
	s := enginefake.New(enginefake.InitializeRule(nil))
	m, _, rec := setup(t, s)
	if _, err := m.Start("", ext.SpawnOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start("builder", ext.SpawnOpts{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(m.Engines(), ","); got != "builder,main" {
		t.Errorf("engines %s", got)
	}
	m.Remove(context.Background(), "builder")
	rec.WaitFor(t, func(msg tea.Msg) bool { d, ok := msg.(ext.EngineDetachMsg); return ok && d.EngineID == "builder" })
	if m.Engine("builder") != nil || m.Engine("") == nil {
		t.Error("remove")
	}
}

func TestHandleStartStopAndCommands(t *testing.T) {
	script := enginefake.New(enginefake.InitializeRule(nil),
		enginefake.Expect(json.RawMessage(`{"type":"user","shouldQuery":false,"inline_pastes":["p"],"pasted_content":[{"id":1}]}`)))
	m, sp, rec := setup(t, script)
	cmd := m.Handle(ext.EngineStartMsg{EngineID: "builder-1", Opts: ext.SpawnOpts{PermissionMode: "acceptEdits"}})
	if cmd == nil {
		t.Fatal("start not handled")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("start: %v", msg)
	}
	rec.WaitFor(t, func(msg tea.Msg) bool { a, ok := msg.(ext.EngineAttachMsg); return ok && a.EngineID == "builder-1" })
	cm := rec.WaitFor(t, func(msg tea.Msg) bool { _, ok := msg.(ext.CommandsMsg); return ok }).(ext.CommandsMsg)
	if cm.EngineID != "builder-1" || cm.Source != ext.SourceEngine || len(cm.Commands) != 2 || cm.Commands[0].Name != "compact" || cm.Commands[0].ArgHint == "" {
		t.Errorf("commands: %+v", cm)
	}
	no := false
	e := m.Engine("builder-1")
	if msg := e.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text("x")}, ShouldQuery: &no,
		InlinePastes: []string{"p"}, PastedContent: json.RawMessage(`[{"id":1}]`)})(); msg != nil {
		t.Fatalf("send: %v", msg)
	}
	if m.Handle(ext.EngineStopMsg{EngineID: "builder-1"})() != nil {
		t.Error("stop returned a message")
	}
	rec.WaitFor(t, func(msg tea.Msg) bool { d, ok := msg.(ext.EngineDetachMsg); return ok && d.EngineID == "builder-1" })
	if code, err := sp.Procs()[0].Wait(); code != 0 || err != nil {
		t.Errorf("script: %d %v", code, err)
	}
	if !strings.Contains(strings.Join(sp.Specs()[0].Args, " "), "--permission-mode acceptEdits") {
		t.Error("opts not used")
	}
	if m.Handle(ext.SessionChangedMsg{}) != nil {
		t.Error("other messages must return nil")
	}
}
