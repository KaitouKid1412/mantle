package eco

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// View is one screen of a panel: a list, a detail page, a form or a confirm
// prompt. A Dialog shows the top view of its stack; esc pops it.
type View interface {
	// Contexts are the keybinding contexts while this view is on top, most
	// specific first ("Plugin", "Tabs", "Select").
	Contexts() []string
	// Action handles a keymap action; handled=false falls through to the key.
	Action(ctx ext.Ctx, d *Dialog, a ext.ActionID) (bool, tea.Cmd)
	// Key handles a key no action claimed.
	Key(ctx ext.Ctx, d *Dialog, k tea.KeyPressMsg) (bool, tea.Cmd)
	// Update receives broadcast and addressed messages. Every view in the stack
	// gets them, so a list keeps refreshing under a confirm prompt.
	Update(ctx ext.Ctx, d *Dialog, msg tea.Msg) tea.Cmd
	// Render draws the view's body (without the dialog title) into width
	// columns and at most height rows (0 = unbounded).
	Render(ctx ext.Ctx, th *theme.Theme, width, height int) []string
	// Hints is the key-hint line shown under the body.
	Hints(ctx ext.Ctx) string
}

// Initer is an optional View interface: the root view's Init runs when the
// dialog opens (start loading data there).
type Initer interface {
	Init(ctx ext.Ctx, d *Dialog) tea.Cmd
}

// Titler is an optional View interface that replaces the dialog subtitle.
type Titler interface {
	Subtitle() string
}

// Dialog is an inline panel built from a stack of views. It implements
// ext.Dialog, ext.ActionHandler and ext.ContextStack.
type Dialog struct {
	id     string
	Title  string
	views  []View
	closed bool
	// Status is a transient line under the title (errors, "Working…").
	Status    string
	StatusTok theme.Token
	// CloseLine is printed into the transcript when the dialog closes, as a
	// result line under the command's echo. NewDialog sets "<Title> closed";
	// dialogs whose outcome already shows (a notice, a hand-off) clear it.
	CloseLine string
}

// NewDialog returns a dialog with root as its first view.
func NewDialog(id, title string, root View) *Dialog {
	return &Dialog{id: id, Title: title, views: []View{root}, CloseLine: title + " closed"}
}

func (d *Dialog) ID() string               { return d.id }
func (d *Dialog) Placement() ext.Placement { return ext.PlaceInline }
func (d *Dialog) KeyContext() string       { return d.KeyContexts()[0] }
func (d *Dialog) Top() View                { return d.views[len(d.views)-1] }
func (d *Dialog) Depth() int               { return len(d.views) }
func (d *Dialog) Closed() bool             { return d.closed }
func (d *Dialog) Root() View               { return d.views[0] }

// Init runs the root view's Init, if it has one.
func (d *Dialog) Init(ctx ext.Ctx) tea.Cmd {
	if v, ok := d.views[0].(Initer); ok {
		return v.Init(ctx, d)
	}
	return nil
}

// HandlePaste gives pasted text to a view that takes it (form fields).
func (d *Dialog) HandlePaste(ctx ext.Ctx, p tea.PasteMsg) (bool, tea.Cmd) {
	if v, ok := d.Top().(interface{ Paste(string) }); ok {
		v.Paste(p.Content)
		ctx.Invalidate(d.id)
		return true, nil
	}
	return false, nil
}

// KeyContexts implements ext.ContextStack.
func (d *Dialog) KeyContexts() []string {
	if c := d.Top().Contexts(); len(c) > 0 {
		return c
	}
	return []string{ext.ContextSelect}
}

// Push shows v on top.
func (d *Dialog) Push(ctx ext.Ctx, v View) {
	d.views = append(d.views, v)
	ctx.Invalidate(d.id)
}

// Pop removes the top view; popping the root closes the dialog.
func (d *Dialog) Pop(ctx ext.Ctx) tea.Cmd {
	if len(d.views) <= 1 {
		return d.Close(ctx)
	}
	d.views = d.views[:len(d.views)-1]
	ctx.Invalidate(d.id)
	return nil
}

// Close closes the dialog.
func (d *Dialog) Close(ctx ext.Ctx) tea.Cmd {
	if d.closed {
		return nil
	}
	d.closed = true
	if d.CloseLine == "" {
		return ctx.CloseDialog(d.id)
	}
	line := "  ⎿  " + ctx.Theme().Paint(theme.Inactive, d.CloseLine)
	return tea.Batch(ctx.CloseDialog(d.id), ctx.Print(line))
}

