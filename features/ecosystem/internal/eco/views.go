package eco

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Base gives a View no-op defaults; embed it and override what you need.
type Base struct{}

func (Base) Contexts() []string                                    { return []string{ext.ContextSelect} }
func (Base) Action(ext.Ctx, *Dialog, ext.ActionID) (bool, tea.Cmd) { return false, nil }
func (Base) Key(ext.Ctx, *Dialog, tea.KeyPressMsg) (bool, tea.Cmd) { return false, nil }
func (Base) Update(ext.Ctx, *Dialog, tea.Msg) tea.Cmd              { return nil }
func (Base) Hints(ext.Ctx) string                                  { return "" }

// ---- TextView ----

// TextView shows read-only lines that scroll with the Select keys.
type TextView struct {
	Base
	Lines  []string // may be styled
	Sub    string
	offset int
	HintFn func(ext.Ctx) string
}

func (v *TextView) Subtitle() string { return v.Sub }

func (v *TextView) Action(ctx ext.Ctx, d *Dialog, a ext.ActionID) (bool, tea.Cmd) {
	switch a {
	case ext.ActSelectNext:
		v.offset++
	case ext.ActSelectPrevious:
		v.offset--
	case ext.ActSelectPageDown:
		v.offset += 10
	case ext.ActSelectPageUp:
		v.offset -= 10
	case ext.ActSelectFirst:
		v.offset = 0
	case ext.ActSelectLast:
		v.offset = len(v.Lines)
	case ext.ActSelectAccept:
		return true, d.Pop(ctx)
	default:
		return false, nil
	}
	return true, nil
}

func (v *TextView) Render(ctx ext.Ctx, th *theme.Theme, width, height int) []string {
	var all []string
	for _, l := range v.Lines {
		if ansi.StringWidth(l) <= width {
			all = append(all, l)
			continue
		}
		all = append(all, wrapLine(l, width)...)
	}
	if height <= 0 || len(all) <= height {
		v.offset = 0
		return all
	}
	maxOff := len(all) - height
	if v.offset > maxOff {
		v.offset = maxOff
	}
	if v.offset < 0 {
		v.offset = 0
	}
	return all[v.offset : v.offset+height]
}

func (v *TextView) Hints(ctx ext.Ctx) string {
	if v.HintFn != nil {
		return v.HintFn(ctx)
	}
	return Hint("↑↓", "scroll", "esc", "back")
}

// wrapLine wraps at word boundaries, except that a line ending in one long
// token (a path or URL) is cut in place so "File: /very/long/path" stays on
// its label's line.
func wrapLine(l string, width int) []string {
	words := strings.Fields(ansi.Strip(l))
	if n := len(words); n > 0 && ansi.StringWidth(words[n-1]) > width/2 {
		return strings.Split(ansi.Hardwrap(l, width, false), "\n")
	}
	return strings.Split(ansi.Hardwrap(ansi.Wordwrap(l, width, ""), width, true), "\n")
}

// ---- MenuView ----

// MenuItem is one choice of a MenuView.
type MenuItem struct {
	Label, Detail string
	Disabled      string // reason it can't be chosen ("" = enabled)
	Run           func(ctx ext.Ctx, d *Dialog) tea.Cmd
}

// MenuView is a list of choices; enter runs the selected one.
type MenuView struct {
	Base
	Intro []string // lines above the choices
	Sub   string
	list  List
	items []MenuItem
}

// NewMenu builds a MenuView.
func NewMenu(sub string, intro []string, items ...MenuItem) *MenuView {
	m := &MenuView{Sub: sub, Intro: intro, items: items}
	rows := make([]Row, len(items))
	for i, it := range items {
		rows[i] = Row{Key: it.Label, Label: it.Label, Detail: it.Detail, Value: i}
		if it.Disabled != "" {
			rows[i].Detail = it.Disabled
			rows[i].Info = true
		}
	}
	m.list.SetRows(rows)
	return m
}

func (m *MenuView) Subtitle() string { return m.Sub }

func (m *MenuView) Action(ctx ext.Ctx, d *Dialog, a ext.ActionID) (bool, tea.Cmd) {
	if m.list.HandleAction(a, 5) {
		return true, nil
	}
	if a == ext.ActSelectAccept {
		r, ok := m.list.Selected()
		if !ok {
			return true, nil
		}
		it := m.items[r.Value.(int)]
		if it.Run == nil {
			return true, nil
		}
		return true, it.Run(ctx, d)
	}
	return false, nil
}

