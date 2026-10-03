package launcher

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"syscall"
)

// PassthroughSubcommands are the claude subcommands the launcher execs
// straight to claude, so they work even when the UI build is broken.
//
// This is a copy of plan 11's canonical list in internal/cli (minus doctor,
// which belongs to the launcher). The launcher stays standard-library-only,
// so it cannot import internal/cli; a test asserts the two lists match.
var PassthroughSubcommands = []string{
	"agents", "attach", "auth", "auto-mode", "gateway", "import", "install",
	"kill", "logs", "mcp", "plugin", "plugins", "purge", "respawn", "rm",
	"setup-token", "stop", "ultrareview", "update", "upgrade",
}

// Kind is what the launcher does with an argv.
type Kind int

const (
	// KindUI supervises mantle-ui with Args.
	KindUI Kind = iota
	// KindPassthrough execs claude with Args unchanged.
	KindPassthrough
	// KindVersions runs `mantle versions`.
	KindVersions
	// KindRollback runs `mantle rollback [<id>]`.
	KindRollback
	// KindDoctor runs `mantle doctor`.
	KindDoctor
)

func (k Kind) String() string {
	return [...]string{"ui", "passthrough", "versions", "rollback", "doctor"}[k]
}

// Dispatch is the classification of the launcher's argv.
type Dispatch struct {
	Kind Kind
	// Args are the arguments for the chosen target, without argv[0]: the
	// UI's or claude's argv, or the launcher command's own arguments.
	Args []string
	// Safe is set by --safe.
	Safe bool
}

// ClassifyArgs decides what to do with argv (without argv[0]).
//
// Order matters, because claude treats an unknown positional word as a
// prompt: the launcher's own commands are matched first, then -p/--print and
// claude subcommands are passed through, and everything else goes to the UI.
// --safe is a launcher flag and is removed wherever it appears before "--".
func ClassifyArgs(argv []string) Dispatch {
	if len(argv) > 0 {
		switch argv[0] {
		case "versions":
			return Dispatch{Kind: KindVersions, Args: argv[1:]}
		case "rollback":
			return Dispatch{Kind: KindRollback, Args: argv[1:]}
		case "doctor":
			return Dispatch{Kind: KindDoctor, Args: argv[1:]}
		}
	}
	args, safe := stripSafe(argv)
	d := Dispatch{Kind: KindUI, Args: args, Safe: safe}
	if hasPrintFlag(args) || (len(args) > 0 && slices.Contains(PassthroughSubcommands, args[0])) {
		d.Kind = KindPassthrough
	}
	return d
}

// stripSafe removes --safe before any "--". The input is returned as is
// when it has no --safe, so argv is forwarded exactly.
func stripSafe(argv []string) ([]string, bool) {
	end := slices.Index(argv, "--")
	if end < 0 {
		end = len(argv)
	}
	if !slices.Contains(argv[:end], "--safe") {
		return argv, false
	}
	out := make([]string, 0, len(argv))
	for i, a := range argv {
		if i < end && a == "--safe" {
			continue
		}
		out = append(out, a)
	}
	return out, true
}

// hasPrintFlag reports whether -p/--print appears before "--", including in a
// cluster of short boolean flags such as -cp. In a cluster, a flag that takes
// a value (-w, -r, -n, -d, ...) consumes the rest of the word, as commander
// does.
func hasPrintFlag(argv []string) bool {
	for _, a := range argv {
		if a == "--" {
			return false
		}
		if a == "--print" || a == "-p" {
			return true
		}
		if len(a) > 2 && a[0] == '-' && a[1] != '-' {
			for _, c := range a[1:] {
				if c == 'p' {
					return true
				}
				if c != 'c' && c != 'v' && c != 'h' {
					break // a value-taking or unknown flag swallows the rest
				}
			}
		}
	}
	return false
}

// ClaudeBinary resolves the claude binary: $MANTLE_CLAUDE_BIN, else PATH.
func ClaudeBinary() (string, error) {
	if p := os.Getenv(EnvClaudeBin); p != "" {
		return p, nil
	}
	p, err := exec.LookPath("claude")
	if err != nil {
		return "", errors.New("claude was not found in PATH (set " + EnvClaudeBin + " to its path)")
	}
	return p, nil
}

// ExecClaude replaces the launcher with claude, argv and environment
// unchanged. It returns only on error.
func ExecClaude(args []string) error {
	bin, err := ClaudeBinary()
	if err != nil {
		return err
	}
	return syscall.Exec(bin, append([]string{bin}, args...), os.Environ())
}
