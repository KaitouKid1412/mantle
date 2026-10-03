package chrome

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// FooterID is the footer component's ID.
const FooterID = "chrome.footer"

// footer is the one-line bar below the prompt: the permission-mode indicator on the
// left; the vim mode, background work and the shortcuts hint on the right.
type footer struct {
	s sessionState
	// statusLine is set when the user has a statusLine command: like Claude Code, the
	// footer then drops the generic shortcuts hint.
	statusLine bool
	hideVim    bool
}

func newFooter() *footer { return &footer{s: newSessionState()} }

func (f *footer) ID() string { return FooterID }

func (f *footer) Init(ctx ext.Ctx) tea.Cmd {
	f.readSettings(ctx)
	return nil
}

func (f *footer) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	changed := f.s.observe(msg)
	if _, ok := msg.(ext.SettingsMsg); ok {
		f.readSettings(ctx)
		changed = true
	}
	if changed {
		ctx.Invalidate(FooterID)
	}
	return nil
}

func (f *footer) readSettings(ctx ext.Ctx) {
	cfg := statusLineConfig(ctx.Settings())
	f.statusLine = cfg.Command != ""
	f.hideVim = cfg.HideVimModeIndicator
}

func (f *footer) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	return ext.Rendered{Text: f.render(ctx, a.Width)}
}

func (f *footer) render(ctx ext.Ctx, w int) string {
	if w <= 0 {
		return ""
	}
	t := ctx.Theme()
	ind := IndicatorFor(f.s.Mode)
	tok := theme.Inactive
	if !ind.Quiet && ind.Token != "" {
		tok = theme.Token(ind.Token)
	}
	cycle := firstKey(ctx, ext.ContextChat, ext.ActChatCycleMode, "shift+tab")
	left := []segment{
		seg(t, tok, ind.Text(), 0),
		seg(t, theme.Inactive, "· "+cycle+" to change", 3),
	}

	var right []segment
	if f.s.Vim != "" && f.s.Vim != "NORMAL" && !f.hideVim {
		right = append(right, seg(t, theme.Text, "-- "+f.s.Vim+" --", 0))
	}
	if n := f.s.Background; n > 0 {
		label := "1 background task"
		if n > 1 {
			label = fmt.Sprintf("%d background tasks", n)
		}
		right = append(right, seg(t, theme.Inactive, label+" · /tasks", 2))
	}
	if !f.statusLine && f.s.EditorEmpty && f.s.EditorMode != "bash" {
		right = append(right, seg(t, theme.Inactive, "? for shortcuts", 4))
	}
	return bar(left, right, w, ctx.Accessibility().ScreenReader)
}
