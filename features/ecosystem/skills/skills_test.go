package skills

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

func tree(t *testing.T) (cwd, home string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	cwd = filepath.Join(root, "proj")
	write(t, filepath.Join(cwd, ".claude", "skills", "deploy", "SKILL.md"), "---\ndescription: Deploy the app\n---\n")
	write(t, filepath.Join(home, ".claude", "skills", "notes", "SKILL.md"), "---\nname: notes\n---\n")
	write(t, filepath.Join(home, ".claude", "settings.json"), `{"skillOverrides": {"notes": "off", "other": "name-only"}}`)
	old := eco.RootsHook
	eco.RootsHook = func(string) discovery.Roots {
		return discovery.Roots{Home: home, ConfigDir: filepath.Join(home, ".claude"), CWD: cwd,
			Getenv: func(string) string { return "" }}
	}
	t.Cleanup(func() { eco.RootsHook = old })
	return cwd, home
}

func fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../../testdata/fixtures/09/control/reload_skills.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func open(t *testing.T, eng *ecotest.Engine, cwd string) (*ecotest.Ctx, ext.Dialog, *view) {
	t.Helper()
	ecotest.ResetState(t)
	ctx := ecotest.NewCtx(eng, cwd)
	d, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d)
	return ctx, d, d.Root().(*view)
}

func TestList(t *testing.T) {
	cwd, _ := tree(t)
	eng := ecotest.NewEngine().Respond(proto.SubReloadSkills, fixture(t))
	ctx, d, _ := open(t, eng, cwd)
	s := ecotest.Screen(ctx, d, 100)
	for _, want := range []string{"Project", "● deploy", "Deploy the app", "User", "○ notes", "[off]",
		"Built-in", "simplify", "Review changed code", "demo:status"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if strings.Index(s, "Project") > strings.Index(s, "User") || strings.Index(s, "User") > strings.Index(s, "Built-in") {
		t.Errorf("section order:\n%s", s)
	}
	if got := eng.Subtypes(); !reflect.DeepEqual(got, []string{proto.SubReloadSkills}) {
		t.Errorf("controls = %v", got)
	}
}

func TestCycleVisibility(t *testing.T) {
	cwd, _ := tree(t)
	eng := ecotest.NewEngine().Respond(proto.SubReloadSkills, fixture(t))
	ctx, d, v := open(t, eng, cwd)
	settings := ctx.SettingsV

	// A project skill without an override goes to local settings.
	v.list.Select("deploy")
	ecotest.Press(t, ctx, d, "space")
	if got := settings.Scopes[ext.ScopeLocal]["skillOverrides"]; !reflect.DeepEqual(got, map[string]any{"deploy": "name-only"}) {
		t.Fatalf("local overrides = %#v", got)
	}
	// notes is "off" in user settings: the next step is "on", which removes the
	// key and keeps the other override in that file.
	v.list.Select("notes")
	ecotest.Press(t, ctx, d, "space")
	if got := settings.Scopes[ext.ScopeUser]["skillOverrides"]; !reflect.DeepEqual(got, map[string]any{"other": "name-only"}) {
		t.Fatalf("user overrides = %#v", got)
	}
	// Each write is followed by a reload so the engine sees it.
	if n := strings.Count(strings.Join(eng.Subtypes(), " "), proto.SubReloadSkills); n != 3 {
		t.Errorf("reload_skills calls = %d", n)
	}
}

func TestEdit(t *testing.T) {
	cwd, _ := tree(t)
	eng := ecotest.NewEngine().Respond(proto.SubReloadSkills, fixture(t))
	ctx, d, v := open(t, eng, cwd)
	x := ecotest.Capture(t)
	v.list.Select("deploy")
	ecotest.Press(t, ctx, d, "e")
	if got := x.Args(); len(got) != 1 || got[0][0] != filepath.Join(cwd, ".claude", "skills", "deploy", "SKILL.md") {
		t.Fatalf("edit = %q", got)
	}
	v.list.Select("simplify")
	ecotest.Press(t, ctx, d, "e")
	if len(x.Cmds) != 1 || !strings.Contains(ecotest.Screen(ctx, d, 100), "built into Claude Code") {
		t.Errorf("built-in skills have no file")
	}
	ecotest.Press(t, ctx, d, "enter")
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "/simplify") || !strings.Contains(s, "Source: Built-in") {
		t.Errorf("detail:\n%s", s)
	}
}

func TestWithoutEngine(t *testing.T) {
	cwd, _ := tree(t)
	ctx, d, _ := open(t, nil, cwd)
	s := ecotest.Screen(ctx, d, 100)
	if !strings.Contains(s, "showing skills found on disk") || !strings.Contains(s, "deploy") || strings.Contains(s, "simplify") {
		t.Errorf("screen:\n%s", s)
	}
}

func TestMerge(t *testing.T) {
	got := Merge(
		[]proto.SlashCommand{{Name: "a", Description: "engine desc"}, {Name: "b"}},
		[]discovery.Skill{{Name: "a", Path: "/x/a/SKILL.md", Scope: discovery.ScopeUser, UserInvocable: true}},
		map[string]discovery.SkillOverride{"b": {Value: discovery.VisibilityOff, Scope: discovery.ScopeManaged}},
	)
	if len(got) != 2 || !got[0].Loaded || got[0].Description != "engine desc" || got[0].Path == "" {
		t.Fatalf("merge = %+v", got)
	}
	if got[1].Visibility != discovery.VisibilityOff || got[1].OverrideScope != discovery.ScopeManaged {
		t.Errorf("override = %+v", got[1])
	}
}

func TestStories(t *testing.T) {
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	if _, ok := r.Command("skills"); !ok {
		t.Fatal("no /skills")
	}
	for _, s := range r.Stories {
		testkit.RunStory(t, s)
	}
}
