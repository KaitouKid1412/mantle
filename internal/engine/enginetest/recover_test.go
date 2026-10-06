package enginetest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

const freshSID = "7b2a9022-1111-4111-8111-111111111111"

// noConversationScript dies like headless claude asked to resume a session that
// never had a turn.
func noConversationScript() *enginefake.Script {
	return enginefake.New(enginefake.Step{Stderr: "No conversation found with session ID: " + freshSID + "\n"}, enginefake.Exit(1))
}

func failingScript(msg string) *enginefake.Script {
	return enginefake.New(enginefake.Step{Stderr: msg + "\n"}, enginefake.Exit(1))
}

// answeringScript answers initialize and one prompt.
func answeringScript() *enginefake.Script {
	return enginefake.New(enginefake.InitializeRule(nil),
		enginefake.Expect(`{"type":"user"}`),
		enginefake.Emit(`{"type":"result","subtype":"success","is_error":false,"result":"recovered","user_message_uuid":"${uuid}"}`))
}

func notices(rec *Recorder) []ext.Notice {
	var out []ext.Notice
	for _, m := range rec.Msgs() {
		if n, ok := m.(ext.NoticeMsg); ok {
			out = append(out, n.Notice)
		}
	}
	return out
}

func exitErrors(rec *Recorder) []error {
	var out []error
	for _, m := range rec.Msgs() {
		if x, ok := m.(ext.EngineExitedMsg); ok && x.Err != nil {
			out = append(out, x.Err)
		}
	}
	return out
}

func args(sp *Spawner, i int) string { return strings.Join(sp.Specs()[i].Args, " ") }

// TestRestartFallsBackToSameID is the /login-before-any-message bug: a restart with
// --resume of a session that never had a turn dies; the engine retries as a fresh
// session with the same id, tells the user, and a prompt sent meanwhile goes through.
func TestRestartFallsBackToSameID(t *testing.T) {
	m, sp, rec := setup(t, enginefake.New(enginefake.InitializeRule(nil)), noConversationScript(), answeringScript())
	e, err := m.Start("", ext.SpawnOpts{Cwd: "/w", Model: "opus"})
	if err != nil {
		t.Fatal(err)
	}
	rec.WaitFor(t, isInitialized)
	if err := e.RestartNow(context.Background(), ext.SpawnOpts{Cwd: "/w", Model: "opus", Resume: freshSID}); err != nil {
		t.Fatal(err)
	}
	// Sent while the restart fails and recovers: must not be lost.
	if msg := e.Send(ext.Prompt{UUID: "u-after", Blocks: []proto.ContentBlock{proto.Text("hello")}})(); msg != nil {
		t.Fatalf("send during recovery: %v", msg)
	}
	res := rec.WaitFor(t, resultFor("u-after")).(ext.EngineEventMsg).Event.(*proto.Result)
	if res.Result != "recovered" {
		t.Errorf("result %+v", res)
	}
	if !strings.Contains(args(sp, 1), "--resume="+freshSID) {
		t.Errorf("restart args %s", args(sp, 1))
	}
	if a := args(sp, 2); !strings.Contains(a, "--session-id="+freshSID) || strings.Contains(a, "--resume") || !strings.Contains(a, "--model opus") {
		t.Errorf("fallback args %s", a)
	}
	ns := notices(rec)
	if len(ns) != 1 || ns[0].Text != "Started a new session: the previous one had no saved messages" {
		t.Errorf("notices %+v", ns)
	}
	if errs := exitErrors(rec); len(errs) != 0 {
		t.Errorf("the failed attempt must not surface as an engine exit: %v", errs)
	}
	if o := e.Options(); o.Resume != "" || o.SessionID != freshSID {
		t.Errorf("options after recovery %+v", o)
	}
}

// TestRestartFallsBackToFreshSession: the same-id retry fails too (id in use), so a
// plain fresh session starts.
func TestRestartFallsBackToFreshSession(t *testing.T) {
	m, sp, rec := setup(t, enginefake.New(enginefake.InitializeRule(nil)), noConversationScript(),
		failingScript("Error: Session ID "+freshSID+" is already in use."), answeringScript())
	e, _ := m.Start("", ext.SpawnOpts{})
	rec.WaitFor(t, isInitialized)
	if err := e.RestartNow(context.Background(), ext.SpawnOpts{Resume: freshSID}); err != nil {
		t.Fatal(err)
	}
	rec.WaitFor(t, func(msg tea.Msg) bool { _, ok := msg.(ext.NoticeMsg); return ok })
	e.Send(ext.Prompt{UUID: "u-1", Blocks: []proto.ContentBlock{proto.Text("hi")}})()
	rec.WaitFor(t, resultFor("u-1"))
	if a := args(sp, 3); strings.Contains(a, "--session-id") || strings.Contains(a, "--resume") {
		t.Errorf("plain fresh args %s", a)
	}
	if ns := notices(rec); len(ns) != 1 || !strings.Contains(ns[0].Text, "no saved messages") {
		t.Errorf("notices %+v", ns)
	}
}

