package render

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
)

var goldenWidths = []int{40, 60, 100, 160}

var markdownCases = []struct{ name, src string }{
	{"headings", "# Heading one\n\n## Heading two\n\n### Heading three\n\n#### Heading four\n\nBody text under the headings, long enough to wrap at the narrow widths we test."},
	{"inline", "Plain, *italic*, **bold**, ***both***, ~~struck~~, `inline code`, and a backslash \\* escape &amp; an entity."},
	{"lists", "- one\n- two\n  - nested a\n  - nested b\n    - deeper\n- three with a long line of text that wraps onto a second line at narrow widths\n\n1. first\n2. second\n   1. sub one\n   2. sub two\n      1. subsub\n3. third\n\n- [ ] open task\n- [x] finished task"},
	{"loose-list", "- item one\n\n  second paragraph of item one\n\n- item two"},
	{"blockquote", "> Quoted text that is long enough to wrap when the terminal is narrow.\n>\n> > Nested quote.\n\nAfter the quote."},
	{"code", "```python\ndef greet(name: str) -> str:\n    # say hello\n    return f\"hello, {name}\"  # a trailing comment that makes this line long\n```\n\n```\nno language here\n\tindented with a tab\n```\n\n    indented code block"},
	{"table", "| Left | Right | Center |\n|:-----|------:|:------:|\n| a | 1 | x |\n| a much longer cell value that will need wrapping | 12345 | centered text |"},
	{"wide-table", "| one | two | three | four | five | six | seven | eight | nine | ten |\n|---|---|---|---|---|---|---|---|---|---|\n| alpha | beta | gamma | delta | epsilon | zeta | eta | theta | iota | kappa |"},
	{"links", "See [the docs](https://example.com/docs/getting-started) or <https://example.org> or https://bare.example.net/path, mail <user@example.com>, and [https://same.example.com](https://same.example.com).\n\n![diagram](https://example.com/d.png)"},
	{"long-url", "Download from https://downloads.example.com/releases/2026/10/very-long-artifact-name-with-many-parts-v1.2.3-darwin-arm64.tar.gz then verify."},
	{"cjk", "日本語の文章は単語の区切りがないので、幅に合わせて文字単位で折り返す必要があります。中文也是一样的。한국어 문장은 띄어쓰기가 있습니다."},
	{"emoji", "Status: ✅ done, 🚧 in progress, 👩‍💻 developer, 🇯🇵 flag, and 👍🏽 skin tone — all two cells wide."},
	{"rule", "Above\n\n---\n\nBelow"},
	{"html", "<details>\n<summary>Click</summary>\n\nHidden\n</details>\n\nLine one<br>line two with <kbd>Ctrl</kbd>."},
	{"hostile", "Text with \x1b[31mred\x1b[0m and \x1b]0;title\x07 and a bell\x07 and &#27;[2J entity."},
}

func TestMarkdownGolden(t *testing.T) {
	for _, tc := range markdownCases {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			for _, w := range goldenWidths {
				lines := Markdown(tc.src, MarkdownOptions{Width: w, Palette: testPalette})
				checkLines(t, lines, w)
				fmt.Fprintf(&b, "── width %d ──\n", w)
				for _, l := range lines {
					b.WriteString(l)
					b.WriteByte('\n')
				}
			}
			golden.RequireEqual(t, b.String())
		})
	}
}

// checkLines asserts the invariants every rendered line must satisfy.
func checkLines(t *testing.T, lines []string, width int) {
	t.Helper()
	for i, l := range lines {
		if w := Width(l); w > width {
			t.Errorf("line %d is %d cells wide, limit %d: %q", i, w, width, Strip(l))
		}
		if strings.ContainsAny(Strip(l), "\n\r\x07\x00") {
			t.Errorf("line %d holds control characters: %q", i, l)
		}
		var st lineState
		for j := 0; j < len(l); j++ {
			if l[j] == 0x1b {
				n := escLen(l, j)
				seq := l[j : j+n]
				if !isSGR(seq) {
					if _, ok := isOSC8(seq); !ok {
						t.Errorf("line %d holds a non-style escape %q", i, seq)
					}
				}
				st.apply(seq)
				j += n - 1
			}
		}
		if st.sgr != "" || st.link != "" {
			t.Errorf("line %d leaves a style or link open: %q", i, l)
		}
	}
}

func TestMarkdownMaxProseWidth(t *testing.T) {
	src := "A paragraph of prose that is long enough to be capped by the maximum prose width setting.\n\n```\n" +
		strings.Repeat("x", 70) + "\n```"
	lines := Markdown(src, MarkdownOptions{Width: 100, MaxProseWidth: 40})
	for _, l := range lines[:3] {
		if Width(l) > 40 {
			t.Fatalf("prose line exceeds the cap: %q", l)
		}
	}
	if last := lines[len(lines)-1]; Width(last) != 70 {
		t.Fatalf("code should use the full width, got %q", last)
	}
}

func TestMarkdownNoHyperlinks(t *testing.T) {
	lines := Markdown("[a](https://x.example)", MarkdownOptions{Width: 80, NoHyperlinks: true})
	if strings.Contains(lines[0], "\x1b]8;") {
		t.Fatalf("hyperlink emitted: %q", lines[0])
	}
	if Strip(lines[0]) != "a (https://x.example)" {
		t.Fatalf("got %q", Strip(lines[0]))
	}
	lines = Markdown("[a](https://x.example)", MarkdownOptions{Width: 80})
	if !strings.Contains(lines[0], ansi.SetHyperlink("https://x.example")) {
		t.Fatalf("hyperlink missing: %q", lines[0])
	}
}

func TestMarkdownNoHighlight(t *testing.T) {
	src := "```go\nfunc main() {}\n```"
	on := Markdown(src, MarkdownOptions{Width: 80, Palette: testPalette})
	off := Markdown(src, MarkdownOptions{Width: 80, Palette: testPalette, NoHighlight: true})
	if reflect.DeepEqual(on, off) {
		t.Fatal("NoHighlight changed nothing")
	}
	if Strip(on[0]) != Strip(off[0]) {
		t.Fatal("text differs")
	}
	if want := Fg(testPalette, TokSyntaxPlain).Render("func main() {}"); off[0] != want {
		t.Fatalf("unhighlighted line = %q, want %q", off[0], want)
	}
}

func TestMarkdownEmpty(t *testing.T) {
	if got := Markdown("", MarkdownOptions{Width: 80}); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
	if got := Markdown("\n\n  \n", MarkdownOptions{Width: 80}); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestOrderedMarkers(t *testing.T) {
	for _, tc := range []struct {
		n, depth int
		want     string
	}{{1, 0, "1"}, {12, 0, "12"}, {1, 1, "a"}, {26, 1, "z"}, {27, 1, "aa"}, {4, 2, "iv"}, {9, 2, "ix"}, {1, 3, "1"}} {
		if got := orderedMarker(tc.n, tc.depth); got != tc.want {
			t.Errorf("orderedMarker(%d, %d) = %q, want %q", tc.n, tc.depth, got, tc.want)
		}
	}
}

func BenchmarkMarkdownOneShot(b *testing.B) {
	src := readTestdata(b, "answer.md")
	o := MarkdownOptions{Width: 100, Palette: testPalette}
	b.ReportAllocs()
	for b.Loop() {
		_ = Markdown(src, o)
	}
}
