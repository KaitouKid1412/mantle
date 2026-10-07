// Package doctor runs mantle's health checks: the Go toolchain installs need,
// the claude binary and its probe status, terminal capabilities, every settings
// and keybindings file, the ~/.mantle version store and the cwd's trust state.
// Each check returns ok, warn or fail with a fix hint. /doctor (plan 09) shows
// them; `mantle doctor` (plan 10) may reuse them.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/internal/claudecli"
	"github.com/KaitouKid1412/mantle/internal/claudecli/discovery"
)

// Status is a check's outcome.
type Status int

const (
	OK Status = iota
	Warn
	Fail
	Skip // not applicable here
)

func (s Status) String() string {
	switch s {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	case Fail:
		return "fail"
	}
	return "skip"
}

// Check is one result. Items holds per-file or per-capability detail.
type Check struct {
	ID     string
	Title  string
	Status Status
	Detail string
	Fix    string
	Items  []Check
}

// Minimum versions.
const (
	MinClaude = "2.1.288"
	MinGo     = "1.27"
)

// TermCaps are terminal capabilities detected by querying the terminal (plan
// 07). A nil field means unknown; the check then falls back to environment
// heuristics.
type TermCaps struct {
	Truecolor     *bool
	KittyKeyboard *bool
	OSC52         *bool
	Focus         *bool
}

// Env is what the checks read. Tests replace any part of it.
type Env struct {
	Home       string
	ConfigDir  string // Claude Code config dir (~/.claude or $CLAUDE_CONFIG_DIR)
	MantleDir  string // ~/.mantle
	CWD        string
	ManagedDir string
	Getenv     func(string) string
	LookPath   func(string) (string, error)
	// GoVersion returns "go1.27.1" for the go binary at path.
	GoVersion func(ctx context.Context, goBin string) (string, error)
	Claude    *claudecli.Runner
	// DiskFree returns free bytes on the filesystem holding path.
	DiskFree func(path string) (uint64, error)
	Term     TermCaps
}

// DefaultEnv reads the real machine.
func DefaultEnv(cwd string) Env {
	home, _ := os.UserHomeDir()
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	if cfg == "" {
		cfg = filepath.Join(home, ".claude")
	}
	return Env{
		Home: home, ConfigDir: cfg, MantleDir: filepath.Join(home, ".mantle"), CWD: cwd,
		ManagedDir: discovery.DefaultManagedDir(),
		Getenv:     os.Getenv, LookPath: exec.LookPath, GoVersion: goVersion,
		Claude: claudecli.Default, DiskFree: diskFree,
	}
}

func goVersion(ctx context.Context, goBin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "env", "GOVERSION")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func (e Env) getenv(k string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(k)
}

// Run runs every check. A check that panics is reported as failed instead of
// taking the others down.
func Run(ctx context.Context, env Env) []Check {
	checks := []struct {
		id, title string
		fn        func(context.Context, Env) Check
	}{
		{"claude", "Claude Code", CheckClaude},
		{"go", "Go toolchain", CheckGo},
		{"terminal", "Terminal", CheckTerminal},
		{"settings", "Settings files", CheckSettings},
		{"mantle-home", "mantle install", CheckMantleHome},
		{"trust", "Workspace trust", CheckTrust},
	}
	out := make([]Check, 0, len(checks))
	for _, c := range checks {
		out = append(out, guarded(ctx, env, c.id, c.title, c.fn))
	}
	return out
}

func guarded(ctx context.Context, env Env, id, title string, fn func(context.Context, Env) Check) (c Check) {
	defer func() {
		if r := recover(); r != nil {
			c = Check{ID: id, Title: title, Status: Fail, Detail: fmt.Sprintf("check crashed: %v\n%s", r, debug.Stack())}
		}
	}()
	c = fn(ctx, env)
	c.ID, c.Title = id, title
	return c
}

// Worst returns the most severe status among checks (Skip counts as OK).
func Worst(checks []Check) Status {
	worst := OK
	for _, c := range checks {
		if c.Status != Skip && c.Status > worst {
			worst = c.Status
		}
	}
	return worst
}

