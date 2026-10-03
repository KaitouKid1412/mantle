package chrome

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

func TestSubagentViewNested(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.H = 12
	ctx.Renderers[ext.KeyDefault] = func(rc ext.RenderCtx, it *ext.Item) ext.Block {
		return ext.Block{Lines: []string{"item " + it.ID}}
	}
	ctx.TranscriptV = fakeTranscript{items: []*ext.Item{
		{ID: "u0", Key: ext.KeyUserPrompt},
		{ID: "tu1", Key: "tool.Agent"},
		{ID: "a1", ParentID: "tu1", Key: ext.KeyAssistantText},
		{ID: "tu2", ParentID: "tu1", Key: "tool.Agent"},
		{ID: "a2", ParentID: "tu2", Key: ext.KeyAssistantText},
		{ID: "other", ParentID: "tuX", Key: ext.KeyAssistantText},
	}}
	d, err := newSubagentView(ctx, subagentViewArgs{TaskID: "t", ToolUseID: "tu1", Title: "Explore · look"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Placement() != ext.PlaceAltScreen || d.KeyContext() != ext.ContextTranscript {
		t.Error("placement/context")
	}
	out := ansi.Strip(d.View(ctx, ext.Area{Width: 60}).Text)
	want := []string{"Subagent: Explore · look", "item a1", "", "item tu2", "", "  item a2"}
	got := strings.Split(out, "\n")
	if strings.Join(got[:len(want)], "|") != strings.Join(want, "|") {
		t.Errorf("view =\n%s", out)
	}
	if strings.Contains(out, "item other") || strings.Contains(out, "item u0") {
		t.Error("unrelated items shown")
	}

	if _, err := newSubagentView(ctx, "bad"); err == nil {
		t.Error("bad args")
	}
	v := d.(*subagentView)
	v.HandleAction(ctx, ext.ActScrollTop)
	if v.follow {
		t.Error("scroll top stops following")
	}
	v.HandleAction(ctx, ext.ActTranscriptExit)
	if ctx.Closed[len(ctx.Closed)-1] != SubagentViewID {
		t.Error("exit closes")
	}
}
