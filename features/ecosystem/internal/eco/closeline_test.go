package eco_test

import (
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// TestCloseLine covers request 12-09: closing a panel prints one result line;
// a dialog that cleared it prints nothing.
func TestCloseLine(t *testing.T) {
	ctx := exttest.NewCtx()
	d := eco.NewDialog("dialog.test", "MCP servers", &recView{})
	ecotest.Press(t, ctx, d, "esc")
	if len(ctx.Printed) != 1 || !strings.Contains(ctx.Printed[0], "⎿  ") || !strings.Contains(ctx.Printed[0], "MCP servers closed") {
		t.Fatalf("printed = %q", ctx.Printed)
	}
	ctx.Printed = nil
	d = eco.NewDialog("dialog.test", "Quiet", &recView{})
	d.CloseLine = ""
	ecotest.Press(t, ctx, d, "esc")
	if len(ctx.Printed) != 0 {
		t.Errorf("cleared close line printed %q", ctx.Printed)
	}
}

func TestRestartDialogCloseLines(t *testing.T) {
	ecotest.ResetState(t)
	eng := ecotest.NewEngine()
	ctx := ecotest.NewCtx(eng, "/work")
	d, _ := eco.NewRestartDialog(ctx, eco.RestartArgs{})
	ecotest.Press(t, ctx, d, "n")
	if len(ctx.Printed) != 1 || !strings.Contains(ctx.Printed[0], "Restart cancelled") {
		t.Fatalf("cancel printed %q", ctx.Printed)
	}
	ctx.Printed = nil
	d, _ = eco.NewRestartDialog(ctx, eco.RestartArgs{})
	ecotest.Press(t, ctx, d, "y")
	if len(ctx.Printed) != 0 || len(eng.Restarts) != 1 {
		t.Errorf("confirm printed %q, restarts %d", ctx.Printed, len(eng.Restarts))
	}
}
