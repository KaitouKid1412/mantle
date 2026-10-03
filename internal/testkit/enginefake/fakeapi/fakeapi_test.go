package fakeapi

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func post(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("x-api-key", FakeAPIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

type sse struct {
	name string
	data map[string]any
}

func readSSE(t *testing.T, r io.Reader) []sse {
	t.Helper()
	var out []sse
	sc := bufio.NewScanner(r)
	var cur sse
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cur.data); err != nil {
				t.Fatalf("bad data: %s", line)
			}
		case line == "":
			if cur.name != "" {
				if cur.data["type"] != cur.name {
					t.Errorf("event %s has data type %v", cur.name, cur.data["type"])
				}
				out = append(out, cur)
			}
			cur = sse{}
		}
	}
	return out
}

const mainReq = `{"model":"m","max_tokens":1000,"stream":true,"tools":[{"name":"Bash"}],"messages":[{"role":"user","content":%s}]}`

func TestStreamingTextAndTool(t *testing.T) {
	s := New(&Script{Turns: []Turn{
		{Match: &Match{Contains: "hello"}, Reply: []Block{{Type: "thinking", Thinking: "hmm, let me think"}, {Type: "text", Text: "Hello there, friend!"}}},
		ToolTurn("", "Bash", map[string]string{"command": "echo hi"}),
	}})
	srv := httptest.NewServer(s)
	defer srv.Close()

	resp := post(t, srv.URL+"/v1/messages", strings.Replace(mainReq, "%s", `"hello"`, 1))
	evs := readSSE(t, resp.Body)
	resp.Body.Close()
	var names []string
	text, thinking, sig := "", "", ""
	for _, e := range evs {
		names = append(names, e.name)
		if d, ok := e.data["delta"].(map[string]any); ok {
			switch d["type"] {
			case "text_delta":
				text += d["text"].(string)
			case "thinking_delta":
				thinking += d["thinking"].(string)
			case "signature_delta":
				sig = d["signature"].(string)
			}
		}
	}
	if names[0] != "message_start" || names[len(names)-1] != "message_stop" || names[len(names)-2] != "message_delta" {
		t.Errorf("order: %v", names)
	}
	if text != "Hello there, friend!" || thinking != "hmm, let me think" || sig == "" {
		t.Errorf("text=%q thinking=%q sig=%q", text, thinking, sig)
	}
	deltas := strings.Count(strings.Join(names, " "), "content_block_delta")
	if deltas < 5 {
		t.Errorf("too few deltas (%d): streaming not exercised", deltas)
	}

	resp = post(t, srv.URL+"/v1/messages", strings.Replace(mainReq, "%s", `"go"`, 1))
	evs = readSSE(t, resp.Body)
	resp.Body.Close()
	input, stop := "", ""
	for _, e := range evs {
		if d, ok := e.data["delta"].(map[string]any); ok {
			if d["type"] == "input_json_delta" {
				input += d["partial_json"].(string)
			}
			if sr, ok := d["stop_reason"].(string); ok {
				stop = sr
			}
		}
	}
	if input != `{"command":"echo hi"}` || stop != "tool_use" {
		t.Errorf("input=%q stop=%q", input, stop)
	}
	if s.Consumed() != 2 || s.Remaining() != 0 {
		t.Errorf("consumed %d", s.Consumed())
	}
}

