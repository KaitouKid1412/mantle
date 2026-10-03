package theme

import (
	"image/color"
	"testing"
)

func TestBuiltinsComplete(t *testing.T) {
	names := map[string]bool{}
	for _, th := range Builtins() {
		names[th.Name] = true
		if m := th.Missing(); len(m) > 0 {
			t.Errorf("%s: missing tokens %v", th.Name, m)
		}
	}
	for _, n := range []string{NameDark, NameLight, NameDarkDaltonized, NameLightDaltonized, NameDarkANSI, NameLightANSI} {
		if !names[n] {
			t.Errorf("builtin %q missing", n)
		}
	}
}

func TestParseColor(t *testing.T) {
	cases := []struct {
		in string
		ok bool
		r  uint8
	}{
		{"#ff0000", true, 0xff},
		{"#f00", true, 0xff},
		{"rgb(255, 0, 0)", true, 0xff},
		{"rgb(256,0,0)", false, 0},
		{"#ggg", false, 0},
		{"", false, 0},
		{"nope", false, 0},
	}
	for _, c := range cases {
		got, ok := ParseColor(c.in)
		if ok != c.ok {
			t.Errorf("%q: ok=%v want %v", c.in, ok, c.ok)
			continue
		}
		if ok {
			rgba := color.RGBAModel.Convert(got).(color.RGBA)
			if rgba.R != c.r {
				t.Errorf("%q: r=%d want %d", c.in, rgba.R, c.r)
			}
		}
	}
	for _, s := range []string{"ansi:red", "brightBlue", "bright-blue", "9", "200"} {
		if _, ok := ParseColor(s); !ok {
			t.Errorf("%q should parse", s)
		}
	}
}

func TestWithAndFallback(t *testing.T) {
	base := Default()
	red := MustColor("#ff0000")
	over := base.With(map[Token]color.Color{Accent: red})
	if over.Color(Accent) != red {
		t.Fatal("override not applied")
	}
	if base.Color(Accent) == red {
		t.Fatal("With mutated the base theme")
	}
	if over.Color("noSuchToken") != over.Color(Text) {
		t.Fatal("unknown token should fall back to Text")
	}
	var nilTheme *Theme
	if nilTheme.Color(Text) != nil {
		t.Fatal("nil theme should return nil colour")
	}
}
