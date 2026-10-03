package launcher

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"
)

// Main is the mantle command. args excludes argv[0]. It returns the exit code.
func Main(args []string) int {
	d := ClassifyArgs(args)
	if d.Kind == KindPassthrough {
		err := ExecClaude(d.Args)
		fmt.Fprintf(os.Stderr, "mantle: %v\n", err)
		return 127
	}
	l, err := DefaultLayout()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mantle: %v\n", err)
		return 1
	}
	c := &Commands{Layout: l, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
	switch d.Kind {
	case KindVersions:
		return c.Versions(d.Args)
	case KindRollback:
		return c.Rollback(d.Args)
	case KindDoctor:
		return c.Doctor(d.Args)
	}
	sup := NewSupervisor(l)
	return sup.Run(LaunchOptions{Args: d.Args, Safe: d.Safe, UIBin: os.Getenv(EnvUIBin)})
}

// Commands implements the launcher's own commands.
type Commands struct {
	Layout Layout
	Stdin  *os.File
	Stdout io.Writer
	Stderr io.Writer
	// ExecClaude replaces the process with claude (doctor's hand-off).
	// Defaults to ExecClaude.
	ExecClaude func(args []string) error
	// LookPath and RunVersion are seams for doctor's tests.
	LookPath   func(string) (string, error)
	RunVersion func(bin string, args ...string) (string, error)
}

func (c *Commands) errf(format string, args ...any) int {
	fmt.Fprintf(c.Stderr, "mantle: "+format+"\n", args...)
	return 1
}

