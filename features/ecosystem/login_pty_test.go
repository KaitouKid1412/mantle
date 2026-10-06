package ecosystem_test

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

const ptyWait = 60 * time.Second

// loginSID is the session the fake engine announces before any turn, as real
// claude does with its SessionStart hook events; no transcript is ever written.
const loginSID = "7b2a9022-0000-4000-8000-000000000001"

// loginScript serves both engine runs (the first one is stopped by the restart):
// startup control requests by rule, the session id before any turn, then one
// reply to the first prompt.
const loginScript = `
{"on": {"type":"control_request","request":{"subtype":"file_suggestions"}}, "respond": {"suggestions":[]}}
{"on": {"type":"control_request","request":{"subtype":"reload_skills"}}, "respond": {"skills":[]}}
{"on": {"type":"control_request","request":{"subtype":"get_context_usage"}}, "respond": {"categories":[],"totalTokens":0,"maxTokens":200000,"percentage":0}}
{"on": {"type":"control_request","request":{"subtype":"mcp_status"}}, "respond": {"mcpServers":[]}}
{"on": {"type":"control_request","request":{"subtype":"list_models"}}, "respond": {"models":[]}}
{"on": {"type":"control_request","request":{"subtype":"get_settings"}}, "respond": {}}
{"on": {"type":"control_request","request":{"subtype":"get_usage"}}, "respond": {}}
{"on": {"type":"control_request","request":{"subtype":"get_hooks_listing"}}, "respond": {}}
{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": INIT}
{"emit": {"type":"system","subtype":"hook_started","hook_id":"h1","hook_name":"SessionStart:startup","hook_event":"SessionStart","uuid":"h1","session_id":"SID"}}
{"expect": {"type":"user"}, "timeout": 60000}
{"emit": {"type":"system","subtype":"init","session_id":"SID","uuid":"i1","cwd":"/tmp","tools":[],"mcp_servers":[],"model":"claude-test","permissionMode":"default","slash_commands":[],"apiKeySource":"none","claude_code_version":"2.1.289","output_style":"default"}}
{"emit": {"type":"assistant","message":{"id":"m1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"pong after login"}],"stop_reason":"end_turn"},"parent_tool_use_id":null,"session_id":"SID","uuid":"a1"}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"pong after login","duration_ms":5,"duration_api_ms":4,"num_turns":1,"session_id":"SID","uuid":"r1","total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}}
`

// stubClaude stands in for the claude binary: `auth status` reports a login,
// `auth login` succeeds, and like real claude a --resume of a session with no
// transcript exits 1 with "No conversation found". Everything else is the
// scripted fake engine. Every call is logged.
const stubClaude = `#!/bin/sh
printf '%s\n' "$*" >> "$STUB_LOG"
case "$1 $2" in
"auth status") echo '{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty","email":"user@example.test","subscriptionType":"max"}'; exit 0;;
"auth login") echo "Login successful."; exit 0;;
esac
for a in "$@"; do
	case "$a" in
	--resume|--resume=*) echo "No conversation found with session ID: ${a#--resume=}" >&2; exit 1;;
	esac
done
exec "$FAKECLAUDE" "$@"
`

// TestLoginOnFreshSession is the user-reported bug: start mantle, run /login
// before any message, then send a prompt. The restart after login must not
// --resume the unsaved session (which kills the engine); the prompt must get a
// reply.
func TestLoginOnFreshSession(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	root := moduleRoot(t)
	bin := t.TempDir()
	for _, pkg := range []string{"./cmd/mantle-ui", "./cmd/fakeclaude"} {
		cmd := exec.Command("go", "build", "-o", bin+"/", pkg)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", pkg, err, out)
		}
	}
	stub := filepath.Join(bin, "claude")
	if err := os.WriteFile(stub, []byte(stubClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	project, _ := filepath.EvalSymlinks(t.TempDir())
	script := filepath.Join(t.TempDir(), "login.jsonl")
	body := strings.NewReplacer("INIT", string(enginefake.DefaultInitializeResponse), "SID", loginSID).Replace(loginScript)
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	stubLog := filepath.Join(t.TempDir(), "calls.log")

	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	trust, _ := json.Marshal(map[string]any{
		"hasCompletedOnboarding": true,
		"projects":               map[string]any{project: map[string]any{"hasTrustDialogAccepted": true}},
	})
	os.WriteFile(filepath.Join(claudeDir, ".claude.json"), trust, 0o600)
	os.WriteFile(filepath.Join(home, ".claude.json"), trust, 0o600)
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		b, _ := os.ReadFile(stubLog)
		t.Logf("--- claude calls ---\n%s", b)
		logs, _ := filepath.Glob(filepath.Join(home, ".mantle", "logs", "*.log"))
		for _, l := range logs {
			b, _ := os.ReadFile(l)
			t.Logf("--- %s ---\n%s", filepath.Base(l), b)
		}
	})

	cmd := exec.Command(filepath.Join(bin, "mantle-ui"))
	cmd.Dir = project
	cmd.Env = []string{
		"MANTLE_DEBUG=1",
		"HOME=" + home,
		"CLAUDE_CONFIG_DIR=" + claudeDir,
		"MANTLE_HOME=" + filepath.Join(home, ".mantle"),
		"MANTLE_CLAUDE_BIN=" + stub,
		"FAKECLAUDE=" + filepath.Join(bin, "fakeclaude"),
		"FAKECLAUDE_SCRIPT=" + script,
		"STUB_LOG=" + stubLog,
		"MANTLE_DRIFT_CHECK=off",
		"TERM=xterm-256color",
		"PATH=" + os.Getenv("PATH"),
	}
	p := testkit.StartProcess(t, cmd, testkit.WithSize(100, 30))
	all := func() string { return strings.Join(p.All(), "\n") }

	p.WaitFor(func(s string) bool { return strings.Contains(s, "manual approval") }, ptyWait) // the footer is up
	// As in the report, the engine is already running (and has announced its
	// session) when /login runs: wait for its spawn, then let the startup settle.
	p.WaitFor(func(string) bool {
		b, _ := os.ReadFile(stubLog)
		return strings.Contains(string(b), "--output-format stream-json")
	}, ptyWait)
	p.Settle(300*time.Millisecond, 5*time.Second)
	p.Type("/login")
	p.Send("enter")
	p.WaitForText("Claude subscription", ptyWait) // the sign-in method menu
	p.Send("enter")                               // claude auth login --claudeai (stub succeeds)
	p.WaitFor(func(string) bool { return strings.Contains(all(), "Signed in as user@example.test") }, ptyWait)
	// The login restarts the engine; type once the new one is up, as a person would.
	p.WaitFor(func(string) bool {
		b, _ := os.ReadFile(stubLog)
		return strings.Count(string(b), "--output-format stream-json") >= 2
	}, ptyWait)
	p.Settle(300*time.Millisecond, 5*time.Second)

	p.Type("ping after login")
	p.Send("enter")
	p.WaitFor(func(string) bool {
		a := all()
		return strings.Contains(a, "pong after login") || strings.Contains(a, "No conversation found") ||
			strings.Contains(a, "not running")
	}, ptyWait)
	if a := all(); !strings.Contains(a, "pong after login") {
		t.Fatalf("no reply after /login:\n%s", a)
	}

	calls, _ := os.ReadFile(stubLog)
	if strings.Contains(string(calls), "--resume") {
		t.Errorf("the restart resumed a session that was never saved:\n%s", calls)
	}
	if !strings.Contains(string(calls), "auth login --claudeai") {
		t.Errorf("/login should run claude auth login:\n%s", calls)
	}
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
