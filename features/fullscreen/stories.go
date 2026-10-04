package fullscreen

import (
	"fmt"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// storyBlocks is a fixed transcript: a prompt, an answer and a long tool output.
func storyBlocks(t *theme.Theme) []Block {
	answer := []string{"", t.Paint(theme.Accent, "⏺") + " The parser needs three changes:", "  1. Read the grammar.", "  2. Write the lexer.", "  3. Add tests."}
	tool := []string{"", t.Paint(theme.Success, "⏺") + " Bash(go test ./...)"}
	for i := 1; i <= 12; i++ {
		tool = append(tool, fmt.Sprintf("  ⎿ ok   example.com/pkg%02d  0.%02ds", i, i))
	}
	return []Block{
		{ID: "u1", Lines: []string{"", t.Paint(theme.Inactive, "❯") + " Explain the parser plan"}},
		{ID: "a1", Lines: answer},
		{ID: "t1", Lines: tool},
		{ID: "u2", Lines: []string{"", t.Paint(theme.Inactive, "❯") + " Now run the tests"}},
	}
}

func storyView(t *theme.Theme, a ext.Area, height int, setup func(v *transcriptView)) ext.Rendered {
	v := newTranscriptView(terminal.Map{})
	v.blocks = storyBlocks(t)
	v.collapsible = []bool{false, false, true, false}
	v.items = []*ext.Item{{ID: "u1", Key: ext.KeyUserPrompt}, {ID: "a1", Key: ext.KeyAssistantText},
		{ID: "t1", Key: ext.ToolKey("Bash")}, {ID: "u2", Key: ext.KeyUserPrompt}}
	v.width = max(10, a.Width-1)
	v.vp.SetHeight(height)
	v.vp.SetBlocks(v.blocks)
	if setup != nil {
		setup(v)
	}
	return v.paint(t, ext.Area{Width: a.Width, MaxHeight: height, Mode: ext.Fullscreen})
}

func addStories(r ext.Registrar, _ *transcriptView) {
	mk := func(id string, setup func(v *transcriptView)) ext.Story {
		return ext.Story{ID: ViewportID + "/" + id, Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
			return storyView(ctx.Theme(), a, 10, setup)
		}}
	}
	r.AddStory(mk("bottom", nil))
	r.AddStory(mk("scrolled-new", func(v *transcriptView) {
		v.vp.Top()
		v.vp.ScrollBy(2)
		v.vp.SetBlocks(append(v.blocks, Block{ID: "a2", Lines: []string{"", "⏺ All tests pass.", "  Done."}}))
	}))
	r.AddStory(mk("selection", func(v *transcriptView) {
		v.vp.Top()
		v.sel = Selection{Anchor: Pos{Line: 3, Col: 2}, Head: Pos{Line: 5, Col: 9}, Active: true}
	}))
	r.AddStory(mk("search", func(v *transcriptView) {
		v.vp.Top()
		v.focused = true
		v.search.Run("pkg0", v.line, v.vp.Total(), 0)
		if m, ok := v.search.Next(); ok {
			v.vp.ScrollTo(m.Line)
		}
	}))
	r.AddStory(ext.Story{ID: HeaderID + "/sticky", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		var hv *transcriptView
		storyView(ctx.Theme(), a, 6, func(v *transcriptView) {
			v.vp.Top()
			v.vp.ScrollBy(8) // inside the tool output, below the first prompt
			hv = v
		})
		h := &stickyHeader{tv: hv}
		text, ok := h.prompt()
		if !ok {
			return ext.Rendered{Text: "(no sticky prompt)"}
		}
		return ext.Rendered{Text: renderHeader(ctx.Theme(), text, a.Width)}
	}})
}
