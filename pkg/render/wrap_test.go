package render

import (
	"image/color"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWrapPlain(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		{"fits", "hello world", 20, []string{"hello world"}},
		{"break at space", "hello world", 8, []string{"hello", "world"}},
		{"exact", "hello world", 11, []string{"hello world"}},
		{"multiple spaces collapse at break", "aaa   bbb", 4, []string{"aaa", "bbb"}},
		{"inner spaces kept", "a  b", 10, []string{"a  b"}},
		{"leading indent kept", "  indented text", 20, []string{"  indented text"}},
		{"hard newline", "one\ntwo", 10, []string{"one", "two"}},
		{"empty", "", 10, []string{""}},
		{"blank lines", "a\n\nb", 10, []string{"a", "", "b"}},
		{"long word hard-wrapped", "abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
		{"long word after text", "x abcdefghij", 4, []string{"x", "abcd", "efgh", "ij"}},
		{"hyphen break", "well-known fact", 6, []string{"well-", "known", "fact"}},
		{"leading hyphen no break", "-flag value", 6, []string{"-flag", "value"}},
		{"trailing spaces dropped", "foo   ", 10, []string{"foo"}},
		{"cjk", "日本語のテキスト", 6, []string{"日本語", "のテキ", "スト"}},
		{"cjk odd width", "日本語", 5, []string{"日本", "語"}},
		{"emoji", "👍👍👍 ok", 4, []string{"👍👍", "👍", "ok"}},
		{"zwj emoji is one cluster", "👩‍💻👩‍💻", 2, []string{"👩‍💻", "👩‍💻"}},
		{"tab expands", "a\tb", 10, []string{"a   b"}},
		{"long url", "see https://example.com/abcdefghijklmnop", 12,
			[]string{"see", "https://exam", "ple.com/abcd", "efghijklmnop"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Wrap(tt.in, tt.width)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Wrap(%q, %d)\n got %q\nwant %q", tt.in, tt.width, got, tt.want)
			}
			for _, l := range got {
				if w := Width(l); w > tt.width && tt.width > 1 {
					t.Errorf("line %q is %d wide, limit %d", l, w, tt.width)
				}
			}
		})
	}
}

