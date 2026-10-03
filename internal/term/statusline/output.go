package statusline

import "strings"

// MaxOutput caps how much command output is kept.
const MaxOutput = 64 << 10

// Lines turns command output into display lines: trailing blank lines are dropped,
// carriage returns removed, and each line is sanitized (SGR colours and OSC 8 links
// kept, every other control sequence dropped, so a script cannot move the cursor or
// clear the screen under the inline renderer).
func Lines(out []byte) []string {
	s := strings.TrimRight(string(out), " \t\r\n")
	if s == "" {
		return nil
	}
	raw := strings.Split(s, "\n")
	lines := make([]string, 0, len(raw))
	for _, l := range raw {
		lines = append(lines, Sanitize(strings.TrimRight(l, "\r")))
	}
	return lines
}

// Sanitize keeps printable text, SGR sequences (CSI … m) and OSC 8 hyperlinks from one
// line; tabs become a space and all other control bytes and sequences are removed.
func Sanitize(s string) string {
	if isPlain(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1b && i+1 < len(s) && s[i+1] == '[':
			end := csiEnd(s, i+2)
			if end < 0 {
				return b.String()
			}
			if s[end] == 'm' {
				b.WriteString(s[i : end+1])
			}
			i = end + 1
		case c == 0x1b && i+1 < len(s) && s[i+1] == ']':
			body, next := oscBody(s, i+2)
			if next < 0 {
				return b.String()
			}
			if strings.HasPrefix(body, "8;") {
				b.WriteString("\x1b]")
				b.WriteString(body)
				b.WriteString("\x1b\\")
			}
			i = next
		case c == 0x1b && i+1 < len(s) && strings.IndexByte("PX^_", s[i+1]) >= 0:
			// DCS, SOS, PM and APC strings: drop through the terminator.
			_, next := oscBody(s, i+2)
			if next < 0 {
				return b.String()
			}
			i = next
		case c == 0x1b:
			// Other escapes (charset, keypad, …): ESC, intermediates 0x20–0x2F, final.
			i++
			for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
				i++
			}
			i++
		case c == '\t':
			b.WriteByte(' ')
			i++
		case c < 0x20 || c == 0x7f:
			i++
		case c == 0xc2 && i+1 < len(s) && s[i+1] >= 0x80 && s[i+1] < 0xa0:
			// C1 controls encoded as UTF-8 (U+0080–U+009F).
			i += 2
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// csiEnd returns the index of the CSI final byte (0x40–0x7E) at or after i, or -1.
func csiEnd(s string, i int) int {
	for ; i < len(s); i++ {
		c := s[i]
		if c >= 0x40 && c <= 0x7e {
			return i
		}
		if c < 0x20 || c > 0x7e {
			return -1
		}
	}
	return -1
}

// oscBody returns the OSC payload starting at i and the index after its terminator
// (BEL or ST), or next = -1 when unterminated.
func oscBody(s string, i int) (body string, next int) {
	for j := i; j < len(s); j++ {
		switch {
		case s[j] == 0x07:
			return s[i:j], j + 1
		case s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\':
			return s[i:j], j + 2
		case s[j] == 0x1b:
			return "", -1
		}
	}
	return "", -1
}

// isPlain reports whether s has no C0, DEL or C1 control characters.
func isPlain(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			return false
		}
		if c == 0xc2 && i+1 < len(s) && s[i+1] >= 0x80 && s[i+1] < 0xa0 {
			return false
		}
	}
	return true
}

// Pad indents every line by n spaces (statusLine.padding).
func Pad(lines []string, n int) []string {
	if n <= 0 || len(lines) == 0 {
		return lines
	}
	pad := strings.Repeat(" ", n)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = pad + l
	}
	return out
}
