package render

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func readTestdata(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// feed appends src in chunks of n bytes and checks the closed-block
// invariants after every chunk.
func feed(t *testing.T, s *Stream, src string, n int) {
	t.Helper()
	var prev [][]string
	for i := 0; i < len(src); i += n {
		s.Append(src[i:min(i+n, len(src))])
		closed := s.ClosedBlocks()
		if len(closed) < len(prev) {
			t.Fatalf("closed blocks shrank from %d to %d", len(prev), len(closed))
		}
		for k := range prev {
			if !reflect.DeepEqual(prev[k], closed[k]) {
				t.Fatalf("closed block %d changed after more input:\n%q\n%q", k, prev[k], closed[k])
			}
		}
		prev = closed
		_ = s.Lines() // the live view renders every step
	}
	s.Finish()
}

func TestStreamEqualsOneShot(t *testing.T) {
	for _, name := range []string{"sample.md", "answer.md"} {
		src := readTestdata(t, name)
		for _, width := range []int{40, 60, 100, 160} {
			o := MarkdownOptions{Width: width, Palette: testPalette}
			want := Markdown(src, o)
			for _, chunk := range []int{1, 7, 64} {
				s := NewStream(o)
				feed(t, s, src, chunk)
				if got := s.Lines(); !reflect.DeepEqual(got, want) {
					t.Fatalf("%s width %d chunk %d: stream differs from one-shot\n%s", name, width, chunk,
						diffLines(want, got))
				}
			}
		}
	}
}

func TestStreamCachesClosedBlocks(t *testing.T) {
	src := readTestdata(t, "answer.md")
	o := MarkdownOptions{Width: 80, Palette: testPalette}
	s := NewStream(o)
	feed(t, s, src, 1)
	nblocks := len(MarkdownBlocks(src, o))
	if s.NumClosed() != nblocks {
		t.Fatalf("closed = %d, want %d", s.NumClosed(), nblocks)
	}
	if st := s.Stats(); st.ClosedRenders != nblocks {
		t.Fatalf("closed renders = %d, want one per block (%d)", st.ClosedRenders, nblocks)
	}
	// Reading again does no work.
	before := s.Stats()
	_ = s.Lines()
	_ = s.ClosedBlocks()
	if s.Stats() != before {
		t.Fatalf("re-reading rendered again: %+v → %+v", before, s.Stats())
	}
}

func TestStreamProgressiveClose(t *testing.T) {
	s := NewStream(MarkdownOptions{Width: 40})
	s.Append("First paragraph.\n\nSecond")
	if got := s.NumClosed(); got != 1 {
		t.Fatalf("closed = %d, want 1", got)
	}
	if got := s.ClosedBlock(0); !reflect.DeepEqual(got, []string{"First paragraph."}) {
		t.Fatalf("closed block = %q", got)
	}
	if got := s.OpenBlocks(); len(got) != 1 || Strip(got[0][0]) != "Second" {
		t.Fatalf("open = %q", got)
	}
}

func TestStreamOpenFence(t *testing.T) {
	s := NewStream(MarkdownOptions{Width: 40})
	s.Append("Intro\n\n```go\nfunc a() {}\n\nfunc b() {}\n")
	// The blank line inside the fence must not close anything.
	if got := s.NumClosed(); got != 1 {
		t.Fatalf("closed = %d, want 1 (the intro)", got)
	}
	s.Append("```\n\nAfter")
	if got := s.NumClosed(); got != 2 {
		t.Fatalf("closed = %d, want 2 after the fence closed", got)
	}
	code := stripAll(s.ClosedBlock(1))
	if !reflect.DeepEqual(code, []string{"func a() {}", "", "func b() {}"}) {
		t.Fatalf("code block = %q", code)
	}
}

func TestStreamSetextAndListsStayOpen(t *testing.T) {
	s := NewStream(MarkdownOptions{Width: 40})
	s.Append("Heading\n")
	s.Append("---\n")
	if s.NumClosed() != 0 {
		t.Fatal("a paragraph that may become a setext heading was closed")
	}
	s.Append("\n- a\n- b\n\n")
	if s.NumClosed() != 1 {
		t.Fatalf("closed = %d, want 1 (the heading)", s.NumClosed())
	}
	s.Append("  continued item\n\nDone")
	if s.NumClosed() != 2 {
		t.Fatalf("closed = %d, want 2", s.NumClosed())
	}
	list := stripAll(s.ClosedBlock(1))
	if !reflect.DeepEqual(list, []string{"- a", "", "- b", "", "  continued item"}) {
		t.Fatalf("list = %q", list)
	}
}

func TestStreamSplitEscapesAndRunes(t *testing.T) {
	s := NewStream(MarkdownOptions{Width: 40})
	for _, part := range []string{"red \x1b[3", "1mtext\x1b[0m 日", "\xe6\x9c", "\xac\r", "\nnext"} {
		s.Append(part)
	}
	s.Finish()
	got := stripAll(s.Lines())
	want := []string{"red text 日本 next"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	if strings.Contains(s.Source(), "\x1b") {
		t.Fatalf("escape leaked into source: %q", s.Source())
	}
}

func TestStreamSetOptionsRerenders(t *testing.T) {
	src := readTestdata(t, "answer.md")
	s := NewStream(MarkdownOptions{Width: 100, Palette: testPalette})
	s.Append(src[:len(src)/2])
	n := s.NumClosed()
	o := MarkdownOptions{Width: 50, Palette: testPalette}
	s.SetOptions(o)
	if s.NumClosed() != n {
		t.Fatal("block count changed with width")
	}
	s.Append(src[len(src)/2:])
	s.Finish()
	if got, want := s.Lines(), Markdown(src, o); !reflect.DeepEqual(got, want) {
		t.Fatalf("after resize:\n%s", diffLines(want, got))
	}
}

func diffLines(want, got []string) string {
	var b strings.Builder
	for i := 0; i < max(len(want), len(got)); i++ {
		w, g := "<none>", "<none>"
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			b.WriteString("line ")
			b.WriteString(strings.Repeat(" ", 0))
			b.WriteString(Strip(w) + "\n   ≠ " + Strip(g) + "\n")
			b.WriteString("  want " + strings.ReplaceAll(w, "\x1b", "␛") + "\n")
			b.WriteString("  got  " + strings.ReplaceAll(g, "\x1b", "␛") + "\n")
			break
		}
	}
	return b.String()
}

func BenchmarkStreamIncremental(b *testing.B) {
	src := readTestdata(b, "answer.md")
	o := MarkdownOptions{Width: 100, Palette: testPalette}
	const chunk = 16 // ~4 tokens per coalesced delta
	b.ReportAllocs()
	for b.Loop() {
		s := NewStream(o)
		for i := 0; i < len(src); i += chunk {
			s.Append(src[i:min(i+chunk, len(src))])
			_ = s.Lines()
		}
		s.Finish()
		_ = s.Lines()
	}
}

func BenchmarkStreamFullRerender(b *testing.B) {
	src := readTestdata(b, "answer.md")
	o := MarkdownOptions{Width: 100, Palette: testPalette}
	const chunk = 16
	b.ReportAllocs()
	for b.Loop() {
		for i := 0; i < len(src); i += chunk {
			_ = Markdown(src[:min(i+chunk, len(src))], o)
		}
	}
}
