package fullscreen

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/term/clipcmd"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// ViewportID is the fullscreen transcript viewport component.
const ViewportID = "fullscreen.viewport"

// EditorID is the prompt editor, where focus returns after the viewport.
const EditorID = "input.editor"

const (
	doubleClick = 400 * time.Millisecond
	tickEvery   = 500 * time.Millisecond
	copyTag     = "fullscreen.selection"
)

type tickMsg struct{ gen int }

// transcriptView is the virtual transcript in the fullscreen layout: the same items and
// renderers as the inline transcript, scrolled with the keyboard and wheel (through
// the Scroll context's actions), with mouse selection and copy, click-to-expand, hover
// and an in-place search when focused.
type transcriptView struct {
	r  *renderer
	vp *Viewport

	blocks      []Block
	collapsible []bool
	items       []*ext.Item
	width       int      // render width (area width minus the gutter)
	area        ext.Area // the last area laid out
	top         int      // first document line on screen (for mouse mapping)
	shown       int      // lines returned by the last View (mouse rows index into them)

	sel       Selection
	dragging  bool
	clickAt   time.Time
	clickPos  Pos
	clicks    int
	dragMoved bool
	hover     int // block under the pointer (-1 none)

	focused    bool
	search     Search
	typing     bool // reading a search query
	query      string
	wheel      Wheel
	tickGen    int
	ticking    bool
	autoScroll bool
	virtual    bool
	ready      bool // Scroll context activated

	panes paneTracker
	print printed // what features printed (Ctx.Print) in this layout

	env terminal.Env
}

func newTranscriptView(env terminal.Env) *transcriptView {
	return &transcriptView{r: newRenderer(), vp: NewViewport(1), hover: -1, autoScroll: true, virtual: true, env: env}
}

func (v *transcriptView) ID() string { return ViewportID }

func (v *transcriptView) Init(ctx ext.Ctx) tea.Cmd {
	v.readSettings(ctx)
	return nil
}

func (v *transcriptView) readSettings(ctx ext.Ctx) {
	s := ctx.Settings()
	v.wheel.Base = scrollSpeed(s, v.env)
	v.wheel.Accelerate = ext.ClaudeBool(s, "wheelScrollAccelerationEnabled", true)
	v.autoScroll = ext.ClaudeBool(s, "autoScrollEnabled", true)
	v.virtual = !truthy(terminal.Get(v.env, "CLAUDE_CODE_DISABLE_VIRTUAL_SCROLL"))
}

func (v *transcriptView) active(ctx ext.Ctx) bool { return ctx.Layout() == ext.Fullscreen }

func (v *transcriptView) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	if !v.active(ctx) {
		if v.ready {
			ctx.SetContextActive(ext.ContextScroll, false)
			v.ready = false
		}
		return nil
	}
	if !v.ready {
		ctx.SetContextActive(ext.ContextScroll, true)
		v.ready = true
	}
	v.panes.observe(msg, v.area.Width)
	switch m := msg.(type) {
	case ext.MouseEvent:
		return v.mouse(ctx, m)
	case ext.LayoutChangedMsg:
		v.invalidate(ctx)
	case tickMsg:
		if m.gen != v.tickGen {
			return nil
		}
		v.ticking = false
		v.invalidate(ctx)
		return v.maybeTick(ctx)
	case clipcmd.CopiedMsg:
		if m.Tag == copyTag {
			return ctx.Notify(ext.Notice{Key: copyTag, Text: m.Notice(), Level: noticeLevel(m.Err), Source: ViewportID})
		}
	case ext.SettingsMsg:
		v.readSettings(ctx)
		v.invalidate(ctx)
	case ext.PrintedMsg:
		v.print.add(ctx.Transcript(), m.Blocks)
		v.invalidate(ctx)
		return nil
	case ext.ScreenClearedMsg:
		v.print.reset() // printed blocks are gone with a cleared screen, as scrollback is
		v.invalidate(ctx)
		return v.maybeTick(ctx)
	case ext.EngineEventMsg, ext.TranscriptHistoryMsg, ext.ThemeChangedMsg,
		ext.TranscriptAttachMsg, tea.WindowSizeMsg:
		v.invalidate(ctx)
		return v.maybeTick(ctx)
	}
	return nil
}

