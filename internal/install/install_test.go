package install

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/launcher"
)

// newDevRepo creates a tiny mantle-shaped module (cmd/mantle and
// cmd/mantle-ui) with no dependencies, so real go builds work offline.
func newDevRepo(t *testing.T) Git {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := filepath.Join(t.TempDir(), "dev")
	os.MkdirAll(dir, 0o755)
	g := Git{Dir: dir, Env: []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null"}}
	mustGit(t, g, "init", "-q", "-b", "main")
	mustGit(t, g, "config", "user.email", "test@example.com")
	mustGit(t, g, "config", "user.name", "Test")
	writeFiles(t, dir, map[string]string{
		"go.mod":                "module example.com/mantle\n\ngo 1.27\n",
		"cmd/mantle/main.go":    "package main\n\nfunc main() { println(\"launcher\") }\n",
		"cmd/mantle-ui/main.go": "package main\n\nfunc main() { println(\"ui v1\") }\n",
	})
	mustGit(t, g, "add", "-A")
	mustGit(t, g, "commit", "-q", "-m", "initial")
	return g
}

func installOpts(t *testing.T, dev Git, home string, out *bytes.Buffer) InstallOptions {
	t.Helper()
	gobin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(gobin); err != nil {
		t.Skip("go tool not available")
	}
	t.Setenv(launcher.EnvClaudeBin, "/bin/echo") // claude --version without the real engine
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	n := 0
	return InstallOptions{
		Layout:  launcher.Layout{Root: home},
		DevRepo: dev.Dir,
		LinkDir: filepath.Join(filepath.Dir(home), "localbin"),
		Go:      gobin,
		Out:     out,
		Now: func() time.Time {
			n++
			return time.Date(2026, 10, 3, 12, 0, n, 0, time.UTC)
		},
	}
}

func TestInstallIdempotentAndUpdates(t *testing.T) {
	if testing.Short() {
		t.Skip("builds Go binaries")
	}
	dev := newDevRepo(t)
	home := filepath.Join(t.TempDir(), "home")
	var out bytes.Buffer
	o := installOpts(t, dev, home, &out)
	ctx := context.Background()
	l := o.Layout
	store := launcher.Store{L: l}

	res, err := Install(ctx, o)
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out.String())
	}
	if !res.Built || res.BuildID == "" {
		t.Fatalf("res = %+v", res)
	}
	if store.CurrentID() != res.BuildID || store.LastGoodID() != res.BuildID {
		t.Errorf("current=%s last-good=%s, want both %s", store.CurrentID(), store.LastGoodID(), res.BuildID)
	}
	if out, err := exec.Command(l.Launcher()).CombinedOutput(); err != nil || !strings.Contains(string(out), "launcher") {
		t.Errorf("launcher: %v %s", err, out)
	}
	if target, err := os.Readlink(res.Link); err != nil || target != l.Launcher() {
		t.Errorf("link %s -> %s (%v)", res.Link, target, err)
	}
	src := Git{Dir: l.Src()}
	if b := mustGit(t, src, "symbolic-ref", "--short", "HEAD"); b != UserBranch {
		t.Errorf("src branch = %q", b)
	}
	if branches := mustGit(t, src, "for-each-ref", "--format=%(refname:short)", "refs/heads/"); branches != UserBranch {
		t.Errorf("src branches = %q, want only user", branches)
	}
	if url := mustGit(t, src, "remote", "get-url", "origin"); url != dev.Dir {
		t.Errorf("origin = %q", url)
	}
	cur, _ := store.Current()
	devHead := mustGit(t, dev, "rev-parse", "HEAD")
	if cur.Manifest.GitSHA != devHead || cur.Manifest.UpstreamSHA != devHead || cur.Manifest.Source != "install" || !strings.HasPrefix(cur.Manifest.GoVersion, "go1.") {
		t.Errorf("manifest = %+v", cur.Manifest)
	}
	if out, err := exec.Command(cur.Binary()).CombinedOutput(); err != nil || !strings.Contains(string(out), "ui v1") {
		t.Errorf("ui: %v %s", err, out)
	}

	// Re-running without changes builds nothing.
	out.Reset()
	res2, err := Install(ctx, o)
	if err != nil {
		t.Fatalf("second install: %v\n%s", err, out.String())
	}
	if res2.Built || res2.BuildID != res.BuildID {
		t.Errorf("second install rebuilt: %+v", res2)
	}
	if vs, _ := store.List(); len(vs) != 1 {
		t.Errorf("%d versions after an idempotent re-run", len(vs))
	}

	// Upstream moves and user has no mods: user fast-forwards, a new build
	// becomes current, last-good stays until it passes probation.
	writeFiles(t, dev.Dir, map[string]string{"cmd/mantle-ui/main.go": "package main\n\nfunc main() { println(\"ui v2\") }\n"})
	mustGit(t, dev, "commit", "-qam", "v2")
	out.Reset()
	res3, err := Install(ctx, o)
	if err != nil {
		t.Fatalf("third install: %v\n%s", err, out.String())
	}
	if !res3.Built || res3.BuildID == res.BuildID || false {
		t.Fatalf("res3 = %+v\n%s", res3, out.String())
	}
	if store.CurrentID() != res3.BuildID || store.LastGoodID() != res.BuildID {
		t.Errorf("current=%s last-good=%s", store.CurrentID(), store.LastGoodID())
	}
	cur, _ = store.Current()
	if out, _ := exec.Command(cur.Binary()).CombinedOutput(); !strings.Contains(string(out), "ui v2") {
		t.Errorf("new build prints %s", out)
	}

}

func TestLinkLauncherKeepsRegularFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mantle"), []byte("mine"), 0o755)
	var out bytes.Buffer
	if _, err := linkLauncher(dir, "/x/mantle", &out); err == nil {
		t.Error("replaced a regular file")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "mantle")); string(data) != "mine" {
		t.Error("regular file changed")
	}
	os.Remove(filepath.Join(dir, "mantle"))
	os.Symlink("/old/mantle", filepath.Join(dir, "mantle"))
	if _, err := linkLauncher(dir, "/x/mantle", &out); err != nil {
		t.Fatal(err)
	}
	if target, _ := os.Readlink(filepath.Join(dir, "mantle")); target != "/x/mantle" {
		t.Errorf("target = %s", target)
	}
}

func TestInstallNeedsGo(t *testing.T) {
	_, err := Install(context.Background(), InstallOptions{Layout: launcher.Layout{Root: t.TempDir()}, DevRepo: t.TempDir(), Go: "/nonexistent/go"})
	if err == nil || !strings.Contains(err.Error(), "go.dev/dl") {
		t.Errorf("err = %v", err)
	}
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
