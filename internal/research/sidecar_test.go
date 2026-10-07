package research

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/config"
)

func TestSidecar(t *testing.T) {
	p := config.Paths{MantleDir: t.TempDir()}
	const sid = "a96a1ace-c010-4efb-84c2-6f5bb00bf5c2"
	if _, ok := LoadSidecar(p, sid); ok {
		t.Fatal("missing file loaded")
	}
	want := Sidecar{Active: true, Viewing: "u1", LastChild: map[string]string{"u0": "u1"}}
	if err := SaveSidecar(p, sid, want); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.MantleDir, "research", sid+".json")
	if SidecarPath(p, sid) != path {
		t.Fatalf("path %s", SidecarPath(p, sid))
	}
	b, _ := os.ReadFile(path)
	if string(b) != `{"v":1,"active":true,"viewing":"u1","lastChild":{"u0":"u1"}}`+"\n" {
		t.Fatalf("file %s", b)
	}
	got, ok := LoadSidecar(p, sid)
	want.V = SidecarVersion
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("load %+v %v", got, ok)
	}
	if left, _ := filepath.Glob(filepath.Join(p.MantleDir, "research", ".*")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}

	// Fork.
	if err := CopySidecar(p, sid, "fork"); err != nil {
		t.Fatal(err)
	}
	if got, ok := LoadSidecar(p, "fork"); !ok || got.Viewing != "u1" {
		t.Fatalf("fork %+v", got)
	}
	if err := CopySidecar(p, "none", "other"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(SidecarPath(p, "other")); !os.IsNotExist(err) {
		t.Fatal("copied a missing sidecar")
	}
}

func TestSidecarIgnoresBadFiles(t *testing.T) {
	p := config.Paths{MantleDir: t.TempDir()}
	dir := filepath.Join(p.MantleDir, "research")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"corrupt":   `{"v":1,"active":tru`,
		"future":    `{"v":2,"active":true}`,
		"noversion": `{"active":true}`,
		"array":     `[]`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if s, ok := LoadSidecar(p, name); ok || s.Active {
			t.Errorf("%s loaded: %+v", name, s)
		}
	}
}

func TestSidecarBadIDs(t *testing.T) {
	p := config.Paths{MantleDir: t.TempDir()}
	for _, sid := range []string{"", ".", "..", "../x", `a\b`, "a/b", "a\x00b"} {
		if SidecarPath(p, sid) != "" {
			t.Errorf("%q has a path", sid)
		}
		if err := SaveSidecar(p, sid, Sidecar{}); err != ErrBadSessionID {
			t.Errorf("%q save: %v", sid, err)
		}
		if _, ok := LoadSidecar(p, sid); ok {
			t.Errorf("%q loaded", sid)
		}
	}
	if err := CopySidecar(p, "x", "../y"); err != ErrBadSessionID {
		t.Errorf("copy to bad id: %v", err)
	}
	if SidecarPath(config.Paths{}, "x") != "" {
		t.Error("no MantleDir")
	}
}
