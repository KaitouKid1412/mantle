package chrome

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// EditorID is plan 04's prompt editor component, which the frame wraps.
const EditorID = "input.editor"

// promptFrame draws a rule above and below the prompt editor. The rules take the
// mode's colour (bash, plan) and the top rule carries the session name. It wraps the
// editor component, so it delegates everything else to it, keeps its ID and moves its
// cursor down one row.
type promptFrame struct {
	next ext.Component
	s    sessionState
}

// wrapFrame is the Wrap function registered for EditorID.
func wrapFrame(next ext.Component) ext.Component {
	return &promptFrame{next: next, s: newSessionState()}
}

func (f *promptFrame) ID() string { return f.next.ID() }

func (f *promptFrame) Init(ctx ext.Ctx) tea.Cmd { return f.next.Init(ctx) }

func (f *promptFrame) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	before := f.s
	if f.s.observe(msg) && f.frameChanged(before) {
		ctx.Invalidate(f.ID())
	}
	return f.next.Update(ctx, msg)
}

func (f *promptFrame) frameChanged(before sessionState) bool {
	return before.Mode != f.s.Mode || before.EditorMode != f.s.EditorMode || before.Title != f.s.Title
}

func (f *promptFrame) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	if ctx.Accessibility().ScreenReader {
		return f.next.View(ctx, a) // no box drawing in screen-reader mode
	}
	inner := a
	if a.MaxHeight > 0 {
		inner.MaxHeight = max(1, a.MaxHeight-2)
	}
	r := f.next.View(ctx, inner)
	if r.Text == "" || a.Width <= 0 {
		return r
	}
	tok := frameToken(f.s.EditorMode, f.s.Mode)
	t := ctx.Theme()
	top := frameRule(t, tok, a.Width, f.s.Title) // custom names only (-n, /rename)
	bottom := frameRule(t, tok, a.Width, "")
	if r.Cursor != nil {
		c := *r.Cursor
		c.Y++
		r.Cursor = &c
	}
	r.Text = top + "\n" + r.Text + "\n" + bottom
	return r
}

// frameToken picks the frame colour: bash input wins, then plan mode.
func frameToken(editorMode, permMode string) theme.Token {
	switch {
	case editorMode == "bash":
		return theme.BashBorder
	case permMode == ModePlan:
		return theme.PlanMode
	}
	return theme.PromptBorder
}

// frameRule is a full-width rule, with label near its right end when it fits.
func frameRule(t *theme.Theme, tok theme.Token, w int, label string) string {
	label = strings.TrimSpace(label)
	if label != "" {
		label = ansi.Truncate(label, max(0, w/2), "…")
		lw := ansi.StringWidth(label)
		if lw > 0 && w >= lw+6 {
			left := w - lw - 4
			return t.Paint(tok, strings.Repeat("─", left)+" ") + t.Paint(theme.Inactive, label) +
				t.Paint(tok, " "+strings.Repeat("─", 2))
		}
	}
	return t.Paint(tok, strings.Repeat("─", w))
}

// Delegation of the editor's optional interfaces: the host only sees the outermost
// component.

func (f *promptFrame) KeyContext() string {
	if fc, ok := f.next.(ext.Focusable); ok {
		return fc.KeyContext()
	}
	return ""
}

func (f *promptFrame) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if fc, ok := f.next.(ext.Focusable); ok {
		return fc.HandleKey(ctx, k)
	}
	return false, nil
}

func (f *promptFrame) HandlePaste(ctx ext.Ctx, p tea.PasteMsg) (bool, tea.Cmd) {
	if fc, ok := f.next.(ext.Focusable); ok {
		return fc.HandlePaste(ctx, p)
	}
	return false, nil
}

func (f *promptFrame) KeyContexts() []string {
	if cs, ok := f.next.(ext.ContextStack); ok {
		return cs.KeyContexts()
	}
	if k := f.KeyContext(); k != "" {
		return []string{k}
	}
	return nil
}

func (f *promptFrame) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	if h, ok := f.next.(ext.ActionHandler); ok {
		return h.HandleAction(ctx, a)
	}
	return false, nil
}

func (f *promptFrame) OnFocus(ctx ext.Ctx) tea.Cmd {
	if fa, ok := f.next.(ext.FocusAware); ok {
		return fa.OnFocus(ctx)
	}
	return nil
}

func (f *promptFrame) OnBlur(ctx ext.Ctx) tea.Cmd {
	if fa, ok := f.next.(ext.FocusAware); ok {
		return fa.OnBlur(ctx)
	}
	return nil
}

func (f *promptFrame) TerminalState(ctx ext.Ctx) ext.TerminalState {
	if ts, ok := f.next.(ext.TerminalStater); ok {
		return ts.TerminalState(ctx)
	}
	return ext.TerminalState{}
}

var (
	_ ext.Focusable      = (*promptFrame)(nil)
	_ ext.ContextStack   = (*promptFrame)(nil)
	_ ext.ActionHandler  = (*promptFrame)(nil)
	_ ext.FocusAware     = (*promptFrame)(nil)
	_ ext.TerminalStater = (*promptFrame)(nil)
)
