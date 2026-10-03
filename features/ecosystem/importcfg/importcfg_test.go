package importcfg

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../../testdata/fixtures/09/claudecli/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPreviewConfirmApply(t *testing.T) {
	log := ecotest.FakeClaude(t, map[string]string{
		"--dry-run": fixture(t, "import-preview.txt"),
		"--yes=":    "Imported 3 items.\n",
	})
	ecotest.ResetState(t)
	eng := ecotest.NewEngine()
	ctx := ecotest.NewCtx(eng, t.TempDir())
	d, _ := New(ctx, "codex")
	ecotest.Open(t, ctx, d)
	if s := ecotest.Screen(ctx, d, 80); !strings.Contains(s, "scan digest: d1g3st-42") || !strings.Contains(s, "Import these") {
		t.Fatalf("preview screen = %q", s)
	}
	ecotest.Press(t, ctx, d, "enter")
	calls := ecotest.Calls(t, log)
	want := [][]string{{"import", "--dry-run", "codex"}, {"import", "--yes=d1g3st-42", "codex"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %q", calls)
	}
	if s := ecotest.Screen(ctx, d, 80); !strings.Contains(s, "Imported 3 items") {
		t.Errorf("result screen = %q", s)
	}
	ecotest.Press(t, ctx, d, "enter")
	if !d.Closed() || len(eng.Restarts) != 1 {
		t.Errorf("enter after import restarts: closed=%v restarts=%d", d.Closed(), len(eng.Restarts))
	}
}

func TestNothingToImport(t *testing.T) {
	log := ecotest.FakeClaude(t, map[string]string{"--dry-run": fixture(t, "import-none.txt")})
	ctx := ecotest.NewCtx(ecotest.NewEngine(), t.TempDir())
	d, _ := New(ctx, "")
	ecotest.Open(t, ctx, d)
	if s := ecotest.Screen(ctx, d, 80); !strings.Contains(s, "No other AI coding agents") || strings.Contains(s, "Import these") {
		t.Fatalf("screen = %q", s)
	}
	ecotest.Press(t, ctx, d, "enter")
	if !d.Closed() || len(ecotest.Calls(t, log)) != 1 {
		t.Errorf("enter closes without importing: closed=%v calls=%q", d.Closed(), ecotest.Calls(t, log))
	}
}

func TestCommand(t *testing.T) {
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	cmd, ok := r.Command("import")
	if !ok {
		t.Fatal("no /import")
	}
	ctx := exttest.NewCtx()
	cmd.Run(ctx, "vscode")
	if len(ctx.Notices) != 1 || len(ctx.Opened) != 0 {
		t.Errorf("bad source: notices=%v opened=%v", ctx.Notices, ctx.Opened)
	}
	cmd.Run(ctx, "gemini")
	if !reflect.DeepEqual(ctx.Opened, []string{DialogID}) {
		t.Errorf("opened = %v", ctx.Opened)
	}
	if c := cmd.Complete(ctx, "c"); len(c) != 2 {
		t.Errorf("completions = %v", c)
	}
}

func TestStories(t *testing.T) {
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	for _, s := range r.Stories {
		testkit.RunStory(t, s)
	}
}
