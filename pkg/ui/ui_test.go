package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

func TestStories(t *testing.T) {
	for _, s := range Stories() {
		t.Run(strings.ReplaceAll(s.ID, "/", "_"), func(t *testing.T) {
			testkit.RunStory(t, s)
		})
	}
}

func plain(s string) string { return ansi.Strip(s) }

func TestSelectNavigation(t *testing.T) {
	c := exttest.NewCtx()
	var accepted []string
	s := NewSelect("sel", Item{Label: "a"}, Item{Label: "b", Disabled: true}, Item{Label: "c"})
	s.OnAccept = func(_ ext.Ctx, i int, it Item) tea.Cmd { accepted = append(accepted, it.Label); return nil }
	act := func(a ext.ActionID) {
		if handled, _ := s.HandleAction(c, a); !handled {
			t.Fatalf("%s not handled", a)
		}
	}
	act(ext.ActSelectNext) // skips disabled b
	if it, _ := s.Current(); it.Label != "c" {
		t.Fatalf("after next: %q", it.Label)
	}
	act(ext.ActSelectNext) // wraps
	if it, _ := s.Current(); it.Label != "a" {
		t.Fatalf("after wrap: %q", it.Label)
	}
	act(ext.ActSelectPrevious) // wraps backwards to c
	act(ext.ActSelectAccept)
	if strings.Join(accepted, ",") != "c" {
		t.Fatalf("accepted = %v", accepted)
	}
	s.Numbered = true
	s.HandleKey(c, tea.KeyPressMsg{Code: '1', Text: "1"})
	if strings.Join(accepted, ",") != "c,a" {
		t.Fatalf("number pick: %v", accepted)
	}
}

func TestSelectFilter(t *testing.T) {
	c := exttest.NewCtx()
	s := NewSelect("sel", Item{Label: "opus"}, Item{Label: "sonnet"}, Item{Label: "haiku"})
	s.Filterable = true
	for _, r := range "snt" {
		s.HandleKey(c, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if it, ok := s.Current(); !ok || it.Label != "sonnet" || len(s.visible) != 1 {
		t.Fatalf("filtered: %v %v", it, s.visible)
	}
	s.HandleKey(c, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if s.Filter() != "sn" {
		t.Fatalf("filter = %q", s.Filter())
	}
	// Escape clears the filter before cancelling.
	cancelled := false
	s.OnCancel = func(ext.Ctx) tea.Cmd { cancelled = true; return nil }
	s.HandleAction(c, ext.ActSelectCancel)
	if s.Filter() != "" || cancelled {
		t.Fatal("first escape should clear the filter")
	}
	s.HandleAction(c, ext.ActSelectCancel)
	if !cancelled {
		t.Fatal("second escape should cancel")
	}
}

func TestTextFieldEditing(t *testing.T) {
	c := exttest.NewCtx()
	f := &TextField{IDValue: "f"}
	for _, r := range "hello world" {
		f.HandleKey(c, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	f.HandleKey(c, tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if f.Value() != "hello " {
		t.Fatalf("ctrl+w: %q", f.Value())
	}
	f.HandleKey(c, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	f.HandlePaste(c, tea.PasteMsg{Content: "say\n"})
	if f.Value() != "say hello " {
		t.Fatalf("paste at start: %q", f.Value())
	}
	r := f.View(c, ext.Area{Width: 40})
	if r.Cursor == nil || r.Cursor.X != 4 {
		t.Fatalf("cursor = %+v", r.Cursor)
	}
	f.HandleKey(c, tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if f.Value() != "say " {
		t.Fatalf("ctrl+k: %q", f.Value())
	}
	var submitted string
	f.OnSubmit = func(_ ext.Ctx, v string) tea.Cmd { submitted = v; return nil }
	f.HandleKey(c, tea.KeyPressMsg{Code: tea.KeyEnter})
	if submitted != "say " {
		t.Fatalf("submit = %q", submitted)
	}
}

func TestTextFieldScrollsHorizontally(t *testing.T) {
	c := exttest.NewCtx()
	f := &TextField{IDValue: "f", Prompt: "> "}
	f.SetValue(strings.Repeat("abcdefghij", 5))
	r := f.View(c, ext.Area{Width: 20})
	if w := ansi.StringWidth(r.Text); w > 20 {
		t.Fatalf("width %d", w)
	}
	if !strings.HasSuffix(plain(r.Text), "hij") || r.Cursor.X > 19 {
		t.Fatalf("view %q cursor %+v", plain(r.Text), r.Cursor)
	}
}

func TestScrollPane(t *testing.T) {
	c := exttest.NewCtx()
	var ls []string
	for i := range 20 {
		ls = append(ls, itoa(i))
	}
	p := &ScrollPane{IDValue: "p", Lines: ls}
	a := ext.Area{Width: 10, MaxHeight: 5}
	p.View(c, a)
	p.HandleAction(c, ext.ActScrollBottom)
	if out := plain(p.View(c, a).Text); !strings.HasPrefix(out, "15") || !strings.Contains(out, "19") {
		t.Fatalf("bottom: %q", out)
	}
	p.HandleKey(c, tea.KeyPressMsg{Code: 'g', Text: "g"})
	if p.Offset() != 0 {
		t.Fatalf("g: offset %d", p.Offset())
	}
	p.HandleAction(c, ext.ActScrollHalfPageDown)
	if p.Offset() != 2 {
		t.Fatalf("half page: %d", p.Offset())
	}
}

func TestFrameWidth(t *testing.T) {
	c := exttest.NewCtx()
	for _, w := range []int{10, 40, 80} {
		out := Frame(c.Theme(), "permission", "A long title that should be truncated nicely", "body line that is long enough to clip at small widths", w)
		for _, l := range strings.Split(out, "\n") {
			if ansi.StringWidth(l) != w {
				t.Fatalf("width %d: line %q is %d cells", w, plain(l), ansi.StringWidth(l))
			}
		}
	}
}

func TestShimmer(t *testing.T) {
	c := exttest.NewCtx()
	th := c.Theme()
	out := Shimmer("Working", 2, 2, th.Color("text"), th.Color("claude"))
	if plain(out) != "Working" {
		t.Fatalf("shimmer changed text: %q", plain(out))
	}
}