// TestRestartFailureNeverLeavesEngineDead: an early failure for another reason
// still ends with a usable engine (here only without the forwarded flags).
func TestRestartFailureNeverLeavesEngineDead(t *testing.T) {
	bad := failingScript("error: unknown option '--bogus'")
	m, sp, rec := setup(t, enginefake.New(enginefake.InitializeRule(nil)), bad, bad, answeringScript())
	e, _ := m.Start("", ext.SpawnOpts{})
	rec.WaitFor(t, isInitialized)
	if err := e.RestartNow(context.Background(), ext.SpawnOpts{Resume: freshSID, ExtraArgs: []string{"--bogus"}}); err != nil {
		t.Fatal(err)
	}
	n := rec.WaitFor(t, func(msg tea.Msg) bool { _, ok := msg.(ext.NoticeMsg); return ok }).(ext.NoticeMsg)
	if !strings.Contains(n.Notice.Text, "Couldn't restart claude (error: unknown option '--bogus')") ||
		!strings.Contains(n.Notice.Text, "without your extra flags") {
		t.Errorf("notice %q", n.Notice.Text)
	}
	e.Send(ext.Prompt{UUID: "u-1", Blocks: []proto.ContentBlock{proto.Text("hi")}})()
	rec.WaitFor(t, resultFor("u-1"))
	if a := args(sp, 3); strings.Contains(a, "--bogus") {
		t.Errorf("minimal args %s", a)
	}
}

// TestRestartGiveUp: when every fallback fails, the user gets an error notice and an
// EngineExitedMsg, and prompts waiting for the recovery fail visibly.
func TestRestartGiveUp(t *testing.T) {
	bad := failingScript("fatal: broken install")
	m, sp, rec := setup(t, enginefake.New(enginefake.InitializeRule(nil)), bad)
	e, _ := m.Start("", ext.SpawnOpts{})
	rec.WaitFor(t, isInitialized)
	e.RestartNow(context.Background(), ext.SpawnOpts{Resume: freshSID, ExtraArgs: []string{"--x"}})
	// The prompt fails either at once or once the last fallback dies.
	sendErr := e.Send(ext.Prompt{UUID: "u-lost", Blocks: []proto.ContentBlock{proto.Text("hi")}})()
	x := rec.WaitFor(t, func(msg tea.Msg) bool { x, ok := msg.(ext.EngineExitedMsg); return ok && x.Err != nil }).(ext.EngineExitedMsg)
	if x.Err == nil {
		t.Fatal("want an exit error")
	}
	ns := notices(rec)
	if len(ns) == 0 || ns[len(ns)-1].Level != ext.NoticeError || !strings.Contains(ns[len(ns)-1].Text, "claude failed to start: fatal: broken install") {
		t.Errorf("notices %+v", ns)
	}
	if got := len(sp.Specs()); got != 4 { // start, resume, fresh, minimal
		t.Errorf("spawns %d", got)
	}
	lost := func(msg tea.Msg) bool {
		r, ok := msg.(ext.ControlResultMsg)
		return ok && r.Subtype == proto.TypeUser && r.RequestID == "u-lost" && r.Err != nil
	}
	if !lost(sendErr) {
		rec.WaitFor(t, lost)
	}
}

// lineIndex returns the index of the first line containing every part, and how many
// lines contain them.
func lineIndex(lines [][]byte, parts ...string) (first, n int) {
	first = -1
	for i, l := range lines {
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(string(l), p)
		}
		if ok {
			if first < 0 {
				first = i
			}
			n++
		}
	}
	return first, n
}

// sendDuringRestart starts RestartNow(o) and, while it runs, sends prompt u-mid and
// then a get_settings control request. It returns the control result.
func sendDuringRestart(t *testing.T, e *engine.Engine, o ext.SpawnOpts, wait time.Duration) ext.ControlResultMsg {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- e.RestartNow(context.Background(), o) }()
	time.Sleep(wait)
	if err := e.SendPrompt(ext.Prompt{UUID: "u-mid", Blocks: []proto.ContentBlock{proto.Text("hi")}}); err != nil {
		t.Fatalf("send during restart: %v", err)
	}
	ctl := make(chan tea.Msg, 1)
	go func() { ctl <- e.Control(proto.SubGetSettings, nil)() }()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-ctl:
		return m.(ext.ControlResultMsg)
	case <-time.After(10 * time.Second):
		t.Fatal("control request during restart never finished")
		return ext.ControlResultMsg{}
	}
}

