package testkit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
)

// Normalize trims trailing spaces on every line and trailing blank lines, so goldens
// don't depend on how a renderer pads.
func Normalize(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return joinTrim(lines) + "\n"
}

// RequireGolden compares out (normalised) with testdata/<TestName>.golden. Run the test
// with -update to rewrite the file. ANSI sequences are kept and shown escaped in diffs.
func RequireGolden(tb testing.TB, out string) {
	tb.Helper()
	golden.RequireEqual(tb, Normalize(out))
}

// RequireGoldenLines is RequireGolden for a slice of lines.
func RequireGoldenLines(tb testing.TB, lines []string) {
	tb.Helper()
	RequireGolden(tb, strings.Join(lines, "\n"))
}
