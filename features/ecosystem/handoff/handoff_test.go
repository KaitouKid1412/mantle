package handoff

import (
	"reflect"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestRegistration(t *testing.T) {
	r, err := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range r.Commands {
		for _, n := range append([]string{c.Name}, c.Aliases...) {
			if seen[n] {
				t.Errorf("name %q registered twice", n)
			}
			seen[n] = true
		}
		if c.Run == nil || c.Source != ext.SourceBuiltin {
			t.Errorf("%s: run=%v source=%q", c.Name, c.Run != nil, c.Source)
		}
	}
	for _, want := range []string{"remote-control", "rc", "teleport", "feedback", "bug", "share", "install-github-app",
		"upgrade", "voice", "background", "bg"} {
		if !seen[want] {
			t.Errorf("missing command %q", want)
		}
	}
	if _, ok := r.Dialogs[UpgradeDialogID]; !ok {
		t.Error("upgrade dialog not registered")
	}
}

func handoffCtx(t *testing.T) (*ecotest.Ctx, *ecotest.Engine) {
	ecotest.ResetState(t)
	eng := ecotest.NewEngine()
	return ecotest.NewCtx(eng, "/work"), eng
}

func TestHandOffUnlessEngineAcceptsIt(t *testing.T) {
	ctx, eng := handoffCtx(t)
	Run("teleport")(ctx, "  abc123 ")
	if !reflect.DeepEqual(ctx.Handoffs(), []string{"/teleport abc123"}) || len(eng.Sent) != 0 {
		t.Fatalf("handed=%q sent=%q", ctx.Handoffs(), eng.Sent)
	}
	// The engine lists usage-credits as headless-capable: pass it through.
	ecotest.Observe(&proto.SystemInit{SlashCommands: []string{"usage-credits"}})
	Run("usage-credits")(ctx, "")
	if !reflect.DeepEqual(eng.Sent, []string{"/usage-credits"}) || len(ctx.Handoffs()) != 1 {
		t.Fatalf("sent=%q handed=%q", eng.Sent, ctx.Handoffs())
	}
	// commands_changed replaces the list.
	ecotest.Observe(&proto.CommandsChanged{Commands: []proto.SlashCommand{{Name: "stop"}}})
	Run("usage-credits")(ctx, "")
	if len(ctx.Handoffs()) != 2 {
		t.Errorf("after commands_changed usage-credits should hand off: %q", ctx.Handoffs())
	}
}

func TestGap(t *testing.T) {
	ctx, eng := handoffCtx(t)
	gap(Gaps[0])(ctx, "")
	if len(ctx.Notices) != 1 || !strings.Contains(ctx.Notices[0].Text, "speech-to-text") {
		t.Fatalf("notices = %+v", ctx.Notices)
	}
	ecotest.Observe(&proto.SystemInit{SlashCommands: []string{"voice"}})
	gap(Gaps[0])(ctx, "tap")
	if !reflect.DeepEqual(eng.Sent, []string{"/voice tap"}) {
		t.Errorf("engine-accepted gap should pass through: %q", eng.Sent)
	}
}

func TestUpgrade(t *testing.T) {
	ctx, eng := handoffCtx(t)
	x := ecotest.Capture(t)
	d, _ := NewUpgradeDialog(ctx, nil)
	ecotest.Press(t, ctx, d, "enter")
	if got := x.Args(); len(got) != 1 || !reflect.DeepEqual(got[0], []string{"update"}) {
		t.Fatalf("exec = %q", got)
	}
	if !d.Closed() || len(eng.Restarts) != 1 {
		t.Errorf("after a successful update: closed=%v restarts=%d", d.Closed(), len(eng.Restarts))
	}

	d, _ = NewUpgradeDialog(ctx, nil)
	ecotest.Press(t, ctx, d, "2")
	if !reflect.DeepEqual(ctx.Handoffs(), []string{"/upgrade"}) || !d.Closed() {
		t.Errorf("plan upgrade: handed=%q closed=%v", ctx.Handoffs(), d.Closed())
	}
}

func TestUpgradeStory(t *testing.T) {
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	for _, s := range r.Stories {
		testkit.RunStory(t, s)
	}
}