// invalidate redraws the viewport and the sticky header that follows it.
func (v *transcriptView) invalidate(ctx ext.Ctx) {
	ctx.Invalidate(ViewportID)
	ctx.Invalidate(HeaderID)
}

func noticeLevel(err error) ext.NoticeLevel {
	if err != nil {
		return ext.NoticeError
	}
	return ext.NoticeSuccess
}

// maybeTick keeps redrawing while an item is running (elapsed times, spinners).
func (v *transcriptView) maybeTick(ctx ext.Ctx) tea.Cmd {
	if v.ticking {
		return nil
	}
	for _, it := range v.items {
		if it != nil && (it.State == ext.Running || it.State == ext.Streaming) {
			v.ticking = true
			v.tickGen++
			gen := v.tickGen
			return ctx.Clock().Tick(tickEvery, func(time.Time) tea.Msg {
				return ext.AddressedMsg{To: ViewportID, Msg: tickMsg{gen}}
			})
		}
	}
	return nil
}

// layout re-renders changed items at the area width and updates the viewport.
func (v *transcriptView) layout(ctx ext.Ctx, a ext.Area) {
	v.area = a
	v.width = max(10, a.Width-1) // one column of gutter, like the inline print width
	v.blocks, v.collapsible, v.items = v.r.blocks(ctx, v.width)
	v.blocks, v.collapsible, v.items = v.print.merge(ctx.Transcript(), v.blocks, v.collapsible, v.items, v.width)
	follow := v.vp.Following()
	v.vp.SetHeight(max(1, a.MaxHeight))
	if !v.autoScroll && follow {
		// Without auto-scroll the view stays put when content arrives.
		prevOff := v.vp.Offset()
		v.vp.SetBlocks(v.blocks)
		if v.vp.Total() > v.vp.Height {
			v.vp.Top()
			v.vp.ScrollBy(prevOff)
		}
		return
	}
	v.vp.SetBlocks(v.blocks)
}

// line returns a document line's styled text.
func (v *transcriptView) line(n int) string {
	b, w, ok := v.vp.BlockAt(n)
	if !ok {
		return ""
	}
	return v.blocks[b].Lines[w]
}

func (v *transcriptView) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	if !v.active(ctx) || a.Width <= 0 {
		return ext.Rendered{}
	}
	v.layout(ctx, a)
	return v.paint(ctx.Theme(), a)
}

// paint draws the visible window of the laid-out document.
func (v *transcriptView) paint(t *theme.Theme, a ext.Area) ext.Rendered {
	h := v.vp.Height
	from := v.vp.Offset()
	to := min(v.vp.Total(), from+h)
	if !v.virtual {
		from, to = 0, v.vp.Total() // the host shows the tail; mouse mapping uses offset 0
	}
	v.top = from
	lines := make([]string, 0, to-from)
	for n := from; n < to; n++ {
		lines = append(lines, v.decorate(t, n, v.line(n)))
	}
	if v.typing || (v.focused && v.search.Query != "") {
		lines = v.withSearchBar(t, lines, a.Width)
	} else if p := v.pill(t); p != "" && len(lines) > 0 {
		lines[len(lines)-1] = withPill(lines[len(lines)-1], p, a.Width)
	}
	// Mouse coordinates are relative to the lines returned here (the host places a
	// short document at the bottom of the band, but the region is only as tall as it).
	v.shown = len(lines)
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}

