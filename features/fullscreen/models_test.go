package fullscreen

import (
	"fmt"
	"slices"
	"testing"
)

func blocks(sizes ...int) []Block {
	var out []Block
	for i, n := range sizes {
		b := Block{ID: fmt.Sprint("b", i)}
		for j := range n {
			b.Lines = append(b.Lines, fmt.Sprintf("b%d.%d", i, j))
		}
		out = append(out, b)
	}
	return out
}

func TestViewportFollowAndScroll(t *testing.T) {
	v := NewViewport(5)
	v.SetBlocks(blocks(3, 4)) // 7 lines
	if v.Offset() != 2 || !v.Following() {
		t.Fatalf("follow: offset %d", v.Offset())
	}
	v.SetBlocks(blocks(3, 4, 2)) // 9 lines
	if v.Offset() != 4 || v.Unseen() != 0 {
		t.Errorf("growth while following: offset %d unseen %d", v.Offset(), v.Unseen())
	}
	v.ScrollBy(-3)
	if v.Following() || v.Offset() != 1 {
		t.Errorf("scrolled up: %d %v", v.Offset(), v.Following())
	}
	v.SetBlocks(blocks(3, 4, 2, 5)) // +5 lines below
	if v.Offset() != 1 || v.Unseen() != 5 {
		t.Errorf("growth while scrolled: offset %d unseen %d", v.Offset(), v.Unseen())
	}
	v.Page(1)
	if v.Offset() != 5 {
		t.Errorf("page down = %d", v.Offset())
	}
	v.Bottom()
	if !v.Following() || v.Unseen() != 0 || v.Offset() != 9 {
		t.Errorf("bottom: %d %v %d", v.Offset(), v.Following(), v.Unseen())
	}
	v.Top()
	if v.Offset() != 0 || v.Following() {
		t.Error("top")
	}
	v.ScrollBy(100)
	if !v.Following() {
		t.Error("scrolling to the end resumes following")
	}
	v.HalfPage(-1)
	if v.Offset() != 7 {
		t.Errorf("half page = %d", v.Offset())
	}
	v.SetHeight(20) // taller than the document
	if v.Offset() != 0 {
		t.Errorf("tall window = %d", v.Offset())
	}
}

func TestViewportBlocks(t *testing.T) {
	v := NewViewport(3)
	v.SetBlocks(blocks(2, 5, 1, 4))
	v.Top()
	v.ScrollBy(3) // lines 3..5 visible: block 1
	from, to := v.Visible(0)
	if from != 1 || to != 2 {
		t.Errorf("visible = %d,%d", from, to)
	}
	from, to = v.Visible(2) // lines 1..7
	if from != 0 || to != 3 {
		t.Errorf("visible with margin = %d,%d", from, to)
	}
	for line, want := range map[int][2]int{0: {0, 0}, 1: {0, 1}, 2: {1, 0}, 6: {1, 4}, 7: {2, 0}, 11: {3, 3}} {
		b, w, ok := v.BlockAt(line)
		if !ok || b != want[0] || w != want[1] {
			t.Errorf("BlockAt(%d) = %d,%d,%v", line, b, w, ok)
		}
	}
	if _, _, ok := v.BlockAt(12); ok {
		t.Error("past the end")
	}
	if v.BlockStart(2) != 7 || v.BlockID(3) != "b3" || v.BlockID(9) != "" {
		t.Error("block metadata")
	}
	v.ScrollTo(10)
	if v.Offset() > 10 || v.Offset()+v.Height <= 10 {
		t.Errorf("ScrollTo: offset %d", v.Offset())
	}
}

func TestWheel(t *testing.T) {
	w := Wheel{}
	if w.Notch(0) != 3 || w.Notch(10) != 3 {
		t.Error("no acceleration by default")
	}
	a := Wheel{Base: 2, Accelerate: true}
	got := []int{a.Notch(0), a.Notch(20), a.Notch(40), a.Notch(500)}
	if !slices.Equal(got, []int{2, 3, 4, 2}) {
		t.Errorf("accelerated = %v", got)
	}
}

func src(lines ...string) LineSource {
	return func(l int) string {
		if l < 0 || l >= len(lines) {
			return ""
		}
		return lines[l]
	}
}

