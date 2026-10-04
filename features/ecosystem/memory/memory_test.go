package memory

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

func tree(t *testing.T, userSettings string) (cwd, home string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	cwd = filepath.Join(root, "proj")
	write(t, filepath.Join(home, ".claude", "CLAUDE.md"), "User memory\n")
	write(t, filepath.Join(cwd, ".claude", "rules", "api.md"), "---\npaths: [src/api/**]\n---\nAPI rules\n")
	if userSettings != "" {
		write(t, filepath.Join(home, ".claude", "settings.json"), userSettings)
	}
	old := eco.RootsHook
	eco.RootsHook = func(string) discovery.Roots {
		return discovery.Roots{Home: home, ConfigDir: filepath.Join(home, ".claude"), CWD: cwd,
			Getenv: func(string) string { return "" }}
	}
	t.Cleanup(func() { eco.RootsHook = old })
	return cwd, home
}

func TestPanel(t *testing.T) {
	cwd, _ := tree(t, "")
	ecotest.ResetState(t)
	eng := ecotest.NewEngine()
	ctx := ecotest.NewCtx(eng, cwd)
	x := ecotest.Capture(t)
	d, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d)
	s := ecotest.Screen(ctx, d, 100)
	for _, want := range []string{"User", "● ~/.claude/CLAUDE.md", "Project", "+ CLAUDE.md", "not created yet",
		".claude/rules/api.md", "rule · for src/api/**", "Auto memory: on", "Open the auto-memory folder"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	// Open the missing project CLAUDE.md: editor, then register_repo_root.
	v := d.Root().(*view)
	v.list.Select(filepath.Join(cwd, "CLAUDE.md"))
	ecotest.Press(t, ctx, d, "enter")
	if got := x.Args(); len(got) != 1 || got[0][0] != filepath.Join(cwd, "CLAUDE.md") {
		t.Fatalf("edit = %q", got)
	}
	if len(eng.Controls) != 1 || eng.Controls[0].Req != (proto.RegisterRepoRootRequest{Directory: cwd, ReloadClaudeMD: true}) {
		t.Fatalf("controls = %#v", eng.Controls)
	}
	if !strings.Contains(ecotest.Screen(ctx, d, 100), "Saved and reloaded") {
		t.Errorf("status missing")
	}

	// Toggle auto memory: writes local settings.
	ecotest.Press(t, ctx, d, "a")
	if got := ctx.SettingsV.Scopes[ext.ScopeLocal]["autoMemoryEnabled"]; got != false {
		t.Fatalf("autoMemoryEnabled = %#v", got)
	}
	// The host writes the file, then delivers SettingsMsg; the panel rescans.
	write(t, filepath.Join(cwd, ".claude", "settings.local.json"), `{"autoMemoryEnabled": false}`)
	ecotest.Drive(t, ctx, d, ext.Msg(ext.SettingsMsg{Changed: []string{"autoMemoryEnabled"}}))
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "Auto memory: off") || strings.Contains(s, "auto-memory folder") {
		t.Errorf("after toggle:\n%s", s)
	}
}

func TestNoReloadSupport(t *testing.T) {
	cwd, _ := tree(t, "")
	ecotest.ResetState(t)
	eng := ecotest.NewEngine()
	eng.Unsupported[proto.SubRegisterRepoRoot] = true
	ctx := ecotest.NewCtx(eng, cwd)
	ecotest.Capture(t)
	d, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d)
	ecotest.Press(t, ctx, d, "enter")
	if !strings.Contains(ecotest.Screen(ctx, d, 100), "next session") {
		t.Errorf("without register_repo_root the panel says when it applies")
	}
}

func TestPauseMemory(t *testing.T) {
	cwd, _ := tree(t, `{"autoMemoryEnabled": true}`)
	ecotest.ResetState(t)
	eng := ecotest.NewEngine()
	ctx := ecotest.NewCtx(eng, cwd)
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	cmd, _ := r.Command("pause-memory")
	exttest.Exec(cmd.Run(ctx, ""))
	// Set in user settings, so the toggle writes there.
	if got := ctx.SettingsV.Scopes[ext.ScopeUser]["autoMemoryEnabled"]; got != false {
		t.Fatalf("autoMemoryEnabled = %#v (scopes %#v)", got, ctx.SettingsV.Scopes)
	}
	if n := ctx.Notices[len(ctx.Notices)-1]; !strings.Contains(n.Text, "paused") {
		t.Errorf("notice = %+v", n)
	}
	// When the engine has /pause-memory, pass it through.
	ecotest.Observe(&proto.SystemInit{SlashCommands: []string{"pause-memory"}})
	cmd.Run(ctx, "")
	if !reflect.DeepEqual(eng.Sent, []string{"/pause-memory"}) {
		t.Errorf("sent = %q", eng.Sent)
	}
	if !reflect.DeepEqual(cmd.Aliases, []string{"memory-pause", "toggle-memory"}) {
		t.Errorf("aliases = %q", cmd.Aliases)
	}
}

func TestEngineAutoDir(t *testing.T) {
	cwd, _ := tree(t, "")
	ecotest.ResetState(t)
	ecotest.Observe(&proto.SystemInit{MemoryPaths: []byte(`{"auto": "/elsewhere/memory"}`)})
	ctx := ecotest.NewCtx(ecotest.NewEngine(), cwd)
	d, _ := New(ctx, nil)
	ecotest.Open(t, ctx, d)
	if s := ecotest.Screen(ctx, d, 100); !strings.Contains(s, "/elsewhere/memory") {
		t.Errorf("init.memory_paths should win:\n%s", s)
	}
}

func TestStories(t *testing.T) {
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	for _, s := range r.Stories {
		testkit.RunStory(t, s)
	}
}
