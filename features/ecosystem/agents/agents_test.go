package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func tree(t *testing.T) (cwd, home string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	cwd = filepath.Join(root, "proj")
	write(t, filepath.Join(cwd, ".claude", "agents", "code-reviewer.md"),
		"---\nname: code-reviewer\ndescription: Reviews diffs\ntools: Read, Grep\nmodel: sonnet\n---\nBody\n")
	write(t, filepath.Join(home, ".claude", "agents", "notes.md"), "---\nname: notes\ndescription: Notes\n---\n")
	old := eco.RootsHook
	eco.RootsHook = func(string) discovery.Roots {
		return discovery.Roots{Home: home, ConfigDir: filepath.Join(home, ".claude"), CWD: cwd,
			Getenv: func(string) string { return "" }}
	}
	t.Cleanup(func() { eco.RootsHook = old })
	return cwd, home
}

func reloadReply(names ...string) string {
	var agents []proto.AgentInfo
	for _, n := range names {
		agents = append(agents, proto.AgentInfo{Name: n})
	}
	b, _ := json.Marshal(proto.ReloadPluginsResponse{Agents: agents})
	return string(b)
}

func open(t *testing.T, eng *ecotest.Engine, cwd string) (*ecotest.Ctx, *ecotest.Execs, ext.Dialog, *view) {
	t.Helper()
	ecotest.ResetState(t)
	ecotest.Observe(&proto.SystemInit{Agents: []string{"code-reviewer", "Explore", "general-purpose"}})
	ctx := ecotest.NewCtx(eng, cwd)
	x := ecotest.Capture(t)
	d, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d)
	return ctx, x, d, d.Root().(*view)
}

func TestList(t *testing.T) {
	cwd, _ := tree(t)
	ctx, _, d, _ := open(t, ecotest.NewEngine(), cwd)
	s := ecotest.Screen(ctx, d, 100)
	for _, want := range []string{"Project", "code-reviewer", "sonnet · Reviews diffs", "User", "notes", "Built-in", "Explore", "general-purpose"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	ecotest.Press(t, ctx, d, "enter")
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "Tools: Read, Grep") || !strings.Contains(s, "File: ") {
		t.Errorf("detail:\n%s", s)
	}
}

func TestCreate(t *testing.T) {
	cwd, _ := tree(t)
	eng := ecotest.NewEngine().Respond(proto.SubReloadPlugins, reloadReply("code-reviewer", "my-agent"))
	ctx, x, d, _ := open(t, eng, cwd)
	ecotest.Press(t, ctx, d, "n", "enter") // project
	for _, r := range "Bad Name" {
		ecotest.Press(t, ctx, d, string(r))
	}
	ecotest.Press(t, ctx, d, "enter")
	if !strings.Contains(ecotest.Screen(ctx, d, 100), "lowercase letters") {
		t.Fatalf("invalid name accepted:\n%s", ecotest.Screen(ctx, d, 100))
	}
	ecotest.Press(t, ctx, d, "ctrl+u")
	for _, r := range "my-agent" {
		ecotest.Press(t, ctx, d, string(r))
	}
	ecotest.Press(t, ctx, d, "enter")
	path := filepath.Join(cwd, ".claude", "agents", "my-agent.md")
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "name: my-agent") {
		t.Fatalf("template not written: %v %q", err, b)
	}
	if got := x.Args(); len(got) != 1 || got[0][0] != path {
		t.Fatalf("edit = %q", got)
	}
	if subs := eng.Subtypes(); len(subs) != 1 || subs[0] != proto.SubReloadPlugins {
		t.Errorf("after editing: %v", subs)
	}
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "Claude has the change now") || !strings.Contains(s, "my-agent") {
		t.Errorf("screen:\n%s", s)
	}
}

func TestDeleteOffersRestartWhenEngineKeepsAgent(t *testing.T) {
	cwd, _ := tree(t)
	// The engine still lists code-reviewer after the reload.
	eng := ecotest.NewEngine().Respond(proto.SubReloadPlugins, reloadReply("code-reviewer"))
	ctx, _, d, v := open(t, eng, cwd)
	v.list.Select("code-reviewer")
	ecotest.Press(t, ctx, d, "x", "y")
	if _, err := os.Stat(filepath.Join(cwd, ".claude", "agents", "code-reviewer.md")); !os.IsNotExist(err) {
		t.Fatalf("file not deleted: %v", err)
	}
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "needs a restart") {
		t.Fatalf("restart offer missing:\n%s", s)
	}
	ecotest.Press(t, ctx, d, "y")
	if len(eng.Restarts) != 1 {
		t.Errorf("restarts = %d", len(eng.Restarts))
	}
}

func TestBuiltInIsReadOnly(t *testing.T) {
	cwd, _ := tree(t)
	ctx, x, d, v := open(t, ecotest.NewEngine(), cwd)
	v.list.Select("Explore")
	ecotest.Press(t, ctx, d, "e", "x")
	if len(x.Cmds) != 0 || d.(interface{ Depth() int }).Depth() != 1 ||
		!strings.Contains(ecotest.Screen(ctx, d, 100), "built into Claude Code") {
		t.Errorf("built-in agents must be read-only")
	}
}

func TestStories(t *testing.T) {
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	if _, ok := r.Command("agents"); !ok {
		t.Fatal("no /agents")
	}
	for _, s := range r.Stories {
		testkit.RunStory(t, s)
	}
}
