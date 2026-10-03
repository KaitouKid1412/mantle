package theme

import (
	"image/color"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ansiNames maps the 16 terminal colour names to their indices. Names are accepted with
// or without an "ansi:" prefix, in camelCase or with separators ("brightRed",
// "bright-red", "bright_red").
var ansiNames = map[string]int{
	"black": 0, "red": 1, "green": 2, "yellow": 3, "blue": 4, "magenta": 5, "cyan": 6, "white": 7,
	"brightblack": 8, "brightred": 9, "brightgreen": 10, "brightyellow": 11,
	"brightblue": 12, "brightmagenta": 13, "brightcyan": 14, "brightwhite": 15,
	"gray": 8, "grey": 8,
}

// ParseColor parses a colour value from a theme file. Accepted forms:
//
//	"#rrggbb", "#rgb"          hex
//	"rgb(r, g, b)"             decimal components 0-255
//	"0".."255"                 ANSI 256 index
//	"ansi:red", "brightBlue"   one of the 16 terminal colours
//
// ok is false when the value is not understood.
func ParseColor(s string) (c color.Color, ok bool) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return nil, false
	case strings.HasPrefix(s, "#"):
		return parseHex(s[1:])
	case strings.HasPrefix(strings.ToLower(s), "rgb(") && strings.HasSuffix(s, ")"):
		parts := strings.Split(s[4:len(s)-1], ",")
		if len(parts) != 3 {
			return nil, false
		}
		var v [3]uint8
		for i, p := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil || n < 0 || n > 255 {
				return nil, false
			}
			v[i] = uint8(n)
		}
		return color.RGBA{R: v[0], G: v[1], B: v[2], A: 0xff}, true
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 || n > 255 {
			return nil, false
		}
		if n < 16 {
			return ansi.BasicColor(n), true
		}
		return ansi.IndexedColor(n), true
	}
	name := strings.ToLower(strings.TrimPrefix(s, "ansi:"))
	name = strings.NewReplacer("-", "", "_", "", " ", "").Replace(name)
	if i, ok := ansiNames[name]; ok {
		return ansi.BasicColor(i), true
	}
	return nil, false
}

func parseHex(h string) (color.Color, bool) {
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return nil, false
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return nil, false
	}
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}, true
}

// MustColor is ParseColor for literals in code; it panics on an invalid value.
func MustColor(s string) color.Color {
	c, ok := ParseColor(s)
	if !ok {
		panic("theme: invalid colour " + strconv.Quote(s))
	}
	return c
}
