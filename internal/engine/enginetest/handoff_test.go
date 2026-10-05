package enginetest

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func buildFakeclaude(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a binary")
	}
	bin := filepath.Join(t.TempDir(), "claude")
	_, file, _, _ := runtime.Caller(0)
	build := exec.Command("go", "build", "-o", bin, "./cmd/fakeclaude")
	build.Dir = filepath.Join(filepath.Dir(file), "..", "..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// TestHandoffAndAdopt hands a live engine from one Manager to another (in-process
// stand-in for exec): the process and its pipes survive, the session carries over,
// and the new owner keeps talking to it.
func TestHandoffAndAdopt(t *testing.T) {
	bin := buildFakeclaude(t)
	script := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(script, []byte(`{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": {"commands":[{"name":"compact","description":"x"}],"capabilities":["ui_surface_v1"],"current_permission_mode":"plan"}}
{"expect": {"type":"user","message":{"content":"before"}}}
{"emit": {"type":"system","subtype":"init","session_id":"sess-7","cwd":"/w","tools":[],"mcp_servers":[],"model":"m","permissionMode":"plan","slash_commands":[],"claude_code_version":"2.1.289","output_style":"default"}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"one","user_message_uuid":"${uuid}"}}
{"expect": {"type":"user","message":{"content":"after"}}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"two","user_message_uuid":"${uuid}"}}
`), 0o644)
	t.Setenv("FAKECLAUDE_SCRIPT", script)

	recA, recB := NewRecorder(), NewRecorder()
	a := engine.NewManager(recA.Send)
	a.Binary, a.RunDir = bin, "-"
	b := engine.NewManager(recB.Send)
	b.Binary, b.RunDir = bin, "-"
	defer b.Close(context.Background())

	e, err := a.Start("", ext.SpawnOpts{})
	if err != nil {
		t.Fatal(err)
	}
	recA.WaitFor(t, isInitialized)
	u1 := "11111111-1111-4111-8111-111111111111"
	e.Send(ext.Prompt{UUID: u1, Blocks: []proto.ContentBlock{proto.Text("before")}})()
	recA.WaitFor(t, resultFor(u1))

	st, err := e.PrepareHandoff(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Session.SessionID != "sess-7" || len(st.Init) == 0 || st.PID == 0 || st.StdinFD <= 2 {
		t.Fatalf("state %+v", st)
	}
	if e.Running() {
		t.Error("the old engine must let go")
	}
	// The hand-off file round-trips (fds are serialised as numbers).
	path := filepath.Join(t.TempDir(), "h.json")
	if err := engine.WriteHandoffFile(path, engine.HandoffFile{Engines: []engine.HandoffState{st}}); err != nil {
		t.Fatal(err)
	}
	var hf engine.HandoffFile
	raw, _ := os.ReadFile(path)
	if json.Unmarshal(raw, &hf) != nil || hf.Engines[0].StdoutFD != st.StdoutFD || hf.Engines[0].Session.SessionID != "sess-7" {
		t.Errorf("handoff file %s", raw)
	}

	nA := len(recA.Msgs())
	e2, err := b.Adopt("", engine.AdoptSpec{PID: st.PID, PGID: st.PGID, Stdin: st.Stdin, Stdout: st.Stdout, Stderr: st.Stderr,
		Init: st.Init, Session: st.Session, Capabilities: st.Capabilities, Pending: st.Pending, Opts: st.Opts})
	if err != nil {
		t.Fatal(err)
	}
	recB.WaitFor(t, func(m tea.Msg) bool { a, ok := m.(ext.EngineAttachMsg); return ok && a.Engine == e2 })
	sc := recB.WaitFor(t, func(m tea.Msg) bool { _, ok := m.(ext.SessionChangedMsg); return ok }).(ext.SessionChangedMsg)
	if sc.Info.SessionID != "sess-7" || sc.Info.PermissionMode != "plan" {
		t.Errorf("session after adopt: %+v", sc.Info)
	}
	recB.WaitFor(t, isInitialized)
	recB.WaitFor(t, func(m tea.Msg) bool { c, ok := m.(ext.CommandsMsg); return ok && len(c.Commands) == 1 })
	if !e2.Supports("ui_surface_v1") {
		t.Error("capabilities not restored")
	}

	u2 := "22222222-2222-4222-8222-222222222222"
	e2.Send(ext.Prompt{UUID: u2, Blocks: []proto.ContentBlock{proto.Text("after")}})()
	res := recB.WaitFor(t, resultFor(u2)).(ext.EngineEventMsg).Event.(*proto.Result)
	if res.Result != "two" {
		t.Errorf("result %+v", res)
	}
	time.Sleep(100 * time.Millisecond)
	for _, m := range recA.Msgs()[nA:] {
		t.Errorf("old manager got %T after the hand-off", m)
	}
	e2.Stop(context.Background())
	if x := recB.WaitFor(t, isExit).(ext.EngineExitedMsg); x.Err != nil {
		t.Logf("exit after adopt: %v (a second waiter may reap first in-process)", x.Err)
	}
}
