package install

import (
	"regexp"
	"strings"
	"sync"
)

// screenBuffer accumulates pty output with escape sequences removed, and a
// read position for Expect.
type screenBuffer struct {
	mu        sync.Mutex
	text      strings.Builder
	pos       int
	tailBytes []byte // incomplete escape sequence carried to the next write
}

func (s *screenBuffer) write(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := append(s.tailBytes, p...)
	clean, rest := stripANSI(data)
	s.tailBytes = rest
	s.text.WriteString(clean)
}

// consume reports whether re matches the text after the read position, and
// moves the position past the match.
func (s *screenBuffer) consume(re *regexp.Regexp) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.text.String()[s.pos:]
	loc := re.FindStringIndex(t)
	if loc == nil {
		return false
	}
	s.pos += loc[1]
	return true
}

func (s *screenBuffer) tail(n int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.text.String()
	return t[max(0, len(t)-n):]
}

// stripANSI removes CSI, OSC, DCS and two-byte escape sequences and other
// control characters except newlines. An escape sequence cut off at the end
// is returned as rest, to be completed by the next write.
func stripANSI(p []byte) (clean string, rest []byte) {
	var b strings.Builder
	i := 0
	for i < len(p) {
		c := p[i]
		if c != 0x1b {
			switch {
			case c == '\n':
				b.WriteByte('\n')
			case c == '\r' || c == '\t':
				b.WriteByte(' ')
			case c < 0x20 || c == 0x7f:
			default:
				b.WriteByte(c)
			}
			i++
			continue
		}
		if i+1 >= len(p) {
			return b.String(), append([]byte(nil), p[i:]...)
		}
		switch p[i+1] {
		case '[': // CSI ... final byte 0x40-0x7e
			j := i + 2
			for j < len(p) && (p[j] < 0x40 || p[j] > 0x7e) {
				j++
			}
			if j >= len(p) {
				return b.String(), append([]byte(nil), p[i:]...)
			}
			if p[j] != 'm' {
				b.WriteByte(' ') // cursor moves often separate words; SGR does not move
			}
			i = j + 1
		case ']', 'P', '_', '^': // OSC, DCS, APC, PM: until BEL or ST
			j := i + 2
			for j < len(p) && p[j] != 0x07 && !(p[j] == 0x1b && j+1 < len(p) && p[j+1] == '\\') {
				j++
			}
			if j >= len(p) || (p[j] == 0x1b && j+1 >= len(p)) {
				return b.String(), append([]byte(nil), p[i:]...)
			}
			if p[j] == 0x07 {
				i = j + 1
			} else {
				i = j + 2
			}
		default: // ESC, intermediate bytes 0x20-0x2f, final byte
			j := i + 1
			for j < len(p) && p[j] >= 0x20 && p[j] <= 0x2f {
				j++
			}
			if j >= len(p) {
				return b.String(), append([]byte(nil), p[i:]...)
			}
			i = j + 1
		}
	}
	return b.String(), nil
}
