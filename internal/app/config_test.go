package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/config"
	"github.com/KaitouKid1412/mantle/internal/keymap"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func TestConfigReloadUpdatesThemeAndKeymap(t *testing.T) {
	home := t.TempDir()
	p := config.PathsFor(home, filepath.Join(home, "proj"), "", "")
	p.Managed = ""
	os.MkdirAll(p.ClaudeDir, 0o755)
	os.WriteFile(p.ScopeFile(ext.ScopeUser), []byte(`{"theme":"dark"}`), 0o644)
	store := config.NewStore(p, "")

	var seen []ext.SettingsMsg
	f := ext.Feature{ID: "test.watch", Order: 10, Setup: func(r ext.Registrar) error {
		ext.Subscribe(r, "test.watch", func(c ext.Ctx, m ext.SettingsMsg) tea.Cmd { seen = append(seen, m); return nil })
		return nil
	}}
	h := NewHost([]ext.Feature{f}, HostOptions{Core: CoreFeatures()})
	r := New(Options{Host: h, Settings: store, NoBackgroundQuery: true, Clock: &manualClock{t: time.Unix(0, 0)}})
	if r.Ctx().Theme().Name != "dark" {
		t.Fatal("initial theme")
	}

	os.WriteFile(p.ScopeFile(ext.ScopeUser), []byte(`{"theme":"light"}`), 0o644)
	drive(t, r, config.ReloadMsg{Snap: config.Load(p, "")})
	if r.Ctx().Theme().Name != "light" {
		t.Fatalf("theme after reload = %q", r.Ctx().Theme().Name)
	}
	if len(seen) != 1 || seen[0].Changed[0] != "theme" {
		t.Fatalf("SettingsMsg = %+v", seen)
	}

	u, _ := keymap.ParseFile("u", []byte(`{"bindings":[{"context":"Global","bindings":{"ctrl+o":"app:toggleTodos"}}]}`), false)
	drive(t, r, config.KeybindingsMsg{Sources: []keymap.Source{u}})
	if got := r.Ctx().KeysFor(ext.ContextGlobal, ext.ActAppToggleTodos); len(got) != 2 {
		t.Fatalf("KeysFor toggleTodos after reload = %v", got)
	}

	ct, err := config.ParseCustomTheme("mine", []byte(`{"base":"light","overrides":{"claude":"#123456"}}`))
	if err != nil {
		t.Fatal(err)
	}
	drive(t, r, config.ThemesMsg{Themes: map[string]config.CustomTheme{"mine": ct}}, ext.ThemePreviewMsg{Name: "custom:mine"})
	if r.Ctx().Theme().Name != "custom:mine" || r.Ctx().Theme().Dark {
		t.Fatalf("custom theme = %q", r.Ctx().Theme().Name)
	}
	if _, ok := r.Ctx().Settings().(ext.ClaudeSettingsWriter); !ok {
		t.Fatal("host settings must implement ClaudeSettingsWriter")
	}
	if _, ok := r.Ctx().Settings().(ext.ScopedSettings); !ok {
		t.Fatal("host settings must implement ScopedSettings")
	}
}
