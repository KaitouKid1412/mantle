package vim

import (
	"strings"
	"testing"
)

// fakeTarget is a minimal Target: one rune per cell, clips are strings.
type fakeTarget struct {
	lines [][]rune
	cur   Pos
	undo  []fakeSnap
	redo  []fakeSnap
	group int
}

type fakeSnap struct {
	text string
	cur  Pos
}

func newFake(s string) *fakeTarget {
	f := &fakeTarget{}
	i := strings.Index(s, "|")
	text := strings.Replace(s, "|", "", 1)
	for _, l := range strings.Split(text, "\n") {
		f.lines = append(f.lines, []rune(l))
	}
	if i >= 0 {
		before := s[:i]
		f.cur.Row = strings.Count(before, "\n")
		f.cur.Col = len([]rune(before[strings.LastIndex(before, "\n")+1:]))
	}
	return f
}

func (f *fakeTarget) String() string {
	var b strings.Builder
	for r, l := range f.lines {
		if r > 0 {
			b.WriteByte('\n')
		}
		for c, ch := range l {
			if f.cur == (Pos{r, c}) {
				b.WriteByte('|')
			}
			b.WriteRune(ch)
		}
		if f.cur == (Pos{r, len(l)}) {
			b.WriteByte('|')
		}
	}
	return b.String()
}

