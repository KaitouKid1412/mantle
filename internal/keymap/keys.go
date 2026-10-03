package keymap

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// Canonical key form: modifiers in the fixed order ctrl, alt, shift, super, then the
// key name, joined by "+": "ctrl+shift+b", "alt+enter", "escape", "?". "meta", "opt"
// and "option" are alt (terminals cannot tell them apart); "cmd", "command" and "win"
// are super. A chord is canonical keys joined by single spaces.

var keyAliases = map[string]string{
	"esc": "escape", "return": "enter", "del": "delete", " ": "space",
	"pgup": "pageup", "pgdown": "pagedown", "page_up": "pageup", "page_down": "pagedown",
	"↑": "up", "↓": "down", "←": "left", "→": "right",
}

// namedKeys are the multi-letter key names accepted in bindings.
var namedKeys = map[string]bool{
	"escape": true, "enter": true, "tab": true, "space": true, "backspace": true,
	"delete": true, "insert": true, "up": true, "down": true, "left": true, "right": true,
	"home": true, "end": true, "pageup": true, "pagedown": true,
	"wheelup": true, "wheeldown": true, "capslock": true,
	"f1": true, "f2": true, "f3": true, "f4": true, "f5": true, "f6": true,
	"f7": true, "f8": true, "f9": true, "f10": true, "f11": true, "f12": true,
}

type mods struct{ ctrl, alt, shift, super bool }

func (m mods) prefix() string {
	var b strings.Builder
	if m.ctrl {
		b.WriteString("ctrl+")
	}
	if m.alt {
		b.WriteString("alt+")
	}
	if m.shift {
		b.WriteString("shift+")
	}
	if m.super {
		b.WriteString("super+")
	}
	return b.String()
}

// CanonicalKey normalises one keystroke written in keybindings.json syntax.
func CanonicalKey(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("empty key")
	}
	if s == "+" || s == " " {
		if s == " " {
			return "space", nil
		}
		return "+", nil
	}
	parts := strings.Split(s, "+")
	if strings.HasSuffix(s, "++") { // "ctrl++"
		parts = append(parts[:len(parts)-2], "+")
	}
	var m mods
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "ctrl", "control":
			m.ctrl = true
		case "alt", "opt", "option", "meta":
			m.alt = true
		case "shift":
			m.shift = true
		case "cmd", "command", "super", "win":
			m.super = true
		default:
			return "", fmt.Errorf("unknown modifier %q in %q", p, s)
		}
	}
	// Key names are case-insensitive, as in Claude Code: "G" is "g"; write "shift+g"
	// for a capital G.
	key := strings.ToLower(parts[len(parts)-1])
	if utf8.RuneCountInString(key) == 1 {
		if a, ok := keyAliases[key]; ok {
			key = a
		}
		return m.prefix() + key, nil
	}
	if a, ok := keyAliases[key]; ok {
		key = a
	}
	if !namedKeys[key] {
		return "", fmt.Errorf("unknown key %q in %q", key, s)
	}
	return m.prefix() + key, nil
}

// CanonicalChord normalises a space-separated chord ("ctrl+x ctrl+k").
func CanonicalChord(s string) (string, error) {
	if s == " " {
		return "space", nil
	}
	f := strings.Fields(s)
	if len(f) == 0 {
		return "", fmt.Errorf("empty chord")
	}
	for i, k := range f {
		c, err := CanonicalKey(k)
		if err != nil {
			return "", err
		}
		f[i] = c
	}
	return strings.Join(f, " "), nil
}

var teaKeyNames = map[rune]string{
	tea.KeyEnter: "enter", tea.KeyTab: "tab", tea.KeyBackspace: "backspace",
	tea.KeyEscape: "escape", tea.KeySpace: "space", tea.KeyUp: "up", tea.KeyDown: "down",
	tea.KeyLeft: "left", tea.KeyRight: "right", tea.KeyInsert: "insert", tea.KeyDelete: "delete",
	tea.KeyHome: "home", tea.KeyEnd: "end", tea.KeyPgUp: "pageup", tea.KeyPgDown: "pagedown",
	tea.KeyCapsLock: "capslock",
	tea.KeyF1:       "f1", tea.KeyF2: "f2", tea.KeyF3: "f3", tea.KeyF4: "f4", tea.KeyF5: "f5",
	tea.KeyF6: "f6", tea.KeyF7: "f7", tea.KeyF8: "f8", tea.KeyF9: "f9", tea.KeyF10: "f10",
	tea.KeyF11: "f11", tea.KeyF12: "f12",
}

// EventKeys returns the canonical forms a key press can match, most specific first.
// A printable symbol typed with shift ("?" on a US layout) matches both "shift+/"-style
// forms (kitty) and the plain symbol.
func EventKeys(k tea.KeyPressMsg) []string {
	m := mods{
		ctrl:  k.Mod.Contains(tea.ModCtrl),
		alt:   k.Mod.Contains(tea.ModAlt) || k.Mod.Contains(tea.ModMeta),
		shift: k.Mod.Contains(tea.ModShift),
		super: k.Mod.Contains(tea.ModSuper),
	}
	name, ok := teaKeyNames[k.Code]
	if !ok {
		code := k.Code
		if k.BaseCode != 0 {
			code = k.BaseCode
		}
		if code == tea.KeyExtended || code == 0 {
			if k.Text == "" {
				return nil
			}
			return []string{m.prefix() + k.Text}
		}
		name = string(unicode.ToLower(code))
	}
	out := []string{m.prefix() + name}
	// Shifted symbols: the text is what the user meant ("?" rather than "shift+/").
	if t := k.Text; t != "" && utf8.RuneCountInString(t) == 1 && t != " " {
		r, _ := utf8.DecodeRuneInString(t)
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			m2 := m
			m2.shift = false
			if alt := m2.prefix() + t; alt != out[0] {
				out = append(out, alt)
			}
		}
	}
	return out
}

// WheelKey is the canonical key for a mouse wheel event.
func WheelKey(up bool) string {
	if up {
		return "wheelup"
	}
	return "wheeldown"
}
