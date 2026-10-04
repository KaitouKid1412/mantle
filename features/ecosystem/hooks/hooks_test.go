package hooks

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	"github.com/KaitouKid1412/mantle/internal/claudecli/discovery"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tree makes a home and project with hooks in user and project settings.
func tree(t *testing.T) (cwd string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd = filepath.Join(root, "proj")
	write(t, filepath.Join(home, ".claude", "settings.json"),
		`{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "~/.claude/hooks/check-bash.sh", "timeout": 10}]}]}}`)
	write(t, filepath.Join(cwd, ".claude", "settings.json"),
		`{"hooks": {"PreToolUse": [{"matcher": "Edit|Write", "hooks": [{"type": "command", "command": "./scripts/fmt.sh", "args": ["--fix"]}]}]}}`)
	old := eco.RootsHook
	eco.RootsHook = func(string) discovery.Roots {
		return discovery.Roots{Home: home, ConfigDir: filepath.Join(home, ".claude"), CWD: cwd,
			Getenv: func(string) string { return "" }}
	}
	t.Cleanup(func() { eco.RootsHook = old })
	return cwd
}

func fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../../testdata/fixtures/09/control/hooks_listing.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBrowseListing(t *testing.T) {
	cwd := tree(t)
	ecotest.ResetState(t)
	eng := ecotest.NewEngine().Respond(proto.SubGetHooksListing, fixture(t))
	ctx := ecotest.NewCtx(eng, cwd)
	x := ecotest.Capture(t)
	d, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d)
	s := ecotest.Screen(ctx, d, 100)
	for _, want := range []string{"● PreToolUse", "3 hooks · Before a tool runs", "● SessionStart", "1 hook · Before Claude"} {
		if !strings.Contains(s, want) {
			t.Errorf("events screen lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "PostToolUse") {
		t.Errorf("empty events are hidden by default")
	}
	ecotest.Press(t, ctx, d, "a")
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "○ PostToolUse") {
		t.Errorf("a shows every event:\n%s", s)
	}
	ecotest.Press(t, ctx, d, "a", "home", "enter")
	s = ecotest.Screen(ctx, d, 100)
	for _, want := range []string{"Hooks · PreToolUse", "Matcher: Bash", "User settings", "Matcher: Edit|Write",
		"Project settings", "Plugin hooks (demo@example-market)", "command · ./scripts/fmt.sh --fix"} {
		if !strings.Contains(s, want) {
			t.Errorf("event screen lacks %q:\n%s", want, s)
		}
	}
	// The project hook's file comes from discovery; e opens it.
	ecotest.Press(t, ctx, d, "down", "e")
	if got := x.Args(); len(got) != 1 || !reflect.DeepEqual(got[0], []string{filepath.Join(cwd, ".claude", "settings.json")}) {
		t.Fatalf("edit = %q", got)
	}
	// Editing refreshes the listing.
	if n := strings.Count(strings.Join(eng.Subtypes(), " "), proto.SubGetHooksListing); n != 2 {
		t.Errorf("listing requests = %d", n)
	}
	// Detail page.
	ecotest.Press(t, ctx, d, "up", "enter")
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "Timeout: 10s") || !strings.Contains(s, "File:") {
		t.Errorf("detail:\n%s", s)
	}
}

func TestManagedHookIsReadOnly(t *testing.T) {
	cwd := tree(t)
	ecotest.ResetState(t)
	eng := ecotest.NewEngine().Respond(proto.SubGetHooksListing, fixture(t))
	ctx := ecotest.NewCtx(eng, cwd)
	x := ecotest.Capture(t)
	d, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d)
	ecotest.Press(t, ctx, d, "down", "enter") // SessionStart
	ecotest.Press(t, ctx, d, "e")             // first row: managed settings
	if len(x.Cmds) != 0 || !strings.Contains(ecotest.Screen(ctx, d, 100), "set by your administrator") {
		t.Errorf("managed hooks must not open an editor")
	}
}

func TestWithoutEngine(t *testing.T) {
	cwd := tree(t)
	ecotest.ResetState(t)
	ctx := ecotest.NewCtx(nil, cwd)
	d, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d)
	s := ecotest.Screen(ctx, d, 100)
	if !strings.Contains(s, "showing hooks from settings files") || !strings.Contains(s, "2 hooks") {
		t.Fatalf("screen:\n%s", s)
	}
	ecotest.Press(t, ctx, d, "enter")
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "./scripts/fmt.sh --fix") || !strings.Contains(s, "User settings") {
		t.Errorf("event screen:\n%s", s)
	}
}

func TestPolicyBanner(t *testing.T) {
	cwd := tree(t)
	ecotest.ResetState(t)
	eng := ecotest.NewEngine().Respond(proto.SubGetHooksListing,
		`{"events": [], "hooks": [], "policy": {"allDisabled": true}}`)
	ctx := ecotest.NewCtx(eng, cwd)
	d, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d)
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "All hooks are disabled") || !strings.Contains(s, "No hooks configured") {
		t.Errorf("screen:\n%s", s)
	}
}

func TestStories(t *testing.T) {
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	if _, ok := r.Command("hooks"); !ok {
		t.Fatal("no /hooks")
	}
	for _, s := range r.Stories {
		t.Run(strings.ReplaceAll(s.ID, "/", "_"), func(t *testing.T) { testkit.RunStory(t, s) })
	}
}