// decorate applies the selection, search matches and the hover mark to one line.
func (v *transcriptView) decorate(t *theme.Theme, n int, s string) string {
	reverse := func(x string) string { return t.Bg(theme.SelectionBg).Render(x) }
	if v.sel.Active && !v.sel.Empty() {
		s = Highlight(s, n, v.sel, reverse)
	}
	for _, m := range v.search.OnLine(n) {
		cur, _ := v.search.At()
		mark := func(x string) string { return t.Bg(theme.Warning).Foreground(t.Color(theme.InverseText)).Render(x) }
		if cur == m {
			mark = func(x string) string {
				return t.Bg(theme.Suggestion).Foreground(t.Color(theme.InverseText)).Bold(true).Render(x)
			}
		}
		s = Highlight(s, n, Selection{Anchor: Pos{n, m.Col}, Head: Pos{n, m.End}, Active: true}, mark)
	}
	if b, w, ok := v.vp.BlockAt(n); ok && b == v.hover && v.collapsible[b] && w == 1 {
		s += t.Paint(theme.Inactive, "  (click to expand)")
	}
	return s
}

// pill is the "jump to bottom" hint shown whenever the reader is scrolled up: with the
// count of new lines when output arrived below, as a plain hint otherwise.
func (v *transcriptView) pill(t *theme.Theme) string {
	switch n := v.vp.Unseen(); {
	case v.vp.Following():
		return ""
	case n == 0:
		return t.Paint(theme.Inactive, "↓ jump to bottom · ctrl+end")
	case n == 1:
		return t.Bg(theme.Suggestion).Foreground(t.Color(theme.InverseText)).Render(" ↓ 1 new line · ctrl+end ")
	default:
		return t.Bg(theme.Suggestion).Foreground(t.Color(theme.InverseText)).Render(fmt.Sprintf(" ↓ %d new lines · ctrl+end ", n))
	}
}

// withPill right-aligns the pill on the last visible line when both fit, and replaces
// the line otherwise.
func withPill(line, pill string, w int) string {
	lw, pw := ansi.StringWidth(line), ansi.StringWidth(pill)
	if pw > w {
		return ansi.Truncate(pill, w, "")
	}
	if lw+4+pw <= w {
		return line + strings.Repeat(" ", w-lw-pw) + pill
	}
	return strings.Repeat(" ", w-pw) + pill
}

func (v *transcriptView) withSearchBar(t *theme.Theme, lines []string, w int) []string {
	status := ""
	switch {
	case v.typing:
		status = "/" + v.query
	case len(v.search.Matches) == 0:
		status = "no matches for " + v.search.Query
	default:
		status = fmt.Sprintf("/%s · %d of %d · n/N next/previous · esc done", v.search.Query, v.search.Current+1, len(v.search.Matches))
	}
	bar := t.Paint(theme.Inactive, ansi.Truncate(status, w, "…"))
	if len(lines) == 0 {
		return []string{bar}
	}
	lines[len(lines)-1] = bar
	return lines
}

// ---- mouse ----

func (v *transcriptView) docPos(x, y int) Pos {
	line := max(0, min(v.top+y, v.vp.Total()-1))
	return Pos{Line: line, Col: max(0, x)}
}

func (v *transcriptView) mouse(ctx ext.Ctx, ev ext.MouseEvent) tea.Cmd {
	m := ev.Msg.Mouse()
	switch ev.Msg.(type) {
	case tea.MouseWheelMsg:
		return nil // wheel scrolling arrives as scroll:* actions (Scroll context)
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			return nil
		}
		p := v.docPos(ev.X, ev.Y)
		now := ctx.Clock().Now()
		if now.Sub(v.clickAt) <= doubleClick && abs(p.Line-v.clickPos.Line) == 0 && abs(p.Col-v.clickPos.Col) <= 1 {
			v.clicks = v.clicks%3 + 1
		} else {
			v.clicks = 1
		}
		v.clickAt, v.clickPos = now, p
		switch v.clicks {
		case 1:
			v.sel = Selection{Anchor: p, Head: p, Active: true}
		case 2:
			v.sel = WordAt(v.line, p)
		case 3:
			v.sel = LineAt(v.line, p.Line)
		}
		v.dragging, v.dragMoved = true, false
		v.invalidate(ctx)
		return v.takeFocus(ctx)
	case tea.MouseMotionMsg:
		if v.dragging && m.Button == tea.MouseLeft {
			v.sel.Head = v.docPos(ev.X, ev.Y)
			v.dragMoved = true
			v.invalidate(ctx)
			return nil
		}
		b, _, ok := v.vp.BlockAt(v.docPos(ev.X, ev.Y).Line)
		if !ok {
			b = -1
		}
		if b != v.hover {
			v.hover = b
			v.invalidate(ctx)
		}
	case tea.MouseReleaseMsg:
		if !v.dragging {
			return nil
		}
		v.dragging = false
		if v.clicks == 1 && !v.dragMoved {
			v.sel = Selection{}
			return v.toggleExpand(ctx, v.docPos(ev.X, ev.Y).Line)
		}
		if !v.sel.Empty() && ext.ClaudeBool(ctx.Settings(), "copyOnSelect", true) {
			return v.copy(ctx)
		}
	}
	return nil
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (v *transcriptView) toggleExpand(ctx ext.Ctx, line int) tea.Cmd {
	b, _, ok := v.vp.BlockAt(line)
	if !ok || !v.collapsible[b] {
		return nil
	}
	v.r.toggle(v.blocks[b].ID)
	v.invalidate(ctx)
	return nil
}