// CheckClaude finds the claude binary, checks its version, and reads plan 02's
// engine state (pin and conformance probe results) when it exists.
func CheckClaude(ctx context.Context, env Env) Check {
	r := env.Claude
	if r == nil {
		r = claudecli.Default
	}
	bin := r.Bin
	if bin == "" {
		var err error
		if bin, err = claudecli.ResolveBinary(); err != nil {
			return Check{Status: Fail, Detail: err.Error(),
				Fix: "Install Claude Code (https://code.claude.com) or set " + claudecli.EnvBinary + " to its path."}
		}
	}
	rr := *r
	rr.Bin = bin
	v, err := rr.Version(ctx)
	if err != nil {
		return Check{Status: Fail, Detail: fmt.Sprintf("%s: %v", bin, err), Fix: "Reinstall Claude Code or check " + claudecli.EnvBinary + "."}
	}
	c := Check{Status: OK, Detail: fmt.Sprintf("%s (%s)", v, bin)}
	if claudecli.CompareVersions(v, MinClaude) < 0 {
		c.Status = Fail
		c.Detail = fmt.Sprintf("%s is older than the minimum %s (%s)", v, MinClaude, bin)
		c.Fix = "Run `claude update`."
		return c
	}
	st, err := readEngineState(filepath.Join(env.MantleDir, "state", "engines.json"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		c.Items = append(c.Items, Check{Title: "Conformance probe", Status: Skip, Detail: "no probe results yet"})
	case err != nil:
		c.Items = append(c.Items, Check{Title: "Conformance probe", Status: Warn, Detail: "unreadable engine state: " + err.Error()})
		c.Status = Warn
	default:
		if st.pinned != "" {
			c.Items = append(c.Items, Check{Title: "Pinned engine", Status: Warn, Detail: "pinned to " + st.pinned,
				Fix: "Unpin once a newer Claude Code passes the probe."})
		}
		switch {
		case st.failed[v]:
			c.Items = append(c.Items, Check{Title: "Conformance probe", Status: Fail,
				Detail: v + " failed the conformance probe", Fix: "Use the pinned engine or update Claude Code."})
		case st.passed[v]:
			c.Items = append(c.Items, Check{Title: "Conformance probe", Status: OK, Detail: v + " passed"})
		default:
			c.Items = append(c.Items, Check{Title: "Conformance probe", Status: Warn,
				Detail: v + " has not been probed yet", Fix: "It runs automatically when mantle next starts the engine."})
		}
		if w := Worst(c.Items); w > c.Status {
			c.Status = w
		}
	}
	return c
}

type engineState struct {
	pinned         string
	passed, failed map[string]bool
}

// readEngineState reads ~/.mantle/state/engines.json loosely: plan 02 owns the
// format, so any of a few plausible shapes is accepted.
func readEngineState(path string) (engineState, error) {
	st := engineState{passed: map[string]bool{}, failed: map[string]bool{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(b, &doc); err != nil {
		return st, err
	}
	for _, k := range []string{"pinned", "pin", "pinnedVersion", "pinnedBinary"} {
		var s string
		if json.Unmarshal(doc[k], &s) == nil && s != "" {
			st.pinned = s
			break
		}
	}
	readSet := func(raw json.RawMessage, into map[string]bool) {
		var list []string
		if json.Unmarshal(raw, &list) == nil {
			for _, v := range list {
				into[v] = true
			}
			return
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) == nil {
			for v := range m {
				into[v] = true
			}
		}
	}
	for _, k := range []string{"passed", "verified", "passing"} {
		readSet(doc[k], st.passed)
	}
	for _, k := range []string{"failed", "failing"} {
		readSet(doc[k], st.failed)
	}
	// {"versions": {"2.1.288": {"status": "pass"|"fail"}}}
	var versions map[string]struct {
		Status string `json:"status"`
		OK     *bool  `json:"ok"`
	}
	if json.Unmarshal(doc["versions"], &versions) == nil {
		for v, e := range versions {
			switch {
			case e.OK != nil && *e.OK, strings.HasPrefix(strings.ToLower(e.Status), "pass"):
				st.passed[v] = true
			case e.OK != nil, strings.HasPrefix(strings.ToLower(e.Status), "fail"):
				st.failed[v] = true
			}
		}
	}
	return st, nil
}

// CheckGo checks the Go toolchain that make install needs to build mantle.
func CheckGo(ctx context.Context, env Env) Check {
	look := env.LookPath
	if look == nil {
		look = exec.LookPath
	}
	bin, err := look("go")
	if err != nil {
		return Check{Status: Warn, Detail: "go not found on PATH; mantle runs, but make install cannot build it",
			Fix: "Install Go " + MinGo + " or newer (https://go.dev/dl)."}
	}
	gv := env.GoVersion
	if gv == nil {
		gv = goVersion
	}
	v, err := gv(ctx, bin)
	if err != nil || v == "" {
		return Check{Status: Warn, Detail: fmt.Sprintf("%s: could not read its version: %v", bin, err)}
	}
	num := strings.TrimPrefix(v, "go")
	if claudecli.CompareVersions(num, MinGo) < 0 {
		return Check{Status: Warn, Detail: fmt.Sprintf("%s is older than %s (%s)", v, MinGo, bin),
			Fix: "Install Go " + MinGo + " or newer; mantle builds with the local toolchain."}
	}
	return Check{Status: OK, Detail: fmt.Sprintf("%s (%s)", v, bin)}
}

// CheckSettings parses every Claude Code settings file, the keybindings files
// and mantle's own settings. claude -p silently ignores an invalid settings
// file, so a syntax error here means the setting is not in effect.
func CheckSettings(ctx context.Context, env Env) Check {
	roots := discovery.Roots{Home: env.Home, ConfigDir: env.ConfigDir, CWD: env.CWD, ManagedDir: env.ManagedDir}
	var items []Check
	for _, f := range discovery.SettingsFiles(roots) {
		if !f.Exists {
			continue
		}
		items = append(items, fileCheck(string(f.Scope)+" settings", f.Path, f.Err,
			"Claude Code ignores this file in headless mode until it is fixed."))
	}
	others := []struct{ title, path string }{
		{"Claude Code keybindings", filepath.Join(env.ConfigDir, "keybindings.json")},
		{"mantle settings", filepath.Join(env.MantleDir, "settings.json")},
		{"mantle keybindings", filepath.Join(env.MantleDir, "keybindings.json")},
	}
	for _, o := range others {
		b, err := os.ReadFile(o.path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err == nil {
			_, err = discovery.ParseJSONObject(b)
		}
		items = append(items, fileCheck(o.title, o.path, err, "mantle falls back to defaults for this file."))
	}
	c := Check{Status: Worst(items), Items: items}
	switch {
	case len(items) == 0:
		c.Detail = "no settings files"
	case c.Status == OK:
		c.Detail = fmt.Sprintf("%d file(s) parse", len(items))
	default:
		bad := 0
		for _, it := range items {
			if it.Status != OK {
				bad++
			}
		}
		c.Detail = fmt.Sprintf("%d of %d file(s) are invalid", bad, len(items))
		c.Fix = "Fix the JSON errors listed below."
	}
	return c
}

func fileCheck(title, path string, err error, consequence string) Check {
	if err != nil {
		return Check{Title: title, Status: Warn, Detail: fmt.Sprintf("%s: %v", path, err), Fix: consequence}
	}
	return Check{Title: title, Status: OK, Detail: path}
}

// CheckMantleHome checks ~/.mantle: the current and last-good symlinks of the
// version store, and free disk space for builds.
func CheckMantleHome(ctx context.Context, env Env) Check {
	if env.MantleDir == "" {
		return Check{Status: Skip, Detail: "no mantle directory"}
	}
	st, err := os.Stat(env.MantleDir)
	if err != nil || !st.IsDir() {
		return Check{Status: Skip, Detail: env.MantleDir + " does not exist yet; the installer creates it"}
	}
	var items []Check
	for _, link := range []string{"current", "last-good"} {
		items = append(items, versionLink(env.MantleDir, link))
	}
	if env.DiskFree != nil {
		if free, err := env.DiskFree(env.MantleDir); err == nil {
			dc := Check{Title: "Free disk space", Status: OK, Detail: humanBytes(free) + " free"}
			switch {
			case free < 200<<20:
				dc.Status, dc.Fix = Fail, "Free some space: mantle builds need a few hundred MB."
			case free < 1<<30:
				dc.Status, dc.Fix = Warn, "Builds and the version store may run out of space soon."
			}
			items = append(items, dc)
		}
	}
	c := Check{Status: Worst(items), Detail: env.MantleDir, Items: items}
	if c.Status == Fail {
		c.Fix = "Run `mantle rollback` or reinstall with scripts/install.sh."
	}
	return c
}

func versionLink(dir, name string) Check {
	p := filepath.Join(dir, name)
	c := Check{Title: name}
	fi, err := os.Lstat(p)
	if err != nil {
		if name == "current" {
			c.Status, c.Detail, c.Fix = Warn, "no installed UI build", "Install with scripts/install.sh."
		} else {
			c.Status, c.Detail = Skip, "none yet"
		}
		return c
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		c.Status, c.Detail = Warn, p+" is not a symlink"
		return c
	}
	target, _ := os.Readlink(p)
	ui := filepath.Join(p, "mantle-ui")
	uiInfo, err := os.Stat(ui)
	if err != nil {
		c.Status, c.Detail = Fail, fmt.Sprintf("%s → %s is broken: %v", name, target, err)
		c.Fix = "Run `mantle rollback`."
		return c
	}
	if uiInfo.Mode()&0o111 == 0 {
		c.Status, c.Detail = Fail, ui+" is not executable"
		return c
	}
	c.Status, c.Detail = OK, name+" → "+target
	return c
}

// CheckTrust reports whether the cwd (or a parent) is trusted, either in
// Claude Code's own state (read-only) or in mantle's trust file. mantle asks
// before starting the engine in an untrusted folder, because headless claude
// skips its own trust dialog.
func CheckTrust(ctx context.Context, env Env) Check {
	if env.CWD == "" {
		return Check{Status: Skip}
	}
	claudeJSON := filepath.Join(env.Home, ".claude.json")
	if d := env.getenv("CLAUDE_CONFIG_DIR"); d != "" {
		claudeJSON = filepath.Join(d, ".claude.json")
	}
	claudeTrusted := trustedInClaudeJSON(claudeJSON)
	mantleTrusted := trustedInMantle(filepath.Join(env.MantleDir, "trust.json"))
	dir := filepath.Clean(env.CWD)
	for {
		switch {
		case claudeTrusted[dir]:
			return Check{Status: OK, Detail: "trusted in Claude Code (" + dir + ")"}
		case mantleTrusted[dir]:
			return Check{Status: OK, Detail: "trusted in mantle (" + dir + ")"}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return Check{Status: Warn, Detail: env.CWD + " is not trusted yet",
		Fix: "mantle asks before starting Claude here; only trust folders whose hooks and MCP servers you trust."}
}

func trustedInClaudeJSON(path string) map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var doc struct {
		Projects map[string]struct {
			HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return out
	}
	for p, v := range doc.Projects {
		if v.HasTrustDialogAccepted {
			out[filepath.Clean(p)] = true
		}
	}
	return out
}

// trustedInMantle reads ~/.mantle/trust.json loosely (plan 05 owns the
// format): a list of paths, a map of path → true, or such a value under
// "trusted" or "projects".
func trustedInMantle(path string) map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var collect func(raw json.RawMessage, depth int)
	collect = func(raw json.RawMessage, depth int) {
		if depth > 3 {
			return
		}
		var list []string
		if json.Unmarshal(raw, &list) == nil {
			for _, p := range list {
				out[filepath.Clean(p)] = true
			}
			return
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return
		}
		for k, v := range m {
			if filepath.IsAbs(k) {
				var ok bool
				if json.Unmarshal(v, &ok) == nil {
					if ok {
						out[filepath.Clean(k)] = true
					}
					continue
				}
				var obj map[string]any
				if json.Unmarshal(v, &obj) == nil {
					for _, key := range []string{"trusted", "accepted", "hasTrustDialogAccepted"} {
						if b, _ := obj[key].(bool); b {
							out[filepath.Clean(k)] = true
						}
					}
				}
				continue
			}
			collect(v, depth+1)
		}
	}
	collect(b, 0)
	return out
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
