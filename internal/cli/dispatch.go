package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
)

// Dispatch is mantle-ui's command-line entry point. It parses argv (without the program
// name) and handles every mode except the UI: exec claude, --version, --help, parse
// errors and launcher commands. When done is true, mantle-ui exits with code;
// otherwise p is a UI command line.
//
// mantle-ui's own developer commands (story, catalog, selftest) must be matched before
// calling Dispatch, or they become a prompt just as `claude story` would.
func Dispatch(ctx context.Context, argv []string, stdout, stderr io.Writer) (p Parsed, done bool, code int) {
	p, err := Parse(argv)
	if err != nil {
		fmt.Fprintf(stderr, "mantle: %v\n", err)
		return p, true, 1
	}
	switch p.Mode {
	case ModeExecClaude:
		return p, true, execOrReport(p.Exec, stderr)
	case ModeVersion:
		fmt.Fprintln(stdout, VersionLine(ctx))
		return p, true, 0
	case ModeHelp:
		if err := WriteHelp(ctx, stdout); err != nil {
			fmt.Fprintf(stderr, "mantle: %v\n", err)
			return p, true, 1
		}
		return p, true, 0
	case ModeLauncher:
		if p.Subcommand == "doctor" {
			// Without the launcher, claude's own checks are the useful answer.
			return p, true, execOrReport(append([]string{"doctor"}, p.ExtraArgs...), stderr)
		}
		fmt.Fprintf(stderr, "mantle: %q is a launcher command; run it as `mantle %s` through the mantle launcher\n", p.Subcommand, p.Subcommand)
		return p, true, 2
	}
	return p, false, 0
}

// execOrReport execs claude. It returns only when the exec failed, or in tests where
// execve is replaced.
func execOrReport(argv []string, stderr io.Writer) int {
	if err := ExecClaude(slices.Clone(argv)); err != nil {
		fmt.Fprintf(stderr, "mantle: %v\n", err)
		return 127
	}
	return 0
}