func TestHardWrap(t *testing.T) {
	got := HardWrap("func main() {   x := 1 }", 10)
	want := []string{"func main(", ") {   x :=", " 1 }"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	got = HardWrap("  indented\n\tline", 6)
	want = []string{"  inde", "nted", "    li", "ne"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestWrapIndent(t *testing.T) {
	got := WrapIndent("one two three four five", 12, "- ", "  ")
	want := []string{"- one two", "  three four", "  five"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	// Styled prefixes are measured by display width.
	pre := Style{Faint: true}.Render("│ ")
	got = WrapIndent("alpha beta gamma", 8, pre, pre)
	if len(got) != 3 || Strip(got[1]) != "│ beta" {
		t.Fatalf("got %q", got)
	}
}

func TestWrapCarriesStyle(t *testing.T) {
	bold := Style{Bold: true, Fg: color.RGBA{R: 255, A: 255}}
	in := "plain " + bold.Render("bold words here") + " tail"
	got := Wrap(in, 10)
	if strings.Join(stripAll(got), "|") != "plain bold|words here|tail" {
		t.Fatalf("text: %q", stripAll(got))
	}
	open := bold.Open()
	// Line 1 ends inside the bold run: it must be closed.
	if !strings.HasSuffix(got[0], ansi.ResetStyle) {
		t.Errorf("line 0 not closed: %q", got[0])
	}
	// Line 2 starts inside the bold run: it must be reopened.
	if !strings.HasPrefix(got[1], open) {
		t.Errorf("line 1 not reopened: %q", got[1])
	}
	// Line 3 is outside the run: no style.
	if got[2] != "tail" {
		t.Errorf("line 2 = %q", got[2])
	}
}

func TestWrapCarriesLink(t *testing.T) {
	in := Link("https://example.com", "a long link text")
	got := Wrap(in, 7)
	if len(got) != 3 {
		t.Fatalf("got %q", got)
	}
	for i, l := range got {
		if !strings.Contains(l, ansi.SetHyperlink("https://example.com")) {
			t.Errorf("line %d lost the link: %q", i, l)
		}
		if !strings.HasSuffix(l, ansi.ResetHyperlink()) {
			t.Errorf("line %d not closed: %q", i, l)
		}
	}
}

func TestWrapStyledLeadingAndTrailingEscapes(t *testing.T) {
	s := Style{Italic: true}
	got := Wrap(s.Render("  quoted  "), 20)
	if len(got) != 1 || Strip(got[0]) != "  quoted" {
		t.Fatalf("got %q", got)
	}
	if !strings.HasPrefix(got[0], s.Open()) || !strings.HasSuffix(got[0], ansi.ResetStyle) {
		t.Fatalf("escapes lost or reordered: %q", got[0])
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		o    SanitizeOptions
		want string
	}{
		{"plain", "hello\tworld\n", SanitizeOptions{}, "hello\tworld\n"},
		{"strip sgr", "\x1b[31mred\x1b[0m", SanitizeOptions{}, "red"},
		{"keep sgr", "\x1b[31mred\x1b[0m", SanitizeOptions{KeepSGR: true}, "\x1b[31mred\x1b[0m"},
		{"drop cursor moves", "a\x1b[2Jb\x1b[10;10Hc", SanitizeOptions{KeepSGR: true}, "abc"},
		{"drop osc title", "\x1b]0;evil\x07ok", SanitizeOptions{KeepSGR: true}, "ok"},
		{"keep link", "\x1b]8;;http://x\x07t\x1b]8;;\x07", SanitizeOptions{KeepLinks: true}, "\x1b]8;;http://x\x07t\x1b]8;;\x07"},
		{"drop link", "\x1b]8;;http://x\x07t\x1b]8;;\x07", SanitizeOptions{}, "t"},
		{"crlf", "a\r\nb", SanitizeOptions{}, "a\nb"},
		{"progress cr", "10%\r20%\r30%\ndone", SanitizeOptions{}, "30%\ndone"},
		{"bell and nul", "a\x07b\x00c", SanitizeOptions{}, "abc"},
		{"c1", "a\u009bb", SanitizeOptions{}, "ab"},
		{"invalid utf8", "a\xffb", SanitizeOptions{}, "a�b"},
		{"unterminated osc", "ok\x1b]0;never ends", SanitizeOptions{}, "ok"},
		{"dcs", "a\x1bPq#0;2;0;0;0\x1b\\b", SanitizeOptions{}, "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sanitize(tt.in, tt.o); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	s := Style{Bold: true}.Render("hello world")
	got := Truncate(s, 8, "…")
	if Strip(got) != "hello w…" || Width(got) != 8 {
		t.Fatalf("got %q", got)
	}
	if !strings.HasSuffix(got, ansi.ResetStyle) {
		t.Fatalf("not closed: %q", got)
	}
	if Truncate("short", 8, "…") != "short" {
		t.Fatal("short string changed")
	}
}

func stripAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = Strip(l)
	}
	return out
}

func FuzzWrap(f *testing.F) {
	f.Add("hello world, this is a test", 7)
	f.Add("日本語 👍 \x1b[1mbold\x1b[m text-with-hyphens", 5)
	f.Fuzz(func(t *testing.T, s string, width int) {
		if width < 2 || width > 200 {
			return
		}
		s = Sanitize(s, SanitizeOptions{KeepSGR: true})
		lines := Wrap(s, width)
		for _, l := range lines {
			if w := Width(l); w > width {
				// a single wide grapheme can exceed a tiny width; nothing else may
				if Width(strings.TrimSpace(Strip(l))) > 2 || width > 2 {
					t.Fatalf("line %q width %d > %d", l, w, width)
				}
			}
		}
		// No text is lost except spaces.
		in := strings.Join(strings.Fields(Strip(ExpandTabs(s, 4))), "")
		out := strings.Join(strings.Fields(strings.Join(stripAll(lines), "")), "")
		if in != out {
			t.Fatalf("text changed:\n in %q\nout %q", in, out)
		}
	})
}
