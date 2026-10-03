package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// ScrollPane shows a window of pre-rendered lines and handles the scroll:* actions
// (and j/k/up/down/space/b/g/G directly, less-style). Context defaults to "Scroll";
// use "Transcript" or "DiffDialog" for those views.
type ScrollPane struct {
	IDValue    string
	Lines      []string
	Context    string
	ShowStatus bool // last row shows "lines 1-20 of 80"
	OnExit     func(ext.Ctx) tea.Cmd

	offset int
	height int // last rendered height (for paging)
}

var _ ext.Focusable = (*ScrollPane)(nil)
var _ ext.ActionHandler = (*ScrollPane)(nil)

func (p *ScrollPane) ID() string                      { return p.IDValue }
func (p *ScrollPane) Init(ext.Ctx) tea.Cmd            { return nil }
func (p *ScrollPane) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (p *ScrollPane) KeyContext() string {
	if p.Context != "" {
		return p.Context
	}
	return ext.ContextScroll
}
func (p *ScrollPane) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return false, nil }

// SetLines replaces the content, keeping the offset when possible.
func (p *ScrollPane) SetLines(ls []string) {
	p.Lines = ls
	p.clamp()
}

// Offset returns the first visible line.
func (p *ScrollPane) Offset() int { return p.offset }

// ScrollTo sets the first visible line.
func (p *ScrollPane) ScrollTo(c ext.Ctx, off int) {
	p.offset = off
	p.clamp()
	c.Invalidate(p.IDValue)
}

func (p *ScrollPane) view() int {
	h := p.height
	if h <= 0 {
		h = 10
	}
	if p.ShowStatus {
		h = max(1, h-1)
	}
	return h
}

func (p *ScrollPane) clamp() {
	p.offset = min(max(p.offset, 0), max(0, len(p.Lines)-p.view()))
}

// HandleAction implements scroll:* actions.
func (p *ScrollPane) HandleAction(c ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	h := p.view()
	switch a {
	case ext.ActScrollLineUp:
		p.ScrollTo(c, p.offset-1)
	case ext.ActScrollLineDown:
		p.ScrollTo(c, p.offset+1)
	case ext.ActScrollHalfPageUp:
		p.ScrollTo(c, p.offset-h/2)
	case ext.ActScrollHalfPageDown:
		p.ScrollTo(c, p.offset+h/2)
	case ext.ActScrollPageUp, ext.ActScrollFullPageUp:
		p.ScrollTo(c, p.offset-h)
	case ext.ActScrollPageDown, ext.ActScrollFullPageDown:
		p.ScrollTo(c, p.offset+h)
	case ext.ActScrollTop:
		p.ScrollTo(c, 0)
	case ext.ActScrollBottom:
		p.ScrollTo(c, len(p.Lines))
	case ext.ActTranscriptExit, ext.ActDiffDismiss, ext.ActHelpDismiss:
		if p.OnExit != nil {
			return true, p.OnExit(c)
		}
		return false, nil
	default:
		return false, nil
	}
	return true, nil
}

// HandleKey covers keys the Scroll context leaves unbound.
func (p *ScrollPane) HandleKey(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	var a ext.ActionID
	switch k.Keystroke() {
	case "up", "k":
		a = ext.ActScrollLineUp
	case "down", "j":
		a = ext.ActScrollLineDown
	case "space":
		a = ext.ActScrollFullPageDown
	case "b":
		a = ext.ActScrollFullPageUp
	case "g", "home":
		a = ext.ActScrollTop
	case "shift+g", "end":
		a = ext.ActScrollBottom
	case "esc", "q":
		if p.OnExit != nil {
			return true, p.OnExit(c)
		}
		return false, nil
	default:
		return false, nil
	}
	return p.HandleAction(c, a)
}

// View renders the visible window.
func (p *ScrollPane) View(c ext.Ctx, a ext.Area) ext.Rendered {
	if a.MaxHeight > 0 {
		p.height = a.MaxHeight
	} else {
		p.height = len(p.Lines)
		if p.ShowStatus {
			p.height++
		}
	}
	p.clamp()
	h := p.view()
	end := min(len(p.Lines), p.offset+h)
	var out []string
	for _, l := range p.Lines[p.offset:end] {
		out = append(out, Pad(l, a.Width))
	}
	if p.ShowStatus {
		status := "(empty)"
		if len(p.Lines) > 0 {
			status = fmt.Sprintf("lines %d-%d of %d", p.offset+1, end, len(p.Lines))
		}
		out = append(out, c.Theme().Paint(theme.Inactive, Pad(status, a.Width)))
	}
	return ext.Rendered{Text: strings.Join(out, "\n")}
}
