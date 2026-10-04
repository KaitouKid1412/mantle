package selfmod

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/launcher"
)

func TestUpdateReappliesModsWithConflictResolution(t *testing.T) {
	e := newE2E(t)
	if _, err := e.ws.StartUpdate(); !errors.Is(err, ErrUpToDate) {
		t.Fatalf("update without upstream changes: %v", err)
	}
	a, _ := e.ws.Start("alpha mod", RequestMod, "")
	e.build(a, demoMod(a.ID))
	e.promote(a)
	b, _ := e.ws.Start("beta mod", RequestMod, "")
	e.build(b, map[string]string{"mods/doc.go": "package mods\n\n// beta\n"})
	e.promote(b)

	// Upstream moves and touches the same line as beta.
	writeFiles(t, e.dev.Dir, map[string]string{"mods/doc.go": "package mods\n\n// upstream\n", "UPSTREAM": "new\n"})
	mustGit(t, e.dev, "add", "-A")
	mustGit(t, e.dev, "commit", "-qm", "upstream change")
	devHead, _ := e.dev.HeadSHA()

	r, err := e.ws.StartUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != RequestUpdate || len(r.Pending) != 2 || r.Base != devHead {
		t.Fatalf("update request = %+v", r)
	}
	stuck, err := e.ws.UpdateStep(r)
	var ce *ConflictError
	if !errors.As(err, &ce) || stuck == nil || stuck.Trailers.Get(TrailerMod) != b.ID || !slices.Equal(ce.Files, []string{"mods/doc.go"}) {
		t.Fatalf("step: stuck %+v err %v", stuck, err)
	}
	if len(r.Pending) != 1 {
		t.Errorf("pending after alpha = %d", len(r.Pending))
	}
	// A half-done resolution is refused.
	if _, err := e.ws.UpdateStep(r); err == nil || !strings.Contains(err.Error(), "conflict markers") {
		t.Fatalf("unresolved step err = %v", err)
	}
	// The builder resolves it.
	writeFiles(t, r.Dir, map[string]string{"mods/doc.go": "package mods\n\n// upstream\n// beta\n"})
	if stuck, err := e.ws.UpdateStep(r); err != nil || stuck != nil {
		t.Fatalf("continue: %+v %v", stuck, err)
	}
	if len(r.Pending) != 0 {
		t.Errorf("pending = %v", r.Pending)
	}
	p := e.promote(r)
	src := e.ws.src()
	if ok, _ := src.IsAncestor(devHead, UserBranch); !ok {
		t.Error("user is not on the new upstream")
	}
	mods, _ := e.ws.Mods()
	if len(mods) != 2 || mods[0].ID != a.ID || mods[1].ID != b.ID {
		t.Errorf("mods after update = %+v", mods)
	}
	if !slices.Equal(p.Version.Manifest.Mods, []string{a.ID, b.ID}) || p.Version.Manifest.UpstreamSHA != devHead {
		t.Errorf("manifest = %+v", p.Version.Manifest)
	}
	data, _ := os.ReadFile(filepath.Join(e.ws.Source, "mods/doc.go"))
	if !strings.Contains(string(data), "// upstream\n// beta") {
		t.Errorf("mods/doc.go = %q", data)
	}
	if _, err := e.ws.StartUpdate(); !errors.Is(err, ErrUpToDate) {
		t.Errorf("second update: %v", err)
	}
}

func TestUpdateRefusesWhenUserMoved(t *testing.T) {
	e := newE2E(t)
	a, _ := e.ws.Start("alpha mod", RequestMod, "")
	e.build(a, demoMod(a.ID))
	e.promote(a)
	writeFiles(t, e.dev.Dir, map[string]string{"UPSTREAM": "x\n"})
	mustGit(t, e.dev, "add", "-A")
	mustGit(t, e.dev, "commit", "-qm", "up")
	r, err := e.ws.StartUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ws.UpdateStep(r); err != nil {
		t.Fatal(err)
	}
	rep := e.vet(r)
	// Another mod is promoted while the update runs.
	c, _ := e.ws.Start("gamma mod", RequestMod, "")
	e.build(c, demoMod(c.ID))
	e.promote(c)
	if _, err := e.ws.Promote(e.ctx, r, rep); err == nil || !strings.Contains(err.Error(), "moved while the update ran") {
		t.Errorf("err = %v", err)
	}
}

func TestExportUpstream(t *testing.T) {
	e := newE2E(t)
	a, _ := e.ws.Start("alpha mod", RequestMod, "")
	e.build(a, demoMod(a.ID))
	e.promote(a)
	devMain, _ := e.dev.HeadSHA()
	branch, err := e.ws.ExportUpstream(e.ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "mantle/mod-"+a.ID {
		t.Errorf("branch = %s", branch)
	}
	if head, _ := e.dev.HeadSHA(); head != devMain {
		t.Error("export moved the dev repo's checked-out branch")
	}
	if clean, _ := e.dev.IsClean(); !clean {
		t.Error("export dirtied the dev repo's working tree")
	}
	mods, err := e.dev.ListMods("main.." + branch)
	if err != nil || len(mods) != 1 || mods[0].ID != a.ID {
		t.Errorf("exported mods = %+v, %v", mods, err)
	}
	if out := mustGit(t, e.dev, "show", "--name-only", "--format=", branch); !strings.Contains(out, "mods/"+a.ID+"/") {
		t.Errorf("exported files = %s", out)
	}
	// Re-exporting overwrites the branch.
	if _, err := e.ws.ExportUpstream(e.ctx, a.ID); err != nil {
		t.Errorf("re-export: %v", err)
	}
	if _, err := e.ws.ExportUpstream(e.ctx, "nope"); err == nil {
		t.Error("export of an unknown mod")
	}
	ents, _ := os.ReadDir(e.ws.Layout.Work())
	for _, en := range ents {
		if strings.HasPrefix(en.Name(), "upstream-") {
			t.Errorf("temporary worktree left: %s", en.Name())
		}
	}
}

func TestDevModeNeverMergesIntoMain(t *testing.T) {
	e := newE2E(t)
	dev := NewDevWorkspace(e.ws.Layout, e.dev.Dir, "main")
	dev.Go, dev.Pipeline, dev.Now = e.ws.Go, e.ws.Pipeline, e.ws.Now
	devMain, _ := e.dev.HeadSHA()
	r, err := dev.Start("dev feature", RequestMod, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Branch != "mantle/mod-dev-feature" {
		t.Errorf("branch = %s", r.Branch)
	}
	e.build(r, demoMod(r.ID))
	rep, err := dev.Vet(e.ctx, r)
	if err != nil || !rep.OK {
		t.Fatalf("vet: %v\n%s", err, TrimForBuilder(rep))
	}
	p, err := dev.Promote(e.ctx, r, rep)
	if err != nil {
		t.Fatal(err)
	}
	if head, _ := e.dev.HeadSHA(); head != devMain {
		t.Error("dev mode moved main")
	}
	if (launcher.Store{L: e.ws.Layout}).CurrentID() != p.Version.ID {
		t.Error("dev build not current")
	}
	if mods, _ := e.dev.ListMods("main..mantle/mod-dev-feature"); len(mods) != 1 {
		t.Errorf("branch mods = %+v", mods)
	}
	if _, err := dev.StartUpdate(); err == nil {
		t.Error("update allowed in dev mode")
	}
	if _, err := dev.ExportUpstream(e.ctx, r.ID); err == nil {
		t.Error("upstream allowed in dev mode")
	}
}
