package selfmod

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/launcher"
)

// tinyUI is a mantle-ui stand-in: "selftest" succeeds, anything else prints
// a first frame and exits 0 (the smoke boot's default script).
const tinyUI = `package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "selftest" {
		fmt.Println("selftest ok")
		return
	}
	fmt.Println("mantle ready")
}
`

type e2e struct {
	t     *testing.T
	dev   Git
	ws    *Workspace
	store launcher.Store
	ctx   context.Context
	out   bytes.Buffer
}

// newE2E installs a tiny mantle-shaped module into a temp home and returns
// a workspace on it with a real pipeline (real go, gofmt and pty smoke boot).
func newE2E(t *testing.T) *e2e {
	t.Helper()
	if testing.Short() {
		t.Skip("builds Go binaries")
	}
	e := &e2e{t: t, dev: newDevRepo(t), ctx: context.Background()}
	writeFiles(t, e.dev.Dir, map[string]string{"cmd/mantle-ui/main.go": tinyUI, "mods/doc.go": "package mods\n"})
	mustGit(t, e.dev, "add", "-A")
	mustGit(t, e.dev, "commit", "-qm", "tiny ui")
	home := filepath.Join(t.TempDir(), "home")
	o := installOpts(t, e.dev, home, &e.out)
	if _, err := Install(e.ctx, o); err != nil {
		t.Fatalf("install: %v\n%s", err, e.out.String())
	}
	gofmt := filepath.Join(runtime.GOROOT(), "bin", "gofmt")
	e.ws = NewWorkspace(o.Layout)
	e.ws.Go = o.Go
	e.ws.Now = o.Now
	e.ws.Pipeline = Config{
		Go:        o.Go,
		Gofmt:     gofmt,
		ArchCheck: func(context.Context, string) ([]string, error) { return nil, nil },
		Smoke:     SmokeConfig{Script: []SmokeAction{Expect(`mantle ready`)}},
	}
	e.store = launcher.Store{L: o.Layout}
	return e
}

// build plays a scripted builder: it writes files into the request worktree.
func (e *e2e) build(r *Request, files map[string]string) { writeFiles(e.t, r.Dir, files) }

func (e *e2e) vet(r *Request) *Report {
	e.t.Helper()
	rep, err := e.ws.Vet(e.ctx, r)
	if err != nil {
		e.t.Fatal(err)
	}
	return rep
}

func (e *e2e) promote(r *Request) *Promotion {
	e.t.Helper()
	rep := e.vet(r)
	if !rep.OK {
		e.t.Fatalf("pipeline failed for %s:\n%s", r.ID, TrimForBuilder(rep))
	}
	p, err := e.ws.Promote(e.ctx, r, rep)
	if err != nil {
		e.t.Fatalf("promote %s: %v", r.ID, err)
	}
	return p
}

func demoMod(pkg string) map[string]string {
	return map[string]string{
		"mods/" + pkg + "/" + pkg + ".go":      "package " + strings.ReplaceAll(pkg, "-", "") + "\n\n// Hello says hi.\nfunc Hello() string { return \"hi\" }\n",
		"mods/" + pkg + "/" + pkg + "_test.go": "package " + strings.ReplaceAll(pkg, "-", "") + "\n\nimport \"testing\"\n\nfunc TestHello(t *testing.T) {\n\tif Hello() != \"hi\" {\n\t\tt.Fatal(\"no\")\n\t}\n}\n",
	}
}

