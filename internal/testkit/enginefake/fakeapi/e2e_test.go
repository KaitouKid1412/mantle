package fakeapi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// claudeE2E runs the real claude headless against an in-process fakeapi.
type claudeE2E struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan map[string]any
	stderr strings.Builder
}

func startClaude(t *testing.T, srvURL, work string, extra ...string) *claudeE2E {
	t.Helper()
	bin, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude not on PATH")
	}
	cfg := t.TempDir()
	if err := SeedConfig(cfg, FakeAPIKey, work); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--output-format", "stream-json", "--input-format", "stream-json", "--verbose",
		"--include-partial-messages", "--permission-prompt-tool", "stdio", "--model", "claude-sonnet-4-5"}, extra...)
	cmd := exec.Command(bin, args...)
	cmd.Dir = work
	cmd.Env = Env(os.Environ(), srvURL, cfg, FakeAPIKey)
	c := &claudeE2E{t: t, cmd: cmd, lines: make(chan map[string]any, 4096)}
	cmd.Stderr = &c.stderr
	c.stdin, _ = cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.stdin.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
		}
	})
	go func() {
		defer close(c.lines)
		br := bufio.NewReaderSize(out, 1<<20)
		for {
			l, err := br.ReadBytes('\n')
			var m map[string]any
			if len(l) > 0 && l[0] == '{' && json.Unmarshal(l, &m) == nil {
				c.lines <- m
			}
			if err != nil {
				return
			}
		}
	}()
	return c
}

func (c *claudeE2E) send(v any) {
	c.t.Helper()
	b, _ := json.Marshal(v)
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		c.t.Fatal(err)
	}
}

func (c *claudeE2E) prompt(uuid, text string) {
	c.send(map[string]any{"type": "user", "session_id": "", "parent_tool_use_id": nil, "uuid": uuid,
		"message": map[string]any{"role": "user", "content": text}, "origin": map[string]any{"kind": "human"}})
}

// until reads lines until match returns true, answering can_use_tool with allow.
func (c *claudeE2E) until(match func(map[string]any) bool) map[string]any {
	c.t.Helper()
	timeout := time.After(60 * time.Second)
	for {
		select {
		case m, ok := <-c.lines:
			if !ok {
				c.t.Fatalf("claude exited; stderr:\n%s", c.stderr.String())
			}
			if m["type"] == "control_request" {
				req, _ := m["request"].(map[string]any)
				if req["subtype"] == "can_use_tool" {
					c.send(map[string]any{"type": "control_response", "response": map[string]any{
						"subtype": "success", "request_id": m["request_id"],
						"response": map[string]any{"behavior": "allow", "toolUseID": req["tool_use_id"]}}})
				}
			}
			if match(m) {
				return m
			}
		case <-timeout:
			c.t.Fatalf("timeout; stderr:\n%s", c.stderr.String())
		}
	}
}

func isType(typ string) func(map[string]any) bool {
	return func(m map[string]any) bool { return m["type"] == typ }
}

// TestClaudeAgainstFakeAPI is spike S14 and the plan 02 done criterion: the real
// engine answers a scripted prompt and runs a Bash tool in a temp dir, offline.
func TestClaudeAgainstFakeAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the real claude binary")
	}
	s := New(&Script{Turns: []Turn{
		{Match: &Match{Contains: "say hello"}, Reply: []Block{{Type: "text", Text: "Hello from fakeapi."}}},
		{Match: &Match{Contains: "make a file"}, Reply: []Block{
			{Type: "text", Text: "Creating it."},
			{Type: "tool_use", Name: "Bash", Input: json.RawMessage(`{"command":"echo hi > out.txt","description":"Write out.txt"}`)},
		}},
		{Match: &Match{ToolResult: "Bash"}, Reply: []Block{{Type: "text", Text: "Done."}}},
	}})
	srv := httptest.NewServer(s)
	defer srv.Close()
	work, _ := filepath.EvalSymlinks(t.TempDir())
	c := startClaude(t, srv.URL, work)

	c.send(map[string]any{"type": "control_request", "request_id": "r1", "request": map[string]any{"subtype": "initialize"}})
	c.until(func(m map[string]any) bool {
		r, _ := m["response"].(map[string]any)
		return m["type"] == "control_response" && r["request_id"] == "r1"
	})

	c.prompt("11111111-1111-4111-8111-111111111111", "say hello")
	res := c.until(isType("result"))
	if res["result"] != "Hello from fakeapi." || res["is_error"] != false {
		t.Fatalf("result: %v", res)
	}

	c.prompt("22222222-2222-4222-8222-222222222222", "make a file")
	res = c.until(isType("result"))
	if res["result"] != "Done." {
		t.Fatalf("result: %v", res)
	}
	b, err := os.ReadFile(filepath.Join(work, "out.txt"))
	if err != nil || strings.TrimSpace(string(b)) != "hi" {
		t.Fatalf("out.txt: %q %v", b, err)
	}
	if s.Remaining() != 0 || len(s.Unmatched()) != 0 {
		t.Errorf("remaining %d unmatched %v", s.Remaining(), s.Unmatched())
	}
	for _, r := range s.Requests() {
		if r.Kind == KindOther && r.Status == 404 {
			t.Logf("unknown endpoint hit: %s %s", r.Method, r.Path)
		}
	}
	fmt.Fprintf(os.Stderr, "fakeapi e2e: %d requests (%d main, %d side)\n",
		len(s.Requests()), len(s.RequestsOf(KindMain)), len(s.RequestsOf(KindSide)))
}
