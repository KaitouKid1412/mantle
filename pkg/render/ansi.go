package render

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Width returns the display width of s in cells, ignoring escape sequences and
// measuring grapheme clusters (CJK and emoji are two cells wide).
func Width(s string) int { return ansi.StringWidth(s) }

// Strip removes every escape sequence from s.
func Strip(s string) string { return ansi.Strip(s) }

// Truncate cuts s to at most width cells, appending tail (counted in the width)
// when anything was cut. Escape sequences are kept intact.
func Truncate(s string, width int, tail string) string {
	if width <= 0 {
		return ""
	}
	if Width(s) <= width {
		return s
	}
	return closeLine(ansi.Truncate(s, width, tail))
}

// Pad right-pads s with spaces to exactly width cells (it never truncates).
func Pad(s string, width int) string {
	if w := Width(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

// escLen returns the length of the escape sequence that starts at s[i]
// (s[i] == ESC). A lone or malformed ESC has length 1.
func escLen(s string, i int) int {
	n := len(s)
	if i+1 >= n {
		return 1
	}
	switch c := s[i+1]; {
	case c == '[': // CSI: params 0x30-0x3F, intermediates 0x20-0x2F, final 0x40-0x7E
		j := i + 2
		for j < n && s[j] >= 0x20 && s[j] <= 0x3f {
			j++
		}
		if j < n && s[j] >= 0x40 && s[j] <= 0x7e {
			return j + 1 - i
		}
		return j - i
	case c == ']' || c == 'P' || c == 'X' || c == '^' || c == '_': // string sequences
		for j := i + 2; j < n; j++ {
			if s[j] == 0x07 && c == ']' {
				return j + 1 - i
			}
			if s[j] == 0x1b && j+1 < n && s[j+1] == '\\' {
				return j + 2 - i
			}
		}
		return n - i // unterminated: swallow the rest
	case c >= 0x20 && c <= 0x2f: // ESC intermediates final
		j := i + 1
		for j < n && s[j] >= 0x20 && s[j] <= 0x2f {
			j++
		}
		if j < n {
			j++
		}
		return j - i
	default:
		return 2
	}
}

// isSGR reports whether seq is a CSI … m sequence without a private prefix.
func isSGR(seq string) bool {
	if len(seq) < 3 || seq[0] != 0x1b || seq[1] != '[' || seq[len(seq)-1] != 'm' {
		return false
	}
	for i := 2; i < len(seq)-1; i++ {
		if c := seq[i]; (c < '0' || c > '9') && c != ';' && c != ':' {
			return false
		}
	}
	return true
}

// isOSC8 reports whether seq is an OSC 8 hyperlink sequence, and returns its URI
// ("" for the closing sequence).
func isOSC8(seq string) (uri string, ok bool) {
	if !strings.HasPrefix(seq, "\x1b]8;") {
		return "", false
	}
	body := seq[4:]
	body = strings.TrimSuffix(body, "\x07")
	body = strings.TrimSuffix(body, "\x1b\\")
	_, uri, found := strings.Cut(body, ";")
	if !found {
		return "", false
	}
	return uri, true
}

// lineState tracks the SGR attributes and hyperlink active at a point in a
// styled string, so a wrapped line can be closed and the next one reopened.
type lineState struct {
	sgr  string // SGR sequences since the last reset, in order
	link string // open OSC 8 sequence, "" if none
}

func (st *lineState) apply(seq string) {
	if isSGR(seq) {
		params := seq[2 : len(seq)-1]
		switch {
		case params == "" || params == "0":
			st.sgr = ""
		case strings.HasPrefix(params, "0;"):
			st.sgr = seq
		default:
			st.sgr += seq
		}
		return
	}
	if uri, ok := isOSC8(seq); ok {
		if uri == "" {
			st.link = ""
		} else {
			st.link = seq
		}
	}
}

// close returns the sequences that end the state at a line end.
func (st *lineState) close() string {
	var b string
	if st.sgr != "" {
		b += ansi.ResetStyle
	}
	if st.link != "" {
		b += ansi.ResetHyperlink()
	}
	return b
}

// reopen returns the sequences that restore the state at a line start.
func (st *lineState) reopen() string { return st.link + st.sgr }

// closeLine appends whatever is needed to end every style and link left open in s.
func closeLine(s string) string {
	var st lineState
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			n := escLen(s, i)
			st.apply(s[i : i+n])
			i += n
			continue
		}
		i++
	}
	return s + st.close()
}

