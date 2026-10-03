package terminal

import "bytes"

const esc = 0x1b

// TmuxWrap wraps an escape sequence in tmux's DCS passthrough (ESC P tmux; … ESC \),
// doubling every ESC inside it, so it reaches the outer terminal. tmux forwards it only
// with `set -g allow-passthrough on`.
func TmuxWrap(seq []byte) []byte {
	if len(seq) == 0 {
		return nil
	}
	var b bytes.Buffer
	b.Grow(len(seq) + 16)
	b.WriteString("\x1bPtmux;")
	for _, c := range seq {
		if c == esc {
			b.WriteByte(esc)
		}
		b.WriteByte(c)
	}
	b.WriteString("\x1b\\")
	return b.Bytes()
}

// Passthrough returns seq wrapped for tmux when inTmux is set, else seq unchanged.
func Passthrough(seq []byte, inTmux bool) []byte {
	if inTmux {
		return TmuxWrap(seq)
	}
	return seq
}

// StripControls removes C0 and C1 control characters (and DEL) from s, replacing
// newlines and tabs with spaces. Use it on any text placed inside an OSC payload so the
// text cannot terminate the sequence or inject another one.
func StripControls(s string) string {
	var b []rune
	changed := false
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			b = append(b, ' ')
			changed = true
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			changed = true
		default:
			b = append(b, r)
		}
	}
	if !changed {
		return s
	}
	return string(b)
}
