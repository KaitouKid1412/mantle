package dialogs

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Sanitize makes untrusted text (tool input, MCP messages, plan text) safe to draw:
// escape sequences and control characters are removed, tabs become spaces, CRLF becomes
// LF, and bidirectional overrides (which can make a command read differently from what
// runs) are shown as visible markers. Newlines are kept.
func Sanitize(s string) string {
	s = ansi.Strip(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune(r)
		case r == '\t':
			b.WriteString("    ")
		case isBidiControl(r):
			b.WriteString("<U+")
			b.WriteString(strings.ToUpper(hex4(r)))
			b.WriteString(">")
		case r == unicode.ReplacementChar:
			b.WriteRune(r)
		case unicode.IsControl(r), r == 0x7f:
			// dropped: C0, C1 and DEL
		case unicode.Is(unicode.Cf, r) && r != 0x200d:
			// zero-width format characters other than ZWJ (emoji sequences)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SanitizeLine is Sanitize for single-line contexts: newlines become "⏎ ".
func SanitizeLine(s string) string {
	return strings.ReplaceAll(Sanitize(s), "\n", "⏎ ")
}

func isBidiControl(r rune) bool {
	return r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 || r == 0x200e || r == 0x200f || r == 0x061c
}

func hex4(r rune) string {
	const digits = "0123456789abcdef"
	out := []byte{'0', '0', '0', '0'}
	for i := 3; i >= 0; i-- {
		out[i] = digits[r&0xf]
		r >>= 4
	}
	return string(out)
}
