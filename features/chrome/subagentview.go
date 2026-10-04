package chrome

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// SubagentViewID is the dialog that shows one subagent's transcript.
const SubagentViewID = "dialog.subagentTranscript"

type subagentViewArgs struct {
	TaskID, ToolUseID, Title string
}

// subagentView is an alt-screen, less-style view of the transcript items nested under
// a subagent's tool call, drawn with the registered renderers. It follows new output
// while scrolled to the bottom.
type subagentView struct {
	args   subagentViewArgs
	offset int  // first visible line
	follow bool // stick to the bottom
	height int  // last body height, for paging
	total  int  // last line count
}

func newSubagentView(_ ext.Ctx, args any) (ext.Dialog, error) {
	a, ok := args.(subagentViewArgs)
	if !ok {
		return nil, fmt.Errorf("subagent view: unexpected args %T", args)
	}
	return &subagentView{args: a, follow: true}, nil
}

func (v *subagentView) ID() string                                        { return SubagentViewID }
func (v *subagentView) Init(ext.Ctx) tea.Cmd                              { return nil }
func (v *subagentView) Placement() ext.Placement                          { return ext.PlaceAltScreen }
func (v *subagentView) KeyContext() string                                { return ext.ContextTranscript }
func (v *subagentView) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return true, nil }

func (v *subagentView) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(ext.EngineEventMsg); ok && isMain(m.EngineID) {
		ctx.Invalidate(SubagentViewID) // new subagent output may have arrived
	}
	return nil
}

func (v *subagentView) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	page := max(1, v.height-1)
	switch a {
	case ext.ActTranscriptExit, ext.ActSelectCancel:
		return true, ctx.CloseDialog(SubagentViewID)
	case ext.ActScrollLineDown:
		v.scroll(1)
	case ext.ActScrollLineUp:
		v.scroll(-1)
	case ext.ActScrollHalfPageDown:
		v.scroll(page / 2)
	case ext.ActScrollHalfPageUp:
		v.scroll(-page / 2)
	case ext.ActScrollFullPageDown, ext.ActScrollPageDown:
		v.scroll(page)
	case ext.ActScrollFullPageUp, ext.ActScrollPageUp:
		v.scroll(-page)
	case ext.ActScrollTop:
		v.offset, v.follow = 0, false
	case ext.ActScrollBottom:
		v.follow = true
	default:
		return false, nil
	}
	ctx.Invalidate(SubagentViewID)
	return true, nil
}

// HandleKey covers keys the Transcript context doesn't bind.
func (v *subagentView) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "down":
		return v.HandleAction(ctx, ext.ActScrollLineDown)
	case "up":
		return v.HandleAction(ctx, ext.ActScrollLineUp)
	case "pgdown":
		return v.HandleAction(ctx, ext.ActScrollFullPageDown)
	case "pgup":
		return v.HandleAction(ctx, ext.ActScrollFullPageUp)
	case "home":
		return v.HandleAction(ctx, ext.ActScrollTop)
	case "end":
		return v.HandleAction(ctx, ext.ActScrollBottom)
	}
	return true, nil
}

func (v *subagentView) scroll(n int) {
	v.offset = max(0, min(v.offset+n, v.total-v.height))
	v.follow = v.offset >= v.total-v.height
}

func (v *subagentView) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	t, sr := ctx.Theme(), ctx.Accessibility().ScreenReader
	_, h := ctx.Size()
	if a.MaxHeight > 0 {
		h = a.MaxHeight
	}
	body := v.bodyLines(ctx, a.Width)
	v.height, v.total = max(1, h-2), len(body)
	if v.follow {
		v.offset = max(0, v.total-v.height)
	}
	v.offset = min(v.offset, max(0, v.total-v.height))
	end := min(v.total, v.offset+v.height)

	header := paint(t, sr, theme.Text, "Subagent: "+v.args.Title)
	pos := ""
	if v.total > v.height {
		pos = fmt.Sprintf(" · lines %d–%d of %d", v.offset+1, end, v.total)
	}
	exit := firstKey(ctx, ext.ContextTranscript, ext.ActTranscriptExit, "esc")
	footer := dim(t, sr, exit+" to close · j/k scroll"+pos)
	lines := append([]string{header}, body[v.offset:end]...)
	lines = append(lines, footer)
	return ext.Rendered{Text: strings.Join(truncateLines(lines, a.Width, 0), "\n")}
}

// bodyLines renders the items nested under the subagent's tool call (live items, or
// the subagent transcript plan 06 nests there on resume), indenting deeper levels
// (subagents of the subagent).
func (v *subagentView) bodyLines(ctx ext.Ctx, w int) []string {
	tr := ctx.Transcript()
	if tr == nil || v.args.ToolUseID == "" {
		return []string{dim(ctx.Theme(), ctx.Accessibility().ScreenReader, "No transcript for this subagent yet.")}
	}
	depth := map[string]int{v.args.ToolUseID: 0}
	var lines []string
	for _, it := range tr.Items() {
		d, ok := depth[it.ParentID]
		if !ok {
			continue
		}
		depth[it.ID] = d + 1
		indent := strings.Repeat("  ", d)
		r := ctx.Renderer(it.Key)
		b := r(ext.RenderCtx{Width: max(10, w-len(indent)), Mode: ext.FullTranscript, Theme: ctx.Theme(),
			Expanded: true, Now: ctx.Clock().Now()}, it)
		for _, l := range b.Lines {
			lines = append(lines, indent+l)
		}
		lines = append(lines, "")
	}
	if len(lines) == 0 {
		return []string{dim(ctx.Theme(), ctx.Accessibility().ScreenReader, "No output from this subagent yet.")}
	}
	return lines[:len(lines)-1]
}

var _ ext.ActionHandler = (*subagentView)(nil)
