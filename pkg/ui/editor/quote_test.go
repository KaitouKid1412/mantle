package editor

import "testing"

func TestSetQuote(t *testing.T) {
	tests := []struct {
		name, buf, quote, want string
	}{
		{"insert into empty", "", "a\nb", "> a\n> b\n\n"},
		{"insert above question", "why?", "a", "> a\n\nwhy?"},
		{"insert multiline question", "why\nnot", "a", "> a\n\nwhy\nnot"},
		{"preformatted", "q", "> a\n> b\n\n", "> a\n> b\n\nq"},
		{"blank inner line", "q", "a\n\nb", "> a\n>\n> b\n\nq"},
		{"replace", "> old\n> more\n\nwhy?", "new", "> new\n\nwhy?"},
		{"replace without blank", "> old\nwhy?", "new", "> new\n\nwhy?"},
		{"replace quote only", "> old", "new", "> new\n\n"},
		{"remove", "> old\n\nwhy?", "", "why?"},
		{"remove keeps later blank", "> old\n\n\nwhy?", "", "\nwhy?"},
		{"remove quote only", "> old\n\n", "", ""},
		{"remove nothing", "why?", "", "why?"},
		{"not a quote", ">x\nwhy?", "a", "> a\n\n>x\nwhy?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEditor()
			e.SetValue(tt.buf)
			e.SetCursor(Pos{})
			e.SetQuote(tt.quote)
			if got := e.Display(); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if tt.buf != tt.want && !e.AtEnd() {
				t.Fatalf("cursor %v not at end", e.Cursor())
			}
		})
	}
}

func TestSetQuoteUndo(t *testing.T) {
	e := newTestEditor()
	typeText(e, "why?")
	e.SetQuote("a\nb")
	e.SetQuote("c")
	if got := e.Quote(); got != "> c" {
		t.Fatalf("Quote() = %q", got)
	}
	e.Undo()
	if got := e.Display(); got != "> a\n> b\n\nwhy?" {
		t.Fatalf("after one undo: %q", got)
	}
	e.Undo()
	if got := e.Display(); got != "why?" {
		t.Fatalf("after two undos: %q", got)
	}
}

func TestSetQuoteKeepsChips(t *testing.T) {
	e := newTestEditor()
	e.InsertChip(NewPasteChip(e.NextChipID(), "line1\nline2\nline3"))
	before := e.Text()
	e.SetQuote("a")
	if got, want := e.Text(), "> a\n\n"+before; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if len(e.Chips()) != 1 {
		t.Fatalf("chips = %d", len(e.Chips()))
	}
}
