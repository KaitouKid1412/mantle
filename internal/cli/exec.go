package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// EnvClaudeBin overrides which claude binary mantle runs. The engine (plan 02) and the
// subcommand runner (plan 09) resolve the binary the same way.
const EnvClaudeBin = "MANTLE_CLAUDE_BIN"

// ErrClaudeNotFound means no claude binary could be resolved.
var ErrClaudeNotFound = errors.New("claude not found (install Claude Code, or set " + EnvClaudeBin + ")")

// ResolveClaude returns the claude binary: $MANTLE_CLAUDE_BIN when set (a path, or a
// name looked up on PATH), otherwise "claude" on PATH.
func ResolveClaude() (string, error) {
	return resolveClaude(os.Getenv, exec.LookPath)
}

func resolveClaude(getenv func(string) string, lookPath func(string) (string, error)) (string, error) {
	name := getenv(EnvClaudeBin)
	if name == "" {
		name = "claude"
	} else if strings.ContainsRune(name, filepath.Separator) {
		if st, err := os.Stat(name); err != nil || st.IsDir() {
			return "", fmt.Errorf("%s=%s: %w", EnvClaudeBin, name, ErrClaudeNotFound)
		}
		return name, nil
	}
	p, err := lookPath(name)
	if err != nil {
		if name != "claude" {
			return "", fmt.Errorf("%s=%s: %w", EnvClaudeBin, name, ErrClaudeNotFound)
		}
		return "", ErrClaudeNotFound
	}
	return p, nil
}

// execve is syscall.Exec; tests replace it.
var execve = syscall.Exec

// ExecClaude replaces the current process with claude, passing argv (without the
// program name) and the environment unchanged. It returns only on failure.
func ExecClaude(argv []string) error {
	bin, err := ResolveClaude()
	if err != nil {
		return err
	}
	if self, err := os.Executable(); err == nil && sameFile(self, bin) {
		return fmt.Errorf("%s resolves to mantle itself; point %s at the real claude", bin, EnvClaudeBin)
	}
	full := append([]string{bin}, argv...)
	if err := execve(bin, full, os.Environ()); err != nil {
		return fmt.Errorf("exec %s: %w", bin, err)
	}
	return nil
}

func sameFile(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(sa, sb)
}
