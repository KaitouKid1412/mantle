package chrome

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/prbadge"
	"github.com/KaitouKid1412/mantle/internal/term/statusline"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// FooterID is the footer component's ID.
const FooterID = "chrome.footer"

// footer is the one-line bar below the prompt, laid out like Claude Code's: the
// permission-mode indicator and one hint on the left (the shortcuts hint in manual
// mode, the mode-cycle key otherwise); the vim mode, the PR badge, footer links,
// background work and the reasoning-effort hint on the right. It steps aside while the
// editor shows a menu or hint below the prompt.
type footer struct {
	s       sessionState
	pr      prWatch
	env     terminal.Env
	fetcher *prbadge.Fetcher
	// statusLine is set when the user has a statusLine command: like Claude Code, the
	// footer then drops the generic shortcuts hint.
	statusLine bool
	hideVim    bool
}

func newFooter() *footer {
	return &footer{s: newSessionState(), env: terminal.OS(), fetcher: prFetcher}
}

func (f *footer) ID() string { return FooterID }

func (f *footer) Init(ctx ext.Ctx) tea.Cmd {
	f.readSettings(ctx)
	return nil
}

func (f *footer) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	hadCwd := f.s.Cwd != ""
	changed := f.s.observe(msg)
	var cmd tea.Cmd
	switch m := msg.(type) {
	case ext.SettingsMsg:
		f.readSettings(ctx)
		changed = true
	case prStatusMsg:
		changed = f.pr.apply(m, ctx.Settings()) || changed
	case ext.EngineEventMsg:
		if isMain(m.EngineID) {
			linksChanged, lookup := f.pr.observeEngine(m.Event)
			changed = changed || linksChanged
			if lookup {
				cmd = f.lookup()
			}
		}
	}
	if !hadCwd && f.s.Cwd != "" {
		cmd = f.lookup() // the session started: find its PR
	}
	if changed {
		ctx.Invalidate(FooterID)
	}
	return cmd
}

// lookup starts a PR lookup for the session's directory.
func (f *footer) lookup() tea.Cmd {
	if !f.pr.enabled || f.s.Cwd == "" || f.fetcher == nil {
		return nil
	}
	force := f.pr.dirty
	f.pr.dirty = false
	return fetchPR(f.fetcher, f.s.Cwd, force)
}

func (f *footer) readSettings(ctx ext.Ctx) {
	cfg := statusLineConfig(ctx.Settings())
	f.statusLine = cfg.Command != ""
	f.hideVim = cfg.HideVimModeIndicator
	f.pr.readSettings(ctx.Settings(), f.env)
}

func (f *footer) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	return ext.Rendered{Text: f.render(ctx, a.Width)}
}

func (f *footer) render(ctx ext.Ctx, w int) string {
	if w <= 0 || f.s.Panel {
		return ""
	}
	t := ctx.Theme()
	ind := IndicatorFor(f.s.Mode)
	tok := theme.Inactive
	if !ind.Quiet && ind.Token != "" {
		tok = theme.Token(ind.Token)
	}
	left := []segment{seg(t, tok, ind.Text(), 0)}
	if ind.Quiet {
		if !f.statusLine && f.s.EditorEmpty && f.s.EditorMode != "bash" {
			left = append(left, seg(t, theme.Inactive, "· ? for shortcuts", 4))
		}
	} else {
		cycle := firstKey(ctx, ext.ContextChat, ext.ActChatCycleMode, "shift+tab")
		left = append(left, seg(t, theme.Inactive, "· "+cycle+" to change", 3))
	}

	var right []segment
	if f.s.Vim != "" && f.s.Vim != "NORMAL" && !f.hideVim {
		right = append(right, seg(t, theme.Text, "-- "+f.s.Vim+" --", 0))
	}
	right = append(right, f.pr.segments(t)...)
	if n := f.s.Background; n > 0 {
		label := "1 background task"
		if n > 1 {
			label = fmt.Sprintf("%d background tasks", n)
		}
		right = append(right, seg(t, theme.Inactive, label+" · /tasks", 2))
	}
	// With a status line the effort hint moves to the status line's first row; in the
	// fullscreen layout it sits above the prompt (effortLine).
	if h := effortHint(ctx, f.s); h != "" && !f.statusLine && ctx.Layout() != ext.Fullscreen {
		right = append(right, seg(t, theme.Inactive, h, 5))
	}
	return bar(left, right, w, ctx.Accessibility().ScreenReader)
}

// defaultEffort is the level Claude Code uses when nothing sets one (the engine's init
// leaves effort out then).
const defaultEffort = "medium"

// effortHint is the reasoning-effort hint ("◑ medium · /effort"): the effort the engine
// reported, else the effortLevel setting, else the default when the model takes an
// effort level; "" when the model doesn't.
func effortHint(ctx ext.Ctx, s sessionState) string {
	e := statusline.NormalizeEffort(s.Effort)
	if e == "" {
		e = statusline.NormalizeEffort(ext.ClaudeString(ctx.Settings(), "effortLevel", ""))
	}
	if e == "" && s.takesEffort() {
		e = defaultEffort
	}
	if e == "" {
		return ""
	}
	return effortGlyph(e) + " " + e + " · /effort"
}

// effortGlyph fills a circle in quarters as the effort rises.
func effortGlyph(e string) string {
	switch e {
	case "low":
		return "◔"
	case "medium":
		return "◑"
	case "high":
		return "◕"
	}
	return "●"
}
