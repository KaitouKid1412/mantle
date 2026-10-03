package editor

import (
	"testing"
)

func TestKeySequences(t *testing.T) {
	tests := []struct {
		name, start, keys, want string
	}{
		{"type", "|", "h i", "hi|"},
		{"left right", "ab|", "left left right", "a|b"},
		{"ctrl+a ctrl+e", "hello |world", "ctrl+a", "|hello world"},
		{"ctrl+e", "|hello world", "ctrl+e", "hello world|"},
		{"home end", "ab|cd", "home", "|abcd"},
		{"alt+f", "|foo bar.baz", "alt+f", "foo| bar.baz"},
		{"alt+f twice", "|foo bar.baz", "alt+f alt+f", "foo bar|.baz"},
		{"alt+b", "foo bar|", "alt+b", "foo |bar"},
		{"alt+b punct", "foo.bar|", "alt+b alt+b", "|foo.bar"},
		{"ctrl+right", "|a b", "ctrl+right", "a| b"},
		{"backspace", "abc|", "backspace", "ab|"},
		{"backspace join", "a\n|b", "backspace", "a|b"},
		{"delete", "a|bc", "delete", "a|c"},
		{"ctrl+d deletes", "a|bc", "ctrl+d", "a|c"},
		{"ctrl+k", "foo |bar baz", "ctrl+k", "foo |"},
		{"ctrl+k at eol joins", "foo|\nbar", "ctrl+k", "foo|bar"},
		{"ctrl+u", "foo bar| baz", "ctrl+u", "| baz"},
		{"ctrl+u at bol joins", "foo\n|bar", "ctrl+u", "foo|bar"},
		{"ctrl+w to whitespace", "git commit -m|", "ctrl+w", "git commit |"},
		{"ctrl+w punct", "path/to/file.go|", "ctrl+w", "|"},
		{"ctrl+w trailing space", "foo bar  |", "ctrl+w", "foo |"},
		{"alt+backspace stops at punct", "path/to/file|", "alt+backspace", "path/to/|"},
		{"alt+d", "|foo bar", "alt+d", "| bar"},
		{"kill and yank", "foo |bar", "ctrl+k ctrl+a ctrl+y", "bar|foo "},
		{"kills accumulate", "a b c|", "ctrl+w ctrl+w ctrl+y", "a b c|"},
		{"yank pop", "one two|", "ctrl+w ctrl+a ctrl+k ctrl+y ctrl+y alt+y", "one two|"},
		{"newline shift+enter", "ab|", "shift+enter c", "ab\nc|"},
		{"newline alt+enter", "a|b", "alt+enter", "a\n|b"},
		{"newline ctrl+j", "a|", "ctrl+j", "a\n|"},
		{"up down multiline", "abc\nde|f", "up", "ab|c\ndef"},
		{"down keeps goal", "abcdef|\nab\nabcdef", "down down", "abcdef\nab\nabcdef|"},
		{"undo typing", "|", "a b c ctrl+_", "|"},
		{"undo ctrl+-", "x|", "ctrl+w ctrl+-", "x|"},
		{"right accepts nothing at end", "ab|", "right", "ab|"},
		{"left wraps line", "a\n|b", "left", "a|\nb"},
		{"right wraps line", "a|\nb", "right", "a\n|b"},
		{"emoji is one cell", "a👍🏽|", "backspace", "a|"},
		{"combining is one cell", "é|", "left", "|é"},
		{"cjk", "日本|語", "backspace", "日|語"},
		{"family emoji", "👨‍👩‍👧|", "backspace", "|"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := fromCursor(t, tt.start)
			for _, k := range keys(tt.keys) {
				e.HandleKey(k)
			}
			if got := withCursor(e); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestYankPopCycles(t *testing.T) {
	e := fromCursor(t, "|")
	for _, w := range []string{"one", "two", "three"} {
		e.InsertString(w)
		e.LineStart()
		e.KillLineEnd()
	}
	e.Yank()
	if got := e.Display(); got != "three" {
		t.Fatalf("yank: %q", got)
	}
	e.YankPop()
	if got := e.Display(); got != "two" {
		t.Fatalf("yank-pop: %q", got)
	}
	e.YankPop()
	e.YankPop()
	if got := e.Display(); got != "three" {
		t.Fatalf("yank-pop wraps: %q", got)
	}
	e.CharBackward()
	if e.YankPop() {
		t.Fatal("yank-pop after a move must do nothing")
	}
}

func TestUndoCoalescing(t *testing.T) {
	e := newTestEditor()
	clk := &fakeClock{t: e.Now()}
	e.Now = clk.now
	typeText(e, "hello world")
	// Words form separate steps.
	e.Undo()
	if got := e.Display(); got != "hello " {
		t.Fatalf("after one undo: %q", got)
	}
	e.Undo()
	if got := e.Display(); got != "" {
		t.Fatalf("after two undos: %q", got)
	}
	e.Redo()
	e.Redo()
	if got := e.Display(); got != "hello world" {
		t.Fatalf("redo: %q", got)
	}

	// A pause breaks a burst.
	e2 := newTestEditor()
	clk2 := &fakeClock{t: e2.Now()}
	e2.Now = clk2.now
	typeText(e2, "ab")
	clk2.advance(2 * coalesceAfter)
	typeText(e2, "cd")
	e2.Undo()
	if got := e2.Display(); got != "ab" {
		t.Fatalf("pause: %q", got)
	}

	// Backspace bursts coalesce separately from typing.
	e3 := newTestEditor()
	typeText(e3, "abcd")
	for i := 0; i < 3; i++ {
		e3.DeleteBackward()
	}
	e3.Undo()
	if got := e3.Display(); got != "abcd" {
		t.Fatalf("backspace undo: %q", got)
	}
}

func TestUndoGroup(t *testing.T) {
	e := newTestEditor()
	e.BeginGroup()
	typeText(e, "one two")
	e.Newline()
	typeText(e, "three")
	e.EndGroup()
	e.Undo()
	if !e.Empty() {
		t.Fatalf("group undo left %q", e.Display())
	}
	if e.Undo() {
		t.Fatal("expected nothing left to undo")
	}
	// An empty group leaves no undo step.
	e.BeginGroup()
	e.EndGroup()
	if e.CanUndo() {
		t.Fatal("empty group created an undo step")
	}
}

func TestBackslashNewline(t *testing.T) {
	e := fromCursor(t, `line one\|`)
	if !e.BackslashNewline() {
		t.Fatal("expected backslash newline")
	}
	if got := withCursor(e); got != "line one\n|" {
		t.Fatalf("got %q", got)
	}
	e2 := fromCursor(t, `no backslash|`)
	if e2.BackslashNewline() {
		t.Fatal("unexpected newline")
	}
}

func TestCtrlDOnEmptyIsUnhandled(t *testing.T) {
	e := newTestEditor()
	if ok, _ := e.HandleKey(key("ctrl+d")); ok {
		t.Fatal("ctrl+d on empty prompt must reach the host")
	}
	if ok, _ := e.HandleKey(key("enter")); ok {
		t.Fatal("enter must reach the host")
	}
	if ok, _ := e.HandleKey(key("up")); ok {
		t.Fatal("up on the first row must reach the host")
	}
}

func TestClearActionKeys(t *testing.T) {
	e := fromCursor(t, "ab|")
	e.KeyMap.ClearActionKeys()
	if ok, _ := e.HandleKey(key("ctrl+_")); ok {
		t.Fatal("undo should be routed by the host")
	}
	if ok, _ := e.HandleKey(key("ctrl+j")); ok {
		t.Fatal("ctrl+j should be routed by the host")
	}
	if ok, _ := e.HandleKey(key("shift+enter")); !ok {
		t.Fatal("shift+enter stays an editor key")
	}
}

func TestGhost(t *testing.T) {
	e := fromCursor(t, "/he|")
	e.SetGhost("lp")
	if !e.GhostVisible() {
		t.Fatal("ghost should show at end")
	}
	e.HandleKey(key("right"))
	if got := withCursor(e); got != "/help|" {
		t.Fatalf("right accepts: %q", got)
	}
	if e.Ghost() != "" {
		t.Fatal("ghost cleared after edit")
	}
	e.SetGhost("x")
	e.CharBackward()
	if e.GhostVisible() || e.AcceptGhost() {
		t.Fatal("ghost hidden when not at end")
	}
}

func TestTyping1000CharsIsOneLine(t *testing.T) {
	e := newTestEditor()
	for i := 0; i < 1000; i++ {
		e.HandleKey(key("a"))
	}
	if e.LineCount() != 1 || len(e.Display()) != 1000 {
		t.Fatalf("lines=%d len=%d", e.LineCount(), len(e.Display()))
	}
}
