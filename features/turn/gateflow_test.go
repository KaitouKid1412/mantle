package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/turn/gates"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// spawnRecorder stands in for the host's spawner.
type spawnRecorder struct {
	proceeded []ext.SpawnOpts
	aborted   []string
}

func (s *spawnRecorder) msg(engine string, o ext.SpawnOpts) ext.SpawnGateMsg {
	return ext.SpawnGateMsg{
		EngineID: engine,
		Opts:     o,
		Proceed:  func(o ext.SpawnOpts) tea.Cmd { s.proceeded = append(s.proceeded, o); return nil },
		Abort:    func(r string) tea.Cmd { s.aborted = append(s.aborted, r); return nil },
	}
}

func (x *h) project(t *testing.T) string {
	t.Helper()
	p := filepath.Join(filepath.Dir(x.home), "work", "proj")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// PD-43: no engine is spawned until every gate passed.
func TestNoSpawnBeforeGatesPass(t *testing.T) {
	x := newH(t)
	proj := x.project(t)
	writeFile(t, filepath.Join(proj, ".mcp.json"), `{"mcpServers":{"db":{"command":"npx"},"docs":{"url":"https://x.test"}}}`)
	sp := &spawnRecorder{}
	x.send(sp.msg(ext.MainEngine, ext.SpawnOpts{Cwd: proj, Settings: `{"model":"m"}`}))

	if x.c.topID() != DialogTrust || len(sp.proceeded)+len(sp.aborted) != 0 {
		t.Fatalf("trust dialog first, nothing spawned: stack=%v spawn=%+v", x.c.ids, sp)
	}
	if v := testkitStrip(x.view(300)); !strings.Contains(v, proj) || !strings.Contains(v, "db (stdio: npx)") {
		t.Fatalf("trust dialog should list the folder and its MCP servers:\n%s", v)
	}
	x.press("enter") // trust
	if x.c.topID() != DialogMcpApproval || len(sp.proceeded) != 0 {
		t.Fatalf("mcp approval next, still no spawn: stack=%v", x.c.ids)
	}
	x.press("down", "space", "enter") // keep db, drop docs
	if len(sp.proceeded) != 1 || len(sp.aborted) != 0 || len(x.c.stack) != 0 {
		t.Fatalf("spawn after the last gate: %+v stack=%v", sp, x.c.ids)
	}
	var s map[string]any
	if err := json.Unmarshal([]byte(sp.proceeded[0].Settings), &s); err != nil {
		t.Fatal(err)
	}
	if s["model"] != "m" || !slices.Equal(toStrings(s["disabledMcpjsonServers"]), []string{"docs"}) {
		t.Fatalf("settings = %s", sp.proceeded[0].Settings)
	}

	// Trust was recorded in mantle's store; the next launch asks nothing.
	if _, err := os.Stat(filepath.Join(x.home, ".mantle", "trust.json")); err != nil {
		t.Fatal(err)
	}
	sp2 := &spawnRecorder{}
	x.send(sp2.msg(ext.MainEngine, ext.SpawnOpts{Cwd: proj}))
	if len(sp2.proceeded) != 1 || x.c.topID() != "" || sp2.proceeded[0].Settings != `{"disabledMcpjsonServers":["docs"]}` {
		t.Fatalf("second launch: %+v stack=%v", sp2, x.c.ids)
	}
}

func toStrings(v any) []string {
	arr, _ := v.([]any)
	var out []string
	for _, a := range arr {
		s, _ := a.(string)
		out = append(out, s)
	}
	return out
}

func TestDecliningTrustAborts(t *testing.T) {
	x := newH(t)
	sp := &spawnRecorder{}
	x.send(sp.msg(ext.MainEngine, ext.SpawnOpts{Cwd: x.project(t)}))
	x.press("esc")
	if len(sp.proceeded) != 0 || !slices.Equal(sp.aborted, []string{"workspace not trusted"}) {
		t.Fatalf("spawn = %+v", sp)
	}
	if _, err := os.Stat(filepath.Join(x.home, ".mantle", "trust.json")); err == nil {
		t.Fatal("a declined folder must not be recorded as trusted")
	}
}

func TestCtrlCDuringGatesAborts(t *testing.T) {
	x := newH(t)
	sp := &spawnRecorder{}
	x.send(sp.msg(ext.MainEngine, ext.SpawnOpts{Cwd: x.project(t)}))
	x.press("ctrl+c")
	if len(sp.proceeded) != 0 || len(sp.aborted) != 1 || len(x.c.stack) != 0 {
		t.Fatalf("spawn = %+v stack=%v", sp, x.c.ids)
	}
}

func TestBypassAndAPIKeyGates(t *testing.T) {
	x := newH(t)
	proj := x.project(t)
	x.vars["ANTHROPIC_API_KEY"] = "sk-test-0123456789abcdefghijKLMN"
	_, _ = gates.RecordTrust(mustEnv(t, x), proj)
	sp := &spawnRecorder{}
	x.send(sp.msg(ext.MainEngine, ext.SpawnOpts{Cwd: proj, ExtraArgs: []string{"--verbose", "--dangerously-skip-permissions", "--settings", `{"a":1}`}}))
	if x.c.topID() != DialogBypassWarning {
		t.Fatalf("bypass warning expected: %v", x.c.ids)
	}
	x.press("enter") // default is "No, exit"
	if !slices.Equal(sp.aborted, []string{"bypass permissions mode declined"}) {
		t.Fatalf("spawn = %+v", sp)
	}

	sp = &spawnRecorder{}
	x.send(sp.msg(ext.MainEngine, ext.SpawnOpts{Cwd: proj, ExtraArgs: []string{"--dangerously-skip-permissions", "--settings", `{"a":1}`}}))
	x.press("2") // accept bypass
	if x.c.topID() != DialogAPIKey || !strings.Contains(x.view(80), "ghijKLMN") || strings.Contains(x.view(80), "0123456789ab") {
		t.Fatalf("api key prompt shows only the key's tail:\n%s", x.view(80))
	}
	x.press("enter") // default is No
	if len(sp.proceeded) != 1 {
		t.Fatalf("spawn = %+v", sp)
	}
	o := sp.proceeded[0]
	if !slices.Equal(o.UnsetEnv, []string{"ANTHROPIC_API_KEY"}) || o.Settings != `{"a":1}` || slices.Contains(o.ExtraArgs, "--settings") {
		t.Fatalf("opts = %+v", o)
	}
	if !x.st.mode(ext.MainEngine).bypass {
		t.Fatal("bypass is available after starting with --dangerously-skip-permissions")
	}
}

func TestBuilderEnginesAreAutoTrusted(t *testing.T) {
	x := newH(t)
	sp := &spawnRecorder{}
	x.send(sp.msg("builder:mod-a3f", ext.SpawnOpts{Cwd: x.project(t)}))
	if len(sp.proceeded) != 1 || x.c.topID() != "" {
		t.Fatalf("builder spawn: %+v stack=%v", sp, x.c.ids)
	}
}

func TestGateRunsAreSerialized(t *testing.T) {
	x := newH(t)
	proj := x.project(t)
	a, b := &spawnRecorder{}, &spawnRecorder{}
	x.send(a.msg(ext.MainEngine, ext.SpawnOpts{Cwd: proj}))
	x.send(b.msg("other", ext.SpawnOpts{Cwd: proj}))
	if len(x.c.stack) != 1 {
		t.Fatalf("one gate dialog at a time: %v", x.c.ids)
	}
	x.press("enter") // trusts proj for both
	if len(a.proceeded) != 1 || len(b.proceeded) != 1 {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
}

func TestGateNotices(t *testing.T) {
	x := newH(t)
	proj := x.project(t)
	_, _ = gates.RecordTrust(mustEnv(t, x), proj)
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"), `{"broken":`)
	sp := &spawnRecorder{}
	x.send(sp.msg(ext.MainEngine, ext.SpawnOpts{Cwd: proj}))
	if len(sp.proceeded) != 1 || len(x.c.Notices) != 1 || !strings.Contains(x.c.Notices[0].Text, "Settings file ignored") {
		t.Fatalf("notices = %+v spawn=%+v", x.c.Notices, sp)
	}
}

func TestLaunchFlagsTokenizes(t *testing.T) {
	f := launchFlags(ext.SpawnOpts{PermissionMode: "plan", ExtraArgs: []string{
		"--append-system-prompt", "--dangerously-skip-permissions", "--strict-mcp-config", "--permission-mode=acceptEdits",
	}})
	if f.DangerouslySkip || !f.StrictMcpConfig || f.PermissionMode != "acceptEdits" {
		t.Fatalf("flags = %+v", f)
	}
	f = launchFlags(ext.SpawnOpts{ExtraArgs: []string{"--allow-dangerously-skip-permissions", "--", "--dangerously-skip-permissions"}})
	if !f.AllowDangerously || f.DangerouslySkip {
		t.Fatalf("flags after -- are not flags: %+v", f)
	}
	mustEqual(t, stripFlag([]string{"-v", "--settings", "x", "--settings=y", "--model", "m"}, "--settings"), []string{"-v", "--model", "m"})
}

func mustEnv(t *testing.T, x *h) gates.Env {
	t.Helper()
	env, err := x.st.env()
	if err != nil {
		t.Fatal(err)
	}
	return env
}

// fakeChecker stands in for plan 02's engine.CheckEngine / engine.Pin.
type fakeChecker struct {
	res    engineCheck
	err    error
	pinned []string
	calls  int
}

func (f *fakeChecker) Check(context.Context) (engineCheck, error) { f.calls++; return f.res, f.err }
func (f *fakeChecker) Pin(binary, version string) error {
	f.pinned = append(f.pinned, binary+"@"+version)
	return nil
}

func trustedProject(t *testing.T, x *h) string {
	t.Helper()
	proj := x.project(t)
	if _, err := gates.RecordTrust(mustEnv(t, x), proj); err != nil {
		t.Fatal(err)
	}
	return proj
}

func TestEngineCheckPassesSilently(t *testing.T) {
	x := newH(t)
	fc := &fakeChecker{res: engineCheck{OK: true, Version: "2.1.288"}}
	x.st.checker = fc
	sp := &spawnRecorder{}
	x.send(sp.msg(ext.MainEngine, ext.SpawnOpts{Cwd: trustedProject(t, x)}))
	if fc.calls != 1 || len(sp.proceeded) != 1 || x.c.topID() != "" || len(x.c.Notices) != 0 {
		t.Fatalf("calls=%d spawn=%+v stack=%v notices=%v", fc.calls, sp, x.c.ids, x.c.Notices)
	}
	// A freshly probed version gets a short notice.
	fc.res.Probed = true
	x.send(sp.msg(ext.MainEngine, ext.SpawnOpts{Cwd: trustedProject(t, x)}))
	if len(x.c.Notices) != 1 || !strings.Contains(x.c.Notices[0].Text, "2.1.288 passed") {
		t.Fatalf("notices = %+v", x.c.Notices)
	}
	// Builders skip the check.
	x.send(sp.msg("builder-mod1", ext.SpawnOpts{Cwd: trustedProject(t, x)}))
	if fc.calls != 2 {
		t.Fatalf("builders must not re-check: %d", fc.calls)
	}
}

func TestEngineCheckFailureOffersPin(t *testing.T) {
	x := newH(t)
	fc := &fakeChecker{res: engineCheck{Version: "2.1.300", Failed: []string{"skills", "hooks"}, LastGood: "2.1.288", LastGoodBinary: "/v/2.1.288"}}
	x.st.checker = fc
	sp := &spawnRecorder{}
	x.send(sp.msg(ext.MainEngine, ext.SpawnOpts{Cwd: x.project(t)}))
	if x.c.topID() != DialogEngineCheck || len(sp.proceeded) != 0 {
		t.Fatalf("version gate comes first: stack=%v", x.c.ids)
	}
	v := testkitStrip(x.view(100))
	for _, want := range []string{"2.1.300 did not pass", "skills, hooks", "Use claude 2.1.288 instead"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q:\n%s", want, v)
		}
	}
	x.press("enter") // pin 2.1.288
	if x.c.topID() != DialogTrust {
		t.Fatalf("trust follows the version gate: %v", x.c.ids)
	}
	x.press("enter")
	if len(sp.proceeded) != 1 || !slices.Equal(fc.pinned, []string{"/v/2.1.288@2.1.288"}) {
		t.Fatalf("pin then spawn: spawn=%+v pinned=%v", sp, fc.pinned)
	}
}

func TestEngineCheckContinueAndExit(t *testing.T) {
	x := newH(t)
	fc := &fakeChecker{res: engineCheck{Version: "2.1.300", Failed: []string{"bare"}}}
	x.st.checker = fc
	sp := &spawnRecorder{}
	proj := trustedProject(t, x)
	x.send(sp.msg(ext.MainEngine, ext.SpawnOpts{Cwd: proj}))
	if strings.Contains(testkitStrip(x.view(100)), "instead") {
		t.Fatal("no pin option without a last good binary")
	}
	x.press("enter") // continue anyway
	if len(sp.proceeded) != 1 || len(fc.pinned) != 0 {
		t.Fatalf("continue: %+v pinned=%v", sp, fc.pinned)
	}
	sp = &spawnRecorder{}
	x.send(sp.msg(ext.MainEngine, ext.SpawnOpts{Cwd: proj}))
	x.press("esc")
	if len(sp.proceeded) != 0 || !slices.Equal(sp.aborted, []string{"claude did not pass mantle's startup checks"}) {
		t.Fatalf("exit: %+v", sp)
	}
}

func TestEngineCheckErrorAborts(t *testing.T) {
	x := newH(t)
	x.st.checker = &fakeChecker{err: errors.New("claude: not found")}
	sp := &spawnRecorder{}
	x.send(sp.msg(ext.MainEngine, ext.SpawnOpts{Cwd: trustedProject(t, x)}))
	if len(sp.proceeded) != 0 || len(sp.aborted) != 1 || !strings.Contains(sp.aborted[0], "claude: not found") {
		t.Fatalf("spawn = %+v", sp)
	}
}
