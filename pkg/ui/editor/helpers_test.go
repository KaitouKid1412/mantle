package editor

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// key builds a key press from a keystroke such as "ctrl+a", "alt+b",
// "shift+enter", "x" or "space".
func key(s string) tea.KeyPressMsg {
	var k tea.Key
	parts := strings.Split(s, "+")
	name := parts[len(parts)-1]
	if s == "+" || strings.HasSuffix(s, "++") {
		name = "+"
		parts = parts[:len(parts)-1]
	}
	for _, m := range parts[:len(parts)-1] {
		switch m {
		case "ctrl":
			k.Mod |= tea.ModCtrl
		case "alt":
			k.Mod |= tea.ModAlt
		case "shift":
			k.Mod |= tea.ModShift
		case "meta":
			k.Mod |= tea.ModMeta
		case "super":
			k.Mod |= tea.ModSuper
		}
	}
	codes := map[string]rune{
		"enter": tea.KeyEnter, "backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
		"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown,
		"home": tea.KeyHome, "end": tea.KeyEnd, "esc": tea.KeyEscape, "tab": tea.KeyTab,
		"space": tea.KeySpace,
	}
	if c, ok := codes[name]; ok {
		k.Code = c
		if name == "space" && k.Mod == 0 {
			k.Text = " "
		}
		return tea.KeyPressMsg(k)
	}
	r := []rune(name)
	k.Code = r[0]
	if k.Mod&^tea.ModShift == 0 {
		k.Text = name
	}
	return tea.KeyPressMsg(k)
}

// keys splits a space-separated key list; "" yields none.
func keys(s string) []tea.KeyPressMsg {
	if s == "" {
		return nil
	}
	var out []tea.KeyPressMsg
	for _, f := range strings.Fields(s) {
		out = append(out, key(f))
	}
	return out
}

// typeText sends each rune as a key press.
func typeText(e *Editor, s string) {
	for _, r := range s {
		if r == '\n' {
			e.Newline()
			continue
		}
		e.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// withCursor renders the buffer with "|" at the cursor.
func withCursor(e *Editor) string {
	var b strings.Builder
	for r := 0; r < e.LineCount(); r++ {
		if r > 0 {
			b.WriteByte('\n')
		}
		for c, cl := range e.lines[r].cells {
			if (Pos{r, c}) == e.cur {
				b.WriteByte('|')
			}
			b.WriteString(cl.text())
		}
		if e.cur.Row == r && e.cur.Col == len(e.lines[r].cells) {
			b.WriteByte('|')
		}
	}
	return b.String()
}

// fromCursor builds an editor whose content is s with "|" marking the
// cursor (end if absent).
func fromCursor(t testing.TB, s string) *Editor {
	t.Helper()
	e := newTestEditor()
	i := strings.Index(s, "|")
	text := strings.Replace(s, "|", "", 1)
	e.SetValue(text)
	if i >= 0 {
		before := s[:i]
		row := strings.Count(before, "\n")
		col := len(toCells(before[strings.LastIndex(before, "\n")+1:]))
		e.SetCursor(Pos{row, col})
	}
	e.undo, e.redo = nil, nil
	return e
}

// fakeClock advances only when told to.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestEditor() *Editor {
	e := New()
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	e.Now = clk.now
	return e
}
