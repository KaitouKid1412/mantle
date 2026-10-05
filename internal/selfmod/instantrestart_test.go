package selfmod

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/KaitouKid1412/mantle/internal/archtest"
	"github.com/KaitouKid1412/mantle/internal/launcher"
	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
)

// instantRestartScript answers two turns. A fresh engine would answer the
// second prompt with "pong 1" again: seeing "pong 2" proves the same claude
// process survived the restart.
const instantRestartScript = `
{"on": {"type": "control_request", "request": {"subtype": "file_suggestions"}}, "respond": {"suggestions": []}}
{"on": {"type": "control_request", "request": {"subtype": "reload_skills"}}, "respond": {"skills": []}}
{"on": {"type": "control_request", "request": {"subtype": "get_context_usage"}}, "respond": {"categories": [], "totalTokens": 0, "maxTokens": 200000, "percentage": 0}}
{"on": {"type": "control_request", "request": {"subtype": "mcp_status"}}, "respond": {"mcpServers": []}}
{"on": {"type": "control_request", "request": {"subtype": "list_models"}}, "respond": {"models": []}}
{"on": {"type": "control_request", "request": {"subtype": "get_settings"}}, "respond": {}}
{"on": {"type": "control_request", "request": {"subtype": "get_usage"}}, "respond": {}}
{"on": {"type": "control_request", "request": {"subtype": "get_hooks_listing"}}, "respond": {}}
{"on": {"type": "control_request", "request": {"subtype": "initialize"}}, "respond": INIT}
{"expect": {"type": "user"}, "timeout": 60000}
{"emit": {"type": "system", "subtype": "init", "session_id": "SESSION", "uuid": "i1", "cwd": "/tmp", "tools": [], "mcp_servers": [], "model": "claude-test", "permissionMode": "default", "slash_commands": [], "apiKeySource": "none", "claude_code_version": "2.1.288", "output_style": "default"}}
{"emit": {"type": "assistant", "session_id": "SESSION", "uuid": "a1", "parent_tool_use_id": null, "message": {"id": "m1", "type": "message", "role": "assistant", "model": "claude-test", "content": [{"type": "text", "text": "pong 1"}], "stop_reason": "end_turn"}}}
{"emit": {"type": "result", "subtype": "success", "session_id": "SESSION", "uuid": "r1", "is_error": false, "result": "pong 1", "num_turns": 1, "duration_ms": 1, "duration_api_ms": 1, "total_cost_usd": 0, "usage": {"input_tokens": 1, "output_tokens": 1}}}
{"expect": {"type": "user"}, "timeout": 60000}
{"emit": {"type": "assistant", "session_id": "SESSION", "uuid": "a2", "parent_tool_use_id": null, "message": {"id": "m2", "type": "message", "role": "assistant", "model": "claude-test", "content": [{"type": "text", "text": "pong 2"}], "stop_reason": "end_turn"}}}
{"emit": {"type": "result", "subtype": "success", "session_id": "SESSION", "uuid": "r2", "is_error": false, "result": "pong 2", "num_turns": 1, "duration_ms": 1, "duration_api_ms": 1, "total_cost_usd": 0, "usage": {"input_tokens": 1, "output_tokens": 1}}}
`

// mantleLogs returns the interesting lines of mantle-ui's debug logs.
func mantleLogs(l launcher.Layout) string {
	files, _ := filepath.Glob(filepath.Join(l.Logs(), "*.log"))
	var b strings.Builder
	for _, f := range files {
		data, _ := os.ReadFile(f)
		for _, ln := range strings.Split(string(data), "\n") {
			if strings.Contains(ln, "selfmod") || strings.Contains(ln, "adopt") || strings.Contains(ln, "handoff") ||
				strings.Contains(ln, "engine") || strings.Contains(ln, "level=WARN") || strings.Contains(ln, "level=ERROR") {
				b.WriteString(filepath.Base(f) + ": " + ln + "\n")
			}
		}
	}
	s := b.String()
	if len(s) > 6000 {
		s = s[len(s)-6000:]
	}
	return s
}

