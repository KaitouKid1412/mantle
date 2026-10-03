package agentview

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

func fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../../testdata/fixtures/09/claudecli/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func open(t *testing.T) (*ecotest.Ctx, *ecotest.Execs, ext.Dialog, string) {
	t.Helper()
	log := ecotest.FakeClaude(t, map[string]string{
		"agents --json": fixture(t),
		"logs 2a3c85ff": "line one\nline two\n",
	})
	ctx := ecotest.NewCtx(ecotest.NewEngine(), t.TempDir())
	ctx.ClockV.T = time.UnixMilli(1791029370211).Add(10 * time.Minute)
	x := ecotest.Capture(t)
	d, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d)
	return ctx, x, d, log
}

func TestList(t *testing.T) {
	ctx, _, d, log := open(t)
	s := ecotest.Screen(ctx, d, 100)
	for _, want := range []string{"Background", "● repo-task", "busy · working · repo · started 10m ago",
		"Interactive · other terminals", "home-1"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if strings.Index(s, "Background") > strings.Index(s, "Interactive") {
		t.Errorf("background sessions come first:\n%s", s)
	}
	ecotest.Press(t, ctx, d, "a")
	calls := ecotest.Calls(t, log)
	if !reflect.DeepEqual(calls[0], []string{"agents", "--json"}) || !reflect.DeepEqual(calls[1], []string{"agents", "--json", "--all"}) {
		t.Errorf("calls = %q", calls)
	}
}

func TestAttachLogsStop(t *testing.T) {
	ctx, x, d, log := open(t)
	ecotest.Press(t, ctx, d, "enter")
	if got := x.Args(); len(got) != 1 || !reflect.DeepEqual(got[0], []string{"attach", "2a3c85ff"}) {
		t.Fatalf("attach = %q", got)
	}
	ecotest.Press(t, ctx, d, "l")
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "line one") || !strings.Contains(s, "repo-task") {
		t.Fatalf("logs:\n%s", s)
	}
	ecotest.Press(t, ctx, d, "esc", "x", "y")
	calls := ecotest.Calls(t, log)
	var stop []string
	for _, c := range calls {
		if c[0] == "stop" {
			stop = c
		}
	}
	if !reflect.DeepEqual(stop, []string{"stop", "2a3c85ff"}) {
		t.Errorf("stop = %q (calls %q)", stop, calls)
	}
	if !strings.Contains(ecotest.Screen(ctx, d, 100), "repo-task stopped") {
		t.Errorf("stop status missing")
	}
}

func TestInteractiveSessionsAreInfoOnly(t *testing.T) {
	ctx, x, d, _ := open(t)
	ecotest.Press(t, ctx, d, "end", "enter", "l", "x")
	if len(x.Cmds) != 0 || d.(interface{ Depth() int }).Depth() != 1 ||
		!strings.Contains(ecotest.Screen(ctx, d, 100), "runs in another terminal") {
		t.Errorf("interactive sessions must not be attached, logged or stopped")
	}
}

func TestRegistration(t *testing.T) {
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	if _, ok := r.Command("agent-view"); !ok {
		t.Fatal("no /agent-view")
	}
	if len(r.Actions) != 1 || r.Actions[0].ID != ActionOpen || !r.Actions[0].ID.IsMantle() {
		t.Errorf("actions = %+v", r.Actions)
	}
	for _, s := range r.Stories {
		testkit.RunStory(t, s)
	}
}
