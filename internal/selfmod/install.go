package selfmod

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/internal/launcher"
)

// UserBranch is the branch of ~/.mantle/src that holds upstream plus mods.
const UserBranch = "user"

// KeepVersions is how many versions GC keeps (plus current, last-good and
// versions in use).
const KeepVersions = 10

// InstallOptions configures Install.
type InstallOptions struct {
	Layout launcher.Layout
	// DevRepo is the mantle checkout the private clone tracks as origin.
	DevRepo string
	// Branch is the upstream branch of DevRepo ("main").
	Branch string
	// LinkDir gets a mantle symlink to the launcher ("" skips it).
	LinkDir string
	Go      string // "go"
	Git     string // "git"
	Out     io.Writer
	// Now is the clock for build ids (time.Now).
	Now func() time.Time
}

func (o *InstallOptions) defaults() {
	if o.Branch == "" {
		o.Branch = "main"
	}
	if o.Go == "" {
		o.Go = "go"
	}
	if o.Git == "" {
		o.Git = "git"
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Now == nil {
		o.Now = time.Now
	}
}

// InstallResult reports what Install did.
type InstallResult struct {
	Launcher string
	Link     string
	BuildID  string
	// Built is false when the current build already matches user's HEAD.
	Built bool
	// Behind is set when origin moved but user has mods, so it was not
	// fast-forwarded (run /mantle update).
	Behind bool
}

// Install builds and installs mantle into the layout: the launcher, the
// ~/.local/bin link, the private source clone with branch user, and a
// mantle-ui version built from user's HEAD. Re-running it is idempotent.
func Install(ctx context.Context, o InstallOptions) (*InstallResult, error) {
	o.defaults()
	l := o.Layout
	if o.DevRepo == "" {
		return nil, errors.New("install: no dev repo")
	}
	if _, err := exec.LookPath(o.Go); err != nil {
		return nil, fmt.Errorf("mantle needs a Go toolchain to build itself and to run /mantle; install one from https://go.dev/dl/ (%v)", err)
	}
	if err := l.EnsureDirs(); err != nil {
		return nil, err
	}
	lock, err := launcher.LockFile(ctx, l.PromoteLock())
	if err != nil {
		return nil, err
	}
	defer lock.Unlock()
	res := &InstallResult{Launcher: l.Launcher()}

	// 1. The launcher, from the dev repo.
	fmt.Fprintf(o.Out, "building the launcher into %s\n", l.Launcher())
	tmp := fmt.Sprintf("%s.tmp-%d", l.Launcher(), os.Getpid())
	if err := o.goBuild(ctx, o.DevRepo, tmp, "./cmd/mantle"); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, l.Launcher()); err != nil {
		os.Remove(tmp)
		return nil, err
	}

	// 2. The PATH link.
	if o.LinkDir != "" {
		link, err := linkLauncher(o.LinkDir, l.Launcher(), o.Out)
		if err != nil {
			fmt.Fprintf(o.Out, "warning: %v\n", err)
		}
		res.Link = link
	}

	// 3. The private clone.
	behind, err := o.syncSource(ctx)
	if err != nil {
		return nil, err
	}
	res.Behind = behind

	// 4. A version built from user's HEAD.
	src := Git{Dir: l.Src(), Bin: o.Git}
	sha, err := src.HeadSHA()
	if err != nil {
		return nil, err
	}
	store := launcher.Store{L: l}
	if cur, err := store.Current(); err == nil && cur.Manifest.GitSHA == sha {
		fmt.Fprintf(o.Out, "build %s already matches %s at %s\n", cur.ID, UserBranch, short(sha))
		res.BuildID = cur.ID
		return res, nil
	}
	firstInstall := store.LastGoodID() == ""
	v, err := BuildVersion(ctx, BuildOptions{Layout: l, Dir: l.Src(), Upstream: "origin/" + o.Branch, Source: "install", Go: o.Go, Git: o.Git, Now: o.Now})
	if err != nil {
		return nil, err
	}
	if err := store.SetCurrent(v.ID); err != nil {
		return nil, err
	}
	if firstInstall {
		// Nothing to roll back to: the first build is its own last-good.
		if err := store.SetLastGood(v.ID); err != nil {
			return nil, err
		}
	}
	res.BuildID, res.Built = v.ID, true
	fmt.Fprintf(o.Out, "installed build %s (current)\n", v.ID)
	if removed, err := store.GC(KeepVersions, nil); err == nil && len(removed) > 0 {
		fmt.Fprintf(o.Out, "removed %d old build(s)\n", len(removed))
	}
	return res, nil
}