func (m *MenuView) Key(ctx ext.Ctx, d *Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	// 1-9 pick a choice directly.
	if s := k.String(); len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		n := int(s[0] - '1')
		if n < len(m.items) && m.items[n].Disabled == "" && m.items[n].Run != nil {
			return true, m.items[n].Run(ctx, d)
		}
	}
	return false, nil
}

func (m *MenuView) Render(ctx ext.Ctx, th *theme.Theme, width, height int) []string {
	var out []string
	for _, l := range m.Intro {
		out = append(out, Wrap(l, width)...)
	}
	if len(m.Intro) > 0 {
		out = append(out, "")
	}
	return append(out, m.list.Render(th, width, height-len(out))...)
}

func (m *MenuView) Hints(ctx ext.Ctx) string {
	return Hint("enter", "choose", "esc", "back")
}

// ---- ConfirmView ----

// ConfirmView asks a yes/no question. Yes runs OnYes; no pops the view.
type ConfirmView struct {
	Base
	Question []string
	YesLabel string
	Danger   bool
	OnYes    func(ctx ext.Ctx, d *Dialog) tea.Cmd
	yes      bool
}

func (c *ConfirmView) Contexts() []string { return []string{ext.ContextConfirmation} }

// Choice reports whether "yes" is highlighted.
func (c *ConfirmView) Choice() bool { return c.yes }

func (c *ConfirmView) Action(ctx ext.Ctx, d *Dialog, a ext.ActionID) (bool, tea.Cmd) {
	switch a {
	case ext.ActConfirmYes:
		if c.yes {
			return true, c.accept(ctx, d)
		}
		return true, d.Pop(ctx)
	case ext.ActConfirmPrevious, ext.ActConfirmNext, ext.ActConfirmToggle, ext.ActConfirmNextField:
		c.yes = !c.yes
		return true, nil
	}
	return false, nil
}

func (c *ConfirmView) accept(ctx ext.Ctx, d *Dialog) tea.Cmd {
	var yes tea.Cmd
	if c.OnYes != nil {
		yes = c.OnYes(ctx, d) // before the pop, so it can adjust the dialog
	}
	return tea.Batch(d.Pop(ctx), yes)
}

func (c *ConfirmView) Key(ctx ext.Ctx, d *Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "y", "Y":
		return true, c.accept(ctx, d)
	case "n", "N":
		return true, d.Pop(ctx)
	}
	return false, nil
}

func (c *ConfirmView) Render(ctx ext.Ctx, th *theme.Theme, width, height int) []string {
	var out []string
	for _, q := range c.Question {
		out = append(out, Wrap(q, width)...)
	}
	out = append(out, "")
	yes := c.YesLabel
	if yes == "" {
		yes = "Yes"
	}
	yesTok := theme.Text
	if c.Danger {
		yesTok = theme.Error
	}
	choice := func(label string, on bool, tok theme.Token) string {
		if on {
			return th.Paint(theme.Suggestion, "❯ ") + th.Paint(tok, label)
		}
		return "  " + th.Paint(theme.Inactive, label)
	}
	out = append(out, choice(yes, c.yes, yesTok), choice("No", !c.yes, theme.Text))
	return out
}

func (c *ConfirmView) Hints(ctx ext.Ctx) string {
	return Hint("y", "yes", "n", "no", "↑↓", "choose", "enter", "confirm")
}

// ---- FormView ----

// Field is one form input.
type Field struct {
	Label    string
	Value    string
	Options  []string // cycle with space/left/right instead of typing
	Secret   bool
	Help     string
	Optional bool
}

// FormView edits a few single-line fields.
type FormView struct {
	Base
	Sub      string
	Intro    []string
	Fields   []Field
	focus    int
	Err      string
	OnSubmit func(ctx ext.Ctx, d *Dialog, values []string) tea.Cmd
}

func (f *FormView) Subtitle() string { return f.Sub }

// Contexts: PaneField binds no letters or escape, so typing reaches the field.
func (f *FormView) Contexts() []string { return []string{ext.ContextPaneField} }

