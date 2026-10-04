package selfmod

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/archtest"
)

// TestRealPipelineOnThisRepo runs the full pipeline, with the default smoke
// boot, over a worktree of this repository's HEAD plus a small mod: the M1
// end-to-end check that the steps work against the real mantle-ui and
// fakeclaude. It takes minutes, so it is opt-in:
//
//	MANTLE_PIPELINE_E2E=1 go test ./internal/selfmod -run RealPipeline -timeout 40m -v
func TestRealPipelineOnThisRepo(t *testing.T) {
	if os.Getenv("MANTLE_PIPELINE_E2E") == "" {
		t.Skip("set MANTLE_PIPELINE_E2E=1 to run the full pipeline over this repository")
	}
	wd, _ := os.Getwd()
	root, err := archtest.ModuleRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	g := Git{Dir: root}
	head, err := g.HeadSHA()
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if err := g.WorktreeAddDetached(wt, head); err != nil {
		t.Fatal(err)
	}
	defer g.WorktreeRemove(wt)

	writeFiles(t, wt, map[string]string{
		"mods/link_e2e-demo.go": "package mods\n\nimport _ \"github.com/KaitouKid1412/mantle/mods/e2e-demo\"\n",
		"mods/e2e-demo/demo.go": `// Package e2edemo is a pipeline test mod.
package e2edemo

import (
	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func init() { ext.Register(Feature()) }

// Feature adds /e2e-hello and its story.
func Feature() ext.Feature {
	return ext.Feature{ID: "mod.e2e-demo", Order: ext.ModOrder, Setup: func(r ext.Registrar) error {
		r.AddCommand(ext.Command{Name: "e2e-hello", Description: "Say hello", Source: ext.SourceMod,
			Run: func(ctx ext.Ctx, args string) tea.Cmd { return ctx.Print("hello from a mod") }})
		r.AddStory(ext.Story{ID: "mod.e2e-demo/hello", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
			return ext.Rendered{Text: "hello from a mod"}
		}})
		return nil
	}}
}
`,
		"mods/e2e-demo/demo_test.go": `package e2edemo

import (
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

func TestFeature(t *testing.T) {
	r, err := exttest.Setup(Feature())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Command("e2e-hello"); !ok || len(r.Stories) != 1 {
		t.Fatalf("commands %v stories %d", r.Commands, len(r.Stories))
	}
}
`,
	})
	cfg := Config{
		Smoke: DefaultSmokeConfig(),
		// The nested go test ./... must not run this test again.
		Env: []string{"MANTLE_PIPELINE_E2E=", "MANTLE_PIPELINE_E2E_STEPS="},
	}
	// MANTLE_PIPELINE_E2E_STEPS=build,selftest,smoke limits the run.
	var steps []StepID
	if s := os.Getenv("MANTLE_PIPELINE_E2E_STEPS"); s != "" {
		for _, id := range strings.Split(s, ",") {
			steps = append(steps, StepID(strings.TrimSpace(id)))
		}
	}
	logDir := filepath.Join(t.TempDir(), "build")
	rep, err := NewPipeline(cfg).Run(context.Background(), Run{BuildID: "e2e", Dir: wt, Base: head, LogDir: logDir, Steps: steps})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Results {
		t.Logf("%-10s ok=%v skipped=%v %s %s", r.Step, r.OK, r.Skipped, r.Duration.Round(1e6), r.Summary)
	}
	smoke, _ := os.ReadFile(filepath.Join(logDir, "09-smoke.log"))
	if !rep.OK {
		t.Fatalf("pipeline failed:\n%s\nsmoke log tail:\n%s", TrimForBuilder(rep), tailBytes(smoke, 3000))
	}
	if r, ok := rep.Result(StepSmoke); ok && r.OK {
		var actions []string
		for _, ln := range strings.Split(string(smoke), "\n") {
			if strings.HasPrefix(ln, "[smoke]") {
				actions = append(actions, ln)
			}
		}
		t.Logf("smoke boot:\n%s", strings.Join(actions, "\n"))
		if !strings.Contains(string(smoke), "[smoke] exited with code 0") {
			t.Error("the smoke boot did not end with exit 0")
		}
	}
}

func tailBytes(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}