// SetStatus sets the status line ("" clears it).
func (d *Dialog) SetStatus(ctx ext.Ctx, text string, tok theme.Token) {
	d.Status, d.StatusTok = text, tok
	ctx.Invalidate(d.id)
}

// HandleAction implements ext.ActionHandler: the top view first, then the
// generic cancel actions pop the view.
func (d *Dialog) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	if ok, cmd := d.Top().Action(ctx, d, a); ok {
		ctx.Invalidate(d.id)
		return true, cmd
	}
	switch a {
	case ext.ActSelectCancel, ext.ActConfirmNo, ext.ActHelpDismiss:
		return true, d.Pop(ctx)
	}
	return false, nil
}

// HandleKey gives unclaimed keys to the top view; esc and ctrl+c always leave.
func (d *Dialog) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if ok, cmd := d.Top().Key(ctx, d, k); ok {
		ctx.Invalidate(d.id)
		return true, cmd
	}
	switch k.String() {
	case "esc", "escape":
		return true, d.Pop(ctx)
	case "ctrl+c":
		return true, d.Close(ctx)
	}
	return false, nil
}

// Update forwards messages to every view. Views change their state on these
// results, so the host's cached render is invalidated after them; other
// broadcast messages (stream deltas, ticks) leave the cache alone.
func (d *Dialog) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	addressed := false
	if am, ok := msg.(ext.AddressedMsg); ok {
		msg, addressed = am.Msg, true
	}
	var cmds []tea.Cmd
	for _, v := range append([]View(nil), d.views...) {
		if cmd := v.Update(ctx, d, msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	switch msg.(type) {
	case ResultMsg, ExecDoneMsg, ext.ControlResultMsg, ext.SettingsMsg:
		ctx.Invalidate(d.id)
	default:
		if addressed {
			ctx.Invalidate(d.id)
		}
	}
	return tea.Batch(cmds...)
}

// View renders the title, status, top view and hints.
func (d *Dialog) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	th := ctx.Theme()
	w := a.Width
	if w < 20 {
		w = 20
	}
	var lines []string
	title := th.Fg(theme.Accent).Bold(true).Render(d.Title)
	if t, ok := d.Top().(Titler); ok && t.Subtitle() != "" {
		title += th.Paint(theme.Inactive, " · "+t.Subtitle())
	}
	lines = append(lines, Truncate(title, w))
	if d.Status != "" {
		tok := d.StatusTok
		if tok == "" {
			tok = theme.Inactive
		}
		for _, l := range Wrap(d.Status, w-2) {
			lines = append(lines, "  "+th.Paint(tok, l))
		}
	}
	lines = append(lines, "")
	bodyH := 0
	if a.MaxHeight > 0 {
		bodyH = a.MaxHeight - len(lines) - 2
		if bodyH < 3 {
			bodyH = 3
		}
	}
	for _, l := range d.Top().Render(ctx, th, w-2, bodyH) {
		lines = append(lines, Truncate("  "+l, w))
	}
	if h := d.Top().Hints(ctx); h != "" {
		lines = append(lines, "")
		for _, l := range Wrap(h, w-2) {
			lines = append(lines, "  "+th.Paint(theme.Inactive, l))
		}
	}
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}

// Truncate cuts a styled line to width cells.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// Wrap word-wraps plain text to width cells.
func Wrap(s string, width int) []string {
	if width < 10 {
		width = 10
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		w := ansi.Wordwrap(para, width, "")
		w = ansi.Hardwrap(w, width, true)
		out = append(out, strings.Split(w, "\n")...)
	}
	return out
}

// Hint joins key hints: Hint("enter", "open", "esc", "close") → "enter open · esc close".
func Hint(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i] == "" {
			continue
		}
		parts = append(parts, pairs[i]+" "+pairs[i+1])
	}
	return strings.Join(parts, " · ")
}

// KeyName returns the first key bound to an action in a context, for hints.
func KeyName(ctx ext.Ctx, context string, a ext.ActionID, fallback string) string {
	if keys := ctx.KeysFor(context, a); len(keys) > 0 {
		return keys[0]
	}
	return fallback
}

// Factory adapts a constructor to an ext.DialogFactory.
func Factory(build func(ext.Ctx, any) (*Dialog, error)) ext.DialogFactory {
	return func(ctx ext.Ctx, args any) (ext.Dialog, error) { return build(ctx, args) }
}
