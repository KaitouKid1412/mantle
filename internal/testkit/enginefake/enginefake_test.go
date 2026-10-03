package enginefake

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestClientDrivesEngine runs a client-mode script against an engine-mode script.
func TestClientDrivesEngine(t *testing.T) {
	engine := MustParse(`
{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": {"commands":[]}}
{"expect": {"type":"user","uuid":"re:^[0-9a-f-]{36}$"}}
{"emit": {"type":"stream_event","event":{"type":"message_start"}}}
{"request": {"subtype":"can_use_tool","tool_name":"Bash","input":{},"tool_use_id":"t1"}}
{"expect": {"type":"control_response","response":{"request_id":"cli_1","response":{"behavior":"allow"}}}}
{"emit": {"type":"result","subtype":"success","user_message_uuid":"${uuid}"}}
`)
	client := MustParse(`
{"client": true}
{"on": {"type":"control_request","request":{"subtype":"can_use_tool"}}, "respond": {"behavior":"allow","toolUseID":"t1"}}
{"emit": {"type":"control_request","request_id":"r1","request":{"subtype":"initialize"}}}
{"expect": {"type":"control_response","response":{"request_id":"r1"}}}
{"emit": {"type":"user","uuid":"${new:prompt}","message":{"role":"user","content":"hi"}}}
{"expect": {"type":"result"}, "save": {"echo": "user_message_uuid"}}
`)
	if !client.Client {
		t.Fatal("client directive not parsed")
	}
	toEngR, toEngW := io.Pipe()
	toCliR, toCliW := io.Pipe()
	engDone := make(chan error, 1)
	go func() {
		_, err := engine.Run(t.Context(), toEngR, toCliW, nil)
		toCliW.Close()
		engDone <- err
	}()
	code, err := client.Run(t.Context(), toCliR, toEngW, nil)
	toEngW.Close()
	if code != 0 || err != nil {
		t.Fatalf("client: %d %v", code, err)
	}
	if err := <-engDone; err != nil {
		t.Fatalf("engine: %v", err)
	}
}

// client drives a Proc like the engine would: send lines, read lines.
type client struct {
	t   *testing.T
	p   *Proc
	out *bufio.Reader
}

func newClient(t *testing.T, s *Script) *client {
	p := Start(s)
	t.Cleanup(p.Kill)
	return &client{t: t, p: p, out: bufio.NewReader(p.Stdout)}
}

func (c *client) send(line string) {
	c.t.Helper()
	if _, err := io.WriteString(c.p.Stdin, line+"\n"); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) recv() map[string]any {
	c.t.Helper()
	type res struct {
		line string
		err  error
	}
	ch := make(chan res, 1)
	go func() { l, err := c.out.ReadString('\n'); ch <- res{l, err} }()
	select {
	case r := <-ch:
		if r.err != nil {
			c.t.Fatalf("recv: %v", r.err)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(r.line), &m); err != nil {
			c.t.Fatalf("recv bad json %q", r.line)
		}
		return m
	case <-time.After(5 * time.Second):
		c.t.Fatal("recv timeout")
		return nil
	}
}

const turnScript = `
# Answer initialize whenever it comes.
{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": {"commands":[],"models":[],"account":{},"pid":1,"current_permission_mode":"default"}}
{"expect": {"type":"user","message":{"content":"re:^hello"}}, "save": {"text": "message.content"}}
{"emit": {"type":"system","subtype":"init","session_id":"s1","cwd":"/w","tools":[],"mcp_servers":[],"model":"m","permissionMode":"default","slash_commands":[],"claude_code_version":"2.1.288","output_style":"default"}}
{"request": {"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"},"tool_use_id":"toolu_1"}, "id": "cli_7"}
{"expect": {"type":"control_response","response":{"subtype":"success","request_id":"cli_7","response":{"behavior":"allow"}}}}
{"emit": {"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"you said ${text} (${uuid})"}]}}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"ok","user_message_uuid":"${uuid}"}}
`

func TestScriptedTurn(t *testing.T) {
	c := newClient(t, MustParse(turnScript))
	c.send(`{"type":"control_request","request_id":"req_1","request":{"subtype":"initialize"}}`)
	if m := c.recv(); m["type"] != "control_response" || m["response"].(map[string]any)["request_id"] != "req_1" {
		t.Fatalf("initialize reply: %v", m)
	}
	c.send(`{"type":"keep_alive"}`)
	c.send(`{"type":"user","uuid":"u-1","message":{"role":"user","content":"hello \"world\""}}`)
	if m := c.recv(); m["subtype"] != "init" {
		t.Fatalf("want init, got %v", m)
	}
	req := c.recv()
	if req["type"] != "control_request" || req["request_id"] != "cli_7" {
		t.Fatalf("want can_use_tool, got %v", req)
	}
	c.send(`{"type":"control_response","response":{"subtype":"success","request_id":"cli_7","response":{"behavior":"allow","updatedInput":{}}}}`)
	a := c.recv()
	text := a["message"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]
	if text != `you said hello "world" (u-1)` {
		t.Errorf("substitution: %q", text)
	}
	if r := c.recv(); r["type"] != "result" || r["user_message_uuid"] != "u-1" {
		t.Errorf("result: %v", r)
	}
	// Steps are done; end_session ends the run with 0.
	c.send(`{"type":"control_request","request_id":"req_2","request":{"subtype":"end_session"}}`)
	if m := c.recv(); m["response"].(map[string]any)["request_id"] != "req_2" {
		t.Errorf("end_session reply: %v", m)
	}
	code, err := c.p.Wait()
	if code != 0 || err != nil {
		t.Errorf("exit %d %v", code, err)
	}
	if n := len(c.p.Received()); n != 5 {
		t.Errorf("received %d lines", n)
	}
}

