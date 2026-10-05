package chrome

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// EffortLineID is the fullscreen effort line's ID.
const EffortLineID = "chrome.effortLine"

// effortLine shows the reasoning-effort hint right-aligned on its own line above the
// prompt in the fullscreen layout, where Claude Code puts it; inline, the hint is part
// of the footer.
type effortLine struct{ s sessionState }

func (e *effortLine) ID() string           { return EffortLineID }
func (e *effortLine) Init(ext.Ctx) tea.Cmd { return nil }

func (e *effortLine) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	changed := e.s.observe(msg)
	if _, ok := msg.(ext.LayoutChangedMsg); ok {
		changed = true
	}
	if _, ok := msg.(ext.SettingsMsg); ok {
		changed = true
	}
	if changed {
		ctx.Invalidate(EffortLineID)
	}
	return nil
}

func (e *effortLine) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	if ctx.Layout() != ext.Fullscreen || a.Width <= 0 || e.s.Panel {
		return ext.Rendered{}
	}
	hint := effortHint(ctx, e.s)
	if hint == "" {
		return ext.Rendered{}
	}
	inset, inner := footerInset(a.Width)
	hint = ansi.Truncate(hint, inner, "…")
	pad := inset + strings.Repeat(" ", max(0, inner-ansi.StringWidth(hint)))
	// A blank row above, between the transcript and the hint, as in Claude Code (the
	// prompt frame leaves its own out while this line is shown).
	if ctx.Accessibility().ScreenReader {
		return ext.Rendered{Text: "\n" + hint}
	}
	return ext.Rendered{Text: "\n" + pad + ctx.Theme().Paint(theme.Inactive, hint)}
}
