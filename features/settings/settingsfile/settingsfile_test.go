package settingsfile

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

func newWriter(t *testing.T) (Writer, string) {
	t.Helper()
	home := t.TempDir()
	return Writer{LockDir: filepath.Join(home, ".mantle", "locks"), Env: patch.Env{Home: home, ProjectRoot: filepath.Join(home, "repo")}}, home
}

func TestRoundTripKeepsOrderAndNumbers(t *testing.T) {
	in := `{
  "permissions": {
    "deny": ["Read(./.env)"],
    "allow": ["Bash(git *)"]
  },
  "model": "opus",
  "cleanupPeriodDays": 30,
  "big": 12345678901234567890,
  "url": "https://example.com/?a=1&b=<2>",
  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo hi"}]}]}
}`
	doc, order, err := Decode([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Encode(doc, order)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "permissions": {
    "deny": [
      "Read(./.env)"
    ],
    "allow": [
      "Bash(git *)"
    ]
  },
  "model": "opus",
  "cleanupPeriodDays": 30,
  "big": 12345678901234567890,
  "url": "https://example.com/?a=1&b=<2>",
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "echo hi"
          }
        ]
      }
    ]
  }
}
`
	if string(out) != want {
		t.Errorf("got\n%s", out)
	}
}

func TestApplyPatch(t *testing.T) {
	w, home := newWriter(t)
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"theme": "dark", "model": "sonnet", "env": {"B": "2", "A": "1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := patch.Patch{Scope: patch.User, Ops: []patch.Op{
		{Kind: patch.Set, Path: []string{"model"}, Value: "opus"},
		{Kind: patch.Set, Path: []string{"modelSettings", "claude-opus-5-5", "effortLevel"}, Value: "xhigh"},
		{Kind: patch.Set, Path: []string{"alwaysThinkingEnabled"}, Value: false},
	}}
	got, err := w.Apply(p)
	if err != nil || got != path {
		t.Fatalf("apply: %s %v", got, err)
	}
	b, _ := os.ReadFile(path)
	want := `{
  "theme": "dark",
  "model": "opus",
  "env": {
    "B": "2",
    "A": "1"
  },
  "alwaysThinkingEnabled": false,
  "modelSettings": {
    "claude-opus-5-5": {
      "effortLevel": "xhigh"
    }
  }
}
`
	if string(b) != want {
		t.Errorf("file\n%s", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode changed to %v", fi.Mode().Perm())
	}
}

func TestMissingFileAndNoop(t *testing.T) {
	w, _ := newWriter(t)
	path, err := w.Apply(patch.SetKey(patch.Local, "Explanatory", "outputStyle"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "{\n  \"outputStyle\": \"Explanatory\"\n}\n" {
		t.Errorf("new file %q", b)
	}
	before, _ := os.Stat(path)
	// Re-applying the same value leaves the file untouched.
	if _, err := w.Apply(patch.SetKey(patch.Local, "Explanatory", "outputStyle")); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("no-op rewrote the file")
	}
}

func TestRefusesInvalidFile(t *testing.T) {
	w, home := newWriter(t)
	path := filepath.Join(home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	broken := []byte(`{"model": "opus",}`)
	os.WriteFile(path, broken, 0o644)
	if _, err := w.Apply(patch.SetKey(patch.User, "dark", "theme")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(broken) {
		t.Error("invalid file was modified")
	}
	os.WriteFile(path, []byte(`["not", "an", "object"]`), 0o644)
	if _, err := w.Apply(patch.SetKey(patch.User, "dark", "theme")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("array top level: %v", err)
	}
}

func TestWritesThroughSymlink(t *testing.T) {
	w, home := newWriter(t)
	real := filepath.Join(home, "dotfiles", "claude-settings.json")
	os.MkdirAll(filepath.Dir(real), 0o755)
	os.WriteFile(real, []byte(`{"model":"sonnet"}`), 0o644)
	link := filepath.Join(home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink("../dotfiles/claude-settings.json", link); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Apply(patch.SetKey(patch.User, "opus", "model")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced by a file")
	}
	b, _ := os.ReadFile(real)
	if string(b) != "{\n  \"model\": \"opus\"\n}\n" {
		t.Errorf("target %q", b)
	}
}

func TestNeverWritesGlobalConfig(t *testing.T) {
	w, home := newWriter(t)
	global := filepath.Join(home, ".claude.json")
	os.WriteFile(global, []byte(`{"numStartups": 1}`), 0o644)
	called := false
	err := w.Update(global, func(m map[string]any) (map[string]any, error) { called = true; return m, nil })
	if !errors.Is(err, patch.ErrGlobalConfig) || called {
		t.Errorf("direct: %v called=%v", err, called)
	}
	// A settings path that is a symlink to the global config is refused too.
	link := filepath.Join(home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(link), 0o755)
	os.Symlink(global, link)
	if _, err := w.Apply(patch.SetKey(patch.User, "dark", "theme")); !errors.Is(err, patch.ErrGlobalConfig) {
		t.Errorf("via symlink: %v", err)
	}
	if b, _ := os.ReadFile(global); string(b) != `{"numStartups": 1}` {
		t.Error("global config modified")
	}
}

func TestConcurrentWritersMerge(t *testing.T) {
	w, home := newWriter(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rule := "Bash(task-" + string(rune('a'+i)) + ")"
			p := patch.Patch{Scope: patch.User, Ops: []patch.Op{{Kind: patch.AddUnique, Path: []string{"permissions", "allow"}, Value: rule}}}
			if _, err := w.Apply(p); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	b, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	doc, _, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	allow, _ := patch.Get(doc, "permissions", "allow")
	if n := len(allow.([]any)); n != 20 {
		t.Errorf("lost updates: %d rules", n)
	}
}

func TestFnErrorWritesNothing(t *testing.T) {
	w, home := newWriter(t)
	path := filepath.Join(home, "x.json")
	boom := errors.New("boom")
	if err := w.Update(path, func(map[string]any) (map[string]any, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file created after error")
	}
}
