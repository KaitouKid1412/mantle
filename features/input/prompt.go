package input

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor"
)

// prefixWidth is the width of the prompt marker ("> " or "! ").
const prefixWidth = 2

// promptComp is the prompt editor in SlotInput. Chrome wraps it with the
// prompt frame; it draws no border itself.
type promptComp struct{ s *state }

var (
	_ ext.Focusable     = (*promptComp)(nil)
	_ ext.ActionHandler = (*promptComp)(nil)
	_ ext.ContextStack  = (*promptComp)(nil)
	_ ext.FocusAware    = (*promptComp)(nil)
)

func (p *promptComp) ID() string { return ComponentID }

func (p *promptComp) Init(c ext.Ctx) tea.Cmd { return nil }

func (p *promptComp) KeyContext() string { return ext.ContextChat }

// KeyContexts is the active context stack, most specific first.
func (p *promptComp) KeyContexts() []string {
	s := p.s
	switch {
	case s.search != nil:
		return []string{ext.ContextHistorySearch}
	case s.ed.InAttachments():
		return []string{ext.ContextAttachments, ext.ContextChat}
	case s.comp.open():
		return []string{ext.ContextAutocomplete, ext.ContextChat}
	}
	return []string{ext.ContextChat}
}

func (p *promptComp) OnFocus(c ext.Ctx) tea.Cmd { p.s.ed.Focus(); p.s.invalidate(c); return nil }
func (p *promptComp) OnBlur(c ext.Ctx) tea.Cmd  { p.s.ed.Blur(); p.s.invalidate(c); return nil }

func (p *promptComp) HandleAction(c ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	return p.s.action(c, a)
}

func (p *promptComp) HandleKey(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	return p.s.key(c, k)
}

func (p *promptComp) HandlePaste(c ext.Ctx, m tea.PasteMsg) (bool, tea.Cmd) {
	return p.s.paste(c, m.Content)
}

func (p *promptComp) Update(c ext.Ctx, msg tea.Msg) tea.Cmd { return p.s.update(c, msg) }

func (p *promptComp) View(c ext.Ctx, a ext.Area) ext.Rendered { return p.s.viewPrompt(c, a) }

// menuComp shows the autocomplete menu, the shortcut help and the history
// search line below the prompt.
type menuComp struct{ s *state }

func (m *menuComp) ID() string                              { return MenuID }
func (m *menuComp) Init(ext.Ctx) tea.Cmd                    { return nil }
func (m *menuComp) Update(ext.Ctx, tea.Msg) tea.Cmd         { return nil }
func (m *menuComp) View(c ext.Ctx, a ext.Area) ext.Rendered { return m.s.viewMenu(c, a) }

// ---- keys and pastes ----

func (s *state) key(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if s.search != nil {
		return s.searchKey(c, k)
	}
	ks := k.Keystroke()
	text := ""
	if k.Mod&^tea.ModShift == 0 {
		text = k.Text
	}
	wasHelp := s.help
	s.help = false
	if s.ed.Empty() && s.mode == modePrompt && !s.vimNormal() && !s.ed.InAttachments() {
		switch text {
		case "?":
			s.help = !wasHelp
			s.invalidate(c)
			return true, nil
		case "!":
			s.mode = modeBash
			return true, s.changed(c)
		}
	}
	if s.mode == modeBash && s.ed.Empty() && ks == "backspace" {
		s.mode = modePrompt
		return true, s.changed(c)
	}
	handled, cmd := s.ed.HandleKey(k)
	if !handled && ks == "tab" && s.ed.AcceptGhost() {
		handled = true
	}
	if !handled {
		if wasHelp {
			s.invalidate(c)
		}
		return false, nil
	}
	s.afterTyping(c, text)
	return true, tea.Batch(cmd, s.changed(c))
}

func (s *state) paste(c ext.Ctx, content string) (bool, tea.Cmd) {
	if s.search != nil {
		s.search.query += editor.SanitizePaste(strings.ReplaceAll(content, "\n", " "))
		s.search.refresh(s)
		s.invalidate(c)
		return true, nil
	}
	s.help = false
	_, cmd := s.ed.InsertPaste(content)
	return true, tea.Batch(cmd, s.changed(c))
}

// changed runs after any edit or cursor move: menus, ghost text, the
// editor state broadcast and a re-render.
func (s *state) changed(c ext.Ctx) tea.Cmd {
	s.escAt = timeZero
	cmd := s.comp.update(c, s)
	s.invalidate(c)
	return tea.Batch(cmd, s.stateCmd(false))
}

// ---- view ----

func (s *state) viewPrompt(c ext.Ctx, a ext.Area) ext.Rendered {
	t := c.Theme()
	s.applyTheme(t)
	w := a.Width - prefixWidth
	if w < 1 {
		w = 1
	}
	if s.search != nil {
		return s.viewSearchPreview(c, a)
	}
	s.ed.SetWidth(w)
	s.ed.SetMaxHeight(a.MaxHeight)
	if s.ed.Empty() && s.ed.Ghost() == "" {
		s.ed.SetPlaceholder(s.placeholder())
	}
	rows, cur := s.ed.Render()

	marker := "> "
	tok := theme.Inactive
	if s.mode == modeBash {
		marker, tok = "! ", theme.BashBorder
	}
	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(" ", prefixWidth))
		} else {
			b.WriteString(t.Paint(tok, marker))
		}
		b.WriteString(r)
	}
	out := ext.Rendered{Text: b.String()}
	if cur != nil && a.Focused {
		cur.X += prefixWidth
		out.Cursor = cur
	}
	return out
}

func (s *state) placeholder() string {
	switch {
	case s.mode == modeBash:
		return "Run a shell command"
	case s.busy:
		return ""
	case s.hist.Len() == 0:
		return `Try "explain how this project is organized"`
	}
	return ""
}

func (s *state) viewMenu(c ext.Ctx, a ext.Area) ext.Rendered {
	t := c.Theme()
	var lines []string
	switch {
	case s.search != nil:
		lines = s.search.view(t, a.Width)
	case s.comp.open():
		lines = s.comp.view(t, a.Width)
	case s.help:
		lines = s.helpLines(c, a.Width)
	}
	for i, l := range lines {
		if ansi.StringWidth(l) > a.Width {
			lines[i] = ansi.Truncate(l, a.Width, "…")
		}
	}
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}
