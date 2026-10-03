package doctor

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	checks "github.com/KaitouKid1412/mantle/internal/claudecli/doctor"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

func fakeChecks(t *testing.T, cs []checks.Check) *int {
	t.Helper()
	runs := 0
	old := RunChecks
	RunChecks = func(context.Context, checks.Env) []checks.Check { runs++; return cs }
	t.Cleanup(func() { RunChecks = old })
	return &runs
}

func TestPanel(t *testing.T) {
	runs := fakeChecks(t, StoryChecks())
	ecotest.ResetState(t)
	eng := ecotest.NewEngine()
	ctx := ecotest.NewCtx(eng, "/work")
	x := ecotest.Capture(t)
	d, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d)
	s := ecotest.Screen(ctx, d, 100)
	for _, want := range []string{"Some checks need attention", "✓ Claude Code", "! Go toolchain", "Run Claude Code's installation check"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}

	// Details of the settings check show its items.
	ecotest.Press(t, ctx, d, "down", "down", "down", "enter")
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "settings.local.json: line 3") {
		t.Errorf("detail = %q", s)
	}
	ecotest.Press(t, ctx, d, "esc")

	// claude doctor runs with the terminal.
	ecotest.Press(t, ctx, d, "end", "up", "up", "up", "enter")
	if got := x.Args(); len(got) != 1 || !reflect.DeepEqual(got[0], []string{"doctor"}) {
		t.Fatalf("exec = %q", got)
	}

	// Update: claude update, then checks re-run and the engine restarts.
	ecotest.Press(t, ctx, d, "down", "down", "enter")
	if got := x.Args(); len(got) != 2 || !reflect.DeepEqual(got[1], []string{"update"}) {
		t.Fatalf("exec = %q", got)
	}
	if *runs != 2 || len(eng.Restarts) != 1 {
		t.Errorf("after update: runs=%d restarts=%d", *runs, len(eng.Restarts))
	}

	ecotest.Press(t, ctx, d, "r")
	if *runs != 3 {
		t.Errorf("r re-runs: %d", *runs)
	}

	// Ask Claude sends the engine's /doctor skill and closes the panel.
	ecotest.Press(t, ctx, d, "end", "up", "up", "enter")
	if !reflect.DeepEqual(eng.Sent, []string{"/doctor"}) || !d.Closed() {
		t.Errorf("ask claude: sent=%q closed=%v", eng.Sent, d.Closed())
	}
}

func TestAskClaudeNeedsEngine(t *testing.T) {
	fakeChecks(t, []checks.Check{{ID: "claude", Title: "Claude Code", Status: checks.OK}})
	ctx := ecotest.NewCtx(nil, "/work")
	d, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d)
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "Everything looks good") {
		t.Errorf("screen = %q", s)
	}
	v := d.Root().(*view)
	for _, r := range v.list.Rows() {
		if r.Key == "act.ask" && !r.Info {
			t.Error("ask claude must be disabled without an engine")
		}
	}
}

func TestStories(t *testing.T) {
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	if _, ok := r.Command("doctor"); !ok {
		t.Fatal("no /doctor")
	}
	for _, s := range r.Stories {
		testkit.RunStory(t, s)
	}
}
