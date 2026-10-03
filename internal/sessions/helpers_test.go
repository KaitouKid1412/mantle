package sessions

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	sidPlain   = "11111111-1111-4111-8111-111111111111"
	sidTools   = "22222222-2222-4222-8222-222222222222"
	sidCompact = "33333333-3333-4333-8333-333333333333"
	sidBranch  = "44444444-4444-4444-8444-444444444444"
	sidMessy   = "55555555-5555-4555-8555-555555555555"
	sidEmpty   = "66666666-6666-4666-8666-666666666666"
	sidOther   = "77777777-7777-4777-8777-777777777777"
)

// fixtureConfig is the checked-in fake Claude config dir.
func fixtureConfig() string { return filepath.Join("..", "..", "testdata", "fixtures", "06", "claude") }

func fixturePath(project, id string) string {
	return filepath.Join(fixtureConfig(), "projects", project, id+".jsonl")
}

func loadFixture(t *testing.T, id string) *Transcript {
	t.Helper()
	tr, err := Load(fixturePath("-work-demo", id))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// copyConfig copies the fixture config dir into a temp dir and gives session files
// distinct mtimes: sidPlain newest, then in id order.
func copyConfig(t *testing.T) Layout {
	t.Helper()
	dst := t.TempDir()
	src := fixtureConfig()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{sidPlain, sidTools, sidCompact, sidBranch, sidMessy, sidEmpty} {
		p := filepath.Join(dst, "projects", "-work-demo", id+".jsonl")
		mt := base.Add(-time.Duration(i) * time.Hour)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(dst, "projects", "-work-other", sidOther+".jsonl")
	mt := base.Add(time.Hour)
	if err := os.Chtimes(other, mt, mt); err != nil {
		t.Fatal(err)
	}
	return Layout{ConfigDir: dst}
}

func entryUUIDs(es []*Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.UUID
	}
	return out
}

func u(prefix string, n int) string {
	return prefix + pad6(n) + "-0000-4000-8000-000000000000"
}

func pad6(n int) string {
	s := "000000" + itoa(n)
	return s[len(s)-6:]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
