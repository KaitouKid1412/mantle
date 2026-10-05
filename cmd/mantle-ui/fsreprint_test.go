package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
)

// fsClearScript: the resumed engine answers /clear with a conversation reset, then a
// prompt in the fresh conversation.
const fsClearScript = startupRules + `
{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": INIT}
{"expect": {"type":"user"}, "timeout": 60000}
{"emit": {"type":"conversation_reset","new_conversation_id":"c-new","trigger":"clear","session_id":"11111111-1111-4111-8111-111111111111","uuid":"cr1"}}
{"emit": {"type":"system","subtype":"init","session_id":"s-fresh","uuid":"i2","cwd":"/tmp","tools":[],"mcp_servers":[],"model":"claude-test","permissionMode":"default","slash_commands":[],"apiKeySource":"none","claude_code_version":"2.1.289","output_style":"default"}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"","duration_ms":1,"duration_api_ms":0,"num_turns":0,"session_id":"s-fresh","uuid":"r1","total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0}}}
{"expect": {"type":"user"}, "timeout": 60000}
{"emit": {"type":"assistant","message":{"id":"m9","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"fresh reply after clear"}],"stop_reason":"end_turn"},"parent_tool_use_id":null,"session_id":"s-fresh","uuid":"a9"}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"fresh reply after clear","duration_ms":5,"duration_api_ms":4,"num_turns":1,"session_id":"s-fresh","uuid":"r2","total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}}
`

// TestFullscreenResumeAndClear: in the fullscreen layout, `-r` → pick a session shows
// its history, and /clear empties the transcript and the next turn shows. Both wait
// on ScreenClearedMsg, which fullscreen must deliver though it has no scrollback.
func TestFullscreenResumeAndClear(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	ui, fake := buildBinaries(t)
	project, _ := filepath.EvalSymlinks(t.TempDir())
	script := filepath.Join(t.TempDir(), "fs.jsonl")
	os.WriteFile(script, []byte(strings.Replace(fsClearScript, "INIT", string(enginefake.DefaultInitializeResponse), 1)), 0o644)

	cmd := exec.Command(ui, "-r")
	cmd.Dir = project
	cmd.Env = smokeEnv(t, fake, script, project)
	for _, kv := range cmd.Env {
		v, ok := strings.CutPrefix(kv, "CLAUDE_CONFIG_DIR=")
		if !ok {
			continue
		}
		os.WriteFile(filepath.Join(v, "settings.json"), []byte(`{"tui":"fullscreen"}`), 0o644)
		// One past conversation in this project (plan 06's fixture, moved here).
		const id = "11111111-1111-4111-8111-111111111111"
		raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "testdata/fixtures/06/claude/projects/-work-demo", id+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(v, "projects", sessions.Slug(project))
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(strings.ReplaceAll(string(raw), `"/work/demo"`, `"`+project+`"`)), 0o600)
	}
	p := testkit.StartProcess(t, cmd, testkit.WithSize(100, 30))

	p.WaitForText("Explain the build", ptyWait) // the picker lists the session
	if !p.AltScreen() {
		t.Fatal("tui=fullscreen should run on the alternate screen")
	}
	p.Send("enter")
	p.WaitFor(func(s string) bool {
		return strings.Contains(s, "The build uses make.") && !strings.Contains(s, "Resume a conversation")
	}, ptyWait)

	p.Type("/clear")
	p.WaitForText("Start a new conversation", ptyWait) // the / menu shows it
	p.Send("enter")
	p.WaitFor(func(s string) bool { return !strings.Contains(s, "The build uses make.") }, ptyWait)

	p.Type("hello again")
	p.Send("enter")
	p.WaitForText("fresh reply after clear", ptyWait)
	if strings.Contains(p.Screen(), "The build uses make.") {
		t.Fatalf("history from before /clear came back\n%s", p.Screen())
	}

	quitWithCtrlC(t, p)
	if code := p.ExitCode(ptyWait); code != 0 {
		t.Fatalf("exit code %d\n%s", code, p.Screen())
	}
}
