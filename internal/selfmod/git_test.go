package selfmod

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// newRepo creates a git repository on branch main with one commit.
func newRepo(t *testing.T) Git {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	g := Git{Dir: dir, Env: []string{
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_DATE=2026-10-03T12:00:00Z", "GIT_COMMITTER_DATE=2026-10-03T12:00:00Z",
	}}
	mustGit(t, g, "init", "-q", "-b", "main")
	mustGit(t, g, "config", "user.email", "test@example.com")
	mustGit(t, g, "config", "user.name", "Test")
	writeFiles(t, dir, map[string]string{
		"go.mod":                   "module example.com/m\n\ngo 1.27\n\ntoolchain go1.27.1\n",
		"cmd/mantle/main.go":       "package main\n",
		"cmd/mantle-ui/main.go":    "package main\n",
		"internal/app/app.go":      "package app\n",
		"mods/doc.go":              "package mods\n",
		"internal/selfmod/doc.go":  "package selfmod\n",
		"internal/launcher/doc.go": "package launcher\n",
	})
	mustGit(t, g, "add", "-A")
	mustGit(t, g, "commit", "-q", "-m", "initial")
	return g
}

func mustGit(t *testing.T, g Git, args ...string) string {
	t.Helper()
	out, err := g.run(args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParseTrailers(t *testing.T) {
	msg := "mantle: demo: add a thing\n\nSome body text.\nMore: not a trailer block because of body\n\nMantle-Mod: demo\nMantle-Request: add a\n  folded line\nMantle-Kind: mod\n"
	ts := ParseTrailers(msg)
	if ts.Get("mantle-mod") != "demo" || ts.Get(TrailerRequest) != "add a folded line" || ts.Get(TrailerKind) != "mod" {
		t.Errorf("trailers = %+v", ts)
	}
	for _, bad := range []string{"subject only", "subject\n\nplain body paragraph", "s\n\nKey: v\nnot a trailer line"} {
		if ts := ParseTrailers(bad); ts != nil {
			t.Errorf("ParseTrailers(%q) = %+v, want nil", bad, ts)
		}
	}
	dup := Trailers{{"Mantle-Mod", "a"}, {"Mantle-Mod", "b"}}
	if dup.Get(TrailerMod) != "b" {
		t.Error("Get should return the last value")
	}
}

func TestCommitMessageRoundTrip(t *testing.T) {
	m := ModCommit{ID: "demo", Request: "make the spinner\nblue  please"}
	msg := CommitMessage(m, KindMod)
	if !strings.HasPrefix(msg, "mantle: demo: make the spinner blue please\n\n") {
		t.Errorf("subject: %q", msg)
	}
	ts := ParseTrailers(msg)
	if ts.Get(TrailerMod) != "demo" || ts.Get(TrailerRequest) != "make the spinner blue please" || ts.Get(TrailerKind) != KindMod {
		t.Errorf("trailers = %+v", ts)
	}
	long := CommitMessage(ModCommit{ID: "x", Request: strings.Repeat("word ", 300)}, KindCoreSeam)
	subject, _, _ := strings.Cut(long, "\n")
	if len([]rune(subject)) > 100 || !strings.HasPrefix(subject, "mantle: core seam for x: ") {
		t.Errorf("subject = %q", subject)
	}
}

func TestSplitModFiles(t *testing.T) {
	core, mods := SplitModFiles([]string{"mods/a/a.go", "internal/app/x.go", "modsx/y.go", "mods/doc.go", "go.mod"})
	if !slices.Equal(core, []string{"internal/app/x.go", "modsx/y.go", "go.mod"}) || !slices.Equal(mods, []string{"mods/a/a.go", "mods/doc.go"}) {
		t.Errorf("core=%v mods=%v", core, mods)
	}
}

func TestCommitModOnlyMods(t *testing.T) {
	g := newRepo(t)
	writeFiles(t, g.Dir, map[string]string{"mods/demo/demo.go": "package demo\n", "mods/demo/demo_test.go": "package demo\n"})
	shas, err := g.CommitMod(ModCommit{ID: "demo", Request: "add demo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(shas) != 1 {
		t.Fatalf("shas = %v, want one commit", shas)
	}
	mods, err := g.ListMods("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 1 || mods[0].ID != "demo" || mods[0].Kind() != KindMod || mods[0].Request != "add demo" || mods[0].State != ModActive {
		t.Errorf("mods = %+v", mods)
	}
	if clean, _ := g.IsClean(); !clean {
		t.Error("worktree not clean after CommitMod")
	}
	if _, err := g.CommitMod(ModCommit{ID: "demo", Request: "again"}); !errors.Is(err, ErrNothingToCommit) {
		t.Errorf("empty commit err = %v", err)
	}
	if _, err := g.CommitMod(ModCommit{ID: "Bad ID"}); err == nil {
		t.Error("invalid id accepted")
	}
}

func TestCommitModSplitsCoreSeam(t *testing.T) {
	g := newRepo(t)
	base, _ := g.HeadSHA()
	writeFiles(t, g.Dir, map[string]string{
		"mods/spin/spin.go":   "package spin\n",
		"internal/app/app.go": "package app\n\n// seam\n",
		"internal/app/new.go": "package app\n",
		"weird name [x].go":   "package m\n",
	})
	os.Remove(filepath.Join(g.Dir, "mods/doc.go"))
	shas, err := g.CommitMod(ModCommit{ID: "spin", Request: "custom spinner"})
	if err != nil {
		t.Fatal(err)
	}
	if len(shas) != 2 {
		t.Fatalf("shas = %v, want core-seam then mod", shas)
	}
	commits, _ := g.Log(base+"..HEAD", "")
	if len(commits) != 2 {
		t.Fatalf("%d commits", len(commits))
	}
	if commits[0].Trailers.Get(TrailerKind) != KindCoreSeam || commits[1].Trailers.Get(TrailerKind) != KindMod {
		t.Errorf("kinds: %q then %q", commits[0].Trailers.Get(TrailerKind), commits[1].Trailers.Get(TrailerKind))
	}
	files := func(sha string) []string {
		out := mustGit(t, g, "show", "--name-only", "--format=", sha)
		f := strings.Split(out, "\n")
		slices.Sort(f)
		return f
	}
	if got := files(shas[0]); !slices.Equal(got, []string{"internal/app/app.go", "internal/app/new.go", "weird name [x].go"}) {
		t.Errorf("core-seam files = %q", got)
	}
	if got := files(shas[1]); !slices.Equal(got, []string{"mods/doc.go", "mods/spin/spin.go"}) {
		t.Errorf("mod files = %q", got)
	}
	mods, _ := g.ListMods("HEAD")
	if len(mods) != 1 || mods[0].Kind() != "core-seam+mod" || len(mods[0].Commits) != 2 {
		t.Errorf("mods = %+v", mods)
	}
}

func TestChangedFiles(t *testing.T) {
	g := newRepo(t)
	base, _ := g.HeadSHA()
	writeFiles(t, g.Dir, map[string]string{"internal/app/app.go": "package app // changed\n"})
	mustGit(t, g, "commit", "-qam", "committed change")
	writeFiles(t, g.Dir, map[string]string{"mods/x/x.go": "package x\n", "cmd/mantle-ui/main.go": "package main // unstaged\n"})
	mustGit(t, g, "mv", "internal/launcher/doc.go", "internal/launcher/renamed.go")
	files, err := g.ChangedFiles(base)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cmd/mantle-ui/main.go", "internal/app/app.go", "internal/launcher/doc.go", "internal/launcher/renamed.go", "mods/x/x.go"}
	if !slices.Equal(files, want) {
		t.Errorf("files = %q\nwant %q", files, want)
	}
}

func TestWorktreeLifecycle(t *testing.T) {
	g := newRepo(t)
	wt := filepath.Join(t.TempDir(), "work", "demo")
	if err := g.WorktreeAdd(wt, "mod/demo", "main"); err != nil {
		t.Fatal(err)
	}
	w := Git{Dir: wt, Env: g.Env}
	if b, _ := w.CurrentBranch(); b != "mod/demo" {
		t.Errorf("branch = %q", b)
	}
	writeFiles(t, wt, map[string]string{"mods/demo/demo.go": "package demo\n"})
	if _, err := w.CommitMod(ModCommit{ID: "demo", Request: "demo"}); err != nil {
		t.Fatal(err)
	}
	// Fast-forward main (checked out in the main worktree) to the mod branch.
	if err := g.MergeFastForward("mod/demo"); err != nil {
		t.Fatal(err)
	}
	if err := g.WorktreeRemove(wt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Error("worktree folder still exists")
	}
	if err := g.DeleteBranch("mod/demo"); err != nil {
		t.Fatal(err)
	}
	if g.BranchExists("mod/demo") || !g.BranchExists("main") {
		t.Error("branch state wrong")
	}
	if mods, _ := g.ListMods("main"); len(mods) != 1 {
		t.Errorf("main mods = %+v", mods)
	}
}

func TestRebaseAndConflicts(t *testing.T) {
	g := newRepo(t)
	mustGit(t, g, "branch", "user")
	wt := filepath.Join(t.TempDir(), "w")
	g.WorktreeAdd(wt, "mod/a", "user")
	w := Git{Dir: wt, Env: g.Env}
	writeFiles(t, wt, map[string]string{"internal/app/app.go": "package app\n\n// from mod\n"})
	if _, err := w.CommitMod(ModCommit{ID: "a", Request: "a"}); err != nil {
		t.Fatal(err)
	}
	// Non-conflicting upstream change: rebase succeeds.
	mustGit(t, g, "checkout", "-q", "user")
	writeFiles(t, g.Dir, map[string]string{"README": "hi\n"})
	mustGit(t, g, "add", "README")
	mustGit(t, g, "commit", "-qm", "readme")
	if err := w.Rebase("user"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := w.IsAncestor("user", "HEAD"); !ok {
		t.Error("user is not an ancestor after rebase")
	}
	// Conflicting upstream change.
	writeFiles(t, g.Dir, map[string]string{"internal/app/app.go": "package app\n\n// from upstream\n"})
	mustGit(t, g, "commit", "-qam", "conflict")
	err := w.Rebase("user")
	var ce *ConflictError
	if !errors.As(err, &ce) || ce.Op != "rebase" || !slices.Equal(ce.Files, []string{"internal/app/app.go"}) {
		t.Fatalf("err = %v", err)
	}
	// Resolve and continue.
	writeFiles(t, wt, map[string]string{"internal/app/app.go": "package app\n\n// both\n"})
	mustGit(t, w, "add", "internal/app/app.go")
	if err := w.RebaseContinue(); err != nil {
		t.Fatal(err)
	}
	commits, _ := w.Log("user..HEAD", "")
	if len(commits) != 1 || commits[0].Trailers.Get(TrailerMod) != "a" {
		t.Errorf("commits after continue = %+v", commits)
	}
	// Conflict again, then abort.
	writeFiles(t, g.Dir, map[string]string{"internal/app/app.go": "package app\n\n// upstream 2\n"})
	mustGit(t, g, "commit", "-qam", "conflict 2")
	if err := w.Rebase("user"); !errors.As(err, &ce) {
		t.Fatalf("err = %v", err)
	}
	if err := w.RebaseAbort(); err != nil {
		t.Fatal(err)
	}
	if clean, _ := w.IsClean(); !clean {
		t.Error("not clean after abort")
	}
}

func TestRevertMod(t *testing.T) {
	g := newRepo(t)
	writeFiles(t, g.Dir, map[string]string{"mods/a/a.go": "package a\n", "internal/app/seam.go": "package app\n"})
	if _, err := g.CommitMod(ModCommit{ID: "a", Request: "mod a"}); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, g.Dir, map[string]string{"mods/b/b.go": "package b\n"})
	if _, err := g.CommitMod(ModCommit{ID: "b", Request: "mod b"}); err != nil {
		t.Fatal(err)
	}
	sha, err := g.RevertMod("a")
	if err != nil {
		t.Fatal(err)
	}
	if sha == "" {
		t.Fatal("no revert sha")
	}
	for _, f := range []string{"mods/a/a.go", "internal/app/seam.go"} {
		if _, err := os.Stat(filepath.Join(g.Dir, f)); !os.IsNotExist(err) {
			t.Errorf("%s still exists after undo", f)
		}
	}
	if _, err := os.Stat(filepath.Join(g.Dir, "mods/b/b.go")); err != nil {
		t.Error("undo of a removed b")
	}
	mods, _ := g.ListMods("HEAD")
	if len(mods) != 2 || mods[0].ID != "a" || mods[0].State != ModUndone || mods[1].State != ModActive {
		t.Errorf("mods = %+v", mods)
	}
	if mods[0].Request != "mod a" || mods[0].Kind() != "core-seam+mod" {
		t.Errorf("undone mod lost its request/kinds: %+v", mods[0])
	}
	if _, err := g.RevertMod("a"); err == nil {
		t.Error("double undo accepted")
	}
	if _, err := g.RevertMod("zzz"); err == nil {
		t.Error("unknown mod accepted")
	}
}

func TestCherryPick(t *testing.T) {
	g := newRepo(t)
	mustGit(t, g, "branch", "other")
	writeFiles(t, g.Dir, map[string]string{"mods/a/a.go": "package a\n"})
	shas, _ := g.CommitMod(ModCommit{ID: "a", Request: "a"})
	mustGit(t, g, "checkout", "-q", "other")
	if err := g.CherryPick(shas...); err != nil {
		t.Fatal(err)
	}
	if mods, _ := g.ListMods("other"); len(mods) != 1 || mods[0].ID != "a" {
		t.Errorf("mods on other = %+v", mods)
	}
}

func TestIdentityFallback(t *testing.T) {
	g := newRepo(t)
	mustGit(t, g, "config", "--unset", "user.email")
	mustGit(t, g, "config", "--unset", "user.name")
	writeFiles(t, g.Dir, map[string]string{"mods/a/a.go": "package a\n"})
	if _, err := g.CommitMod(ModCommit{ID: "a", Request: "a"}); err != nil {
		t.Fatalf("commit without identity: %v", err)
	}
	if email := mustGit(t, g, "log", "-1", "--format=%ae"); email != "mantle@localhost" {
		t.Errorf("author email = %q", email)
	}
}
