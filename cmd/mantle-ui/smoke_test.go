package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
)

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
	return []string{
		"HOME=" + home,
		"CLAUDE_CONFIG_DIR=" + claudeDir,
		"MANTLE_HOME=" + filepath.Join(home, ".mantle"),
		"MANTLE_CLAUDE_BIN=" + fake,
		"FAKECLAUDE_SCRIPT=" + script,
		"TERM=xterm-256color",
		"PATH=" + os.Getenv("PATH"),
	}
}

const smokeScript = `
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

	p.WaitFor(func(s string) bool { return strings.Contains(strings.Join(p.All(), "\n"), "pong from the fake engine") }, 20*time.Second)

	// Quit: ctrl+c twice (the core action asks for confirmation).
	p.Send("ctrl+c")
	time.Sleep(100 * time.Millisecond)
	p.Send("ctrl+c")
	if code := p.ExitCode(10 * time.Second); code != 0 {
		t.Fatalf("exit code %d\n%s", code, strings.Join(p.All(), "\n"))
	}
}
