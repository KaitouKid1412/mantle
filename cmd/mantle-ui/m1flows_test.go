package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
)

// m1Script: the first prompt triggers a Bash permission request; once allowed, the
// fake answers. Startup control requests are answered by rule.
const m1Script = startupRules + `
{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": INIT}
{"expect": {"type":"user"}, "timeout": 60000}
{"emit": {"type":"system","subtype":"init","session_id":"s-m1","uuid":"i1","cwd":"/tmp","tools":["Bash"],"mcp_servers":[],"model":"claude-test","permissionMode":"default","slash_commands":["compact","context"],"apiKeySource":"none","claude_code_version":"2.1.289","output_style":"default"}}
{"emit": {"type":"assistant","message":{"id":"m1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls -la /tmp/m1-probe","description":"List the probe directory"}}],"stop_reason":"tool_use"},"parent_tool_use_id":null,"session_id":"s-m1","uuid":"a1"}}
{"request": {"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls -la /tmp/m1-probe","description":"List the probe directory"},"tool_use_id":"tu1"}, "id": "perm1"}
{"expect": {"type":"control_response","response":{"request_id":"perm1","response":{"behavior":"allow"}}}, "timeout": 60000}
{"emit": {"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"probe-file.txt"}]},"parent_tool_use_id":null,"session_id":"s-m1","uuid":"u2"}}
{"emit": {"type":"assistant","message":{"id":"m2","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"listing finished OK"}],"stop_reason":"end_turn"},"parent_tool_use_id":null,"session_id":"s-m1","uuid":"a2"}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"listing finished OK","duration_ms":5,"duration_api_ms":4,"num_turns":2,"session_id":"s-m1","uuid":"r1","total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}}
`

// TestM1FlowsBothLayouts runs the M1 flows in the inline and fullscreen layouts: the
// user's statusLine command, the / menu with the engine's commands, a permission
// prompt answered from the keyboard, and the streamed reply.
func TestM1FlowsBothLayouts(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	ui, fake := buildBinaries(t)
	for _, layout := range []string{"default", "fullscreen"} {
		t.Run(layout, func(t *testing.T) {
			project, _ := filepath.EvalSymlinks(t.TempDir())
			script := filepath.Join(t.TempDir(), "m1.jsonl")
			os.WriteFile(script, []byte(strings.Replace(m1Script, "INIT", string(enginefake.DefaultInitializeResponse), 1)), 0o644)

			cmd := exec.Command(ui)
			cmd.Dir = project
			cmd.Env = smokeEnv(t, fake, script, project)
			claudeDir := ""
			for _, kv := range cmd.Env {
				if v, ok := strings.CutPrefix(kv, "CLAUDE_CONFIG_DIR="); ok {
					claudeDir = v
				}
			}
			settings := `{"tui":"` + layout + `","statusLine":{"type":"command","command":"echo STATUSLINE-OK"}}`
			os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0o644)
			p := testkit.StartProcess(t, cmd, testkit.WithSize(110, 34))

			all := func() string { return strings.Join(p.All(), "\n") }
			p.WaitFor(func(s string) bool { return strings.Contains(s, "shift+tab to change") }, ptyWait)
			if fs := layout == "fullscreen"; p.AltScreen() != fs {
				t.Fatalf("tui=%s: alt screen = %v", layout, p.AltScreen())
			}

			// The / menu (plan 04) lists the engine's commands.
			p.Type("/")
			p.WaitForText("/add-dir", ptyWait)
			p.Type("compa") // filter: the engine's /compact
			p.WaitForText("/compact", ptyWait)
			p.Send("esc")
			p.Send("ctrl+u")

			// A prompt that needs a Bash permission (plan 05), allowed with enter.
			p.Type("list the probe directory")
			p.Send("enter")
			// Wait for the dialog itself: the command also shows in the transcript (from
			// the tool_use) before the permission request opens it.
			p.WaitFor(func(s string) bool {
				return strings.Contains(s, "ls -la /tmp/m1-probe") && strings.Contains(s, "Do you want to proceed?")
			}, ptyWait)
			p.Send("enter")
			p.WaitFor(func(string) bool { return strings.Contains(all(), "listing finished OK") }, ptyWait)

			// statusLine (plan 07) runs the user's command once a session exists.
			p.WaitForText("STATUSLINE-OK", ptyWait)

			quitWithCtrlC(t, p)
			if code := p.ExitCode(ptyWait); code != 0 {
				t.Fatalf("exit code %d\n%s", code, all())
			}
			if p.AltScreen() {
				t.Fatal("alt screen left on after exit")
			}
		})
	}
}

// TestResumePickerBothLayouts: `mantle -r` opens plan 06's resume picker before any
// engine starts, in both layouts; escape leaves it.
func TestResumePickerBothLayouts(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	ui, fake := buildBinaries(t)
	for _, layout := range []string{"default", "fullscreen"} {
		t.Run(layout, func(t *testing.T) {
			project, _ := filepath.EvalSymlinks(t.TempDir())
			script := filepath.Join(t.TempDir(), "r.jsonl")
			os.WriteFile(script, []byte(startupRules+`{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": `+string(enginefake.DefaultInitializeResponse)+"}\n"), 0o644)
			cmd := exec.Command(ui, "-r")
			cmd.Dir = project
			cmd.Env = smokeEnv(t, fake, script, project)
			for _, kv := range cmd.Env {
				if v, ok := strings.CutPrefix(kv, "CLAUDE_CONFIG_DIR="); ok {
					os.WriteFile(filepath.Join(v, "settings.json"), []byte(`{"tui":"`+layout+`"}`), 0o644)
				}
			}
			p := testkit.StartProcess(t, cmd, testkit.WithSize(100, 30))
			p.WaitForText("Resume a conversation", ptyWait)
			if !p.AltScreen() {
				t.Fatal("the resume picker is an alt-screen view")
			}
			p.Send("esc")
			p.WaitFor(func(s string) bool { return !strings.Contains(s, "Resume a conversation") }, ptyWait)
			if strings.Contains(p.Screen(), "Resume a conversation") {
				t.Fatal("picker still open")
			}
			p.Signal(syscall.SIGTERM)
			p.ExitCode(ptyWait)
		})
	}
}