// Versions lists installed builds: `mantle versions [--json]`.
func (c *Commands) Versions(args []string) int {
	asJSON := false
	for _, a := range args {
		switch a {
		case "--json":
			asJSON = true
		case "-h", "--help":
			fmt.Fprintln(c.Stdout, "usage: mantle versions [--json]")
			return 0
		default:
			return c.errf("versions: unknown argument %q", a)
		}
	}
	store := Store{L: c.Layout}
	vs, err := store.List()
	if err != nil {
		return c.errf("%v", err)
	}
	cur, lg := store.CurrentID(), store.LastGoodID()
	if asJSON {
		type row struct {
			Manifest
			ID       string `json:"id"`
			Current  bool   `json:"current"`
			LastGood bool   `json:"last_good"`
			Healthy  bool   `json:"healthy"`
		}
		rows := []row{}
		for _, v := range vs {
			rows = append(rows, row{Manifest: v.Manifest, ID: v.ID, Current: v.ID == cur, LastGood: v.ID == lg, Healthy: IsHealthy(c.Layout, v.ID)})
		}
		enc := json.NewEncoder(c.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			return c.errf("%v", err)
		}
		return 0
	}
	if len(vs) == 0 {
		fmt.Fprintf(c.Stdout, "No builds installed in %s. Run `make install` in a mantle checkout.\n", c.Layout.Versions())
		return 0
	}
	tw := tabwriter.NewWriter(c.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  BUILD\tCREATED\tMODS\tSTATUS")
	for _, v := range vs {
		mark := " "
		if v.ID == cur {
			mark = "*"
		}
		var status []string
		if v.ID == cur {
			status = append(status, "current")
		}
		if v.ID == lg {
			status = append(status, "last-good")
		}
		switch {
		case IsHealthy(c.Layout, v.ID):
			status = append(status, "healthy")
		case v.ID == cur:
			st := LoadProbation(c.Layout, v.ID)
			status = append(status, fmt.Sprintf("probation (%d/%d failed)", st.Failures, MaxProbationFailures))
		default:
			status = append(status, "untested")
		}
		if v.ManifestErr != nil {
			status = append(status, "no manifest")
		}
		mods := "-"
		if n := len(v.Manifest.Mods); n > 0 {
			mods = strings.Join(v.Manifest.Mods, ",")
			if len(mods) > 40 {
				mods = fmt.Sprintf("%d mods", n)
			}
		}
		fmt.Fprintf(tw, "%s %s\t%s\t%s\t%s\n", mark, v.ID, v.Created().Local().Format("2006-01-02 15:04"), mods, strings.Join(status, ", "))
	}
	tw.Flush()
	return 0
}

// Rollback flips current: `mantle rollback [<build-id>]`. Without an id it
// goes to last-good, or to the newest build older than current if current
// is last-good. It rebuilds nothing.
func (c *Commands) Rollback(args []string) int {
	var id string
	switch {
	case len(args) == 1 && (args[0] == "-h" || args[0] == "--help"):
		fmt.Fprintln(c.Stdout, "usage: mantle rollback [<build-id>]")
		return 0
	case len(args) == 1:
		id = args[0]
	case len(args) > 1:
		return c.errf("usage: mantle rollback [<build-id>]")
	}
	store := Store{L: c.Layout}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	lock, err := LockFile(ctx, c.Layout.PromoteLock())
	if err != nil {
		return c.errf("cannot take %s (a promotion may be running): %v", c.Layout.PromoteLock(), err)
	}
	defer lock.Unlock()

	cur := store.CurrentID()
	if id == "" {
		id, err = rollbackTarget(store, cur)
		if err != nil {
			return c.errf("%v", err)
		}
	}
	v, err := store.Get(id)
	if err != nil {
		return c.errf("%v (see `mantle versions`)", err)
	}
	if !isExecutable(v.Binary()) {
		return c.errf("build %s has no runnable mantle-ui", id)
	}
	if id == cur {
		fmt.Fprintf(c.Stdout, "%s is already current.\n", id)
		return 0
	}
	ResetProbation(c.Layout, id)
	if err := store.SetCurrent(id); err != nil {
		return c.errf("%v", err)
	}
	if cur != "" {
		fmt.Fprintf(c.Stdout, "current: %s -> %s (active on the next launch)\n", cur, id)
	} else {
		fmt.Fprintf(c.Stdout, "current: %s (active on the next launch)\n", id)
	}
	return 0
}

func rollbackTarget(store Store, cur string) (string, error) {
	if lg := store.LastGoodID(); lg != "" && lg != cur {
		return lg, nil
	}
	vs, err := store.List()
	if err != nil {
		return "", err
	}
	seen := cur == ""
	for _, v := range vs {
		if seen && v.ID != cur && isExecutable(v.Binary()) {
			return v.ID, nil
		}
		if v.ID == cur {
			seen = true
		}
	}
	return "", errors.New("no older build to roll back to (see `mantle versions`)")
}

// Doctor checks the mantle installation, then offers `claude doctor`.
func (c *Commands) Doctor(args []string) int {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Fprintln(c.Stdout, "usage: mantle doctor")
			return 0
		}
	}
	lookPath := c.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	runVersion := c.RunVersion
	if runVersion == nil {
		runVersion = commandOutput
	}
	l := c.Layout
	store := Store{L: l}
	failed := false
	ok := func(format string, a ...any) { fmt.Fprintf(c.Stdout, "  ok    "+format+"\n", a...) }
	warn := func(format string, a ...any) { fmt.Fprintf(c.Stdout, "  warn  "+format+"\n", a...) }
	fail := func(format string, a ...any) {
		failed = true
		fmt.Fprintf(c.Stdout, "  FAIL  "+format+"\n", a...)
	}

	fmt.Fprintf(c.Stdout, "mantle doctor (%s)\n", l.Root)
	if exe, err := os.Executable(); err == nil {
		ok("launcher %s", exe)
	}
	if !isDir(l.Root) {
		fail("%s does not exist; run `make install` in a mantle checkout", l.Root)
	}

	cur, curErr := store.Current()
	switch {
	case curErr != nil:
		fail("current build: %v", curErr)
	case !isExecutable(cur.Binary()):
		fail("current build %s has no runnable mantle-ui", cur.ID)
	case IsHealthy(l, cur.ID):
		ok("current build %s (healthy)", cur.ID)
	default:
		st := LoadProbation(l, cur.ID)
		warn("current build %s is on probation (%d/%d failed launches)", cur.ID, st.Failures, MaxProbationFailures)
	}
	if lg, err := store.LastGood(); err != nil {
		warn("last-good: %v (rollback has no target)", err)
	} else if !isExecutable(lg.Binary()) {
		fail("last-good build %s has no runnable mantle-ui", lg.ID)
	} else {
		ok("last-good build %s", lg.ID)
	}
	if vs, err := store.List(); err == nil {
		ok("%d build(s) installed", len(vs))
	}

	if p, err := lookPath("go"); err != nil {
		warn("go not found in PATH: /mantle needs a Go toolchain (https://go.dev/dl/)")
	} else if out, err := runVersion(p, "version"); err != nil {
		warn("go version failed: %v", err)
	} else {
		ok("%s", out)
	}
	if p, err := lookPath("git"); err != nil {
		warn("git not found in PATH: /mantle needs git")
	} else if out, err := runVersion(p, "--version"); err == nil {
		ok("%s", out)
	}
	claude := os.Getenv(EnvClaudeBin)
	if claude == "" {
		claude, _ = lookPath("claude")
	}
	if claude == "" {
		fail("claude not found in PATH (set %s)", EnvClaudeBin)
	} else if out, err := runVersion(claude, "--version"); err != nil {
		fail("claude --version failed: %v", err)
	} else {
		ok("claude %s", out)
	}

	if !isDir(filepath.Join(l.Src(), ".git")) && !isFile(filepath.Join(l.Src(), ".git")) {
		warn("%s is not a git clone; /mantle cannot build mods (run `make install`)", l.Src())
	} else {
		ok("source clone %s", l.Src())
	}

	if n := l.RemoveStaleRunFiles(nil); n > 0 {
		ok("removed %d stale run file(s)", n)
	}
	if runs, _ := l.RunFiles(); len(runs) > 0 {
		uis, engines := 0, 0
		for _, rf := range runs {
			if rf.IsEngine() {
				engines++
			} else {
				uis++
			}
		}
		ok("%d mantle instance(s) and %d engine(s) running", uis, engines)
	}
	if data, err := os.ReadFile(l.DisabledFeatures()); err == nil {
		var disabled map[string]any
		if json.Unmarshal(data, &disabled) == nil && len(disabled) > 0 {
			warn("%d feature(s) disabled after panics (%s)", len(disabled), l.DisabledFeatures())
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		link := filepath.Join(home, ".local", "bin", "mantle")
		if target, err := os.Readlink(link); err != nil {
			warn("%s is not a symlink to the launcher", link)
		} else if filepath.Clean(target) != filepath.Clean(l.Launcher()) {
			warn("%s points to %s, not %s", link, target, l.Launcher())
		}
		if !pathContains(filepath.Join(home, ".local", "bin")) {
			warn("~/.local/bin is not in PATH")
		}
	}

	if failed {
		fmt.Fprintln(c.Stdout, "Some checks failed.")
	}
	if claude == "" {
		return 1
	}
	if c.Stdin == nil || !IsTerminal(c.Stdin) {
		fmt.Fprintln(c.Stdout, "Run `claude doctor` for the engine's own checks.")
		return boolCode(failed)
	}
	fmt.Fprint(c.Stdout, "Run `claude doctor` for the engine's own checks? [y/N] ")
	ans, _ := bufio.NewReader(c.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(ans)); a != "y" && a != "yes" {
		return boolCode(failed)
	}
	execClaude := c.ExecClaude
	if execClaude == nil {
		execClaude = ExecClaude
	}
	if err := execClaude([]string{"doctor"}); err != nil {
		return c.errf("%v", err)
	}
	return 0
}

func boolCode(failed bool) int {
	if failed {
		return 1
	}
	return 0
}

// commandOutput runs bin with args and a timeout and returns its first
// output line.
func commandOutput(bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return line, nil
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

func pathContains(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}
