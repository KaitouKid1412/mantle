package ui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// TextField is a single-line text input with readline basics (ctrl+a/e/b/f/u/k/w,
// alt+b/f, arrows, home/end, backspace/delete). It places the real terminal cursor
// (Rendered.Cursor) so IMEs work. Enter calls OnSubmit; escape calls OnCancel.
// For the prompt editor see pkg/ui/editor.
type TextField struct {
	IDValue     string
	Prompt      string // shown before the text ("> ")
	Placeholder string
	Mask        rune // non-zero: render every rune as Mask (secrets)
	CharLimit   int
	Context     string // keybinding context ("" = none; Global still applies)

	OnSubmit func(ext.Ctx, string) tea.Cmd
	OnCancel func(ext.Ctx) tea.Cmd
	OnChange func(ext.Ctx, string) tea.Cmd

	value []rune
	pos   int
}

var _ ext.Focusable = (*TextField)(nil)

func (f *TextField) ID() string                      { return f.IDValue }
func (f *TextField) Init(ext.Ctx) tea.Cmd            { return nil }
func (f *TextField) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (f *TextField) KeyContext() string              { return f.Context }

// Value returns the text.
func (f *TextField) Value() string { return string(f.value) }

// SetValue replaces the text and puts the cursor at the end.
func (f *TextField) SetValue(s string) {
	f.value = []rune(s)
	f.pos = len(f.value)
}

func (f *TextField) changed(c ext.Ctx) tea.Cmd {
	c.Invalidate(f.IDValue)
	if f.OnChange != nil {
		return f.OnChange(c, f.Value())
	}
	return nil
}

func (f *TextField) insert(c ext.Ctx, s string) tea.Cmd {
	rs := []rune(strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s))
	if f.CharLimit > 0 && len(f.value)+len(rs) > f.CharLimit {
		rs = rs[:max(0, f.CharLimit-len(f.value))]
	}
	if len(rs) == 0 {
		return nil
	}
	f.value = append(f.value[:f.pos], append(rs, f.value[f.pos:]...)...)
	f.pos += len(rs)
	return f.changed(c)
}

func (f *TextField) wordLeft() int {
	i := f.pos
	for i > 0 && unicode.IsSpace(f.value[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(f.value[i-1]) {
		i--
	}
	return i
}

func (f *TextField) wordRight() int {
	i := f.pos
	for i < len(f.value) && unicode.IsSpace(f.value[i]) {
		i++
	}
	for i < len(f.value) && !unicode.IsSpace(f.value[i]) {
		i++
	}
	return i
}

// HandleKey edits the text.
func (f *TextField) HandleKey(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	moved := func(p int) (bool, tea.Cmd) {
		f.pos = min(max(p, 0), len(f.value))
		c.Invalidate(f.IDValue)
		return true, nil
	}
	cut := func(from, to int) (bool, tea.Cmd) {
		if from >= to {
			return true, nil
		}
		f.value = append(f.value[:from], f.value[to:]...)
		f.pos = from
		return true, f.changed(c)
	}
	switch k.Keystroke() {
	case "enter":
		if f.OnSubmit != nil {
			return true, f.OnSubmit(c, f.Value())
		}
		return true, nil
	case "esc":
		if f.OnCancel != nil {
			return true, f.OnCancel(c)
		}
		return false, nil
	case "left", "ctrl+b":
		return moved(f.pos - 1)
	case "right", "ctrl+f":
		return moved(f.pos + 1)
	case "home", "ctrl+a":
		return moved(0)
	case "end", "ctrl+e":
		return moved(len(f.value))
	case "alt+b", "ctrl+left":
		return moved(f.wordLeft())
	case "alt+f", "ctrl+right":
		return moved(f.wordRight())
	case "backspace", "ctrl+h":
		return cut(max(0, f.pos-1), f.pos)
	case "delete", "ctrl+d":
		return cut(f.pos, min(len(f.value), f.pos+1))
	case "ctrl+w", "alt+backspace":
		return cut(f.wordLeft(), f.pos)
	case "alt+d":
		return cut(f.pos, f.wordRight())
	case "ctrl+u":
		return cut(0, f.pos)
	case "ctrl+k":
		return cut(f.pos, len(f.value))
	}
	if k.Text != "" && k.Mod&^tea.ModShift == 0 {
		return true, f.insert(c, k.Text)
	}
	return false, nil
}

// HandlePaste inserts pasted text (newlines become spaces).
func (f *TextField) HandlePaste(c ext.Ctx, p tea.PasteMsg) (bool, tea.Cmd) {
	return true, f.insert(c, p.Content)
}

// View renders prompt + text, scrolled horizontally to keep the cursor visible.
func (f *TextField) View(c ext.Ctx, a ext.Area) ext.Rendered {
	t := c.Theme()
	prompt := t.Paint(theme.Inactive, f.Prompt)
	pw := ansi.StringWidth(f.Prompt)
	room := max(1, a.Width-pw-1)
	if len(f.value) == 0 {
		ph := ansi.Truncate(f.Placeholder, room, "…")
		cur := tea.NewCursor(pw, 0)
		return ext.Rendered{Text: prompt + t.Paint(theme.Subtle, ph), Cursor: cur}
	}
	shown := f.value
	if f.Mask != 0 {
		shown = []rune(strings.Repeat(string(f.Mask), len(f.value)))
	}
	// Horizontal scroll: keep the cursor within the window.
	start := 0
	width := func(rs []rune) int { return ansi.StringWidth(string(rs)) }
	for width(shown[start:f.pos]) > room {
		start++
	}
	end := len(shown)
	for end > start && width(shown[start:end]) > room {
		end--
	}
	text := string(shown[start:end])
	cur := tea.NewCursor(pw+width(shown[start:f.pos]), 0)
	return ext.Rendered{Text: prompt + t.Paint(theme.Text, text), Cursor: cur}
}
