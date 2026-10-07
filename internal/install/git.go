package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Git runs git in one repository.
type Git struct {
	Dir string
	// Bin is the git binary; "" means "git".
	Bin string
	// Env is appended to the environment of every git command.
	Env []string
}

// run executes git and returns stdout with surrounding whitespace trimmed.
func (g Git) run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	bin := g.Bin
	if bin == "" {
		bin = "git"
	}
	c := exec.CommandContext(ctx, bin, append([]string{"-C", g.Dir}, args...)...)
	c.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"), g.Env...)
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), msg, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// BranchExists reports whether a local branch exists.
func (g Git) BranchExists(name string) bool {
	_, err := g.run("show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// IsAncestor reports whether a is an ancestor of b.
func (g Git) IsAncestor(a, b string) (bool, error) {
	_, err := g.run("merge-base", "--is-ancestor", a, b)
	if err == nil {
		return true, nil
	}
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// mergeEnv returns base with every KEY=VALUE of overrides replacing (or adding) its key.
func mergeEnv(base, overrides []string) []string {
	keys := map[string]bool{}
	for _, kv := range overrides {
		k, _, _ := strings.Cut(kv, "=")
		keys[k] = true
	}
	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !keys[k] {
			out = append(out, kv)
		}
	}
	return append(out, overrides...)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