func TestSelectionText(t *testing.T) {
	lines := src("\x1b[1mhello world\x1b[0m", "second line", "日本語 text")
	s := Selection{Anchor: Pos{0, 6}, Head: Pos{1, 6}, Active: true}
	if got := Text(lines, s, 80); got != "world\nsecond" {
		t.Errorf("Text = %q", got)
	}
	// Reversed anchor/head gives the same range.
	r := Selection{Anchor: Pos{1, 6}, Head: Pos{0, 6}, Active: true}
	if Text(lines, r, 80) != Text(lines, s, 80) {
		t.Error("reverse range")
	}
	// A line that fills the width is soft-wrapped: no newline.
	wrapped := src("abcdef", "ghi")
	if got := Text(wrapped, Selection{Anchor: Pos{0, 0}, Head: Pos{1, 3}, Active: true}, 6); got != "abcdefghi" {
		t.Errorf("soft wrap = %q", got)
	}
	// Wide characters take two cells.
	if got := Text(lines, Selection{Anchor: Pos{2, 0}, Head: Pos{2, 4}, Active: true}, 80); got != "日本" {
		t.Errorf("wide = %q", got)
	}
	if Text(lines, Selection{}, 80) != "" || !(Selection{Anchor: Pos{1, 1}, Head: Pos{1, 1}, Active: true}).Empty() {
		t.Error("empty selection")
	}
	if !s.Contains(Pos{0, 6}) || s.Contains(Pos{1, 6}) || s.Contains(Pos{0, 5}) {
		t.Error("Contains")
	}
}

func TestWordAndLineSelection(t *testing.T) {
	lines := src("open ./cmd/main.go now", "  ⏺ hi")
	w := WordAt(lines, Pos{0, 8})
	if got := Text(lines, w, 80); got != "./cmd/main.go" {
		t.Errorf("word = %q", got)
	}
	if got := Text(lines, WordAt(lines, Pos{0, 4}), 80); got != "" {
		t.Errorf("space selects itself (trimmed) = %q", got)
	}
	if got := Text(lines, LineAt(lines, 1), 80); got != "  ⏺ hi" {
		t.Errorf("line = %q", got)
	}
	if !WordAt(lines, Pos{0, 99}).Empty() {
		t.Error("past the end")
	}
}

func TestHighlight(t *testing.T) {
	mark := func(s string) string { return "[" + s + "]" }
	s := Selection{Anchor: Pos{0, 2}, Head: Pos{1, 3}, Active: true}
	if got := Highlight("\x1b[31mabcdef\x1b[0m", 0, s, mark); got != "ab[cdef]" {
		t.Errorf("first = %q", got)
	}
	if got := Highlight("xyz123", 1, s, mark); got != "[xyz]123" {
		t.Errorf("last = %q", got)
	}
	if got := Highlight("\x1b[31mq\x1b[0m", 2, s, mark); got != "\x1b[31mq\x1b[0m" {
		t.Errorf("outside keeps style = %q", got)
	}
}

func TestSearch(t *testing.T) {
	lines := src("Hello world", "say hello", "HELLO 日本語 hello", "nothing")
	var s Search
	s.Run("hello", lines, 4, 1)
	if len(s.Matches) != 4 {
		t.Fatalf("matches = %+v", s.Matches)
	}
	if m, _ := s.At(); m.Line != 1 || m.Col != 4 || m.End != 9 {
		t.Errorf("current = %+v", m)
	}
	if m, _ := s.Next(); m.Line != 2 || m.Col != 0 {
		t.Errorf("next = %+v", m)
	}
	if m, _ := s.Next(); m.Line != 2 || m.Col != 13 {
		t.Errorf("after wide chars = %+v", m)
	}
	if m, _ := s.Next(); m.Line != 0 {
		t.Errorf("wrap = %+v", m)
	}
	if m, _ := s.Prev(); m.Line != 2 || m.Col != 13 {
		t.Errorf("prev wraps back = %+v", m)
	}
	s.Run("Hello", lines, 4, 0)
	if len(s.Matches) != 1 {
		t.Errorf("smart case = %+v", s.Matches)
	}
	s.Run("日本", lines, 4, 0)
	if len(s.Matches) != 1 || s.Matches[0].Col != 6 || s.Matches[0].End != 10 {
		t.Errorf("wide query = %+v", s.Matches)
	}
	if len(s.OnLine(2)) != 1 || len(s.OnLine(0)) != 0 {
		t.Error("OnLine")
	}
	s.Run("", lines, 4, 0)
	if _, ok := s.Next(); ok {
		t.Error("empty query")
	}
}
