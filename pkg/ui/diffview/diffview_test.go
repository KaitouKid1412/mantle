package diffview

import (
	"encoding/json"
	"fmt"
	"image/color"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/KaitouKid1412/mantle/pkg/render"
)

func rgb(hex uint32) color.Color {
	return color.RGBA{R: uint8(hex >> 16), G: uint8(hex >> 8), B: uint8(hex), A: 0xff}
}

// testPalette holds arbitrary colours for goldens.
var testPalette = render.MapPalette{
	render.TokText:              rgb(0xdddddd),
	render.TokInactive:          rgb(0x888888),
	render.TokSubtle:            rgb(0x555555),
	render.TokSuccess:           rgb(0x55bb55),
	render.TokError:             rgb(0xee5555),
	render.TokDiffAdded:         rgb(0x1f4a2a),
	render.TokDiffRemoved:       rgb(0x5a2626),
	render.TokDiffAddedDimmed:   rgb(0x26302a),
	render.TokDiffRemovedDimmed: rgb(0x352a2a),
	render.TokDiffAddedWord:     rgb(0x2f7a40),
	render.TokDiffRemovedWord:   rgb(0x8a3434),
	render.TokSyntaxKeyword:     rgb(0xc678dd),
	render.TokSyntaxString:      rgb(0x98c379),
	render.TokSyntaxNumber:      rgb(0xd19a66),
	render.TokSyntaxComment:     rgb(0x7f848e),
	render.TokSyntaxFunction:    rgb(0x61afef),
	render.TokSyntaxPlain:       rgb(0xabb2bf),
	render.TokSyntaxPunctuation: rgb(0xabb2bf),
	render.TokSyntaxOperator:    rgb(0x56b6c2),
}

const oldGo = `package main

import "fmt"

func main() {
	name := "world"
	fmt.Println("hello, " + name)
}

func unused() int {
	return 1
}
`

const newGo = `package main

import (
	"fmt"
	"os"
)

func main() {
	name := os.Getenv("NAME")
	fmt.Println("hello, " + name)
}

func unused() int {
	return 2
}
`

func TestFromStrings(t *testing.T) {
	hunks := FromStrings(oldGo, newGo, 1)
	if len(hunks) != 2 {
		t.Fatalf("hunks = %d, want 2: %+v", len(hunks), hunks)
	}
	want := Hunk{OldStart: 10, OldLines: 3, NewStart: 13, NewLines: 3,
		Lines: []string{" func unused() int {", "-\treturn 1", "+\treturn 2", " }"}}
	if !reflect.DeepEqual(hunks[1], want) {
		t.Fatalf("hunk 1 = %+v\nwant %+v", hunks[1], want)
	}
	if hunks[0].OldStart != 2 || hunks[0].NewStart != 2 || hunks[0].OldLines != 6 || hunks[0].NewLines != 9 {
		t.Fatalf("hunk 0 header = %+v", hunks[0])
	}
	add, rem := Counts(hunks)
	if add != 6 || rem != 3 {
		t.Fatalf("counts = +%d -%d, want +6 -3", add, rem)
	}
	if FromStrings("same", "same", 3) != nil {
		t.Fatal("identical texts produced hunks")
	}
}

func TestStructuredPatchJSON(t *testing.T) {
	raw := `[{"oldStart":3,"oldLines":2,"newStart":3,"newLines":2,"lines":[" a","-b","+c"]}]`
	var hunks []Hunk
	if err := json.Unmarshal([]byte(raw), &hunks); err != nil {
		t.Fatal(err)
	}
	if hunks[0].OldStart != 3 || len(hunks[0].Lines) != 3 {
		t.Fatalf("decoded %+v", hunks)
	}
}

func TestWordRanges(t *testing.T) {
	a, b := "\treturn computeTotal(items, 1)", "\treturn computeTotal(items, 2)"
	ra, rb := wordRanges(a, b)
	if len(ra) != 1 || a[ra[0][0]:ra[0][1]] != "1" {
		t.Fatalf("ra = %v", ra)
	}
	if len(rb) != 1 || b[rb[0][0]:rb[0][1]] != "2" {
		t.Fatalf("rb = %v", rb)
	}
	// A rewritten line gets no word ranges.
	ra, rb = wordRanges("completely different", "nothing alike here at all")
	if ra != nil || rb != nil {
		t.Fatalf("rewrite produced ranges %v %v", ra, rb)
	}
}

