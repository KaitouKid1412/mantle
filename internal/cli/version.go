package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
)

// Version and BuildID identify this mantle build. The version store (plan 10) sets them
// with -ldflags "-X github.com/KaitouKid1412/mantle/internal/cli.Version=…"; when unset
// they come from the Go build info.
var (
	Version = ""
	BuildID = ""
)

// Build returns mantle's version and build ID.
func Build() (version, id string) {
	version, id = Version, BuildID
	if bi, ok := debug.ReadBuildInfo(); ok {
		if version == "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
		if id == "" {
			var rev string
			var dirty bool
			for _, s := range bi.Settings {
				switch s.Key {
				case "vcs.revision":
					rev = s.Value
				case "vcs.modified":
					dirty = s.Value == "true"
				}
			}
			if len(rev) > 12 {
				rev = rev[:12]
			}
			if rev != "" && dirty {
				rev += "-dirty"
			}
			id = rev
		}
	}
	if version == "" {
		version = "dev"
	}
	if id == "" {
		id = "unknown build"
	}
	return version, id
}

// Timeouts for the two claude calls mantle makes on its own: --version and --help.
var (
	VersionTimeout = 5 * time.Second
	HelpTimeout    = 10 * time.Second
)

var engineVersionRE = regexp.MustCompile(`\d+\.\d+\.\d+[0-9A-Za-z.+-]*`)

// EngineVersion runs `claude --version` and returns the version number ("2.1.288").
func EngineVersion(ctx context.Context) (string, error) {
	out, err := runClaudeInfo(ctx, VersionTimeout, "--version")
	if err != nil {
		return "", err
	}
	v := engineVersionRE.FindString(string(out))
	if v == "" {
		return "", fmt.Errorf("unrecognised claude --version output %q", firstLine(string(out)))
	}
	return v, nil
}

// VersionLine is what `mantle --version` prints:
// "mantle <version> (<build id>), engine claude <version>".
func VersionLine(ctx context.Context) string {
	v, id := Build()
	ev, err := EngineVersion(ctx)
	if err != nil {
		return fmt.Sprintf("mantle %s (%s), engine claude unavailable: %v", v, id, err)
	}
	return fmt.Sprintf("mantle %s (%s), engine claude %s", v, id, ev)
}

// helpText is mantle's own usage. claude's options follow it at runtime; mantle never
// carries a copy of claude's help.
const helpText = `Usage: mantle [options] [prompt]
       mantle <claude subcommand> [args...]
       mantle versions | rollback [<build>] | doctor

mantle is a terminal UI for Claude Code. It runs your installed claude as its engine,
so sessions, settings, tools and plugins are Claude Code's own.

mantle options:
  --safe          start the last build that passed probation
  --research      start in research mode (a tree-structured conversation)
  -h, --help      show this help and claude's
  -v, --version   show the mantle and engine versions

mantle commands:
  versions        list the installed mantle builds
  rollback        switch back to an earlier build
  doctor          check mantle's installation, then offer claude's checks

All claude options are accepted with the same meaning. -p/--print and claude's
subcommands run claude directly. claude's options:

`

// WriteHelp writes mantle's usage, then the output of `claude --help`.
func WriteHelp(ctx context.Context, w io.Writer) error {
	if _, err := io.WriteString(w, helpText); err != nil {
		return err
	}
	out, err := runClaudeInfo(ctx, HelpTimeout, "--help")
	if err != nil {
		_, werr := fmt.Fprintf(w, "  (claude --help is unavailable: %v)\n", err)
		return werr
	}
	_, err = w.Write(out)
	return err
}

// runClaudeInfo runs claude with one information flag. stdin is /dev/null, so claude
// can never read a prompt from it, and CLAUDECODE is dropped so a mantle started from
// inside Claude Code still gets a plain answer.
func runClaudeInfo(ctx context.Context, timeout time.Duration, flag string) ([]byte, error) {
	if flag != "--version" && flag != "--help" {
		return nil, fmt.Errorf("runClaudeInfo: refusing %q", flag)
	}
	bin, err := ResolveClaude()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, flag)
	cmd.Stdin = nil
	cmd.Env = withoutEnv(os.Environ(), "CLAUDECODE")
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("claude %s timed out after %s", flag, timeout)
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if msg := firstLine(stderr.String()); msg != "" {
				return nil, fmt.Errorf("claude %s: %s", flag, msg)
			}
		}
		return nil, fmt.Errorf("claude %s: %w", flag, err)
	}
	return stdout.Bytes(), nil
}

func withoutEnv(env []string, names ...string) []string {
	out := env[:0:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, n := range names {
			if k == n {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
