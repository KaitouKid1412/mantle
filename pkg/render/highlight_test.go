package render

import (
	"strings"
	"testing"
)

func TestHighlightLinesAndCache(t *testing.T) {
	h := &Highlighter{}
	code := "package main\n\n/* multi\nline */\nfunc main() { x := \"s\" }\n"
	lines := h.Highlight(code, "go", "", testPalette)
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5: %q", len(lines), lines)
	}
	for i, want := range strings.Split(strings.TrimSuffix(code, "\n"), "\n") {
		if Strip(lines[i]) != want {
			t.Errorf("line %d = %q, want %q", i, Strip(lines[i]), want)
		}
	}
	if !strings.Contains(lines[0], Fg(testPalette, TokSyntaxKeyword).Open()) {
		t.Errorf("keyword not coloured: %q", lines[0])
	}
	if !strings.Contains(lines[2], clsComment.style(testPalette).Open()) {
		t.Errorf("comment not coloured: %q", lines[2])
	}
	_ = h.Highlight(code, "go", "", NoColor) // other palette: same lexing
	if hits, misses := h.Stats(); hits != 1 || misses != 1 {
		t.Fatalf("hits %d misses %d, want 1/1", hits, misses)
	}
}

func TestHighlightLexerLookup(t *testing.T) {
	h := &Highlighter{}
	for _, tc := range []struct{ lang, file, want string }{
		{"go", "", "Go"},
		{"Python", "", "Python"},
		{"{.rust}", "", "Rust"},
		{"tsx", "", "TypeScript"},
		{"sh", "", "Bash"},
		{"", "main.go", "Go"},
		{"", "/a/b/Dockerfile", "Docker"},
		{"text", "", ""},
		{"no-such-language", "", ""},
		{"", "", ""},
	} {
		if got := h.LexerFor(tc.lang, tc.file); got != tc.want {
			t.Errorf("LexerFor(%q, %q) = %q, want %q", tc.lang, tc.file, got, tc.want)
		}
	}
}

func TestHighlightLimits(t *testing.T) {
	h := &Highlighter{MaxLines: 3}
	code := "a := 1\nb := 2\nc := 3\nd := 4"
	plain := Fg(testPalette, TokSyntaxPlain)
	for i, l := range h.Highlight(code, "go", "", testPalette) {
		if want := plain.Render(strings.Split(code, "\n")[i]); l != want {
			t.Fatalf("over the limit, line %d = %q, want plain %q", i, l, want)
		}
	}
	var nilH *Highlighter
	if got := nilH.Highlight("x", "go", "", NoColor); len(got) != 1 || got[0] != "x" {
		t.Fatalf("nil highlighter: %q", got)
	}
}

func TestHighlightSegmentsMatchHighlight(t *testing.T) {
	code := "def f(x):\n    return x + 1  # inc"
	h := &Highlighter{}
	lines := h.Highlight(code, "python", "", testPalette)
	segs := h.Segments(code, "python", "", testPalette)
	for i := range lines {
		if got := RenderSegments(segs[i]); got != lines[i] {
			t.Fatalf("line %d: segments %q != highlight %q", i, got, lines[i])
		}
	}
}

func BenchmarkHighlightCached(b *testing.B) {
	code := strings.Repeat("func f(a, b int) int { return a + b } // add\n", 200)
	h := &Highlighter{}
	_ = h.Highlight(code, "go", "", testPalette)
	b.ReportAllocs()
	for b.Loop() {
		_ = h.Highlight(code, "go", "", testPalette)
	}
}
