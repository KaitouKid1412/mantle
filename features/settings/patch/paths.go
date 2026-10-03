package patch

import (
	"errors"
	"fmt"
	"path/filepath"
)

// Env holds what is needed to locate settings files. Callers fill it from the process
// environment; tests use temp directories.
type Env struct {
	Home        string // $HOME
	ConfigDir   string // $CLAUDE_CONFIG_DIR, or "" for the default ~/.claude
	ProjectRoot string // project directory holding .claude/
	Managed     string // managed settings file; "" = the OS default
}

// ClaudeDir is the directory holding user settings, keybindings, themes, etc.
func (e Env) ClaudeDir() string {
	if e.ConfigDir != "" {
		return e.ConfigDir
	}
	return filepath.Join(e.Home, ".claude")
}

// GlobalConfigPath is Claude Code's global config file (~/.claude.json, or
// $CLAUDE_CONFIG_DIR/.claude.json). mantle reads it but must never write it: it has no
// lock file and the engine rewrites it constantly.
func (e Env) GlobalConfigPath() string {
	if e.ConfigDir != "" {
		return filepath.Join(e.ConfigDir, ".claude.json")
	}
	return filepath.Join(e.Home, ".claude.json")
}

// ErrGlobalConfig is returned for any attempt to target the global config file.
var ErrGlobalConfig = errors.New("refusing to write Claude Code's global config (~/.claude.json); use the engine's /config command")

// Path returns the settings file for a writable scope.
func (e Env) Path(s Scope) (string, error) {
	var p string
	switch s {
	case User:
		if e.Home == "" && e.ConfigDir == "" {
			return "", errors.New("home directory unknown")
		}
		p = filepath.Join(e.ClaudeDir(), "settings.json")
	case Project, Local:
		if e.ProjectRoot == "" {
			return "", fmt.Errorf("%s needs a project directory", s.Label())
		}
		name := "settings.json"
		if s == Local {
			name = "settings.local.json"
		}
		p = filepath.Join(e.ProjectRoot, ".claude", name)
	default:
		return "", fmt.Errorf("%w: %s", ErrReadOnly, s)
	}
	if err := e.CheckWritable(p); err != nil {
		return "", err
	}
	return p, nil
}

// CheckWritable fails if p is a file mantle must never write. Every writer in this area
// calls it before touching disk.
func (e Env) CheckWritable(p string) error {
	clean := filepath.Clean(p)
	if filepath.Base(clean) == ".claude.json" {
		return ErrGlobalConfig
	}
	for _, g := range []string{e.GlobalConfigPath(), filepath.Join(e.Home, ".claude.json")} {
		if g != "" && sameFile(clean, g) {
			return ErrGlobalConfig
		}
	}
	return nil
}

func sameFile(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}
