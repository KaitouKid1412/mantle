package enginetest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
)

// probeScript answers the probe's requests like an engine that sees (or, with
// bare, doesn't see) the planted CLAUDE.md, hook and skill.
func probeScript(dir string, bare bool) string {
	cmds := `[{"name":"compact","description":"x"},{"name":"mantle-probe","description":"x"}]`
	hooks := `{"events":[],"hooks":[{"event":"Stop","source":"projectSettings","type":"command","displayText":"echo mantle-probe"}]}`
	mem := fmt.Sprintf(`{"categories":[],"totalTokens":1,"maxTokens":2,"percentage":0,"memoryFiles":[{"path":%q,"type":"Project","tokens":9}]}`, filepath.Join(dir, "CLAUDE.md"))
	if bare {
		cmds = `[{"name":"compact","description":"x"}]`
		hooks = `{"events":[],"hooks":[]}`
		mem = `{"categories":[],"totalTokens":1,"maxTokens":2,"percentage":0,"memoryFiles":[]}`
	}
	return fmt.Sprintf(`{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": {"commands":%s,"models":[{"value":"default","displayName":"Default"}],"account":{},"pid":1,"current_permission_mode":"default"}}
{"on": {"type":"control_request","request":{"subtype":"list_models"}}, "respond": {"models":[{"value":"default","displayName":"Default"}]}}
{"on": {"type":"control_request","request":{"subtype":"get_hooks_listing"}}, "respond": %s}
{"on": {"type":"control_request","request":{"subtype":"get_context_usage"}}, "respond": %s}
`, cmds, hooks, mem)
}

func realDir(t *testing.T) string {
	d, _ := filepath.EvalSymlinks(t.TempDir())
	return d
}

func TestProbeFake(t *testing.T) {
	for _, bare := range []bool{false, true} {
		dir := realDir(t)
		sp := &Spawner{Scripts: []*enginefake.Script{enginefake.MustParse(probeScript(dir, bare))}}
		res, err := engine.Probe(context.Background(), engine.ProbeOptions{Binary: "claude", Spawner: sp, Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		failed := strings.Join(res.Failed(), ",")
		if bare && (res.OK || failed != "skills,hooks,memory") {
			t.Errorf("bare: ok=%v failed=%s", res.OK, failed)
		}
		if !bare && (!res.OK || res.Version != enginefake.Version) {
			t.Errorf("full: ok=%v version=%s failed=%s %+v", res.OK, res.Version, failed, res.Checks)
		}
		for _, f := range []string{"CLAUDE.md", ".claude/settings.json", ".claude/skills/mantle-probe/SKILL.md"} {
			if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
				t.Errorf("marker %s not planted", f)
			}
		}
		if spec := strings.Join(sp.Specs()[0].Args, " "); !strings.Contains(spec, "--no-session-persistence") {
			t.Errorf("probe must not persist a session: %s", spec)
		}
	}
}

// TestProbeReal runs the probe against the real claude, isolated and offline.
func TestProbeReal(t *testing.T) {
	r := NewReal(t, &fakeapi.Script{})
	o := r.Opts()
	res, err := engine.Probe(context.Background(), engine.ProbeOptions{Binary: r.Manager.Binary, Env: o.Env, Unset: o.UnsetEnv})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("probe failed: %+v", res.Checks)
	}
	for _, q := range r.API.Requests() {
		if q.Kind == fakeapi.KindMain || q.Kind == fakeapi.KindSide {
			t.Errorf("probe made a model call: %s %s (%s)", q.Method, q.Path, q.Reason)
		} else {
			t.Logf("probe request: %s %s -> %d", q.Method, q.Path, q.Status)
		}
	}
	t.Logf("probe %s in %v: %+v", res.Version, res.Duration.Round(1e6), res.Checks)
}

// TestCheckEngine drives CheckEngine with fakeclaude as the engine binary.
func TestCheckEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "claude")
	_, file, _, _ := runtime.Caller(0)
	build := exec.Command("go", "build", "-o", bin, "./cmd/fakeclaude")
	build.Dir = filepath.Join(filepath.Dir(file), "..", "..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	state := filepath.Join(tmp, "engines.json")
	dir := realDir(t)
	script := filepath.Join(tmp, "probe.jsonl")
	check := func(version string, bare bool) engine.EngineCheck {
		t.Helper()
		os.WriteFile(script, []byte(probeScript(dir, bare)), 0o644)
		t.Setenv("FAKECLAUDE_SCRIPT", script)
		t.Setenv("FAKECLAUDE_VERSION", version)
		c, err := engine.CheckEngine(context.Background(), engine.CheckOptions{StatePath: state, Binary: bin,
			Probe: engine.ProbeOptions{Dir: dir}})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if c := check("2.1.288", false); !c.OK || !c.Probed || c.Version != "2.1.288" {
		t.Fatalf("first: %+v", c)
	}
	if c := check("2.1.288", false); !c.OK || c.Probed {
		t.Errorf("known good version must not be probed again: %+v", c)
	}
	c := check("2.1.290", true)
	wantBin := bin // the recorded binary, unless the installer keeps an immutable copy
	if p := engine.VersionBinary("2.1.288"); p != "" {
		wantBin = p
	}
	if c.OK || !c.Probed || c.LastGood != "2.1.288" || c.LastGoodBinary != wantBin {
		t.Errorf("bad version: %+v", c)
	}
	st, _ := engine.LoadEngineState(state)
	if _, ok := st.Failed["2.1.290"]; !ok || len(st.Passed) != 1 {
		t.Errorf("state %+v", st)
	}
	b, _ := json.Marshal(st)
	if !strings.Contains(string(b), `"skills","hooks","memory"`) {
		t.Errorf("failed checks not recorded: %s", b)
	}

	// Pinning.
	if err := engine.Pin(state, c.LastGoodBinary, c.LastGood); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MANTLE_CLAUDE_BIN", "")
	if got, _ := engine.ResolveBinary(state); got != wantBin {
		t.Errorf("pinned binary %q", got)
	}
	if err := engine.Unpin(state); err != nil {
		t.Fatal(err)
	}
	if st, _ := engine.LoadEngineState(state); st.Pinned != "" {
		t.Error("unpin")
	}
}
