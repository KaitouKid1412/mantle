package termsetup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeDefaults stands in for the `defaults` tool: one exported plist per domain.
type fakeDefaults struct {
	domains map[string][]byte
	calls   []string
}

func (f *fakeDefaults) Run(name string, args []string, stdin []byte) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if name != "defaults" || len(args) < 2 {
		return nil, fmt.Errorf("unexpected command %s %v", name, args)
	}
	switch args[0] {
	case "export":
		b, ok := f.domains[args[1]]
		if !ok {
			return nil, errors.New("domain does not exist")
		}
		return b, nil
	case "import":
		b, err := os.ReadFile(args[2])
		if err != nil {
			return nil, err
		}
		f.domains[args[1]] = b
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected defaults %v", args)
}

func fixedNow() time.Time { return time.Date(2026, 10, 3, 17, 45, 0, 0, time.UTC) }

func TestApplyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "User", "keybindings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := fixture(t, "vscode_comments.json")
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	a := Applier{Now: fixedNow}
	in, err := a.Load(Detected{Terminal: VSCode}, Env{Home: dir, GOOS: "linux", XDGConfigHome: filepath.Join(dir, "nope")}, false)
	if err != nil {
		t.Fatal(err)
	}
	if in.Exists {
		t.Fatalf("loaded from the wrong place: %s", in.Path)
	}
	in = Input{Terminal: VSCode, Path: path, Exists: true, Content: old}
	p := Plan(in)
	backup, err := a.Apply(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := path + ".mantle-backup-20261003-174500"; backup != want {
		t.Errorf("backup %q, want %q", backup, want)
	}
	if b, _ := os.ReadFile(backup); string(b) != string(old) {
		t.Error("backup content differs")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(p.New) {
		t.Error("file not updated")
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	// Applying the same (now stale) proposal again must refuse.
	if _, err := a.Apply(p); !errors.Is(err, ErrStale) {
		t.Errorf("stale apply: %v", err)
	}
	// A second backup in the same second gets a suffix instead of overwriting.
	p2 := p
	p2.Old, p2.New = got, append(append([]byte(nil), got...), '\n')
	b2, err := a.Apply(p2)
	if err != nil || b2 != path+".mantle-backup-20261003-174500-1" {
		t.Errorf("second backup %q, %v", b2, err)
	}
}

func TestApplyCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alacritty", "alacritty.toml")
	p := Plan(Input{Terminal: Alacritty, Path: path})
	backup, err := (Applier{Now: fixedNow}).Apply(p)
	if err != nil || backup != "" {
		t.Fatalf("backup %q, %v", backup, err)
	}
	if b, _ := os.ReadFile(path); string(b) != alacrittyBlock {
		t.Errorf("content %q", b)
	}
}

func TestApplyRefuses(t *testing.T) {
	a := Applier{}
	if _, err := a.Apply(Proposal{Status: Edit, Kind: TargetFile, Path: "/h/.claude.json"}); !errors.Is(err, ErrForbidden) {
		t.Errorf(".claude.json: %v", err)
	}
	if _, err := a.Apply(Proposal{Status: Native}); err == nil {
		t.Error("applied a non-edit")
	}
}

func TestApplyDefaults(t *testing.T) {
	old := fixture(t, "iterm2_nokeymap.plist")
	fake := &fakeDefaults{domains: map[string][]byte{"com.googlecode.iterm2": old}}
	backups := filepath.Join(t.TempDir(), "backups")
	a := Applier{Runner: fake, Now: fixedNow, BackupDir: backups}

	in, err := a.Load(Detected{Terminal: ITerm2}, Env{Home: "/h", GOOS: "darwin"}, false)
	if err != nil || !in.Exists || string(in.Content) != string(old) {
		t.Fatalf("load: %v %v", in.Exists, err)
	}
	p := Plan(in)
	backup, err := a.Apply(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(backups, "com.googlecode.iterm2-20261003-174500.plist"); backup != want {
		t.Errorf("backup %q", backup)
	}
	if b, _ := os.ReadFile(backup); string(b) != string(old) {
		t.Error("backup content differs")
	}
	if string(fake.domains["com.googlecode.iterm2"]) != string(p.New) {
		t.Error("domain not imported")
	}
	if entries, _ := os.ReadDir(backups); len(entries) != 1 {
		t.Errorf("temp file left behind: %d entries", len(entries))
	}
	if _, err := a.Apply(p); !errors.Is(err, ErrStale) {
		t.Errorf("stale: %v", err)
	}

	missing, err := a.Load(Detected{Terminal: AppleTerminal}, Env{Home: "/h", GOOS: "darwin"}, false)
	if err != nil || missing.Exists {
		t.Errorf("missing domain: %+v %v", missing, err)
	}
	if _, err := (Applier{Runner: fake}).Apply(p); err == nil {
		t.Error("applied without a backup dir")
	}
}
