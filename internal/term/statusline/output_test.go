package statusline

import (
	"slices"
	"testing"
)

func TestLines(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"\n\n  \n", nil},
		{"one\n", []string{"one"}},
		{"a\r\nb\r\n", []string{"a", "b"}},
		{"a\n\nb\n\n", []string{"a", "", "b"}},
		{"  lead\n", []string{"  lead"}},
	}
	for _, tt := range tests {
		if got := Lines([]byte(tt.in)); !slices.Equal(got, tt.want) {
			t.Errorf("Lines(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "hello ✓", "hello ✓"},
		{"sgr kept", "\x1b[1;32mok\x1b[0m", "\x1b[1;32mok\x1b[0m"},
		{"cursor moves dropped", "a\x1b[2J\x1b[Hb\x1b[10;5Hc", "abc"},
		{"osc8 kept, BEL normalised", "\x1b]8;;https://x.test\afoo\x1b]8;;\a", "\x1b]8;;https://x.test\x1b\\foo\x1b]8;;\x1b\\"},
		{"osc8 ST kept", "\x1b]8;id=1;u\x1b\\t\x1b]8;;\x1b\\", "\x1b]8;id=1;u\x1b\\t\x1b]8;;\x1b\\"},
		{"title osc dropped", "\x1b]0;evil\ax", "x"},
		{"clipboard osc dropped", "\x1b]52;c;Zm9v\x1b\\x", "x"},
		{"unterminated osc", "a\x1b]8;;u", "a"},
		{"unterminated csi", "a\x1b[31", "a"},
		{"other escapes", "a\x1b(Bb\x1b=c", "abc"},
		{"tabs and bells", "a\tb\a\bc\x7f", "a bc"},
		{"c1 controls", "a\u009b31mb", "a31mb"},
	}
	for _, tt := range tests {
		if got := Sanitize(tt.in); got != tt.want {
			t.Errorf("%s: Sanitize(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestPad(t *testing.T) {
	if got := Pad([]string{"a", ""}, 2); !slices.Equal(got, []string{"  a", "  "}) {
		t.Errorf("Pad = %q", got)
	}
	in := []string{"a"}
	if got := Pad(in, 0); &got[0] != &in[0] {
		t.Error("Pad(0) should return the input")
	}
	if Pad(nil, 3) != nil {
		t.Error("Pad(nil)")
	}
}
