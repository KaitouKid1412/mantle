package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	home := t.TempDir()
	project := filepath.Join(home, "proj")
	p := PathsFor(home, project, "", "")
	p.Managed = filepath.Join(home, "managed", "managed-settings.json")
	return p
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPrecedenceAndMerge(t *testing.T) {
	p := testPaths(t)
	write(t, p.ScopeFile(ext.ScopeUser), `{"theme":"light","verbose":true,"permissions":{"allow":["Bash(ls)"],"defaultMode":"default"},"env":{"A":"user","B":"user"}}`)
	write(t, p.ScopeFile(ext.ScopeProject), `{"theme":"dark-ansi","permissions":{"allow":["Read"],"deny":["Bash(rm)"]},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"p"}]}]}}`)
	write(t, p.ScopeFile(ext.ScopeLocal), `{"theme":"light-daltonized","permissions":{"allow":["Bash(ls)"]},"env":{"B":"local"},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"l"}]}]}}`)
	write(t, p.Managed, `{"permissions":{"defaultMode":"plan"}}`)
	write(t, filepath.Join(filepath.Dir(p.Managed), "managed-settings.d", "10-extra.json"), `{"companyAnnouncements":["hi"]}`)
	s := NewStore(p, `{"theme":"dark"}`)

	if v, _ := s.Claude("theme"); v != "dark" {
		t.Errorf("theme = %v, want flag value dark", v)
	}
	perms := s.snap.Merged["permissions"].(map[string]any)
	if got := fmt.Sprint(perms["allow"]); got != "[Bash(ls) Read]" {
		t.Errorf("allow = %s", got)
	}
	if perms["defaultMode"] != "plan" || fmt.Sprint(perms["deny"]) != "[Bash(rm)]" {
		t.Errorf("permissions = %v", perms)
	}
	env := s.snap.Merged["env"].(map[string]any)
	if env["A"] != "user" || env["B"] != "local" {
		t.Errorf("env = %v", env)
	}
	stop := s.snap.Merged["hooks"].(map[string]any)["Stop"].([]any)
	if len(stop) != 2 {
		t.Errorf("hooks should accumulate: %v", stop)
	}
	if got := s.UI().CompanyAnnouncements; len(got) != 1 || got[0] != "hi" {
		t.Errorf("managed drop-in not read: %v", got)
	}
	if !s.UI().Verbose {
		t.Error("user-only key lost")
	}
	srcs := s.ClaudeSources()
	if len(srcs) != 5 || srcs[0].Scope != ext.ScopePolicy || !srcs[0].Exists || srcs[4].Scope != ext.ScopeUser {
		t.Errorf("sources = %+v", srcs)
	}
	if s.ClaudeScope(ext.ScopeProject)["theme"] != "dark-ansi" {
		t.Error("ClaudeScope(project) wrong")
	}
}

func TestInvalidScopeIgnoredAndGlobalFallback(t *testing.T) {
	p := testPaths(t)
	write(t, p.ScopeFile(ext.ScopeUser), `{"theme": "light",`) // broken
	write(t, p.ClaudeJSON, `{"theme":"dark-daltonized","projects":{}}`)
	s := NewStore(p, "")
	if v, _ := s.Claude("theme"); v != "dark-daltonized" {
		t.Errorf("theme = %v, want ~/.claude.json fallback", v)
	}
	var userSrc ext.SettingsSource
	for _, src := range s.ClaudeSources() {
		if src.Scope == ext.ScopeUser {
			userSrc = src
		}
	}
	if !userSrc.Exists || !errors.Is(userSrc.Err, ErrInvalid) {
		t.Errorf("user source = %+v", userSrc)
	}
}

func TestWriterKeepsOrderAndFormatting(t *testing.T) {
	p := testPaths(t)
	path := p.ScopeFile(ext.ScopeUser)
	write(t, path, "{\n  \"zeta\": 1.50,\n  \"alpha\": {\"b\": 1, \"a\": \"<x>\"},\n  \"model\": \"opus\"\n}\n")
	if err := (Writer{Paths: p}).Set(path, "model", "sonnet"); err != nil {
		t.Fatal(err)
	}
	if err := (Writer{Paths: p}).Set(path, "newKey", true); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "{\n  \"zeta\": 1.50,\n  \"alpha\": {\n    \"b\": 1,\n    \"a\": \"<x>\"\n  },\n  \"model\": \"sonnet\",\n  \"newKey\": true\n}\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriterSafety(t *testing.T) {
	p := testPaths(t)
	w := Writer{Paths: p}
	// ~/.claude.json, directly and through a symlink.
	write(t, p.ClaudeJSON, `{}`)
	if err := w.Set(p.ClaudeJSON, "x", 1); !errors.Is(err, ErrForbidden) {
		t.Errorf("claude.json write: %v", err)
	}
	link := filepath.Join(p.Home, "sneaky.json")
	if err := os.Symlink(p.ClaudeJSON, link); err != nil {
		t.Fatal(err)
	}
	if err := w.Set(link, "x", 1); !errors.Is(err, ErrForbidden) {
		t.Errorf("symlinked claude.json write: %v", err)
	}
	// Invalid file is never overwritten.
	bad := p.ScopeFile(ext.ScopeLocal)
	write(t, bad, `{not json`)
	if err := w.Set(bad, "x", 1); !errors.Is(err, ErrInvalid) {
		t.Errorf("invalid file: %v", err)
	}
	if b, _ := os.ReadFile(bad); string(b) != `{not json` {
		t.Error("invalid file was modified")
	}
	// Symlinked settings are written through the link; the link survives.
	real := filepath.Join(p.Home, "dotfiles", "settings.json")
	write(t, real, `{"a":1}`)
	user := p.ScopeFile(ext.ScopeUser)
	os.MkdirAll(filepath.Dir(user), 0o755)
	if err := os.Symlink(real, user); err != nil {
		t.Fatal(err)
	}
	if err := w.Set(user, "b", 2); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(user); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink replaced by a file")
	}
	if b, _ := os.ReadFile(real); !strings.Contains(string(b), `"b": 2`) {
		t.Errorf("target not written: %s", b)
	}
}

