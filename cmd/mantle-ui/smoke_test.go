package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
)

// ptyWait bounds every wait in the pty tests. It is generous on purpose: these tests
// also run inside the /mantle pipeline's fully parallel `go test ./...`, where a boot
// can take many seconds; a correct run finishes long before it.
const ptyWait = 60 * time.Second

// buildBinaries builds mantle-ui and fakeclaude into a temp dir.
func buildBinaries(t *testing.T) (ui, fake string) {
	t.Helper()
	dir := t.TempDir()
	for _, pkg := range []string{"./cmd/mantle-ui", "./cmd/fakeclaude"} {
		cmd := exec.Command("go", "build", "-o", dir+"/", pkg)
		cmd.Dir = moduleRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", pkg, err, out)
		}
	}
	return filepath.Join(dir, "mantle-ui"), filepath.Join(dir, "fakeclaude")
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd()
	for d := wd; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	t.Fatal("no go.mod")
	return ""
}

// smokeEnv is an isolated HOME / CLAUDE_CONFIG_DIR / MANTLE_HOME with the project
// directory already trusted, so the startup gates pass without a dialog.
func smokeEnv(t *testing.T, fake, script, project string) []string {
	t.Helper()
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	trust, _ := json.Marshal(map[string]any{
		"hasCompletedOnboarding": true,
		"projects":               map[string]any{project: map[string]any{"hasTrustDialogAccepted": true}},
	})
	os.WriteFile(filepath.Join(claudeDir, ".claude.json"), trust, 0o600)
	os.WriteFile(filepath.Join(home, ".claude.json"), trust, 0o600)
	// On failure, show mantle-ui's debug log: the screen alone rarely says why.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		logs, _ := filepath.Glob(filepath.Join(home, ".mantle", "logs", "*.log"))
		for _, l := range logs {
			b, _ := os.ReadFile(l)
			t.Logf("--- %s ---\n%s", filepath.Base(l), b)
		}
	})
	return []string{
		"MANTLE_DEBUG=1",
		"HOME=" + home,
		"CLAUDE_CONFIG_DIR=" + claudeDir,
		"MANTLE_HOME=" + filepath.Join(home, ".mantle"),
		"MANTLE_CLAUDE_BIN=" + fake,
		"FAKECLAUDE_SCRIPT=" + script,
		"MANTLE_DRIFT_CHECK=off", // no background `claude --version` probe (plan 11)
		"TERM=xterm-256color",
		"PATH=" + os.Getenv("PATH"),
	}
}

// startupRules answer the control requests mantle may send around startup, in any
// order relative to the first prompt (the @-completion prefetch, context usage, MCP
// status, …), as the real engine does. Explicit rules, not a catch-all: end_session
// must still reach the fake's built-in handler. The steps stay strict about the
// conversation itself.
const startupRules = `
{"on": {"type":"control_request","request":{"subtype":"file_suggestions"}}, "respond": {"suggestions":[]}}
{"on": {"type":"control_request","request":{"subtype":"get_context_usage"}}, "respond": {"categories":[],"totalTokens":0,"maxTokens":200000,"percentage":0}}
{"on": {"type":"control_request","request":{"subtype":"mcp_status"}}, "respond": {"mcpServers":[]}}
{"on": {"type":"control_request","request":{"subtype":"list_models"}}, "respond": {"models":[]}}
{"on": {"type":"control_request","request":{"subtype":"get_settings"}}, "respond": {}}
{"on": {"type":"control_request","request":{"subtype":"get_usage"}}, "respond": {}}
{"on": {"type":"control_request","request":{"subtype":"get_hooks_listing"}}, "respond": {}}
`

const smokeScript = startupRules + `
{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": ` + "INIT" + `}
{"expect": {"type":"user"}, "timeout": 20000}
{"emit": {"type":"system","subtype":"init","session_id":"s-smoke","uuid":"i1","cwd":"/tmp","tools":[],"mcp_servers":[],"model":"claude-test","permissionMode":"default","slash_commands":[],"apiKeySource":"none","claude_code_version":"2.1.288","output_style":"default"}}
{"emit": {"type":"assistant","message":{"id":"m1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"pong from the fake engine"}],"stop_reason":"end_turn"},"parent_tool_use_id":null,"session_id":"s-smoke","uuid":"a1"}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"pong from the fake engine","duration_ms":5,"duration_api_ms":4,"num_turns":1,"session_id":"s-smoke","uuid":"r1","total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}}
`

func TestSmokeBoot(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	ui, fake := buildBinaries(t)
	project := t.TempDir()
	project, _ = filepath.EvalSymlinks(project)
	script := filepath.Join(t.TempDir(), "smoke.jsonl")
	os.WriteFile(script, []byte(strings.Replace(smokeScript, "INIT", string(enginefake.DefaultInitializeResponse), 1)), 0o644)

	cmd := exec.Command(ui, "ping the fake")
	cmd.Dir = project
	cmd.Env = smokeEnv(t, fake, script, project)
	p := testkit.StartProcess(t, cmd, testkit.WithSize(100, 30))

	p.WaitFor(func(s string) bool { return strings.Contains(strings.Join(p.All(), "\n"), "pong from the fake engine") }, ptyWait)

	// Spike S16: the engine runs in its own process group (terminal signals such as
	// ctrl+c inside $EDITOR or ctrl+z reach only mantle-ui's group), recorded for the
	// launcher in $MANTLE_HOME/run.
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
		PID   int `json:"pid"`
		PGID  int `json:"pgid"`
		UIPID int `json:"ui_pid"`
	}
	raw, _ := os.ReadFile(recs[0])
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	uiPID := cmd.Process.Pid
	uiPGID, _ := syscall.Getpgid(uiPID)
	if rec.UIPID != uiPID || rec.PGID == 0 || rec.PGID == uiPGID {
		t.Fatalf("engine pgid %d, ui pid %d pgid %d, record %+v", rec.PGID, uiPID, uiPGID, rec)
	}

	quitWithCtrlC(t, p)
	if code := p.ExitCode(ptyWait); code != 0 {
		t.Fatalf("exit code %d\n%s", code, strings.Join(p.All(), "\n"))
	}
}

// quitWithCtrlC presses ctrl+c until mantle asks for the confirming second press,
// then presses it again. An early ctrl+c may be taken by a turn that is still
// finishing (interrupt) or by a non-empty prompt (clear), as in Claude Code.
func quitWithCtrlC(t *testing.T, p *testkit.Process) {
	t.Helper()
	for range 5 {
		p.Send("ctrl+c")
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(strings.ToLower(p.Screen()), "again to exit") {
				p.Send("ctrl+c")
				return
			}
			select {
			case <-p.Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	t.Fatalf("no exit confirmation after ctrl+c\n%s", p.Screen())
}
