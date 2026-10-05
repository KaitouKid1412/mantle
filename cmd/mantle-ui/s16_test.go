package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
)

// TestSuspendResumeUnderShell is spike S16's job-control check: mantle-ui runs under an
// interactive shell (so its process group is a real job), ctrl+z suspends it and the
// shell takes the terminal back, `fg` resumes it and the UI redraws, and the engine,
// in its own process group, keeps running so the session continues afterwards.
func TestSuspendResumeUnderShell(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	ui, fake := buildBinaries(t)
	project, _ := filepath.EvalSymlinks(t.TempDir())
	script := filepath.Join(t.TempDir(), "s16.jsonl")
	// One turn before the suspend.
	turn := func(n string) string {
		return `{"expect": {"type":"user"}, "timeout": 60000}
{"emit": {"type":"assistant","message":{"id":"m` + n + `","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"reply ` + n + `"}],"stop_reason":"end_turn"},"parent_tool_use_id":null,"session_id":"s16","uuid":"a` + n + `"}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"reply ` + n + `","duration_ms":1,"duration_api_ms":1,"num_turns":1,"session_id":"s16","uuid":"r` + n + `","total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}}
`
	}
	body := startupRules + `{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": ` + string(enginefake.DefaultInitializeResponse) + "}\n" + turn("1")
	os.WriteFile(script, []byte(body), 0o644)

	cmd := exec.Command(bash, "--norc", "--noprofile", "-i")
	cmd.Dir = project
	cmd.Env = append(smokeEnv(t, fake, script, project), "PS1=SHELL$ ")
	p := testkit.StartProcess(t, cmd, testkit.WithSize(100, 30))
	p.WaitForText("SHELL$", ptyWait)

	p.Type(ui + " 'first turn'\r")
	p.WaitFor(func(string) bool { return strings.Contains(strings.Join(p.All(), "\n"), "reply 1") }, ptyWait)

	p.Send("ctrl+z")
	p.WaitFor(func(string) bool {
		all := strings.Join(p.All(), "\n")
		return strings.Contains(all, "Stopped") && strings.HasSuffix(strings.TrimSpace(p.Screen()), "SHELL$")
	}, ptyWait)

	p.Type("fg\r")
	// The UI redraws its live area after SIGCONT.
	p.WaitFor(func(s string) bool { return strings.Contains(s, "shift+tab") || strings.Contains(s, "for shortcuts") }, ptyWait)

	// The engine (its own process group) was not stopped or killed with the UI.
	var mantleHome string
	for _, kv := range cmd.Env {
		if v, ok := strings.CutPrefix(kv, "MANTLE_HOME="); ok {
			mantleHome = v
		}
	}
	recs, _ := filepath.Glob(filepath.Join(mantleHome, "run", "*.json"))
	if len(recs) != 1 {
		t.Fatalf("run records: %v", recs)
	}
	var rec struct {
		PID int `json:"pid"`
	}
	raw, _ := os.ReadFile(recs[0])
	json.Unmarshal(raw, &rec)
	if err := syscall.Kill(rec.PID, 0); err != nil {
		t.Fatalf("engine pid %d gone after suspend/resume: %v", rec.PID, err)
	}
	out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(rec.PID)).Output()
	if strings.HasPrefix(strings.TrimSpace(string(out)), "T") {
		t.Fatalf("engine left stopped after fg (ps stat %q)", out)
	}

	quitWithCtrlC(t, p)
	p.WaitFor(func(s string) bool { return strings.HasSuffix(strings.TrimSpace(s), "SHELL$") }, ptyWait)
	p.Type("exit\r")
	if code := p.ExitCode(ptyWait); code != 0 {
		t.Logf("shell exit %d", code)
	}
}

// TestCtrlCInsideEditor is spike S16's $EDITOR check: ctrl+g opens the prompt in
// $EDITOR through tea.ExecProcess (terminal back in cooked mode); ctrl+c there sends
// SIGINT to the foreground process group, which kills the editor but must leave
// mantle-ui running (Bubble Tea ignores signals during exec) and the engine untouched
// (its own process group).
func TestCtrlCInsideEditor(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	ui, fake := buildBinaries(t)
	project, _ := filepath.EvalSymlinks(t.TempDir())
	script := filepath.Join(t.TempDir(), "editor.jsonl")
	os.WriteFile(script, []byte(startupRules+`{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": `+string(enginefake.DefaultInitializeResponse)+"}\n"), 0o644)
	editor := filepath.Join(t.TempDir(), "slow-editor")
	os.WriteFile(editor, []byte("#!/bin/sh\necho EDITOR RUNNING\nsleep 60\n"), 0o755)

	cmd := exec.Command(bash, "--norc", "--noprofile", "-i")
	cmd.Dir = project
	cmd.Env = append(smokeEnv(t, fake, script, project), "PS1=SHELL$ ", "EDITOR="+editor, "VISUAL="+editor)
	p := testkit.StartProcess(t, cmd, testkit.WithSize(100, 30))
	p.WaitForText("SHELL$", ptyWait)
	p.Type(ui + "\r")
	p.WaitFor(func(s string) bool { return strings.Contains(s, "for shortcuts") }, ptyWait)
	enginePID := engineRunPID(t, cmd.Env)

	p.Send("ctrl+g")
	p.WaitForText("EDITOR RUNNING", ptyWait)
	p.Send("ctrl+c")

	// mantle-ui comes back (the editor failed, so the prompt is unchanged).
	p.WaitFor(func(s string) bool {
		return strings.Contains(s, "for shortcuts") && !strings.Contains(s, "EDITOR RUNNING")
	}, ptyWait)
	if strings.HasSuffix(strings.TrimSpace(p.Screen()), "SHELL$") {
		t.Fatalf("mantle-ui exited on ctrl+c inside the editor:\n%s", p.Screen())
	}
	if err := syscall.Kill(enginePID, 0); err != nil {
		t.Fatalf("engine pid %d killed by ctrl+c inside the editor: %v", enginePID, err)
	}

	quitWithCtrlC(t, p)
	p.WaitFor(func(s string) bool { return strings.HasSuffix(strings.TrimSpace(s), "SHELL$") }, ptyWait)
}

// engineRunPID reads the main engine's pid from $MANTLE_HOME/run (written by plan 02's
// engine manager), waiting briefly for the record to appear.
func engineRunPID(t *testing.T, env []string) int {
	t.Helper()
	var mantleHome string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "MANTLE_HOME="); ok {
			mantleHome = v
		}
	}
	deadline := time.Now().Add(ptyWait)
	for time.Now().Before(deadline) {
		recs, _ := filepath.Glob(filepath.Join(mantleHome, "run", "*.json"))
		if len(recs) == 1 {
			var rec struct {
				PID int `json:"pid"`
			}
			raw, _ := os.ReadFile(recs[0])
			if json.Unmarshal(raw, &rec) == nil && rec.PID > 0 {
				return rec.PID
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no engine run record")
	return 0
}