func TestWriterConcurrentMerges(t *testing.T) {
	p := testPaths(t)
	path := p.ScopeFile(ext.ScopeUser)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := (Writer{Paths: p}).Set(path, fmt.Sprintf("k%02d", i), i); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var doc map[string]any
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc) != 20 {
		t.Fatalf("lost updates: %d keys, err %v\n%s", len(doc), err, raw)
	}
}

func TestSetMantleAndClaude(t *testing.T) {
	p := testPaths(t)
	s := NewStore(p, "")
	s.SetSpecs([]ext.SettingSpec{{Key: "selfmod.confirm", Type: "enum", Default: "on-visual-change"}})
	if s.Mantle("selfmod.confirm") != "on-visual-change" {
		t.Fatal("spec default not used")
	}
	msg := s.SetMantle("selfmod.confirm", "always")()
	if sm, ok := msg.(ext.SettingsMsg); !ok || sm.Changed[0] != "mantle:selfmod.confirm" {
		t.Fatalf("SetMantle msg = %#v", msg)
	}
	if s.Mantle("selfmod.confirm") != "always" {
		t.Fatal("in-memory value not updated")
	}
	if b, _ := os.ReadFile(p.MantleSettings()); !strings.Contains(string(b), `"selfmod.confirm": "always"`) {
		t.Fatalf("mantle settings file: %s", b)
	}
	// Claude settings never get mantle keys.
	if _, err := os.Stat(p.ScopeFile(ext.ScopeUser)); err == nil {
		t.Fatal("SetMantle touched Claude Code settings")
	}
	msg = s.SetClaude(ext.ScopeUser, "skillOverrides", map[string]any{"x": "off"})()
	rm, ok := msg.(ReloadMsg)
	if !ok {
		t.Fatalf("SetClaude msg = %#v", msg)
	}
	if changed := s.Apply(rm); strings.Join(changed, ",") != "skillOverrides" {
		t.Fatalf("changed = %v", changed)
	}
	if msg := s.SetClaude(ext.ScopePolicy, "x", 1)(); msg.(WriteFailedMsg).Err == nil {
		t.Fatal("policy scope must not be writable")
	}
}

func TestWatcherReloads(t *testing.T) {
	p := testPaths(t)
	write(t, p.ScopeFile(ext.ScopeUser), `{"theme":"dark"}`)
	os.MkdirAll(p.MantleDir, 0o755)
	os.MkdirAll(p.ThemesDir(), 0o755)
	msgs := make(chan tea.Msg, 16)
	w, err := Watch(p, "", func(m tea.Msg) { msgs <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	s := NewStore(p, "")

	write(t, p.ScopeFile(ext.ScopeUser), `{"theme":"light"}`)
	rm := waitMsg[ReloadMsg](t, msgs)
	if changed := s.Apply(rm); strings.Join(changed, ",") != "theme" {
		t.Fatalf("changed = %v", changed)
	}

	write(t, p.ClaudeKeybindings(), `{"bindings":[{"context":"Chat","bindings":{"ctrl+e":"chat:externalEditor"}}]}`)
	km := waitMsg[KeybindingsMsg](t, msgs)
	if len(km.Sources) != 2 || len(km.Sources[0].Bindings) != 1 {
		t.Fatalf("keybindings = %+v", km.Sources)
	}

	write(t, filepath.Join(p.ThemesDir(), "mine.json"), `{"name":"Mine","base":"light","overrides":{"claude":"#ff0000","notAToken":"#00ff00","error":"bogus"}}`)
	tm := waitMsg[ThemesMsg](t, msgs)
	ct, ok := tm.Themes["mine"]
	if !ok || ct.Name != "Mine" || ct.Theme.Name != "custom:mine" || ct.Theme.Base != "light" {
		t.Fatalf("themes = %+v", tm.Themes)
	}
	light, _ := theme.Builtin("light")
	if ct.Theme.Color(theme.Accent) != theme.MustColor("#ff0000") || ct.Theme.Color(theme.Error) != light.Color(theme.Error) || ct.Theme.Has("notAToken") {
		t.Fatal("overrides applied wrongly")
	}
}

func waitMsg[T tea.Msg](t *testing.T, ch chan tea.Msg) T {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-ch:
			if v, ok := m.(T); ok {
				return v
			}
		case <-deadline:
			var zero T
			t.Fatalf("no %T within 5s", zero)
			return zero
		}
	}
}

func TestDisabledStore(t *testing.T) {
	p := testPaths(t)
	now := time.Unix(100, 0).UTC()
	if err := RecordDisabled(p, "mod.a", "panic", now); err != nil {
		t.Fatal(err)
	}
	RecordDisabled(p, "mod.b", "panic", now)
	RecordDisabled(p, "mod.a", "again", now)
	ids := DisabledIDs(ReadDisabled(p))
	if strings.Join(ids, ",") != "mod.b,mod.a" {
		t.Fatalf("ids = %v", ids)
	}
	ClearDisabled(p, "mod.b")
	if ids := DisabledIDs(ReadDisabled(p)); strings.Join(ids, ",") != "mod.a" {
		t.Fatalf("after clear = %v", ids)
	}
}
