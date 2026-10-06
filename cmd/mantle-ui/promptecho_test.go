package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
)

// echoReplayDelay is how long the fake engine holds the replay of a prompt, as the
// real one does while hooks run and MCP servers start.
const echoReplayDelay = 3 * time.Second

// echoScript replays the prompt (same uuid) only after echoReplayDelay, then answers.
const echoScript = startupRules + `
{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": INIT}
{"expect": {"type":"user","message":{"content":"say hello to the echo"}}, "timeout": 60000}
{"emit": {"type":"system","subtype":"session_state_changed","state":"running","session_id":"s-echo","uuid":"st1"}}
{"delay": DELAY}
{"emit": {"type":"system","subtype":"init","session_id":"s-echo","uuid":"i1","cwd":"/tmp","tools":[],"mcp_servers":[],"model":"claude-test","permissionMode":"default","slash_commands":[],"apiKeySource":"none","claude_code_version":"2.1.289","output_style":"default"}}
{"emit": {"type":"user","message":{"role":"user","content":"say hello to the echo"},"parent_tool_use_id":null,"session_id":"s-echo","uuid":"${uuid}","isReplay":true}}
{"emit": {"type":"assistant","message":{"id":"m1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"hello from the fake engine"}],"stop_reason":"end_turn"},"parent_tool_use_id":null,"session_id":"s-echo","uuid":"a1"}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"hello from the fake engine","duration_ms":3100,"duration_api_ms":4,"num_turns":1,"session_id":"s-echo","uuid":"r1","total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}}
{"emit": {"type":"system","subtype":"session_state_changed","state":"idle","session_id":"s-echo","uuid":"st2"}}
`

// TestPromptShowsOnEnter: the prompt is in the transcript as soon as Enter is pressed,
// as in Claude Code, not only when the engine replays it; once the replay arrives it
// is still there exactly once. Both layouts.
func TestPromptShowsOnEnter(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	ui, fake := buildBinaries(t)
	for _, layout := range []string{"default", "fullscreen"} {
		t.Run(layout, func(t *testing.T) {
			project, _ := filepath.EvalSymlinks(t.TempDir())
			script := filepath.Join(t.TempDir(), "echo.jsonl")
			s := strings.Replace(echoScript, "INIT", string(enginefake.DefaultInitializeResponse), 1)
			s = strings.Replace(s, "DELAY", strconv.Itoa(int(echoReplayDelay/time.Millisecond)), 1)
			os.WriteFile(script, []byte(s), 0o644)

			cmd := exec.Command(ui)
			cmd.Dir = project
			cmd.Env = smokeEnv(t, fake, script, project)
			for _, kv := range cmd.Env {
				if v, ok := strings.CutPrefix(kv, "CLAUDE_CONFIG_DIR="); ok {
					os.WriteFile(filepath.Join(v, "settings.json"), []byte(`{"tui":"`+layout+`"}`), 0o644)
				}
			}
			p := testkit.StartProcess(t, cmd, testkit.WithSize(100, 30))
			all := func() string { return strings.Join(p.All(), "\n") }
			p.WaitFor(func(s string) bool { return strings.Contains(s, "manual approval") }, ptyWait) // the footer is up

			const prompt = "say hello to the echo"
			p.Type(prompt)
			p.WaitForText(prompt, ptyWait) // in the box
			sent := time.Now()
			p.Send("enter")
			// The box empties and the prompt line is in the transcript.
			p.WaitFor(func(string) bool { return promptLines(p.All(), prompt) == 1 && boxEmpty(p.Screen()) }, ptyWait)
			if d := time.Since(sent); d >= echoReplayDelay {
				t.Skipf("the prompt showed after %v, not before the replay: too slow to tell", d)
			}

			p.WaitFor(func(string) bool { return strings.Contains(all(), "hello from the fake engine") }, ptyWait)
			p.Settle(200*time.Millisecond, 5*time.Second)
			lines := p.All()
			if n := promptLines(lines, prompt); n != 1 {
				t.Fatalf("the prompt shows %d times after the replay, want 1\n%s", n, all())
			}
			at, answer := -1, -1
			for i, l := range lines {
				if strings.Contains(l, "❯ "+prompt) {
					at = i
				}
				if strings.Contains(l, "hello from the fake engine") {
					answer = i
				}
			}
			if at < 0 || answer < at {
				t.Fatalf("prompt at %d, answer at %d\n%s", at, answer, all())
			}

			quitWithCtrlC(t, p)
			if code := p.ExitCode(ptyWait); code != 0 {
				t.Fatalf("exit code %d\n%s", code, all())
			}
		})
	}
}

// promptLines counts the transcript lines that show the prompt.
func promptLines(lines []string, prompt string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, "❯ "+prompt) {
			n++
		}
	}
	return n
}

// boxEmpty reports whether the input box (the "❯" line between the two rules at the
// bottom) is empty.
func boxEmpty(screen string) bool {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); strings.HasPrefix(t, "❯") {
			return t == "❯"
		}
	}
	return false
}