// answeringSettingsScript answers initialize, get_settings and one prompt.
func answeringSettingsScript() *enginefake.Script {
	return enginefake.New(enginefake.InitializeRule(nil),
		enginefake.On(`{"type":"control_request","request":{"subtype":"get_settings"}}`, map[string]any{"effective": map[string]any{}}),
		enginefake.Expect(`{"type":"user"}`),
		enginefake.Emit(`{"type":"result","subtype":"success","is_error":false,"result":"recovered","user_message_uuid":"${uuid}"}`))
}

// assertOnlyIn checks that procs[want] got u-mid and the get_settings request exactly
// once each, in that order, after its initialize, and no other process got either.
func assertOnlyIn(t *testing.T, sp *Spawner, want int) {
	t.Helper()
	procs := sp.Procs()
	for i, p := range procs {
		lines := p.Received()
		pi, pn := lineIndex(lines, `"type":"user"`, `u-mid`)
		ci, cn := lineIndex(lines, `"subtype":"get_settings"`)
		ii, _ := lineIndex(lines, `"subtype":"initialize"`)
		if i != want {
			if pn != 0 || cn != 0 {
				t.Errorf("proc %d got the held messages (prompt %d, control %d)", i, pn, cn)
			}
			continue
		}
		if pn != 1 || cn != 1 {
			t.Errorf("proc %d: prompt sent %d times, control %d times; want 1 each", i, pn, cn)
		}
		if !(ii >= 0 && ii < pi && pi < ci) {
			t.Errorf("proc %d order: initialize %d, prompt %d, control %d", i, ii, pi, ci)
		}
	}
}

// TestRestartHoldsMessages: a prompt and a control request sent while Restart stops
// the old engine reach the new session exactly once, in order, after its initialize
// (they used to reach the dying engine and be lost).
func TestRestartHoldsMessages(t *testing.T) {
	// The old engine answers end_session but keeps running, so the stop takes a while.
	slowStop := enginefake.New(enginefake.InitializeRule(nil),
		enginefake.On(`{"type":"control_request","request":{"subtype":"end_session"}}`, nil))
	m, sp, rec := setup(t, slowStop, answeringSettingsScript())
	e, _ := m.Start("", ext.SpawnOpts{})
	rec.WaitFor(t, isInitialized)
	r := sendDuringRestart(t, e, ext.SpawnOpts{}, 200*time.Millisecond)
	if r.Err != nil {
		t.Errorf("control during restart: %v", r.Err)
	}
	rec.WaitFor(t, resultFor("u-mid"))
	m.Close(context.Background())
	assertOnlyIn(t, sp, 1)
}

// TestRestartHoldsMessagesAcrossFallback: the restart's resume dies; the held prompt
// and control request go to the fallback session, exactly once.
func TestRestartHoldsMessagesAcrossFallback(t *testing.T) {
	slowStop := enginefake.New(enginefake.InitializeRule(nil),
		enginefake.On(`{"type":"control_request","request":{"subtype":"end_session"}}`, nil))
	m, sp, rec := setup(t, slowStop, noConversationScript(), answeringSettingsScript())
	e, _ := m.Start("", ext.SpawnOpts{})
	rec.WaitFor(t, isInitialized)
	r := sendDuringRestart(t, e, ext.SpawnOpts{Resume: freshSID}, 200*time.Millisecond)
	if r.Err != nil {
		t.Errorf("control during restart: %v", r.Err)
	}
	rec.WaitFor(t, resultFor("u-mid"))
	m.Close(context.Background())
	assertOnlyIn(t, sp, 2)
	if a := args(sp, 2); !strings.Contains(a, "--session-id="+freshSID) {
		t.Errorf("fallback args %s", a)
	}
}