func (o InstallOptions) goBuild(ctx context.Context, dir, out, pkg string) error {
	cmd := exec.CommandContext(ctx, o.Go, "build", "-trimpath", "-o", out, pkg)
	cmd.Dir = dir
	cmd.Env = mergeEnv(os.Environ(), []string{"GOTOOLCHAIN=local", "GOFLAGS="})
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build %s in %s: %v\n%s", pkg, dir, err, strings.TrimSpace(string(b)))
	}
	return nil
}

// syncSource creates or updates ~/.mantle/src. It reports whether origin has
// commits that user lacks and could not be fast-forwarded because of mods.
func (o InstallOptions) syncSource(ctx context.Context) (behind bool, err error) {
	l := o.Layout
	src := Git{Dir: l.Src(), Bin: o.Git}
	upstream := "origin/" + o.Branch
	if _, err := os.Stat(filepath.Join(l.Src(), ".git")); errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(o.Out, "cloning %s into %s\n", o.DevRepo, l.Src())
		parent := Git{Dir: filepath.Dir(l.Src()), Bin: o.Git}
		if _, err := parent.run("clone", "--quiet", "--no-checkout", "--origin", "origin", o.DevRepo, l.Src()); err != nil {
			return false, err
		}
		if _, err := src.run("checkout", "--quiet", "-b", UserBranch, upstream); err != nil {
			return false, err
		}
		// Drop the clone's default branch so only user remains.
		if out, err := src.run("for-each-ref", "--format=%(refname:short)", "refs/heads/"); err == nil {
			for _, b := range strings.Fields(out) {
				if b != UserBranch {
					src.DeleteBranch(b)
				}
			}
		}
		return false, nil
	}
	if url, _ := src.run("remote", "get-url", "origin"); url != o.DevRepo {
		if _, err := src.run("remote", "set-url", "origin", o.DevRepo); err != nil {
			return false, err
		}
	}
	fmt.Fprintf(o.Out, "fetching %s into %s\n", o.DevRepo, l.Src())
	if _, err := src.run("fetch", "--quiet", "origin"); err != nil {
		return false, err
	}
	if !src.BranchExists(UserBranch) {
		_, err := src.run("checkout", "--quiet", "-b", UserBranch, upstream)
		return false, err
	}
	if branch, _ := src.CurrentBranch(); branch != UserBranch {
		return false, fmt.Errorf("%s is on branch %q; check out %s first", l.Src(), branch, UserBranch)
	}
	if upToDate, err := src.IsAncestor(upstream, UserBranch); err != nil || upToDate {
		return false, err
	}
	ffable, err := src.IsAncestor(UserBranch, upstream)
	if err != nil {
		return false, err
	}
	if !ffable {
		fmt.Fprintf(o.Out, "%s has mods and %s moved; run /mantle update to rebase them\n", UserBranch, upstream)
		return true, nil
	}
	if clean, err := src.IsClean(); err != nil || !clean {
		return true, fmt.Errorf("%s has uncommitted changes; not updating it", l.Src())
	}
	fmt.Fprintf(o.Out, "fast-forwarding %s to %s\n", UserBranch, upstream)
	return false, src.MergeFastForward(upstream)
}

