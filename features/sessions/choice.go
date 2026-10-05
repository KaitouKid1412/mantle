package sessions

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// choice is one option of a choiceDialog.
type choice struct {
	label, detail string
	run           func(ext.Ctx) tea.Cmd
	disabled      bool
}

// choiceDialog is a small numbered menu (Select context: up/down, enter, esc, 1-9).
// keys adds single-key actions on the highlighted option ("w" writes a file).
type choiceDialog struct {
	id, title, subtitle string
	choices             []choice
	sel                 int
	keys                map[string]func(ctx ext.Ctx, c choice, i int) tea.Cmd
	hint                string
	onCancel            func(ext.Ctx) tea.Cmd
}

func (d *choiceDialog) ID() string                      { return d.id }
func (d *choiceDialog) Init(ext.Ctx) tea.Cmd            { return nil }
func (d *choiceDialog) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (d *choiceDialog) KeyContext() string              { return ext.ContextSelect }
func (d *choiceDialog) Placement() ext.Placement        { return ext.PlaceInline }

func (d *choiceDialog) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return true, nil }

func (d *choiceDialog) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	defer ctx.Invalidate(d.id)
	switch a {
	case ext.ActSelectNext:
		d.move(1)
	case ext.ActSelectPrevious:
		d.move(-1)
	case ext.ActSelectFirst:
		d.sel = 0
	case ext.ActSelectLast:
		d.sel = len(d.choices) - 1
	case ext.ActSelectAccept:
		return true, d.accept(ctx, d.sel)
	case ext.ActSelectCancel, ext.ActAppInterrupt:
		return true, d.cancel(ctx)
	default:
		return false, nil
	}
	return true, nil
}

func (d *choiceDialog) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	defer ctx.Invalidate(d.id)
	key := k.String()
	switch key {
	case "up":
		d.move(-1)
		return true, nil
	case "down":
		d.move(1)
		return true, nil
	case "enter":
		return true, d.accept(ctx, d.sel)
	case "esc":
		return true, d.cancel(ctx)
	}
	if n, err := strconv.Atoi(key); err == nil && n >= 1 && n <= len(d.choices) {
		d.sel = n - 1
		return true, d.accept(ctx, d.sel)
	}
	if fn := d.keys[key]; fn != nil && d.sel < len(d.choices) {
		return true, tea.Sequence(ctx.CloseDialog(d.id), fn(ctx, d.choices[d.sel], d.sel))
	}
	return true, nil
}

func (d *choiceDialog) move(delta int) {
	n := len(d.choices)
	if n == 0 {
		return
	}
	for i := 0; i < n; i++ {
		d.sel = (d.sel + delta + n) % n
		if !d.choices[d.sel].disabled {
			return
		}
	}
}

func (d *choiceDialog) accept(ctx ext.Ctx, i int) tea.Cmd {
	if i < 0 || i >= len(d.choices) || d.choices[i].disabled {
		return nil
	}
	return tea.Sequence(ctx.CloseDialog(d.id), d.choices[i].run(ctx))
}

func (d *choiceDialog) cancel(ctx ext.Ctx) tea.Cmd {
	cmds := []tea.Cmd{ctx.CloseDialog(d.id)}
	if d.onCancel != nil {
		cmds = append(cmds, d.onCancel(ctx))
	}
	return tea.Sequence(cmds...)
}

func (d *choiceDialog) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	th := ctx.Theme()
	w := max(a.Width, 20)
	lines := []string{fit(th.Fg(theme.Accent).Bold(true).Render(d.title), w)}
	if d.subtitle != "" {
		lines = append(lines, strings.Split(wrapLines([]string{th.Paint(theme.Inactive, d.subtitle)}, w), "\n")...)
	}
	lines = append(lines, "")
	for i, c := range d.choices {
		num := strconv.Itoa(i+1) + ". "
		label := num + c.label
		switch {
		case c.disabled:
			label = "  " + th.Paint(theme.Subtle, label)
		case i == d.sel:
			label = th.Paint(theme.Suggestion, "› ") + th.Fg(theme.Suggestion).Bold(true).Render(label)
		default:
			label = "  " + label
		}
		if c.detail != "" {
			label += th.Paint(theme.Inactive, "  "+c.detail)
		}
		lines = append(lines, fit(label, w))
	}
	hint := "enter select · esc cancel"
	if d.hint != "" {
		hint = d.hint
	}
	lines = append(lines, "", fit(th.Paint(theme.Inactive, hint), w))
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}
