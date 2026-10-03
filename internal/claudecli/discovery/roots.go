// Package discovery finds the files behind Claude Code's ecosystem: agents,
// skills, slash commands, memory (CLAUDE.md and friends), hooks and the settings
// files that declare them. The engine reports names (initialize.agents,
// init.skills, get_hooks_listing); discovery adds paths and scopes so panels can
// open and edit the files.
//
// Everything is read-only and tolerant: a missing or malformed file is reported
// on its item, never fatal.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package discovery

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Scope says where an item is configured.
type Scope string

const (
	ScopeManaged  Scope = "managed"   // administrator policy directory
	ScopeUser     Scope = "user"      // ~/.claude
	ScopeProject  Scope = "project"   // checked-in .claude/ or CLAUDE.md
	ScopeLocal    Scope = "local"     // .claude/settings.local.json, CLAUDE.local.md
	ScopePlugin   Scope = "plugin"    // an enabled plugin
	ScopeClaudeAI Scope = "claude.ai" // skills synced from claude.ai
	ScopeAuto     Scope = "auto"      // auto memory
)

// Roots are the directories discovery reads. Tests point them at temp trees.
type Roots struct {
	Home       string // the user's home directory
	ConfigDir  string // $CLAUDE_CONFIG_DIR, or ~/.claude
	CWD        string // the session's working directory
	ManagedDir string // policy directory; empty skips managed files
	// Plugins adds plugin roots the engine loaded that are not installed on
	// disk, e.g. --plugin-dir (from init.plugins).
	Plugins []PluginRoot
	// NestedDepth is how many directory levels below CWD to search for nested
	// .claude directories (skills and commands there load when Claude works on
	// files below them). 0 disables the search.
	NestedDepth int
	// Getenv reads environment variables; nil means os.Getenv.
	Getenv func(string) string
}

// DefaultRoots returns the roots for the current user and cwd.
func DefaultRoots(cwd string) Roots {
	home, _ := os.UserHomeDir()
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	if cfg == "" {
		cfg = filepath.Join(home, ".claude")
	}
	return Roots{Home: home, ConfigDir: cfg, CWD: cwd, ManagedDir: DefaultManagedDir(), NestedDepth: 3}
}

// DefaultManagedDir is Claude Code's administrator policy directory.
func DefaultManagedDir() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "windows":
		return `C:\Program Files\ClaudeCode`
	}
	return "/etc/claude-code"
}

func (r Roots) getenv(k string) string {
	if r.Getenv != nil {
		return r.Getenv(k)
	}
	return os.Getenv(k)
}

// isUserConfig reports whether dir/.claude is the user config dir, which must
// not be read a second time as a project directory.
func (r Roots) isUserConfig(dotClaude string) bool {
	return r.ConfigDir != "" && sameDir(dotClaude, r.ConfigDir)
}

// ancestors returns cwd and its parents up to, but not including, the
// filesystem root, nearest first.
func ancestors(cwd string) []string {
	if cwd == "" {
		return nil
	}
	var out []string
	dir := filepath.Clean(cwd)
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return out
		}
		out = append(out, dir)
		dir = parent
	}
}

// CanonicalRoot returns the main checkout of the git repository that contains
// dir (worktrees resolve to the repository they belong to), or dir itself when
// it is not in a repository. Auto memory is keyed by this directory.
func CanonicalRoot(dir string) string {
	for _, d := range ancestors(dir) {
		gitPath := filepath.Join(d, ".git")
		st, err := os.Stat(gitPath)
		if err != nil {
			continue
		}
		if st.IsDir() {
			return d
		}
		// A worktree: ".git" is a file "gitdir: <repo>/.git/worktrees/<name>".
		b, err := os.ReadFile(gitPath)
		if err != nil {
			return d
		}
		gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
		if !ok {
			return d
		}
		gitdir = strings.TrimSpace(gitdir)
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(d, gitdir)
		}
		common := filepath.Join(gitdir, "..", "..")
		if c, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
			common = strings.TrimSpace(string(c))
			if !filepath.IsAbs(common) {
				common = filepath.Join(gitdir, common)
			}
		}
		common = filepath.Clean(common)
		if filepath.Base(common) == ".git" {
			return filepath.Dir(common)
		}
		return d
	}
	return filepath.Clean(dir)
}

// ProjectSlug is the directory name Claude Code uses under ~/.claude/projects:
// the path with every character other than a letter or digit replaced by "-".
func ProjectSlug(path string) string {
	b := []byte(path)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	sa, errA := os.Stat(a)
	sb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(sa, sb)
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// mdFiles lists *.md files directly in dir, sorted.
func mdFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// mdFilesRecursive lists *.md files under dir (sorted by path), skipping
// hidden directories and following no symlinked directories.
func mdFilesRecursive(dir string) []string {
	var out []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(p), ".md") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