func TestOverlay(t *testing.T) {
	segs := []render.Segment{{Text: "hello "}, {Text: "world"}}
	got := overlay(segs, [][2]int{{3, 8}})
	var parts []string
	for _, s := range got {
		parts = append(parts, fmt.Sprintf("%s:%v", s.Text, s.marked))
	}
	want := "hel:false|lo :true|wo:true|rld:false"
	if strings.Join(parts, "|") != want {
		t.Fatalf("got %s want %s", strings.Join(parts, "|"), want)
	}
}

var diffCases = []struct {
	name  string
	hunks func() []Hunk
	o     Options
}{
	{"edit", func() []Hunk { return FromStrings(oldGo, newGo, 3) }, Options{Filename: "main.go"}},
	{"edit-plain", func() []Hunk { return FromStrings(oldGo, newGo, 3) }, Options{NoHighlight: true}},
	{"multi-hunk", func() []Hunk { return FromStrings(oldGo, newGo, 0) }, Options{Filename: "main.go"}},
	{"rejected", func() []Hunk { return FromStrings(oldGo, newGo, 3) }, Options{Filename: "main.go", Dimmed: true}},
	{"capped", func() []Hunk { return FromStrings(oldGo, newGo, 3) }, Options{Filename: "main.go", MaxLines: 8}},
	{"folded", func() []Hunk { return FromStrings(oldGo, newGo, 8) }, Options{Filename: "main.go", Context: 1}},
	{"new-file", func() []Hunk { return FromStrings("", "line one\nline two\nline three\n", 3) }, Options{}},
	{"long-lines", func() []Hunk {
		return FromStrings("const message = \"a fairly long string literal that keeps going and going past the edge\"\n",
			"const message = \"a fairly long string literal that keeps going and going past the right edge\"\n", 3)
	}, Options{Filename: "x.js"}},
	{"no-palette", func() []Hunk { return FromStrings("a\nb\nc\n", "a\nB\nc\n", 3) }, Options{}},
}

func TestRenderGolden(t *testing.T) {
	for _, tc := range diffCases {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			for _, w := range []int{40, 60, 100, 160} {
				o := tc.o
				o.Width = w
				if tc.name != "no-palette" {
					o.Palette = testPalette
				}
				lines := Render(tc.hunks(), o)
				checkLines(t, lines, w)
				if o.MaxLines > 0 && len(lines) > o.MaxLines {
					t.Errorf("%d lines exceed the cap of %d", len(lines), o.MaxLines)
				}
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

func checkLines(t *testing.T, lines []string, width int) {
	t.Helper()
	for i, l := range lines {
		if w := render.Width(l); w > width {
			t.Errorf("line %d is %d wide (limit %d): %q", i, w, width, render.Strip(l))
		}
		if strings.ContainsAny(render.Strip(l), "\t\n\r") {
			t.Errorf("line %d holds raw control characters: %q", i, l)
		}
	}
}

func TestRenderSanitizes(t *testing.T) {
	hunks := []Hunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1,
		Lines: []string{"-safe", "+\x1b]0;pwned\x07evil\x1b[2J text"}}}
	for _, l := range Render(hunks, Options{Width: 60}) {
		if strings.Contains(l, "\x1b]0") || strings.Contains(l, "\x1b[2J") {
			t.Fatalf("escape leaked: %q", l)
		}
	}
}

func TestRenderCapCountsHidden(t *testing.T) {
	var lines []string
	for i := range 50 {
		lines = append(lines, fmt.Sprintf("+line %d", i))
	}
	out := Render([]Hunk{{OldStart: 0, NewStart: 1, NewLines: 50, Lines: lines}}, Options{Width: 40, MaxLines: 10})
	if len(out) != 10 {
		t.Fatalf("got %d lines", len(out))
	}
	if got := render.Strip(out[9]); !strings.Contains(got, "+41 lines") {
		t.Fatalf("footer = %q", got)
	}
}

func BenchmarkRender(b *testing.B) {
	hunks := FromStrings(oldGo, newGo, 3)
	o := Options{Width: 100, Palette: testPalette, Filename: "main.go"}
	b.ReportAllocs()
	for b.Loop() {
		_ = Render(hunks, o)
	}
}