func TestEndToEndModLifecycle(t *testing.T) {
	e := newE2E(t)
	first := e.store.CurrentID()

	// A scripted builder creates mods/demo-command; the pipeline passes and
	// promotes it.
	r, err := e.ws.Start("add a demo command", RequestMod, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "demo-command" || r.Branch != "mod/demo-command" || r.State != StateBuilding {
		t.Fatalf("request = %+v", r)
	}
	e.build(r, demoMod(r.ID))
	p := e.promote(r)
	if p.Rebased || len(p.Commits) != 1 {
		t.Errorf("promotion = %+v", p)
	}
	if e.store.CurrentID() != p.Version.ID || p.Version.ID == first {
		t.Errorf("current = %s, promoted %s", e.store.CurrentID(), p.Version.ID)
	}
	if !slices.Equal(p.Version.Manifest.Mods, []string{"demo-command"}) || p.Version.Manifest.Source != RequestMod || p.Version.Manifest.Pipeline == nil || !p.Version.Manifest.Pipeline.OK {
		t.Errorf("manifest = %+v", p.Version.Manifest)
	}
	if _, err := os.Stat(r.Dir); !os.IsNotExist(err) {
		t.Error("worktree kept after promotion")
	}
	if e.ws.src().BranchExists(r.Branch) {
		t.Error("request branch kept after promotion")
	}
	loaded, _ := e.ws.Load(r.ID)
	if loaded.State != StatePromoted || loaded.BuildID != p.Version.ID || loaded.Round != 1 {
		t.Errorf("saved request = %+v", loaded)
	}
	var cache []ModsCacheEntry
	data, _ := os.ReadFile(e.ws.Layout.ModsCache())
	if json.Unmarshal(data, &cache) != nil || len(cache) != 1 || cache[0].ID != "demo-command" {
		t.Errorf("mods.json = %s", data)
	}

	// The launcher runs the new version and it passes probation.
	sup := &launcher.Supervisor{Layout: e.ws.Layout, Stderr: &e.out, Env: os.Environ(), Terminal: nopTerminal{}}
	if code := sup.Run(launcher.LaunchOptions{}); code != 0 {
		t.Fatalf("launcher exit %d\n%s", code, e.out.String())
	}
	if !launcher.IsHealthy(e.ws.Layout, p.Version.ID) || e.store.LastGoodID() != p.Version.ID {
		t.Errorf("new build did not pass probation (last-good %s)", e.store.LastGoodID())
	}

	// show and status.
	d, err := e.ws.Show("demo-command")
	if err != nil || d.Request == nil || d.Report == nil || !d.Report.OK || len(d.Stats) != 1 || !strings.Contains(d.Stats[0], "mods/demo-command/demo-command.go") {
		t.Errorf("show = %+v, %v", d, err)
	}
	st, err := e.ws.Status()
	if err != nil || st.Current != p.Version.ID || st.Probation != nil || len(st.Active) != 0 {
		t.Errorf("status = %+v, %v", st, err)
	}

	// An edit to internal/launcher is rejected by step 1.
	bad, err := e.ws.Start("make the launcher faster", RequestMod, "")
	if err != nil {
		t.Fatal(err)
	}
	e.build(bad, map[string]string{"internal/launcher/fast.go": "package launcher\n"})
	rep := e.vet(bad)
	if rep.OK || len(rep.Results) != 1 || rep.Results[0].Step != StepProtected {
		t.Errorf("protected edit report = %+v", rep.Results)
	}
	if loaded, _ := e.ws.Load(bad.ID); loaded.State != StateFailed || !strings.Contains(loaded.LastFailure, "internal/launcher/fast.go") {
		t.Errorf("failed request = %+v", loaded)
	}
	if st, _ := e.ws.Status(); len(st.Active) != 1 || st.Active[0].ID != bad.ID {
		t.Errorf("status active = %+v", st.Active)
	}
	if _, err := e.ws.Promote(e.ctx, bad, rep); err == nil {
		t.Error("promoted a failed request")
	}
	if err := e.ws.Abandon(bad); err != nil {
		t.Fatal(err)
	}

	// /mantle undo demo-command reverts and promotes.
	undo, err := e.ws.StartUndo("demo-command")
	if err != nil {
		t.Fatal(err)
	}
	if undo.ID != "undo-demo-command" {
		t.Errorf("undo id = %s", undo.ID)
	}
	pu := e.promote(undo)
	if len(pu.Version.Manifest.Mods) != 0 {
		t.Errorf("undone build still lists mods %v", pu.Version.Manifest.Mods)
	}
	mods, _ := e.ws.Mods()
	if len(mods) != 1 || mods[0].State != ModUndone {
		t.Errorf("mods after undo = %+v", mods)
	}
	if _, err := os.Stat(filepath.Join(e.ws.Source, "mods/demo-command")); !os.IsNotExist(err) {
		t.Error("undo left the mod's files in user")
	}
	if _, err := e.ws.StartUndo("demo-command"); err == nil {
		t.Error("second undo accepted")
	}

	// /mantle rollback flips current only.
	from, to, err := e.ws.Rollback(e.ctx)
	if err != nil || from != pu.Version.ID || to != p.Version.ID || e.store.CurrentID() != p.Version.ID {
		t.Errorf("rollback %s -> %s (%v), current %s", from, to, err, e.store.CurrentID())
	}
}

func TestEndToEndConcurrentPromotionRebases(t *testing.T) {
	e := newE2E(t)
	a, err := e.ws.Start("first feature alpha", RequestMod, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.ws.Start("second feature beta", RequestMod, "")
	if err != nil {
		t.Fatal(err)
	}
	e.build(a, demoMod(a.ID))
	e.build(b, demoMod(b.ID))
	repB := e.vet(b)
	if !repB.OK {
		t.Fatalf("b failed:\n%s", TrimForBuilder(repB))
	}
	e.promote(a)
	// user moved under b: promotion rebases and re-runs steps 6-9.
	pb, err := e.ws.Promote(e.ctx, b, repB)
	if err != nil {
		t.Fatal(err)
	}
	if !pb.Rebased {
		t.Error("b was not rebased")
	}
	if !slices.Equal(pb.Version.Manifest.Mods, []string{a.ID, b.ID}) {
		t.Errorf("mods = %v", pb.Version.Manifest.Mods)
	}
	if _, err := os.Stat(filepath.Join(e.ws.Layout.Build(b.ID), "promote", "report.json")); err != nil {
		t.Error("no post-rebase report")
	}
}

func TestEndToEndEditKeepsModID(t *testing.T) {
	e := newE2E(t)
	r, _ := e.ws.Start("greeting mod", RequestMod, "")
	e.build(r, demoMod(r.ID))
	e.promote(r)
	ed, err := e.ws.StartEdit(r.ID, "say hello instead")
	if err != nil {
		t.Fatal(err)
	}
	if ed.Original != "greeting mod" || ed.Kind != RequestEdit || ed.Target != r.ID {
		t.Errorf("edit request = %+v", ed)
	}
	if prompt := BuildPrompt(ed); !strings.Contains(prompt, "greeting mod") || !strings.Contains(prompt, "say hello instead") || !strings.Contains(prompt, "mods/"+r.ID+"/") {
		t.Errorf("edit prompt:\n%s", prompt)
	}
	// A later mod: squashing rewrites it too.
	later, _ := e.ws.Start("later mod", RequestMod, "")
	e.build(later, demoMod(later.ID))
	e.promote(later)
	ed, err = e.ws.Load(ed.ID) // base moved on: start the edit again
	if err == nil {
		e.ws.Abandon(ed)
	}
	ed, err = e.ws.StartEdit(r.ID, "say hello instead")
	if err != nil {
		t.Fatal(err)
	}
	pkg := strings.ReplaceAll(r.ID, "-", "")
	e.build(ed, map[string]string{"mods/" + r.ID + "/extra.go": "package " + pkg + "\n\n// Hello2 says hello.\nfunc Hello2() string { return \"hello\" }\n"})
	p := e.promote(ed)
	if !p.Squashed {
		t.Error("edit was not squashed")
	}
	mods, _ := e.ws.Mods()
	if len(mods) != 2 || mods[0].ID != r.ID || mods[1].ID != later.ID {
		t.Fatalf("mods after edit = %+v", mods)
	}
	if len(mods[0].Commits) != 1 || mods[0].Request != "greeting mod" || mods[0].Kind() != KindMod {
		t.Errorf("edited mod = %+v; want one commit with the original request", mods[0])
	}
	files := mustGit(t, e.ws.src(), "show", "--name-only", "--format=", mods[0].Commits[0].SHA)
	if !strings.Contains(files, "mods/"+r.ID+"/extra.go") {
		t.Errorf("edit not folded into the mod commit: %s", files)
	}
	if !slices.Equal(p.Version.Manifest.Mods, []string{r.ID, later.ID}) {
		t.Errorf("manifest mods = %v", p.Version.Manifest.Mods)
	}
	// undo still reverts the whole mod.
	u, _ := e.ws.StartUndo(r.ID)
	e.promote(u)
	if _, err := os.Stat(filepath.Join(e.ws.Source, "mods", r.ID)); !os.IsNotExist(err) {
		t.Error("undo after edit left files behind")
	}
	if _, err := e.ws.StartEdit("nope", "x"); err == nil {
		t.Error("edit of an unknown mod accepted")
	}
}

func TestEditWithMovedBranchKeepsSeparateCommits(t *testing.T) {
	e := newE2E(t)
	r, _ := e.ws.Start("greeting mod", RequestMod, "")
	e.build(r, demoMod(r.ID))
	e.promote(r)
	ed, _ := e.ws.StartEdit(r.ID, "say hello instead")
	pkg := strings.ReplaceAll(r.ID, "-", "")
	e.build(ed, map[string]string{"mods/" + r.ID + "/extra.go": "package " + pkg + "\n\n// Hello2 says hello.\nfunc Hello2() string { return \"hello\" }\n"})
	rep := e.vet(ed)
	other, _ := e.ws.Start("other mod", RequestMod, "")
	e.build(other, demoMod(other.ID))
	e.promote(other)
	p, err := e.ws.Promote(e.ctx, ed, rep)
	if err != nil {
		t.Fatal(err)
	}
	if p.Squashed || !p.Rebased {
		t.Errorf("promotion = %+v", p)
	}
	mods, _ := e.ws.Mods()
	if len(mods) != 2 || len(mods[0].Commits) != 2 || mods[0].Request != "greeting mod" {
		t.Errorf("mods = %+v", mods)
	}
}

func TestPromoteRefusesDirtySource(t *testing.T) {
	e := newE2E(t)
	r, _ := e.ws.Start("dirty test", RequestMod, "")
	e.build(r, demoMod(r.ID))
	rep := e.vet(r)
	writeFiles(t, e.ws.Source, map[string]string{"stray.txt": "x\n"})
	if _, err := e.ws.Promote(e.ctx, r, rep); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("err = %v", err)
	}
	os.Remove(filepath.Join(e.ws.Source, "stray.txt"))
	if _, err := e.ws.Promote(e.ctx, r, rep); err != nil {
		t.Errorf("promote after cleanup: %v", err)
	}
}

func TestPromoteConflictKeepsRebase(t *testing.T) {
	e := newE2E(t)
	a, _ := e.ws.Start("conflict one", RequestMod, "")
	b, _ := e.ws.Start("conflict two", RequestMod, "")
	e.build(a, map[string]string{"mods/doc.go": "package mods\n\n// one\n"})
	e.build(b, map[string]string{"mods/doc.go": "package mods\n\n// two\n"})
	repB := e.vet(b)
	e.promote(a)
	_, err := e.ws.Promote(e.ctx, b, repB)
	var ce *ConflictError
	if !errors.As(err, &ce) || !slices.Equal(ce.Files, []string{"mods/doc.go"}) {
		t.Fatalf("err = %v", err)
	}
}

func TestStartValidation(t *testing.T) {
	e := newE2E(t)
	if _, err := e.ws.Start("   ", RequestMod, ""); err == nil {
		t.Error("empty request accepted")
	}
	if _, err := e.ws.StartUndo("missing"); err == nil {
		t.Error("undo of a missing mod accepted")
	}
	if _, err := e.ws.Load("../x"); err == nil {
		t.Error("bad id loaded")
	}
	r1, _ := e.ws.Start("same words", RequestMod, "")
	r2, _ := e.ws.Start("same words", RequestMod, "")
	if r1.ID == r2.ID || r2.ID != "same-words-2" {
		t.Errorf("ids %s %s", r1.ID, r2.ID)
	}
	reqs, _ := e.ws.Requests()
	if len(reqs) != 2 {
		t.Errorf("requests = %d", len(reqs))
	}
}

type nopTerminal struct{}

func (nopTerminal) Save()    {}
func (nopTerminal) Restore() {}
