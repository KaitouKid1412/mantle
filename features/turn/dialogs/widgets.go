package dialogs

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// option is one choice in a list.
type option struct {
	id    string
	label string
	hint  string // dim suffix, e.g. "(esc)"
}

// optionList is a numbered single-choice list with a cursor.
type optionList struct {
	items  []option
	cursor int
}

func (l *optionList) move(d int) {
	if len(l.items) == 0 {
		return
	}
	l.cursor = min(max(l.cursor+d, 0), len(l.items)-1)
}

func (l *optionList) selected() option {
	if l.cursor < 0 || l.cursor >= len(l.items) {
		return option{}
	}
	return l.items[l.cursor]
}

// pick moves the cursor to the n-th (1-based) option; false if out of range.
func (l *optionList) pick(n int) bool {
	if n < 1 || n > len(l.items) {
		return false
	}
	l.cursor = n - 1
	return true
}

func (l *optionList) index(id string) int {
	for i, it := range l.items {
		if it.id == id {
			return i
		}
	}
	return -1
}

func (l *optionList) focus(id string) {
	if i := l.index(id); i >= 0 {
		l.cursor = i
	}
}

// lines renders the list: "❯ 1. Yes" for the focused item, "  2. …" for the others.
func (l optionList) lines(width int, st Styles, active bool) []string {
	var out []string
	for i, it := range l.items {
		pointer := "  "
		if i == l.cursor && active {
			pointer = "❯ "
		}
		num := itoa(i+1) + ". "
		ls := wrapIndent(pointer+num, it.label, width)
		indent := strings.Repeat(" ", ansi.StringWidth(pointer+num))
		if it.hint != "" {
			last := ls[len(ls)-1]
			if ansi.StringWidth(last)+1+ansi.StringWidth(it.hint) <= width {
				ls[len(ls)-1] = last + " \x00" + it.hint
			} else {
				ls = append(ls, indent+"\x00"+it.hint)
			}
		}
		focused := i == l.cursor && active
		for j, line := range ls {
			text, hint, _ := strings.Cut(line, "\x00")
			if focused {
				if j == 0 {
					text = render(st.Accent, pointer) + render(st.Selected, strings.TrimPrefix(text, pointer))
				} else {
					text = render(st.Selected, text)
				}
			}
			ls[j] = text + render(st.Dim, hint)
		}
		out = append(out, ls...)
	}
	return out
}

// textField is a small line editor for feedback, amendments, "Other" answers and form
// fields. It hard-wraps by cells so the cursor maps directly to a screen cell.
type textField struct {
	r           []rune
	pos         int
	placeholder string
	multiline   bool
}

func (f *textField) Value() string { return string(f.r) }

func (f *textField) SetValue(s string) {
	f.r = []rune(s)
	f.pos = len(f.r)
}

func (f *textField) insert(s string) {
	s = Sanitize(s)
	if !f.multiline {
		s = strings.ReplaceAll(s, "\n", " ")
	}
	ins := []rune(s)
	f.r = append(f.r[:f.pos], append(ins, f.r[f.pos:]...)...)
	f.pos += len(ins)
}

func (f *textField) deleteRange(from, to int) {
	if from < 0 {
		from = 0
	}
	if to > len(f.r) {
		to = len(f.r)
	}
	if from >= to {
		return
	}
	f.r = append(f.r[:from], f.r[to:]...)
	f.pos = from
}

func (f *textField) lineStart() int {
	i := f.pos
	for i > 0 && f.r[i-1] != '\n' {
		i--
	}
	return i
}

func (f *textField) lineEnd() int {
	i := f.pos
	for i < len(f.r) && f.r[i] != '\n' {
		i++
	}
	return i
}

func (f *textField) wordLeft() int {
	i := f.pos
	for i > 0 && unicode.IsSpace(f.r[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(f.r[i-1]) {
		i--
	}
	return i
}

func (f *textField) wordRight() int {
	i := f.pos
	for i < len(f.r) && unicode.IsSpace(f.r[i]) {
		i++
	}
	for i < len(f.r) && !unicode.IsSpace(f.r[i]) {
		i++
	}
	return i
}

// HandleKey applies readline-style editing keys. It returns false for keys it doesn't
// edit with (enter, esc, tab, arrows up/down, …), which the dialog handles.
func (f *textField) HandleKey(k tea.KeyPressMsg) bool {
	switch k.Keystroke() {
	case "backspace", "ctrl+h":
		if f.pos > 0 {
			f.deleteRange(f.pos-1, f.pos)
		}
	case "delete":
		f.deleteRange(f.pos, f.pos+1)
	case "ctrl+d":
		if len(f.r) == 0 {
			return false
		}
		f.deleteRange(f.pos, f.pos+1)
	case "left", "ctrl+b":
		f.pos = max(0, f.pos-1)
	case "right", "ctrl+f":
		f.pos = min(len(f.r), f.pos+1)
	case "home", "ctrl+a":
		f.pos = f.lineStart()
	case "end", "ctrl+e":
		f.pos = f.lineEnd()
	case "alt+left", "alt+b", "ctrl+left":
		f.pos = f.wordLeft()
	case "alt+right", "alt+f", "ctrl+right":
		f.pos = f.wordRight()
	case "ctrl+u":
		f.deleteRange(f.lineStart(), f.pos)
	case "ctrl+k":
		f.deleteRange(f.pos, f.lineEnd())
	case "ctrl+w", "alt+backspace":
		f.deleteRange(f.wordLeft(), f.pos)
	case "ctrl+j", "shift+enter", "alt+enter":
		if !f.multiline {
			return false
		}
		f.insert("\n")
	default:
		if k.Text == "" || k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) != 0 {
			return false
		}
		f.insert(k.Text)
	}
	return true
}

// lines renders the field at width cells with prefix on the first line. The character
// under the cursor gets st.Cursor when focused; an empty field shows its placeholder.
func (f *textField) lines(width int, st Styles, focused bool, prefix string) []string {
	pw := ansi.StringWidth(prefix)
	w := max(1, width-pw)
	pad := strings.Repeat(" ", pw)
	if len(f.r) == 0 {
		cur := ""
		if focused {
			cur = render(st.Cursor, " ")
		}
		ph := ""
		if f.placeholder != "" {
			ph = render(st.Dim, ansi.Truncate(f.placeholder, w-1, "…"))
		}
		return []string{strings.TrimRight(prefix+cur+ph, " ")}
	}
	var out []string
	var cur strings.Builder
	col := 0
	flush := func() {
		p := pad
		if len(out) == 0 {
			p = prefix
		}
		out = append(out, strings.TrimRight(p+cur.String(), " "))
		cur.Reset()
		col = 0
	}
	for i := 0; i <= len(f.r); i++ {
		atCursor := focused && i == f.pos
		if i == len(f.r) {
			if atCursor {
				if col >= w {
					flush()
				}
				cur.WriteString(render(st.Cursor, " "))
			}
			break
		}
		r := f.r[i]
		if r == '\n' {
			if atCursor {
				cur.WriteString(render(st.Cursor, " "))
			}
			flush()
			continue
		}
		rw := max(1, ansi.StringWidth(string(r)))
		if col+rw > w {
			flush()
		}
		if atCursor {
			cur.WriteString(render(st.Cursor, string(r)))
		} else {
			cur.WriteRune(r)
		}
		col += rw
	}
	flush()
	return out
}