// copy copies the selection's plain text.
func (v *transcriptView) copy(ctx ext.Ctx) tea.Cmd {
	text := Text(v.line, v.sel, v.width)
	if text == "" {
		return nil
	}
	return clipcmd.Copy(copyTag, text)
}

func (v *transcriptView) takeFocus(ctx ext.Ctx) tea.Cmd {
	if ctx.Focused() == ViewportID {
		return nil
	}
	return ctx.Focus(ViewportID)
}

// ---- focus and keys ----

func (v *transcriptView) KeyContext() string { return ext.ContextScroll }

func (v *transcriptView) OnFocus(ctx ext.Ctx) tea.Cmd {
	v.focused = true
	v.invalidate(ctx)
	return nil
}

func (v *transcriptView) OnBlur(ctx ext.Ctx) tea.Cmd {
	v.focused, v.typing = false, false
	v.invalidate(ctx)
	return nil
}

func (v *transcriptView) HandlePaste(ctx ext.Ctx, _ tea.PasteMsg) (bool, tea.Cmd) {
	return true, ctx.Focus(EditorID)
}

// HandleKey: less-style keys while the transcript has focus. "/" searches; any other
// printable key hands focus back to the prompt.
func (v *transcriptView) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	v.invalidate(ctx)
	key := k.String()
	if v.typing {
		switch key {
		case "enter":
			v.typing = false
			v.runSearch()
		case "esc":
			v.typing, v.query = false, ""
		case "backspace":
			if r := []rune(v.query); len(r) > 0 {
				v.query = string(r[:len(r)-1])
			}
		default:
			if k.Text != "" {
				v.query += k.Text
			}
		}
		return true, nil
	}
	switch key {
	case "/":
		v.typing, v.query = true, ""
	case "n":
		v.jump(v.search.Next())
	case "N", "shift+n":
		v.jump(v.search.Prev())
	case "j", "down":
		v.vp.ScrollBy(1)
	case "k", "up":
		v.vp.ScrollBy(-1)
	case "space", "f":
		v.vp.Page(1)
	case "b":
		v.vp.Page(-1)
	case "d":
		v.vp.HalfPage(1)
	case "u":
		v.vp.HalfPage(-1)
	case "g", "home":
		v.vp.Top()
	case "G", "shift+g", "end":
		v.vp.Bottom()
	case "enter":
		if b, _, ok := v.vp.BlockAt(v.vp.Offset()); ok {
			return true, v.toggleExpand(ctx, v.vp.BlockStart(b))
		}
	case "esc", "q", "i":
		if v.search.Query != "" {
			v.search = Search{}
			return true, nil
		}
		v.sel = Selection{}
		return true, ctx.Focus(EditorID)
	default:
		return true, ctx.Focus(EditorID)
	}
	return true, nil
}

func (v *transcriptView) runSearch() {
	v.search.Run(v.query, v.line, v.vp.Total(), v.vp.Offset())
	if m, ok := v.search.At(); ok {
		v.vp.ScrollTo(m.Line)
	}
}

func (v *transcriptView) jump(m Match, ok bool) {
	if ok {
		v.vp.ScrollTo(m.Line)
	}
}

