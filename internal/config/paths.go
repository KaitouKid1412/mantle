package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// EnvMantleHome overrides ~/.mantle (tests, isolated installs).
const EnvMantleHome = "MANTLE_HOME"

// Paths locates every file mantle reads or writes.
type Paths struct {
	Home       string
	ClaudeDir  string // $CLAUDE_CONFIG_DIR, else ~/.claude
	ClaudeJSON string // $CLAUDE_CONFIG_DIR/.claude.json, else ~/.claude.json (read-only!)
	MantleDir  string // $MANTLE_HOME, else ~/.mantle
	Project    string // project directory (the session cwd)
	Managed    string // managed-settings.json for this OS
	// ManagedPlists are MDM configuration-profile plists (macOS), device-level then
	// per-user; later ones win. Read with /usr/bin/plutil.
	ManagedPlists []string
}

// MDMDomain is the preference domain of Claude Code's managed settings on macOS.
const MDMDomain = "com.anthropic.claudecode"

// macManagedPlists are the device-level and per-user managed-preferences plists.
func macManagedPlists(user string) []string {
	out := []string{"/Library/Managed Preferences/" + MDMDomain + ".plist"}
	if user != "" {
		out = append(out, "/Library/Managed Preferences/"+user+"/"+MDMDomain+".plist")
	}
	return out
}

// DefaultPaths resolves paths from the environment for a project directory.
func DefaultPaths(project string) (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	p := PathsFor(home, project, os.Getenv("CLAUDE_CONFIG_DIR"), os.Getenv(EnvMantleHome))
	if runtime.GOOS == "darwin" {
		p.ManagedPlists = macManagedPlists(os.Getenv("USER"))
	}
	return p, nil
}

// PathsFor builds Paths from explicit values ("" = default).
func PathsFor(home, project, claudeConfigDir, mantleHome string) Paths {
	p := Paths{Home: home, Project: project, Managed: ManagedPath(runtime.GOOS)}
	if claudeConfigDir != "" {
		p.ClaudeDir = claudeConfigDir
		p.ClaudeJSON = filepath.Join(claudeConfigDir, ".claude.json")
	} else {
		p.ClaudeDir = filepath.Join(home, ".claude")
		p.ClaudeJSON = filepath.Join(home, ".claude.json")
	}
	if mantleHome != "" {
		p.MantleDir = mantleHome
	} else {
		p.MantleDir = filepath.Join(home, ".mantle")
	}
	return p
}

// ManagedPath is the managed (policy) settings file for an OS.
func ManagedPath(goos string) string {
	switch goos {
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json"
	case "windows":
		return `C:\Program Files\ClaudeCode\managed-settings.json`
	}
	return "/etc/claude-code/managed-settings.json"
}

// ScopeFile is the settings file of a scope ("" for flag settings and for project
// scopes without a project).
func (p Paths) ScopeFile(scope string) string {
	switch scope {
	case ext.ScopeUser:
		return filepath.Join(p.ClaudeDir, "settings.json")
	case ext.ScopeProject:
		if p.Project == "" {
			return ""
		}
		return filepath.Join(p.Project, ".claude", "settings.json")
	case ext.ScopeLocal:
		if p.Project == "" {
			return ""
		}
		return filepath.Join(p.Project, ".claude", "settings.local.json")
	case ext.ScopePolicy:
		return p.Managed
	}
	return ""
}

// ManagedDropIns lists managed-settings.d/*.json, sorted (later files win).
func (p Paths) ManagedDropIns() []string {
	if p.Managed == "" {
		return nil
	}
	dir := filepath.Join(filepath.Dir(p.Managed), "managed-settings.d")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// mantle's own files.

func (p Paths) MantleSettings() string    { return filepath.Join(p.MantleDir, "settings.json") }
func (p Paths) MantleKeybindings() string { return filepath.Join(p.MantleDir, "keybindings.json") }
func (p Paths) StateDir() string          { return filepath.Join(p.MantleDir, "state") }
func (p Paths) LocksDir() string          { return filepath.Join(p.MantleDir, "locks") }
func (p Paths) LogsDir() string           { return filepath.Join(p.MantleDir, "logs") }
func (p Paths) DisabledFile() string      { return filepath.Join(p.StateDir(), "disabled.json") }

// Claude Code files mantle reads.

func (p Paths) ClaudeKeybindings() string { return filepath.Join(p.ClaudeDir, "keybindings.json") }
func (p Paths) ThemesDir() string         { return filepath.Join(p.ClaudeDir, "themes") }

// forbidden reports whether path is (or links to) ~/.claude.json, which mantle never
// writes: Claude Code rewrites it without a lock.
func (p Paths) forbidden(path string) bool {
	if path == "" || p.ClaudeJSON == "" {
		return false
	}
	same := func(a, b string) bool {
		if filepath.Clean(a) == filepath.Clean(b) {
			return true
		}
		ia, err1 := os.Stat(a)
		ib, err2 := os.Stat(b)
		return err1 == nil && err2 == nil && os.SameFile(ia, ib)
	}
	if same(path, p.ClaudeJSON) || filepath.Base(path) == ".claude.json" {
		return true
	}
	if real, err := filepath.EvalSymlinks(path); err == nil && same(real, p.ClaudeJSON) {
		return true
	}
	return false
}

// ErrForbidden is returned for writes to ~/.claude.json.
var ErrForbidden = errors.New("mantle never writes ~/.claude.json")