// TestHeldControlRequestCanceled: a held control request whose context ends leaves
// the queue and is never sent.
func TestHeldControlRequestCanceled(t *testing.T) {
	slowStop := enginefake.New(enginefake.InitializeRule(nil),
		enginefake.On(`{"type":"control_request","request":{"subtype":"end_session"}}`, nil))
	m, sp, rec := setup(t, slowStop, answeringSettingsScript())
	e, _ := m.Start("", ext.SpawnOpts{})
	rec.WaitFor(t, isInitialized)
	done := make(chan error, 1)
	go func() { done <- e.RestartNow(context.Background(), ext.SpawnOpts{}) }()
	time.Sleep(200 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := e.Request(ctx, proto.RawRequest{Subtype: proto.SubGetSettings}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("held request: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e.Send(ext.Prompt{UUID: "u-mid", Blocks: []proto.ContentBlock{proto.Text("hi")}})()
	rec.WaitFor(t, resultFor("u-mid"))
	m.Close(context.Background())
	if _, n := lineIndex(sp.Procs()[1].Received(), `"subtype":"get_settings"`); n != 0 {
		t.Errorf("canceled request was sent %d times", n)
	}
}

// TestRestartSpawnErrorFallsBack: the new process can't even be started with the
// restart's options; the engine starts a fresh session instead of staying dead.
func TestRestartSpawnErrorFallsBack(t *testing.T) {
	m, sp, rec := setup(t, enginefake.New(enginefake.InitializeRule(nil)), answeringScript())
	sp.ErrFor = func(n int, spec engine.SpawnSpec) error {
		if strings.Contains(strings.Join(spec.Args, " "), "--resume=") {
			return errors.New("exec: resume refused")
		}
		return nil
	}
	e, _ := m.Start("", ext.SpawnOpts{})
	rec.WaitFor(t, isInitialized)
	if err := e.RestartNow(context.Background(), ext.SpawnOpts{Resume: freshSID}); err != nil {
		t.Fatalf("restart should recover: %v", err)
	}
	e.Send(ext.Prompt{UUID: "u-1", Blocks: []proto.ContentBlock{proto.Text("hi")}})()
	rec.WaitFor(t, resultFor("u-1"))
	n := rec.WaitFor(t, func(msg tea.Msg) bool { _, ok := msg.(ext.NoticeMsg); return ok }).(ext.NoticeMsg)
	if !strings.Contains(n.Notice.Text, "exec: resume refused") || !strings.HasSuffix(n.Notice.Text, "; started a new session") {
		t.Errorf("notice %q", n.Notice.Text)
	}
}

// TestStartWithoutResumeNoFallback: a plain first start that crashes is reported as
// before (no silent retries).
func TestStartWithoutResumeNoFallback(t *testing.T) {
	m, sp, rec := setup(t, failingScript("boom"))
	if _, err := m.Start("", ext.SpawnOpts{}); err != nil {
		t.Fatal(err)
	}
	rec.WaitFor(t, isExit)
	if len(sp.Specs()) != 1 || len(notices(rec)) != 0 {
		t.Errorf("spawns %d notices %v", len(sp.Specs()), notices(rec))
	}
}

// TestRestartFallbackWithFakeclaudeBinary reproduces the bug end to end with the
// fakeclaude binary: FAKECLAUDE_NO_SESSIONS makes --resume fail like the real engine.
func TestRestartFallbackWithFakeclaudeBinary(t *testing.T) {
	bin := buildFakeclaude(t)
	script := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(script, []byte(`{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": {"commands":[]}}
{"expect": {"type":"user"}, "timeout": 30000}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"after login","user_message_uuid":"${uuid}"}}
`), 0o644)
	t.Setenv("FAKECLAUDE_SCRIPT", script)
	t.Setenv("FAKECLAUDE_NO_SESSIONS", "1")
	rec := NewRecorder()
	m := engine.NewManager(rec.Send)
	m.Binary, m.RunDir = bin, "-"
	defer m.Close(context.Background())
	e, err := m.Start("", ext.SpawnOpts{})
	if err != nil {
		t.Fatal(err)
	}
	rec.WaitFor(t, isInitialized)
	// What /login does: restart with --resume <current session id>.
	if msg := e.Restart(ext.SpawnOpts{Resume: freshSID})(); msg != nil {
		t.Fatalf("restart: %v", msg)
	}
	e.Send(ext.Prompt{UUID: "u-1", Blocks: []proto.ContentBlock{proto.Text("hi")}})()
	res := rec.WaitFor(t, resultFor("u-1")).(ext.EngineEventMsg).Event.(*proto.Result)
	if res.Result != "after login" {
		t.Errorf("result %+v", res)
	}
	if ns := notices(rec); len(ns) != 1 || !strings.Contains(ns[0].Text, "no saved messages") {
		t.Errorf("notices %+v", ns)
	}
	if !e.Running() || e.Options().SessionID != freshSID {
		t.Errorf("engine running=%v opts=%+v", e.Running(), e.Options())
	}
}