func (f *FormView) Action(ctx ext.Ctx, d *Dialog, a ext.ActionID) (bool, tea.Cmd) {
	// Only navigation actions that don't collide with typing.
	switch a {
	case ext.ActConfirmNo:
		return true, d.Pop(ctx)
	}
	return false, nil
}

func (f *FormView) Key(ctx ext.Ctx, d *Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	fld := &f.Fields[f.focus]
	switch k.String() {
	case "tab", "down":
		f.focus = (f.focus + 1) % len(f.Fields)
		return true, nil
	case "shift+tab", "up":
		f.focus = (f.focus + len(f.Fields) - 1) % len(f.Fields)
		return true, nil
	case "enter":
		if f.focus < len(f.Fields)-1 {
			f.focus++
			return true, nil
		}
		return true, f.submit(ctx, d)
	case "ctrl+s":
		return true, f.submit(ctx, d)
	case "esc", "escape":
		return true, d.Pop(ctx)
	case "backspace":
		if len(fld.Options) == 0 && fld.Value != "" {
			r := []rune(fld.Value)
			fld.Value = string(r[:len(r)-1])
		}
		return true, nil
	case "ctrl+u":
		if len(fld.Options) == 0 {
			fld.Value = ""
		}
		return true, nil
	case "left", "right", "space":
		if len(fld.Options) > 0 {
			fld.Value = cycle(fld.Options, fld.Value, k.String() != "left")
			return true, nil
		}
	}
	if len(fld.Options) == 0 && k.Text != "" {
		fld.Value += k.Text
		f.Err = ""
		return true, nil
	}
	return false, nil
}

func cycle(opts []string, cur string, fwd bool) string {
	for i, o := range opts {
		if o == cur {
			if fwd {
				return opts[(i+1)%len(opts)]
			}
			return opts[(i+len(opts)-1)%len(opts)]
		}
	}
	return opts[0]
}

func (f *FormView) submit(ctx ext.Ctx, d *Dialog) tea.Cmd {
	vals := make([]string, len(f.Fields))
	for i, fld := range f.Fields {
		vals[i] = strings.TrimSpace(fld.Value)
		if vals[i] == "" && !fld.Optional && len(fld.Options) == 0 {
			f.focus = i
			f.Err = fld.Label + " is required."
			return nil
		}
	}
	if f.OnSubmit == nil {
		return d.Pop(ctx)
	}
	return f.OnSubmit(ctx, d, vals)
}

// Paste support: a pasted value lands in the focused text field.
func (f *FormView) Paste(text string) {
	fld := &f.Fields[f.focus]
	if len(fld.Options) == 0 {
		fld.Value += strings.ReplaceAll(strings.ReplaceAll(text, "\r", ""), "\n", " ")
	}
}

func (f *FormView) Render(ctx ext.Ctx, th *theme.Theme, width, height int) []string {
	var out []string
	for _, l := range f.Intro {
		out = append(out, Wrap(l, width)...)
	}
	if len(f.Intro) > 0 {
		out = append(out, "")
	}
	labelW := 0
	for _, fld := range f.Fields {
		if w := ansi.StringWidth(fld.Label); w > labelW {
			labelW = w
		}
	}
	for i, fld := range f.Fields {
		focused := i == f.focus
		label := fld.Label + strings.Repeat(" ", labelW-ansi.StringWidth(fld.Label))
		val := fld.Value
		if fld.Secret {
			val = strings.Repeat("•", len([]rune(val)))
		}
		if len(fld.Options) > 0 {
			val = "‹ " + val + " ›"
		}
		mark := "  "
		if focused {
			mark = th.Paint(theme.Suggestion, "❯ ")
			val += th.Paint(theme.Suggestion, "▏")
		}
		line := mark + th.Paint(theme.Inactive, label) + "  " + val
		if fld.Value == "" && !focused && fld.Optional {
			line += th.Paint(theme.Subtle, "(optional)")
		}
		out = append(out, Truncate(line, width))
		if focused && fld.Help != "" {
			out = append(out, Truncate("  "+strings.Repeat(" ", labelW+2)+th.Paint(theme.Subtle, fld.Help), width))
		}
	}
	if f.Err != "" {
		out = append(out, "", th.Paint(theme.Error, f.Err))
	}
	return out
}

func (f *FormView) Hints(ctx ext.Ctx) string {
	return Hint("tab", "next field", "enter", "next/submit", "ctrl+s", "submit", "esc", "cancel")
}