func TestExpectMismatchFails(t *testing.T) {
	c := newClient(t, MustParse(`{"expect": {"type":"user","priority":"now"}}`))
	c.send(`{"type":"user","priority":"later"}`)
	code, err := c.p.Wait()
	if code != FailCode || err == nil || !strings.Contains(err.Error(), "priority") {
		t.Errorf("code=%d err=%v", code, err)
	}
}

func TestExpectTimeoutAndEOF(t *testing.T) {
	c := newClient(t, MustParse(`{"expect": {"type":"user"}, "timeout": 50}`))
	if code, err := c.p.Wait(); code != FailCode || err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("timeout: code=%d err=%v", code, err)
	}
	c = newClient(t, MustParse(`{"expect": {"type":"user"}}`))
	c.p.Stdin.Close()
	if code, err := c.p.Wait(); code != FailCode || err == nil || !strings.Contains(err.Error(), "input closed") {
		t.Errorf("eof: code=%d err=%v", code, err)
	}
}

func TestExitAndStderr(t *testing.T) {
	s := New(Step{Stderr: "boom\n"}, Exit(7))
	p := Start(s)
	errOut, _ := io.ReadAll(p.Stderr)
	if code, err := p.Wait(); code != 7 || err != nil {
		t.Errorf("code=%d err=%v", code, err)
	}
	if string(errOut) != "boom\n" {
		t.Errorf("stderr %q", errOut)
	}
}

func TestRespondErrorAndPatterns(t *testing.T) {
	s := New(
		ExpectRespond(map[string]any{"type": "control_request", "request": map[string]any{"subtype": "$any"}}, nil),
		Step{Expect: toRaw(`{"type":"control_request"}`), RespondError: "Unsupported control request subtype: x"},
	)
	c := newClient(t, s)
	c.send(`{"type":"control_request","request_id":"a","request":{"subtype":"get_settings"}}`)
	if m := c.recv(); m["response"].(map[string]any)["subtype"] != "success" {
		t.Errorf("got %v", m)
	}
	c.send(`{"type":"control_request","request_id":"b","request":{"subtype":"x"}}`)
	m := c.recv()["response"].(map[string]any)
	if m["subtype"] != "error" || m["request_id"] != "b" {
		t.Errorf("got %v", m)
	}
}

func TestParseErrors(t *testing.T) {
	for _, src := range []string{
		`{"emit": {"a":1}, "delay": 5}`,
		`{"bogus": 1}`,
		`{"emit": {"a":}`,
		`{}`,
	} {
		if _, err := ParseString(src); err == nil {
			t.Errorf("%s: want error", src)
		}
	}
	if s, err := ParseString("# comment\n// another\n\n{\"#\": \"note only\"}\n{\"delay\": 1}\n"); err != nil || len(s.Steps) != 2 {
		t.Errorf("comments: %v %v", s, err)
	}
}

// TestBinary builds cmd/fakeclaude and runs it as a process.
func TestBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")
	build := exec.Command("go", "build", "-o", bin, "./cmd/fakeclaude")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	script := filepath.Join(dir, "s.jsonl")
	os.WriteFile(script, []byte(`{"expect": {"type":"user"}}
{"emit": {"type":"result","subtype":"success","result":"${uuid}"}}
`), 0o644)
	logPath := filepath.Join(dir, "log.json")
	cmd := exec.Command(bin, "--output-format", "stream-json", "--verbose")
	cmd.Env = append(os.Environ(), "FAKECLAUDE_SCRIPT="+script, "FAKECLAUDE_LOG="+logPath, "CLAUDE_CODE_ENTRYPOINT=x")
	cmd.Stdin = strings.NewReader(`{"type":"user","uuid":"abc"}` + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != `{"type":"result","subtype":"success","result":"abc"}` {
		t.Errorf("stdout %q", out)
	}
	var log struct {
		Argv  []string
		Env   map[string]string
		Stdin []string
	}
	b, _ := os.ReadFile(logPath)
	if err := json.Unmarshal(b, &log); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(log.Argv) != "[--output-format stream-json --verbose]" || log.Env["CLAUDE_CODE_ENTRYPOINT"] != "x" || len(log.Stdin) != 1 {
		t.Errorf("log %+v", log)
	}
	v, _ := exec.Command(bin, "--version").Output()
	if strings.TrimSpace(string(v)) != "2.1.288 (Claude Code)" {
		t.Errorf("version %q", v)
	}
}