// linkLauncher points dir/mantle at the launcher. An existing symlink is
// replaced; an existing regular file is left alone.
func linkLauncher(dir, target string, out io.Writer) (string, error) {
	link := filepath.Join(dir, "mantle")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if fi, err := os.Lstat(link); err == nil {
		if fi.Mode()&fs.ModeSymlink == 0 {
			return "", fmt.Errorf("%s exists and is not a symlink; not replacing it", link)
		}
		if cur, _ := os.Readlink(link); cur == target {
			return link, nil
		}
	}
	tmp := fmt.Sprintf("%s.tmp-%d", link, os.Getpid())
	os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, link); err != nil {
		os.Remove(tmp)
		return "", err
	}
	fmt.Fprintf(out, "linked %s -> %s\n", link, target)
	inPath := false
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			inPath = true
		}
	}
	if !inPath {
		fmt.Fprintf(out, "note: %s is not in your PATH; add it to run mantle\n", dir)
	}
	return link, nil
}

// BuildOptions configures BuildVersion.
type BuildOptions struct {
	Layout launcher.Layout
	// Dir is the checkout to build (user's HEAD in ~/.mantle/src).
	Dir string
	// Upstream is the ref the mods sit on ("origin/main"), for the manifest.
	Upstream string
	// Source is recorded in the manifest ("install", "promote", ...).
	Source string
	// Binary, if set, is installed instead of building Dir again.
	Binary string
	// Pipeline is the report of the run that vetted this build.
	Pipeline *launcher.PipelineSummary
	Go, Git  string
	Now      func() time.Time
}

// BuildVersion builds mantle-ui from o.Dir (unless o.Binary is given) and
// installs it as a new immutable version with its manifest. It does not
// flip current.
func BuildVersion(ctx context.Context, o BuildOptions) (launcher.Version, error) {
	if o.Go == "" {
		o.Go = "go"
	}
	if o.Git == "" {
		o.Git = "git"
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	g := Git{Dir: o.Dir, Bin: o.Git}
	sha, err := g.HeadSHA()
	if err != nil {
		return launcher.Version{}, err
	}
	bin := o.Binary
	if bin == "" {
		tmpDir, err := os.MkdirTemp(o.Layout.Builds(), "install-")
		if err != nil {
			return launcher.Version{}, err
		}
		defer os.RemoveAll(tmpDir)
		bin = filepath.Join(tmpDir, launcher.UIBinaryName)
		inst := InstallOptions{Go: o.Go}
		if err := inst.goBuild(ctx, o.Dir, bin, "./cmd/mantle-ui"); err != nil {
			return launcher.Version{}, err
		}
	}
	m := launcher.Manifest{
		GitSHA:        sha,
		GoVersion:     commandLine(ctx, o.Dir, o.Go, "env", "GOVERSION"),
		ClaudeVersion: claudeVersion(ctx),
		Created:       o.Now().UTC(),
		Source:        o.Source,
		Pipeline:      o.Pipeline,
	}
	if o.Upstream != "" {
		if base, err := g.MergeBase(o.Upstream, "HEAD"); err == nil {
			m.UpstreamSHA = base
			if mods, err := g.ListMods(base + "..HEAD"); err == nil {
				for _, mod := range mods {
					if mod.State == ModActive {
						m.Mods = append(m.Mods, mod.ID)
					}
				}
			}
		}
	}
	store := launcher.Store{L: o.Layout}
	id := store.UniqueID(launcher.NewBuildID(o.Now(), sha))
	return store.Install(id, bin, m)
}

func claudeVersion(ctx context.Context) string {
	bin, err := launcher.ClaudeBinary()
	if err != nil {
		return ""
	}
	v := commandLine(ctx, "", bin, "--version")
	v, _, _ = strings.Cut(v, " ")
	return v
}

// commandLine runs a command with a short timeout and returns its first
// output line ("" on error).
func commandLine(ctx context.Context, dir, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return firstLine(string(out))
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