func (f *fakeTarget) text() string {
	parts := make([]string, len(f.lines))
	for i, l := range f.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

func (f *fakeTarget) setText(s string) {
	f.lines = nil
	for _, l := range strings.Split(s, "\n") {
		f.lines = append(f.lines, []rune(l))
	}
}

func (f *fakeTarget) LineCount() int           { return len(f.lines) }
func (f *fakeTarget) LineLen(r int) int        { return len(f.lines[r]) }
func (f *fakeTarget) Grapheme(r, c int) string { return string(f.lines[r][c]) }
func (f *fakeTarget) Cursor() Pos              { return f.cur }
func (f *fakeTarget) SetCursor(p Pos)          { f.cur = p }
func (f *fakeTarget) Insert(p Pos, c Clip) Pos { return f.InsertText(p, c.(string)) }
func (f *fakeTarget) BeginGroup()              { f.checkpoint(); f.group++ }
func (f *fakeTarget) EndGroup()                { f.group-- }
func (f *fakeTarget) offset(p Pos) int {
	n := 0
	for r := 0; r < p.Row; r++ {
		n += len(f.lines[r]) + 1
	}
	return n + p.Col
}

func (f *fakeTarget) pos(off int) Pos {
	for r, l := range f.lines {
		if off <= len(l) {
			return Pos{r, off}
		}
		off -= len(l) + 1
	}
	return Pos{len(f.lines) - 1, len(f.lines[len(f.lines)-1])}
}

func (f *fakeTarget) Copy(from, to Pos) Clip {
	rs := []rune(f.text())
	return string(rs[f.offset(from):f.offset(to)])
}

func (f *fakeTarget) Delete(from, to Pos) Clip {
	f.checkpoint()
	rs := []rune(f.text())
	a, b := f.offset(from), f.offset(to)
	out := string(rs[a:b])
	f.setText(string(rs[:a]) + string(rs[b:]))
	return out
}

func (f *fakeTarget) InsertText(p Pos, s string) Pos {
	f.checkpoint()
	rs := []rune(f.text())
	a := f.offset(p)
	f.setText(string(rs[:a]) + s + string(rs[a:]))
	return f.pos(a + len([]rune(s)))
}

func (f *fakeTarget) Key(k Key) bool {
	switch {
	case k.Text != "":
		f.cur = f.InsertText(f.cur, k.Text)
		return true
	case k.Name == "backspace":
		if off := f.offset(f.cur); off > 0 {
			p := f.pos(off - 1)
			f.Delete(p, f.cur)
			f.cur = p
		}
		return true
	}
	return false
}

func (f *fakeTarget) checkpoint() {
	if f.group > 0 {
		return
	}
	f.undo = append(f.undo, fakeSnap{f.text(), f.cur})
	f.redo = nil
}

func (f *fakeTarget) Undo() bool {
	if len(f.undo) == 0 {
		return false
	}
	f.redo = append(f.redo, fakeSnap{f.text(), f.cur})
	sn := f.undo[len(f.undo)-1]
	f.setText(sn.text)
	f.cur = sn.cur
	f.undo = f.undo[:len(f.undo)-1]
	return true
}

func (f *fakeTarget) Redo() bool {
	if len(f.redo) == 0 {
		return false
	}
	f.undo = append(f.undo, fakeSnap{f.text(), f.cur})
	sn := f.redo[len(f.redo)-1]
	f.setText(sn.text)
	f.cur = sn.cur
	f.redo = f.redo[:len(f.redo)-1]
	return true
}

// feedKeys feeds space-separated tokens: named keys (esc, enter, backspace,
// ctrl+r, left, ...) or text; "␣" types a space.
func feedKeys(m *Machine, s string) Result {
	var res Result
	for _, tok := range strings.Fields(s) {
		res = m.Feed(tokKey(tok))
	}
	return res
}

func tokKey(tok string) Key {
	switch tok {
	case "esc", "enter", "backspace", "delete", "left", "right", "up", "down", "home", "end", "tab":
		return Key{Name: tok}
	case "␣":
		return Key{Text: " "}
	}
	if strings.HasPrefix(tok, "ctrl+") {
		return Key{Name: tok}
	}
	return Key{Text: tok}
}

// chars splits a word into single-character tokens.
func chars(s string) string {
	var out []string
	for _, r := range s {
		if r == ' ' {
			out = append(out, "␣")
			continue
		}
		out = append(out, string(r))
	}
	return strings.Join(out, " ")
}

func normalMachine(t *testing.T, start string) (*Machine, *fakeTarget) {
	t.Helper()
	f := newFake(start)
	m := New(f)
	m.SetMode(Normal)
	return m, f
}

func TestNormalMode(t *testing.T) {
	tests := []struct{ start, keys, want string }{
		// motions
		{"|foo bar baz", "w", "foo |bar baz"},
		{"|foo bar baz", "2 w", "foo bar |baz"},
		{"|foo.bar baz", "w", "foo|.bar baz"},
		{"|foo.bar baz", "W", "foo.bar |baz"},
		{"foo bar |baz", "b", "foo |bar baz"},
		{"foo.bar |baz", "B", "|foo.bar baz"},
		{"|foo bar", "e", "fo|o bar"},
		{"fo|o bar", "e", "foo ba|r"},
		{"foo ba|r", "g e", "fo|o bar"},
		{"  foo |bar", "^", "  |foo bar"},
		{"  foo |bar", "0", "|  foo bar"},
		{"|foo bar", "$", "foo ba|r"},
		{"|a,b,c", "f ,", "a|,b,c"},
		{"|a,b,c", "2 f ,", "a,b|,c"},
		{"|a,b,c", "t ,", "|a,b,c"},
		{"|a,b,c", "f , ;", "a,b|,c"},
		{"a,b,|c", "F , ,", "a,b|,c"},
		{"a,b,c|", "F ,", "a,b|,c"},
		{"|x (a (b) c)", "%", "x (a (b) c|)"},
		{"x (a (b) c|)", "%", "x |(a (b) c)"},
		{"one\n|two\nthree", "j", "one\ntwo\n|three"},
		{"one\ntwo\nth|ree", "k k", "on|e\ntwo\nthree"},
		{"one\ntwo\n|three", "g g", "|one\ntwo\nthree"},
		{"|one\ntwo\nthree", "G", "one\ntwo\n|three"},
		{"|one\ntwo\nthree", "2 G", "one\n|two\nthree"},
		{"long line|\nab", "j", "long line\na|b"},
		{"a\n\n|b", "{", "a\n|\nb"},
		{"|a\n\nb", "}", "a\n|\nb"},
		{"foo\n|bar", "w", "foo\nba|r"},
		{"|abc", "l l l l", "ab|c"},
		{"ab|c", "h h h h", "|abc"},
		// x X r ~ J
		{"a|bc", "x", "a|c"},
		{"a|bcd", "2 x", "a|d"},
		{"ab|c", "x", "a|b"},
		{"abc|", "X", "a|c"},
		{"a|bc", "r z", "a|zc"},
		{"a|bcd", "3 r z", "azz|z"},
		{"|aBc", "~ ~", "Ab|c"},
		{"|foo\n  bar", "J", "foo| bar"},
		{"|a\nb\nc", "3 J", "a b| c"},
		// operators
		{"|foo bar baz", "d w", "|bar baz"},
		{"|foo bar baz", "d 2 w", "|baz"},
		{"|foo bar baz", "2 d w", "|baz"},
		{"foo |bar\nbaz", "d w", "foo| \nbaz"},
		{"|foo bar", "d e", "| bar"},
		{"foo |bar baz", "d b", "|bar baz"},
		{"foo |bar baz", "d $", "foo| "},
		{"foo |bar baz", "D", "foo| "},
		{"foo |bar baz", "d 0", "|bar baz"},
		{"a,b|,c", "d t c", "a,b|c"},
		{"a,b,|c", "d F a", "|c"},
		{"one\n|two\nthree", "d d", "one\n|three"},
		{"one\ntwo\n|three", "d d", "one\n|two"},
		{"|one\ntwo\nthree", "2 d d", "|three"},
		{"|one\ntwo", "d j", "|"},
		{"one\n|two\nthree", "d k", "|three"},
		{"|one", "d d", "|"},
		{"one\n|two", "y y P", "one\n|two\ntwo"},
		{"one\n|two", "y y p", "one\ntwo\n|two"},
		{"|ab", "y l p", "a|ab"},
		{"|ab", "y l P", "|aab"},
		{"|ab", "x p", "b|a"},
		{"|one\ntwo", "d d p", "two\n|one"},
		{"|foo", "y w 3 p", "ffoofoofo|ooo"},
		{"|a\nb\nc", "y G G p", "a\nb\nc\n|a\nb\nc"},
		{"|ab", ">", "|ab"},
		{"|ab\ncd", "> j", "  |ab\n  cd"},
		{"  |ab", "< <", "|ab"},
		// text objects
		{"foo b|ar baz", "d i w", "foo | baz"},
		{"foo b|ar baz", "d a w", "foo |baz"},
		{"foo b|ar", "d a w", "fo|o"},
		{"x = \"h|ello\" y", "d i \"", "x = \"|\" y"},
		{"x = \"h|ello\" y", "d a \"", "x = |y"},
		{"f(a, |b)", "d i (", "f(|)"},
		{"f(a, |b)", "d a (", "|f"},
		{"f(a, (|b))", "d 2 i (", "f(|)"},
		{"f[|a]", "d i ]", "f[|]"},
		{"{\n  |a\n}", "d i {", "{\n|}"},
		{"a\nb|\n\nc", "d i p", "|\nc"},
		{"a\n|b\n\nc", "d a p", "|c"},
		{"foo.b|ar", "d i W", "|"},
		// counts and dot
		{"|a b c d", "d w .", "|c d"},
		{"|a b c d e", "d w 2 .", "|d e"},
		{"|abcdef", "2 x .", "|ef"},
		// undo/redo
		{"|foo bar", "d w u", "|foo bar"},
		{"|foo bar", "d w u ctrl+r", "|bar"},
		{"|abc", "x x u u", "|abc"},
	}
	for _, tt := range tests {
		t.Run(tt.start+"/"+tt.keys, func(t *testing.T) {
			m, f := normalMachine(t, tt.start)
			feedKeys(m, tt.keys)
			if got := f.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if m.Mode() != Normal {
				t.Errorf("mode %v, want NORMAL", m.Mode())
			}
		})
	}
}

func TestInsertCommands(t *testing.T) {
	tests := []struct{ start, keys, want string }{
		{"a|bc", "i " + chars("XY") + " esc", "aX|Ybc"},
		{"a|bc", "a " + chars("X") + " esc", "ab|Xc"},
		{"  a|bc", "I " + chars("X") + " esc", "  |Xabc"},
		{"a|bc", "A " + chars("X") + " esc", "abc|X"},
		{"a|bc", "o " + chars("X") + " esc", "abc\n|X"},
		{"a|bc", "O " + chars("X") + " esc", "|X\nabc"},
		{"|foo bar", "c w " + chars("baz") + " esc", "ba|z bar"},
		{"|foo bar", "c i w " + chars("x") + " esc", "|x bar"},
		{"foo |bar", "C " + chars("x") + " esc", "foo |x"},
		{"|foo\nbar", "c c " + chars("x") + " esc", "|x\nbar"},
		{"|abc", "s " + chars("X") + " esc", "|Xbc"},
		{"|abc", "S " + chars("X") + " esc", "|X"},
		{"|ab", "3 i " + chars("x") + " esc", "xx|xab"},
		{"|a", "2 o " + chars("x") + " esc", "a\nx\n|x"},
		{"|foo bar", "c w " + chars("X") + " esc w .", "X |X"},
		{"|a\nb", "A " + chars("!") + " esc j .", "a!\nb|!"},
		{"|abc", "R " + chars("XY") + " esc", "X|Yc"},
		{"|ab", "R " + chars("XYZ") + " esc", "XY|Z"},
		{"|abc", "R " + chars("XY") + " backspace esc", "|Xbc"},
		{"|abc", "i " + chars("xy") + " esc u", "|abc"},
	}
	for _, tt := range tests {
		t.Run(tt.start+"/"+tt.keys, func(t *testing.T) {
			m, f := normalMachine(t, tt.start)
			feedKeys(m, tt.keys)
			if got := f.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVisualMode(t *testing.T) {
	tests := []struct{ start, keys, want string }{
		{"|foo bar", "v e d", "| bar"},
		{"|foo bar", "v l l y P", "fo|ofoo bar"},
		{"one\n|two\nthree", "V d", "one\n|three"},
		{"|one\ntwo\nthree", "V j d", "|three"},
		{"|abc", "v l ~", "|ABc"},
		{"|abc", "v l U", "|ABc"},
		{"foo b|ar baz", "v i w d", "foo | baz"},
		{"|ab", "v l r x", "|xx"},
		{"|ab\ncd", "V j >", "  |ab\n  cd"},
		{"|foo bar", "y w w v e p", "foo foo| "},
		{"|abc", "v l c " + chars("X") + " esc", "|Xc"},
		{"|abc", "v esc", "|abc"},
		{"|abc", "v v", "|abc"},
	}
	for _, tt := range tests {
		t.Run(tt.start+"/"+tt.keys, func(t *testing.T) {
			m, f := normalMachine(t, tt.start)
			feedKeys(m, tt.keys)
			if got := f.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if m.Mode() != Normal {
				t.Errorf("mode %v, want NORMAL", m.Mode())
			}
		})
	}
}

func TestSelection(t *testing.T) {
	m, _ := normalMachine(t, "|abc def")
	feedKeys(m, "v e")
	from, to, lw, ok := m.Selection()
	if !ok || lw || from != (Pos{0, 0}) || to != (Pos{0, 3}) {
		t.Fatalf("selection %v %v %v %v", from, to, lw, ok)
	}
	feedKeys(m, "V")
	if m.Mode() != VisualLine {
		t.Fatalf("mode %v", m.Mode())
	}
	from, to, lw, _ = m.Selection()
	if !lw || from != (Pos{0, 0}) || to != (Pos{0, 7}) {
		t.Fatalf("line selection %v %v", from, to)
	}
}

func TestHostKeys(t *testing.T) {
	m, _ := normalMachine(t, "|abc")
	if r := m.Feed(Key{Name: "enter"}); r.Handled {
		t.Fatal("enter belongs to the host in NORMAL mode")
	}
	if r := m.Feed(Key{Name: "esc"}); r.Handled {
		t.Fatal("esc with nothing pending belongs to the host")
	}
	if r := feedKeys(m, "d esc"); !r.Handled {
		t.Fatal("esc cancelling a pending operator is handled")
	}
	if r := m.Feed(Key{Name: "ctrl+s"}); r.Handled {
		t.Fatal("ctrl chords belong to the host")
	}
	if r := m.Feed(Key{Text: "/"}); r.Event != EventHistorySearch {
		t.Fatal("/ opens history search")
	}
	if r := m.Feed(Key{Text: "k"}); r.Event != EventHistoryPrev {
		t.Fatal("k on the first line recalls history")
	}
	if r := m.Feed(Key{Name: "down"}); r.Event != EventHistoryNext {
		t.Fatal("down on the last line recalls history")
	}
	m.SetMode(Insert)
	if r := m.Feed(Key{Name: "enter"}); r.Handled {
		t.Fatal("enter belongs to the host in INSERT mode")
	}
}

func TestInsertRemaps(t *testing.T) {
	f := newFake("|")
	m := New(f)
	m.SetRemaps(map[string]string{"jj": "<Esc>"})
	if r := m.Feed(Key{Text: "a"}); !r.Handled || r.Pending {
		t.Fatal("plain key")
	}
	if r := m.Feed(Key{Text: "j"}); !r.Pending {
		t.Fatal("j should wait for the remap")
	}
	if f.text() != "a" {
		t.Fatalf("pending key must not be typed yet: %q", f.text())
	}
	m.Feed(Key{Text: "j"})
	if m.Mode() != Normal || f.text() != "a" {
		t.Fatalf("jj should escape: mode %v text %q", m.Mode(), f.text())
	}
	// A different second key types both.
	m.SetMode(Insert)
	f.cur = Pos{0, 1}
	m.Feed(Key{Text: "j"})
	m.Feed(Key{Text: "k"})
	if f.text() != "ajk" || m.Mode() != Insert {
		t.Fatalf("jk should be typed: %q %v", f.text(), m.Mode())
	}
	// A timeout flushes the prefix.
	m.Feed(Key{Text: "j"})
	m.Flush()
	if f.text() != "ajkj" {
		t.Fatalf("flush: %q", f.text())
	}
}

func TestModeStrings(t *testing.T) {
	if Insert.Indicator() != "-- INSERT --" || Normal.Indicator() != "" ||
		VisualLine.Indicator() != "-- VISUAL LINE --" || Replace.String() != "REPLACE" {
		t.Fatal("mode strings")
	}
}