// TestInstantRestartAcrossExec is B11 end to end: a real mantle-ui with
// selfmod.instantRestart runs /mantle restart, execs the current build in
// place (same pid) handing the engine over, and the same claude process
// answers the next turn.
func TestInstantRestartAcrossExec(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries and drives a pty")
	}
	wd, _ := os.Getwd()
	root, err := archtest.ModuleRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	if main, _ := os.ReadFile(filepath.Join(root, "cmd", "mantle-ui", "main.go")); !strings.Contains(string(main), "AdoptFile") {
		t.Skip("cmd/mantle-ui does not adopt handed-over engines yet (plan 01's Adopt hook)")
	}
	dir := t.TempDir()
	for _, pkg := range []string{"./cmd/mantle-ui", "./cmd/fakeclaude"} {
		cmd := exec.Command("go", "build", "-o", dir+"/", pkg)
		cmd.Dir = root
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", pkg, err, b)
		}
	}
	home := filepath.Join(dir, "home")
	l := launcher.Layout{Root: filepath.Join(home, ".mantle")}
	store := launcher.Store{L: l}
	if _, err := store.Install("b1", filepath.Join(dir, "mantle-ui"), launcher.Manifest{}); err != nil {
		t.Fatal(err)
	}
	store.SetCurrent("b1")
	launcher.MarkHealthy(l, "b1")
	os.WriteFile(l.Settings(), []byte(`{"selfmod.instantRestart": true}`), 0o644)

	script := filepath.Join(dir, "engine.jsonl")
	const sessionID = "1b4e28ba-2fa1-11d2-883f-0016d3cca427"
	body := strings.Replace(instantRestartScript, "INIT", string(enginefake.DefaultInitializeResponse), 1)
	os.WriteFile(script, []byte(strings.ReplaceAll(body, "SESSION", sessionID)), 0o644)
	project, _ := filepath.EvalSymlinks(t.TempDir())
	env, err := smokeHome(home, project)
	if err != nil {
		t.Fatal(err)
	}
	env = append(env,
		"PATH="+os.Getenv("PATH"), "TERM=xterm-256color",
		launcher.EnvClaudeBin+"="+filepath.Join(dir, "fakeclaude"), "FAKECLAUDE_SCRIPT="+script,
		launcher.EnvLauncherPID+"="+strconv.Itoa(os.Getpid()), launcher.EnvBuildID+"=b1",
		"ANTHROPIC_BASE_URL=http://127.0.0.1:9", "MANTLE_DEBUG=1",
	)
	// The session file real claude writes after the first turn (the fake does
	// not): the restart's --resume=<id> must resolve.
	getenv := func(k string) string {
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, k+"="); ok {
				return v
			}
		}
		return ""
	}
	projDir := sessions.LayoutFromEnv(getenv).ProjectDir(project)
	os.MkdirAll(projDir, 0o755)
	record := `{"type":"user","sessionId":"` + sessionID + `","cwd":"` + project + `","uuid":"u1","timestamp":"2026-10-04T00:00:00.000Z","message":{"role":"user","content":"ping one"}}` + "\n"
	os.WriteFile(sessions.SessionFile(projDir, sessionID), []byte(record), 0o600)

	cmd := exec.Command(store.L.Version("b1")+"/mantle-ui", "ping one")
	cmd.Dir = project
	cmd.Env = env
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		f.Close()
	}()
	out := &screenBuffer{}
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				out.write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	expect := func(re string) {
		t.Helper()
		rx := regexp.MustCompile(re)
		deadline := time.Now().Add(60 * time.Second)
		for !out.consume(rx) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %q; screen tail:\n%s\nlogs:\n%s", re, out.tail(2500), mantleLogs(l))
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	enginePID := func() int {
		runs, _ := l.RunFiles()
		for _, rf := range runs {
			if rf.IsEngine() && rf.UIPID == cmd.Process.Pid {
				return rf.PID
			}
		}
		return 0
	}

	expect(`pong 1`)
	before := enginePID()
	if before == 0 {
		t.Fatal("no engine run record")
	}
	time.Sleep(300 * time.Millisecond) // let the turn settle (idle)
	f.Write([]byte("/mantle restart\r"))
	// The process execs itself: the UI comes back under the same pid.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if rf, err := launcher.ReadRunFile(l.RunFile(cmd.Process.Pid)); err == nil && rf.Version == "b1" {
			if _, err := os.Stat(strings.TrimSuffix(l.RunFile(cmd.Process.Pid), ".json") + ".handoff.json"); os.IsNotExist(err) {
				break // adopted: the new process deleted the hand-off file
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no in-place restart; screen tail:\n%s\nlogs:\n%s", out.tail(2500), mantleLogs(l))
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(time.Second)
	f.Write([]byte("ping two\r"))
	expect(`pong 2`)
	if after := enginePID(); after != before {
		t.Errorf("engine pid %d after the restart, was %d: claude did not survive", after, before)
	}
	if syscall.Kill(before, 0) != nil {
		t.Error("the original claude process is gone")
	}

	f.Write([]byte("\x03"))
	time.Sleep(100 * time.Millisecond)
	f.Write([]byte("\x03"))
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("exit: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("mantle-ui did not exit; screen tail:\n%s", out.tail(1500))
	}
}
