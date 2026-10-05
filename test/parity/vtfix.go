package parity

// stringSanitizer works around an x/vt parser limit. Inside OSC, DCS, APC, PM and SOS
// strings the parser takes the bytes 0x80–0x9F for 8-bit C1 controls, so a UTF-8
// window title such as "✳ Claude Code" (✳ is E2 9C B3, and 0x9C is ST) ends the
// string early and the rest of the title is printed onto the screen. Real terminals
// decode those strings as UTF-8.
//
// The sanitizer replaces every non-ASCII byte inside such a string with '?'. That
// keeps each string's length and terminator, and never touches visible text. It is
// streaming: a string may span writes.
type stringSanitizer struct{ state int }

const (
	ssGround    = iota
	ssEsc       // after ESC
	ssString    // inside OSC/DCS/APC/PM/SOS
	ssStringEsc // ESC inside a string: ST, or an abort that starts a new sequence
)

// filter returns p with string payloads sanitized. It never changes p's length.
func (s *stringSanitizer) filter(p []byte) []byte {
	out := p
	copied := false
	for i, b := range p {
		switch s.state {
		case ssGround:
			if b == 0x1b {
				s.state = ssEsc
			}
		case ssEsc, ssStringEsc:
			switch {
			case s.state == ssStringEsc && b == '\\':
				s.state = ssGround
			case b == ']' || b == 'P' || b == '_' || b == '^' || b == 'X':
				s.state = ssString
			case b == 0x1b:
				s.state = ssEsc
			default:
				s.state = ssGround
			}
		case ssString:
			switch {
			case b == 0x07 || b == 0x18 || b == 0x1a: // BEL ends an OSC; CAN and SUB abort
				s.state = ssGround
			case b == 0x1b:
				s.state = ssStringEsc
			case b >= 0x80:
				if !copied {
					out, copied = append([]byte(nil), p...), true
				}
				out[i] = '?'
			}
		}
	}
	return out
}