// SanitizeOptions controls which escape sequences Sanitize keeps.
type SanitizeOptions struct {
	KeepSGR   bool // keep colour/attribute sequences
	KeepLinks bool // keep OSC 8 hyperlinks
}

// Sanitize neutralises untrusted text (tool output, engine text) for display:
// it drops escape sequences (except SGR/OSC 8 when asked), C0/C1 control
// characters other than newline and tab, and invalid UTF-8; it applies carriage
// returns the way a terminal would for progress output (the text after the last
// \r on a line wins) and normalises CRLF.
func Sanitize(s string, o SanitizeOptions) string {
	if isPlain(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	lineStart := 0 // index in b where the current output line starts
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1b:
			n := escLen(s, i)
			seq := s[i : i+n]
			if (o.KeepSGR && isSGR(seq)) || (o.KeepLinks && isSafeOSC8(seq)) {
				b.WriteString(seq)
			}
			i += n
		case c == '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				i++ // CRLF: let the \n case handle it
				continue
			}
			// Bare CR: later text overwrites this line. Keep it simple and
			// restart the line, unless the CR ends the input or the line.
			if i+1 < len(s) && s[i+1] != '\r' {
				out := b.String()[:lineStart]
				b.Reset()
				b.WriteString(out)
			}
			i++
		case c == '\n':
			b.WriteByte('\n')
			lineStart = b.Len()
			i++
		case c == '\t':
			b.WriteByte('\t')
			i++
		case c < 0x20 || c == 0x7f:
			i++
		case c < utf8.RuneSelf:
			b.WriteByte(c)
			i++
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			switch {
			case r == utf8.RuneError && n == 1:
				b.WriteRune(utf8.RuneError)
			case r >= 0x80 && r <= 0x9f: // C1 controls
			case r == 0x2028 || r == 0x2029: // line/paragraph separators
				b.WriteByte('\n')
				lineStart = b.Len()
			default:
				b.WriteString(s[i : i+n])
			}
			i += n
		}
	}
	return b.String()
}

// isSafeOSC8 accepts hyperlinks with printable, reasonably short URIs.
func isSafeOSC8(seq string) bool {
	uri, ok := isOSC8(seq)
	if !ok || len(uri) > 2048 {
		return false
	}
	for i := 0; i < len(uri); i++ {
		if uri[i] < 0x20 || uri[i] == 0x7f {
			return false
		}
	}
	return true
}

// isPlain reports whether s contains only printable ASCII, tabs and newlines.
func isPlain(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x80 || c == 0x7f || (c < 0x20 && c != '\n' && c != '\t') {
			return false
		}
	}
	return true
}

// ExpandTabs replaces tabs with spaces up to the next multiple of tabWidth,
// counting columns per line and ignoring escape sequences.
func ExpandTabs(s string, tabWidth int) string { return ExpandTabsFrom(s, tabWidth, 0) }

// ExpandTabsFrom is ExpandTabs for text whose lines start at column start of
// the screen, so tab stops land where a terminal would put them.
func ExpandTabsFrom(s string, tabWidth, start int) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	if tabWidth <= 0 {
		tabWidth = 4
	}
	var b strings.Builder
	col := start
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1b:
			n := escLen(s, i)
			b.WriteString(s[i : i+n])
			i += n
		case c == '\t':
			n := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
			i++
		case c == '\n':
			b.WriteByte('\n')
			col = start
			i++
		default:
			g, w := nextGrapheme(s, i)
			b.WriteString(g)
			col += w
			i += len(g)
		}
	}
	return b.String()
}

// nextGrapheme returns the grapheme cluster starting at s[i] and its width.
// Callers handle ESC, tab and newline before calling.
func nextGrapheme(s string, i int) (string, int) {
	c := s[i]
	if c < utf8.RuneSelf && (i+1 == len(s) || s[i+1] < utf8.RuneSelf) {
		if c < 0x20 || c == 0x7f {
			return s[i : i+1], 0
		}
		return s[i : i+1], 1
	}
	g, w := ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
	if g == "" { // defensive: never stall
		_, n := utf8.DecodeRuneInString(s[i:])
		return s[i : i+n], 1
	}
	return g, w
}
