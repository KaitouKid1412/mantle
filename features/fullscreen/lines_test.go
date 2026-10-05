package fullscreen

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// linesStore is a store with plan 03's Lines view: a prompt, a focus-view summary and a
// tool item.
type linesStore struct{ store }

func (s *linesStore) Lines(c ext.Ctx, w int) ([]string, []ext.Block) {
	return []string{"u1", "summary:t0", "t1"}, []ext.Block{
		{Lines: []string{"❯ question"}},
		{Lines: []string{"· Used 3 tools (ctrl+o to see them)"}},
		{Lines: []string{"⏺ Bash(ls)"}, Collapsible: true},
	}
}

func TestViewportUsesStoreLines(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.LayoutMode = ext.Fullscreen
	ctx.Renderers[ext.KeyDefault] = func(rc ext.RenderCtx, it *ext.Item) ext.Block {
		if rc.Expanded {
			return ext.Block{Lines: []string{"⏺ Bash(ls)", "  a.txt b.txt"}}
		}
		return ext.Block{Lines: []string{"renderer was called"}}
	}
	ls := &linesStore{store{items: []*ext.Item{
		textItem("u1", ext.KeyUserPrompt, "❯ question"),
		textItem("t1", ext.ToolKey("Bash"), "⏺ Bash(ls)"),
	}}}
	ctx.TranscriptV = ls
	v := newTranscriptView(terminal.Map{})
	v.Init(ctx)
	out := ansi.Strip(v.View(ctx, ext.Area{Width: 60, MaxHeight: 10, Mode: ext.Fullscreen}).Text)
	if !strings.Contains(out, "Used 3 tools") || strings.Contains(out, "renderer was called") {
		t.Fatalf("store lines not used:\n%s", out)
	}
	if len(v.items) != 3 || v.items[1] != nil {
		t.Errorf("synthetic block should have a nil item: %+v", v.items)
	}
	v.maybeTick(ctx) // nil items must be safe
	(&stickyHeader{tv: v}).prompt()

	// Expanding the tool block re-renders it with Expanded.
	v.toggleExpand(ctx, v.vp.BlockStart(2)+1)
	out = ansi.Strip(v.View(ctx, ext.Area{Width: 60, MaxHeight: 10, Mode: ext.Fullscreen}).Text)
	if !strings.Contains(out, "a.txt b.txt") {
		t.Errorf("expanded:\n%s", out)
	}
}