func TestSideCallsAndMatching(t *testing.T) {
	s := New(&Script{Turns: []Turn{
		{Match: &Match{ToolResult: "Bash"}, Reply: []Block{{Type: "text", Text: "Done."}}},
	}, SideReply: "A Title"})
	srv := httptest.NewServer(s)
	defer srv.Close()

	// Side call (no tools): generic reply, nothing consumed. Non-streaming JSON.
	resp := post(t, srv.URL+"/v1/messages", `{"model":"haiku","max_tokens":50,"messages":[{"role":"user","content":"title this"}]}`)
	var msg map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&msg)
	resp.Body.Close()
	if msg["content"].([]any)[0].(map[string]any)["text"] != "A Title" || s.Consumed() != 0 {
		t.Errorf("side: %v", msg)
	}

	// Main request without the tool_result: unmatched, not consumed.
	post(t, srv.URL+"/v1/messages", strings.Replace(mainReq, "%s", `"nope"`, 1)).Body.Close()
	if s.Consumed() != 0 || len(s.Unmatched()) != 1 {
		t.Errorf("unmatched: %v", s.Unmatched())
	}

	// With a tool_result for a Bash tool_use: matches.
	body := `{"model":"m","max_tokens":10,"stream":true,"tools":[{"name":"Bash"}],"messages":[
		{"role":"user","content":"go"},
		{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"hi"}]}]}`
	post(t, srv.URL+"/v1/messages", body).Body.Close()
	if s.Consumed() != 1 {
		t.Error("tool_result match")
	}

	// Count tokens and unknown paths.
	resp = post(t, srv.URL+"/v1/messages/count_tokens", `{"messages":[{"role":"user","content":"hello world"}]}`)
	var ct map[string]int
	_ = json.NewDecoder(resp.Body).Decode(&ct)
	resp.Body.Close()
	if ct["input_tokens"] < 1 {
		t.Errorf("count_tokens %v", ct)
	}
	if resp := post(t, srv.URL+"/v1/nope", `{}`); resp.StatusCode != 404 {
		t.Errorf("unknown path: %d", resp.StatusCode)
	}

	kinds := map[Kind]int{}
	for _, r := range s.Requests() {
		kinds[r.Kind]++
		if r.Header.Get("X-Api-Key") != "REDACTED" {
			t.Errorf("key not redacted: %v", r.Header)
		}
	}
	if kinds[KindSide] != 1 || kinds[KindMain] != 2 || kinds[KindCountTokens] != 1 || kinds[KindOther] != 1 {
		t.Errorf("kinds %v", kinds)
	}
}

func TestScriptedError(t *testing.T) {
	s := New(&Script{Turns: []Turn{{Error: &APIError{Status: 529, Type: "overloaded_error", Message: "busy"}}}})
	srv := httptest.NewServer(s)
	defer srv.Close()
	resp := post(t, srv.URL+"/v1/messages", strings.Replace(mainReq, "%s", `"x"`, 1))
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != 529 || body["error"].(map[string]any)["type"] != "overloaded_error" {
		t.Errorf("%d %v", resp.StatusCode, body)
	}
}

func TestParseValidation(t *testing.T) {
	for _, bad := range []string{
		`{"turns":[{"reply":[]}]}`,
		`{"turns":[{"reply":[{"type":"image"}]}]}`,
		`{"turns":[{"reply":[{"type":"tool_use"}]}]}`,
		`{"turns":[{"match":{"regex":"("},"reply":[{"type":"text","text":"x"}]}]}`,
		`{"turns":[{"error":{"status":200}}]}`,
		`{"bogus":1}`,
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%s: want error", bad)
		}
	}
	s, err := Parse([]byte(`[{"reply":[{"type":"text","text":"x"}]}]`))
	if err != nil || len(s.Turns) != 1 {
		t.Errorf("bare array: %v", err)
	}
}

func TestSeedConfigAndEnv(t *testing.T) {
	dir := t.TempDir()
	work := t.TempDir()
	if err := SeedConfig(dir, FakeAPIKey, work); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".claude.json"))
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil || cfg["hasCompletedOnboarding"] != true {
		t.Fatalf("config %s", b)
	}
	if len(cfg["projects"].(map[string]any)) != 1 {
		t.Error("trusted project missing")
	}
	env := strings.Join(Env([]string{"PATH=/bin", "ANTHROPIC_API_KEY=real", "CLAUDE_CONFIG_DIR=/home/x/.claude", "CLAUDECODE=1"}, "http://h", dir, FakeAPIKey), "\n")
	if strings.Contains(env, "real") || strings.Contains(env, "/home/x") || strings.Contains(env, "CLAUDECODE") ||
		!strings.Contains(env, "ANTHROPIC_BASE_URL=http://h") || !strings.Contains(env, "PATH=/bin") {
		t.Errorf("env:\n%s", env)
	}
}
