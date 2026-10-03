package gates

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TrustSource says which record granted trust.
type TrustSource string

const (
	TrustNone   TrustSource = ""
	TrustClaude TrustSource = "claude" // ~/.claude.json projects[path].hasTrustDialogAccepted
	TrustMantle TrustSource = "mantle" // ~/.mantle/trust.json
	TrustEnv    TrustSource = "env"    // CLAUDE_CODE_SANDBOXED, as Claude Code honours it
)

// TrustState is the result of the workspace-trust check for one directory.
type TrustState struct {
	Cwd string
	// ProjectKey is the path Claude Code keys this workspace by: the repository root
	// (the main worktree's root for a linked worktree), or cwd outside a repository.
	ProjectKey string
	// GitRoot is the root of the enclosing work tree, "" outside a repository.
	GitRoot string

	Trusted     bool
	Source      TrustSource
	TrustedPath string // the record that granted trust: the project key, cwd or an ancestor

	// HomeDir is set when cwd is the home directory. Trust there is session-only: the
	// host keeps it in memory and RecordTrust doesn't persist it.
	HomeDir bool
	// ConfigErr reports an unreadable ~/.claude.json (PD-38); it is never "repaired".
	ConfigErr error
}

// CheckTrust decides whether cwd is trusted. It mirrors Claude Code 2.1.288:
//   - the project key itself;
//   - then every directory from cwd up to the repository root (inside a repository) or up
//     to the filesystem root (outside one).
//
// Each candidate is checked in Claude Code's projects map (read-only) and in mantle's own
// trust store. Both the logical cwd and its symlink-resolved form are tried, because
// Claude Code records physical paths. gc may be nil, in which case it is loaded.
func CheckTrust(env Env, cwd string, gc *GlobalConfig) TrustState {
	if gc == nil {
		gc = LoadGlobalConfig(env)
	}
	cwd = absClean(cwd)
	st := TrustState{Cwd: cwd, ConfigErr: gc.Err}
	st.GitRoot, st.ProjectKey = projectRoots(cwd)
	if st.ProjectKey == "" {
		st.ProjectKey = cwd
	}
	st.HomeDir = env.Home != "" && samePath(cwd, env.Home)

	if v := env.getenv("CLAUDE_CODE_SANDBOXED"); v != "" && v != "0" && !strings.EqualFold(v, "false") {
		st.Trusted, st.Source, st.TrustedPath = true, TrustEnv, cwd
		return st
	}

	store := loadTrustStore(env)
	check := func(p string) bool {
		if gc.trusted(p) {
			st.Trusted, st.Source, st.TrustedPath = true, TrustClaude, p
			return true
		}
		if store.has(p) {
			st.Trusted, st.Source, st.TrustedPath = true, TrustMantle, p
			return true
		}
		return false
	}
	for _, key := range variants(st.ProjectKey) {
		if check(key) {
			return st
		}
	}
	for _, start := range variants(cwd) {
		gitRoot, _ := projectRoots(start)
		for _, p := range ancestors(start, gitRoot) {
			if check(p) {
				return st
			}
		}
	}
	return st
}

// NeedsTrust reports whether the trust dialog must be shown before spawning in cwd.
func NeedsTrust(env Env, cwd string) bool {
	return !CheckTrust(env, cwd, nil).Trusted
}

// RecordTrust stores the user's acceptance for cwd's project in mantle's trust store
// (never in ~/.claude.json). It returns persisted=false for the home directory, where
// trust is session-only.
func RecordTrust(env Env, cwd string) (persisted bool, err error) {
	cwd = absClean(cwd)
	if env.Home != "" && samePath(cwd, env.Home) {
		return false, nil
	}
	_, key := projectRoots(cwd)
	if key == "" {
		key = cwd
	}
	err = updateJSON(env.TrustStorePath(), func(s *trustStore) error {
		if s.Folders == nil {
			s.Folders = map[string]trustRecord{}
		}
		s.Version = 1
		rec := trustRecord{AcceptedAt: time.Now().UTC().Format(time.RFC3339)}
		for _, p := range variants(key) {
			s.Folders[p] = rec
		}
		return nil
	})
	return err == nil, err
}

// RevokeTrust removes mantle's record for cwd's project. Claude Code's own record, if
// any, is left alone.
func RevokeTrust(env Env, cwd string) error {
	cwd = absClean(cwd)
	_, key := projectRoots(cwd)
	if key == "" {
		key = cwd
	}
	return updateJSON(env.TrustStorePath(), func(s *trustStore) error {
		for _, p := range variants(key) {
			delete(s.Folders, p)
		}
		return nil
	})
}

type trustStore struct {
	Version int                    `json:"version"`
	Folders map[string]trustRecord `json:"folders"`
}

type trustRecord struct {
	AcceptedAt string `json:"acceptedAt"`
}

func loadTrustStore(env Env) *trustStore {
	var s trustStore
	if readJSON(env.TrustStorePath(), &s) != nil {
		return &trustStore{}
	}
	return &s
}

func (s *trustStore) has(p string) bool {
	_, ok := s.Folders[p]
	return ok
}

// ancestors lists dir and its parents, stopping after bound when bound is an ancestor of
// dir, or at the filesystem root otherwise.
func ancestors(dir, bound string) []string {
	var out []string
	for p := dir; ; {
		out = append(out, p)
		if bound != "" && p == bound {
			return out
		}
		parent := filepath.Dir(p)
		if parent == p {
			return out
		}
		p = parent
	}
}

// projectRoots finds the enclosing work tree root and the project key. For a linked
// worktree (".git" is a file pointing into <main>/.git/worktrees/<name>), the project
// key is the main worktree's root; otherwise both are the work tree root.
func projectRoots(dir string) (gitRoot, key string) {
	for p := dir; ; {
		gitPath := filepath.Join(p, ".git")
		if fi, err := os.Lstat(gitPath); err == nil {
			if fi.IsDir() {
				return p, p
			}
			if fi.Mode().IsRegular() {
				if main := mainWorktree(p, gitPath); main != "" {
					return p, main
				}
				return p, p
			}
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", ""
		}
		p = parent
	}
}

// mainWorktree resolves a ".git" file to the main worktree's root, or "".
func mainWorktree(root, gitFile string) string {
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	gitdir, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return ""
	}
	gitdir = strings.TrimSpace(gitdir)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(root, gitdir)
	}
	common := gitdir
	if c, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
		common = strings.TrimSpace(string(c))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitdir, common)
		}
	}
	common = filepath.Clean(common)
	if filepath.Base(common) != ".git" {
		return ""
	}
	return filepath.Dir(common)
}

// variants returns p and, when different, its symlink-resolved form.
func variants(p string) []string {
	out := []string{p}
	if r, err := filepath.EvalSymlinks(p); err == nil && r != p {
		out = append(out, r)
	}
	return out
}

func samePath(a, b string) bool {
	a, b = absClean(a), absClean(b)
	if a == b {
		return true
	}
	ra, ea := filepath.EvalSymlinks(a)
	rb, eb := filepath.EvalSymlinks(b)
	return ea == nil && eb == nil && ra == rb
}

func absClean(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return filepath.Clean(p)
}
