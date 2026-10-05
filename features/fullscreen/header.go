package fullscreen

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// HeaderID is the sticky prompt header component.
const HeaderID = "fullscreen.header"

// stickyHeader shows the user prompt of the turn in view while the reader is scrolled up
// and that prompt is above the top of the viewport, so they know which question an
// answer belongs to. At the bottom (following new output) it stays hidden, as in Claude
// Code.
type stickyHeader struct {
	tv *transcriptView
}

func (h *stickyHeader) ID() string           { return HeaderID }
func (h *stickyHeader) Init(ext.Ctx) tea.Cmd { return nil }
func (h *stickyHeader) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	// The header follows the viewport, which changes on every scroll or new item.
	ctx.Invalidate(HeaderID)
	return nil
}

func (h *stickyHeader) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	if ctx.Layout() != ext.Fullscreen || a.Width <= 0 {
		return ext.Rendered{}
	}
	// The host lays the header out before the viewport: bring the viewport up to date
	// first (cheap: rendered items are cached).
	if h.tv.area.Width > 0 {
		h.tv.layout(ctx, h.tv.area)
	}
	text, ok := h.prompt()
	if !ok {
		return ext.Rendered{}
	}
	return ext.Rendered{Text: renderHeader(ctx.Theme(), text, a.Width)}
}

// renderHeader is the prompt line.
func renderHeader(t *theme.Theme, text string, w int) string {
	return t.Paint(theme.Inactive, "❯ ") + t.Paint(theme.Text, ansi.Truncate(text, max(1, w-2), "…"))
}

// prompt finds the last user prompt starting above the viewport's first line.
func (h *stickyHeader) prompt() (string, bool) {
	tv := h.tv
	top := tv.vp.Offset()
	if top == 0 || len(tv.items) == 0 || tv.vp.Following() {
		return "", false
	}
	b, _, ok := tv.vp.BlockAt(top)
	if !ok {
		return "", false
	}
	for i := b; i >= 0; i-- {
		if tv.items[i] == nil || tv.items[i].Key != ext.KeyUserPrompt {
			continue
		}
		if tv.vp.BlockStart(i)+1 >= top { // the prompt line itself is still visible
			return "", false
		}
		for _, l := range tv.blocks[i].Lines {
			if s := strings.TrimSpace(ansi.Strip(l)); s != "" {
				return strings.TrimLeft(s, "❯>› "), true
			}
		}
	}
	return "", false
}
