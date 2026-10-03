package testkit

import (
	"strings"
	"unicode/utf8"
)

var namedKeys = map[string]string{
	"enter": "\r", "return": "\r", "tab": "\t", "esc": "\x1b", "escape": "\x1b",
	"backspace": "\x7f", "space": " ", "delete": "\x1b[3~", "del": "\x1b[3~", "insert": "\x1b[2~",
	"up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C", "left": "\x1b[D",
	"home": "\x1b[H", "end": "\x1b[F", "pageup": "\x1b[5~", "pagedown": "\x1b[6~",
	"f1": "\x1bOP", "f2": "\x1bOQ", "f3": "\x1bOR", "f4": "\x1bOS",
}

// csiFinal is the final byte of xterm's modified-key form CSI 1;<mod><final> for keys
// that have one.
var csiFinal = map[string]byte{
	"up": 'A', "down": 'B', "right": 'C', "left": 'D', "home": 'H', "end": 'F',
}

// csiTilde is the number of keys sent as CSI <n>;<mod>~ when modified.
var csiTilde = map[string]int{
	"delete": 3, "del": 3, "insert": 2, "pageup": 5, "pagedown": 6,
}

// csiU is the code of keys sent in the kitty/CSI-u form when modified in a way the
// legacy encoding cannot express (shift+enter, ctrl+enter).
var csiU = map[string]int{"enter": 13, "return": 13, "tab": 9, "esc": 27, "escape": 27, "backspace": 127, "space": 32}

// KeySeq returns the bytes a terminal sends for a key written in keybindings.json
// syntax ("ctrl+c", "shift+tab", "alt+enter", "ctrl+up", "a", "?"). Modified keys the
// legacy encoding can't express use CSI u (as kitty, Ghostty, WezTerm and iTerm2 do).
func KeySeq(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	if key == "+" || utf8.RuneCountInString(key) == 1 {
		return key, true
	}
	parts := strings.Split(strings.ToLower(key), "+")
	base := parts[len(parts)-1]
	if base == "" { // "ctrl++"
		base = "+"
	}
	var ctrl, alt, shift bool
	for _, m := range parts[:len(parts)-1] {
		switch m {
		case "ctrl", "control":
			ctrl = true
		case "alt", "opt", "option", "meta":
			alt = true
		case "shift":
			shift = true
		default:
			return "", false
		}
	}
	// Preserve the case of a single-rune base from the original string.
	if utf8.RuneCountInString(base) == 1 {
		base = key[len(key)-len(base):]
	}
	mod := 1
	if shift {
		mod++
	}
	if alt {
		mod += 2
	}
	if ctrl {
		mod += 4
	}
	prefix := ""
	if alt {
		prefix = "\x1b"
	}

	if seq, ok := namedKeys[base]; ok {
		switch {
		case mod == 1:
			return seq, true
		case base == "tab" && shift && !ctrl && !alt:
			return "\x1b[Z", true
		case alt && !ctrl && !shift:
			return prefix + seq, true
		}
		if f, ok := csiFinal[base]; ok {
			return "\x1b[1;" + itoa(mod) + string(f), true
		}
		if n, ok := csiTilde[base]; ok {
			return "\x1b[" + itoa(n) + ";" + itoa(mod) + "~", true
		}
		if n, ok := csiU[base]; ok {
			return "\x1b[" + itoa(n) + ";" + itoa(mod) + "u", true
		}
		return "", false
	}

	r, size := utf8.DecodeRuneInString(base)
	if size != len(base) {
		return "", false
	}
	if ctrl {
		c, ok := ctrlByte(r)
		if !ok || shift {
			return "\x1b[" + itoa(int(r)) + ";" + itoa(mod) + "u", true
		}
		return prefix + string(c), true
	}
	if shift && r >= 'a' && r <= 'z' {
		r -= 'a' - 'A'
	}
	return prefix + string(r), true
}

func ctrlByte(r rune) (byte, bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return byte(r-'a') + 1, true
	case r == ' ' || r == '@' || r == '2':
		return 0, true
	case r == '[':
		return 0x1b, true
	case r == '\\':
		return 0x1c, true
	case r == ']':
		return 0x1d, true
	case r == '^':
		return 0x1e, true
	case r == '_' || r == '-' || r == '/':
		return 0x1f, true
	}
	return 0, false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// splitChord splits "ctrl+x ctrl+k" into its keys. A lone " " is the space key.
func splitChord(s string) []string {
	if s == " " {
		return []string{" "}
	}
	f := strings.Fields(s)
	if len(f) == 0 {
		return []string{s}
	}
	return f
}