// ---- actions (Scroll context) ----

// action handles scroll:* and selection:* in the fullscreen layout; elsewhere it
// declines so other features (the ctrl+o viewer, pkg/ui panes) keep their keys.
func (v *transcriptView) action(id ext.ActionID) ext.ActionFunc {
	return func(ctx ext.Ctx) (bool, tea.Cmd) {
		if !v.active(ctx) {
			return false, nil
		}
		v.invalidate(ctx)
		switch id {
		case ext.ActScrollLineUp:
			v.vp.ScrollBy(-v.wheel.Notch(ctx.Clock().Now().UnixMilli()))
		case ext.ActScrollLineDown:
			v.vp.ScrollBy(v.wheel.Notch(ctx.Clock().Now().UnixMilli()))
		case ext.ActScrollPageUp, ext.ActScrollFullPageUp:
			v.vp.Page(-1)
		case ext.ActScrollPageDown, ext.ActScrollFullPageDown:
			v.vp.Page(1)
		case ext.ActScrollHalfPageUp:
			v.vp.HalfPage(-1)
		case ext.ActScrollHalfPageDown:
			v.vp.HalfPage(1)
		case ext.ActScrollTop:
			v.vp.Top()
		case ext.ActScrollBottom:
			v.vp.Bottom()
		case ext.ActSelectionCopy:
			if v.sel.Empty() {
				return false, nil
			}
			return true, v.copy(ctx)
		case ext.ActSelectionClear:
			if v.sel.Empty() {
				return false, nil
			}
			v.sel = Selection{}
		case ext.ActSelectionExtendLeft, ext.ActSelectionExtendRight, ext.ActSelectionExtendUp,
			ext.ActSelectionExtendDown, ext.ActSelectionExtendLineStart, ext.ActSelectionExtendLineEnd:
			return v.extend(id)
		default:
			return false, nil
		}
		return true, nil
	}
}

// extend moves the selection head (shift+arrows / home / end).
func (v *transcriptView) extend(id ext.ActionID) (bool, tea.Cmd) {
	if !v.sel.Active {
		return false, nil // nothing selected: let the prompt have shift+arrows
	}
	h := v.sel.Head
	switch id {
	case ext.ActSelectionExtendLeft:
		if h.Col > 0 {
			h.Col--
		} else if h.Line > 0 {
			h.Line--
			h.Col = len(cells(v.line(h.Line)))
		}
	case ext.ActSelectionExtendRight:
		if h.Col < len(cells(v.line(h.Line))) {
			h.Col++
		} else if h.Line+1 < v.vp.Total() {
			h.Line, h.Col = h.Line+1, 0
		}
	case ext.ActSelectionExtendUp:
		h.Line = max(0, h.Line-1)
	case ext.ActSelectionExtendDown:
		h.Line = min(v.vp.Total()-1, h.Line+1)
	case ext.ActSelectionExtendLineStart:
		h.Col = 0
	case ext.ActSelectionExtendLineEnd:
		h.Col = len(cells(v.line(h.Line)))
	}
	v.sel.Head = h
	v.vp.ScrollTo(h.Line)
	return true, nil
}

// scrollActions are the actions the viewport registers.
var scrollActions = []ext.ActionID{
	ext.ActScrollLineUp, ext.ActScrollLineDown, ext.ActScrollPageUp, ext.ActScrollPageDown,
	ext.ActScrollFullPageUp, ext.ActScrollFullPageDown, ext.ActScrollHalfPageUp, ext.ActScrollHalfPageDown,
	ext.ActScrollTop, ext.ActScrollBottom,
	ext.ActSelectionCopy, ext.ActSelectionClear,
	ext.ActSelectionExtendLeft, ext.ActSelectionExtendRight, ext.ActSelectionExtendUp,
	ext.ActSelectionExtendDown, ext.ActSelectionExtendLineStart, ext.ActSelectionExtendLineEnd,
}

var (
	_ ext.Focusable  = (*transcriptView)(nil)
	_ ext.FocusAware = (*transcriptView)(nil)
)
