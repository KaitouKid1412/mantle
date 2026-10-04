package enginetest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// TestMain lets the test binary act as the two halves of a real exec hand-off.
func TestMain(m *testing.M) {
	switch os.Getenv("MANTLE_HANDOFF_STAGE") {
	case "1":
		os.Exit(handoffStage1())
	case "2":
		os.Exit(handoffStage2())
	}
	os.Exit(m.Run())
}

// waitMsg waits for a message matching match on ch.
func waitMsg(ch <-chan tea.Msg, match func(tea.Msg) bool) (tea.Msg, error) {
	timeout := time.After(15 * time.Second)
	for {
		select {
		case m := <-ch:
			if match(m) {
				return m, nil
			}
		case <-timeout:
			return nil, fmt.Errorf("timed out")
		}
	}
}

func resultMsg(uuid string) func(tea.Msg) bool {
	return func(m tea.Msg) bool { return resultFor(uuid)(m) }
}

// handoffStage1: start the engine, run a turn, hand off, exec stage 2 in place.
func handoffStage1() int {
	ch := make(chan tea.Msg, 1024)
	m := engine.NewManager(func(msg tea.Msg) { ch <- msg })
	m.Binary, m.RunDir = os.Getenv("MANTLE_HANDOFF_BIN"), "-"
	e, err := m.Start("", ext.SpawnOpts{})
	if err != nil {
		fmt.Println("stage1 start:", err)
		return 1
	}
	if _, err := waitMsg(ch, isInitialized); err != nil {
		fmt.Println("stage1 init:", err)
		return 1
	}
	u := "11111111-1111-4111-8111-111111111111"
	e.Send(ext.Prompt{UUID: u, Blocks: []proto.ContentBlock{proto.Text("before")}})()
	if _, err := waitMsg(ch, resultMsg(u)); err != nil {
		fmt.Println("stage1 result:", err)
		return 1
	}
	st, err := e.PrepareHandoff(context.Background())
	if err != nil {
		fmt.Println("stage1 handoff:", err)
		return 1
	}
	path := os.Getenv("MANTLE_HANDOFF_FILE")
	if err := engine.WriteHandoffFile(path, engine.HandoffFile{Engines: []engine.HandoffState{st}}); err != nil {
		fmt.Println("stage1 write:", err)
		return 1
	}
	fmt.Printf("stage1 pid=%d engine=%d\n", os.Getpid(), st.PID)
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "MANTLE_HANDOFF_STAGE=") {
			env = append(env, kv)
		}
	}
	env = append(env, "MANTLE_HANDOFF_STAGE=2")
	err = syscall.Exec(os.Args[0], os.Args, env)
	fmt.Println("stage1 exec:", err)
	return 1
}

// handoffStage2: adopt the engine from the file and run another turn.
func handoffStage2() int {
	ch := make(chan tea.Msg, 1024)
	m := engine.NewManager(func(msg tea.Msg) { ch <- msg })
	m.Binary, m.RunDir = os.Getenv("MANTLE_HANDOFF_BIN"), "-"
	es, err := m.AdoptFile(os.Getenv("MANTLE_HANDOFF_FILE"))
	if err != nil || len(es) != 1 {
		fmt.Println("stage2 adopt:", err)
		return 1
	}
	e := es[0]
	sc, err := waitMsg(ch, func(m tea.Msg) bool { _, ok := m.(ext.SessionChangedMsg); return ok })
	if err != nil {
		fmt.Println("stage2 session:", err)
		return 1
	}
	u := "22222222-2222-4222-8222-222222222222"
	e.Send(ext.Prompt{UUID: u, Blocks: []proto.ContentBlock{proto.Text("after")}})()
	res, err := waitMsg(ch, resultMsg(u))
	if err != nil {
		fmt.Println("stage2 result:", err)
		return 1
	}
	r := res.(ext.EngineEventMsg).Event.(*proto.Result)
	fmt.Printf("stage2 pid=%d session=%s result=%s\n", os.Getpid(), sc.(ext.SessionChangedMsg).Info.SessionID, r.Result)
	m.Close(context.Background())
	x, err := waitMsg(ch, func(m tea.Msg) bool { _, ok := m.(ext.EngineExitedMsg); return ok })
	if err != nil {
		fmt.Println("stage2 exit:", err)
		return 1
	}
	fmt.Printf("stage2 exited err=%v\n", x.(ext.EngineExitedMsg).Err)
	return 0
}

// TestHandoffAcrossExec runs the hand-off for real: stage 1 execs stage 2 in place.
func TestHandoffAcrossExec(t *testing.T) {
	bin := buildFakeclaude(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "s.jsonl")
	os.WriteFile(script, []byte(`{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": {"commands":[]}}
{"expect": {"type":"user","message":{"content":"before"}}}
{"emit": {"type":"system","subtype":"init","session_id":"sess-9","cwd":"/w","tools":[],"mcp_servers":[],"model":"m","permissionMode":"default","slash_commands":[],"claude_code_version":"2.1.289","output_style":"default"}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"one","user_message_uuid":"${uuid}"}}
{"expect": {"type":"user","message":{"content":"after"}}, "timeout": 15000}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"two","user_message_uuid":"${uuid}"}}
`), 0o644)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "MANTLE_HANDOFF_STAGE=1", "MANTLE_HANDOFF_BIN="+bin,
		"MANTLE_HANDOFF_FILE="+filepath.Join(dir, "handoff.json"), "FAKECLAUDE_SCRIPT="+script)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	s := out.String()
	t.Log(s)
	var pid1, pid2 string
	fmt.Sscanf(s[strings.Index(s, "stage1 pid="):], "stage1 pid=%s", &pid1)
	if i := strings.Index(s, "stage2 pid="); i >= 0 {
		fmt.Sscanf(s[i:], "stage2 pid=%s", &pid2)
	}
	if pid1 == "" || pid1 != pid2 {
		t.Errorf("exec must keep the pid: %q vs %q", pid1, pid2)
	}
	if !strings.Contains(s, "session=sess-9 result=two") || !strings.Contains(s, "stage2 exited err=<nil>") {
		t.Errorf("stage 2 did not finish the session:\n%s", s)
	}
	if _, err := os.Stat(filepath.Join(dir, "handoff.json")); err == nil {
		t.Error("the hand-off file must be deleted after adopting")
	}
}
