// Package gates decides what must be confirmed before mantle spawns an engine in a
// directory. Headless `claude -p` skips the workspace-trust dialog, `.mcp.json` server
// approval and the bypass-permissions warning, and project hooks run as soon as it
// starts, so mantle runs these checks itself, before any spawn.
//
// The package is pure apart from file reads and mantle's own stores. It never writes
// ~/.claude.json or any Claude Code settings file.
//
// Parity: PD-30, PD-31, PD-32, PD-33, PD-34, PD-35, PD-37, PD-38.
package gates

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// Env locates the files the gates read and write. Build it with FromOS, or fill it by
// hand in tests (a temp Home is enough).
type Env struct {
	// Home is the user's home directory.
	Home string
	// ClaudeConfigDir is $CLAUDE_CONFIG_DIR, or "" when unset. It relocates both
	// ~/.claude and ~/.claude.json.
	ClaudeConfigDir string
	// MantleDir is mantle's state root ($MANTLE_HOME); "" means Home/.mantle.
	MantleDir string
	// ManagedDir holds managed-settings.json and managed-settings.d/; "" means the OS
	// default. Tests point it at a temp dir.
	ManagedDir string
	// Getenv reads environment variables; nil means os.Getenv.
	Getenv func(string) string
}

// FromOS builds an Env from the process environment.
func FromOS() (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}
	if home == "" {
		return Env{}, errors.New("gates: no home directory")
	}
	return Env{Home: home, ClaudeConfigDir: os.Getenv("CLAUDE_CONFIG_DIR"), MantleDir: os.Getenv("MANTLE_HOME")}, nil
}

func (e Env) getenv(k string) string {
	if e.Getenv != nil {
		return e.Getenv(k)
	}
	return os.Getenv(k)
}

// ClaudeDir is ~/.claude, or $CLAUDE_CONFIG_DIR when set.
func (e Env) ClaudeDir() string {
	if e.ClaudeConfigDir != "" {
		return e.ClaudeConfigDir
	}
	return filepath.Join(e.Home, ".claude")
}

// GlobalConfigPath is the path of Claude Code's global state file: a legacy
// <claude dir>/.config.json when present, otherwise .claude.json in $CLAUDE_CONFIG_DIR or
// the home directory.
func (e Env) GlobalConfigPath() string {
	legacy := filepath.Join(e.ClaudeDir(), ".config.json")
	if st, err := os.Stat(legacy); err == nil && st.Mode().IsRegular() {
		return legacy
	}
	dir := e.Home
	if e.ClaudeConfigDir != "" {
		dir = e.ClaudeConfigDir
	}
	return filepath.Join(dir, ".claude.json")
}

// UserSettingsPath is the user-scope settings file.
func (e Env) UserSettingsPath() string { return filepath.Join(e.ClaudeDir(), "settings.json") }

func (e Env) mantleDir() string {
	if e.MantleDir != "" {
		return e.MantleDir
	}
	return filepath.Join(e.Home, ".mantle")
}

// TrustStorePath is mantle's own record of trusted folders.
func (e Env) TrustStorePath() string { return filepath.Join(e.mantleDir(), "trust.json") }

// GateStorePath holds mantle's other one-time gate answers (bypass warning, auto mode,
// API key).
func (e Env) GateStorePath() string { return filepath.Join(e.mantleDir(), "state", "gates.json") }

func (e Env) managedDir() string {
	if e.ManagedDir != "" {
		return e.ManagedDir
	}
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "windows":
		return `C:\Program Files\ClaudeCode`
	}
	return "/etc/claude-code"
}
