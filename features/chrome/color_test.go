package chrome

import (
	"slices"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestColorCommand(t *testing.T) {
	ctx := exttest.NewCtx()
	eng := &fakeEngine{}
	ctx.Engines[ext.MainEngine] = eng
	f := wrapFrame(storyEditor{"x"}).(*promptFrame)

	for _, m := range exttest.Exec(colorCommand(ctx, " Red ")) {
		f.Update(ctx, m)
	}
	if f.color != "red" || !slices.Contains(ctx.Invalidated, EditorID) {
		t.Fatalf("color = %q", f.color)
	}
	if !slices.Equal(eng.controls, []string{proto.SubSetColor}) || eng.reqs[0].(proto.SetColorRequest).Color != "red" {
		t.Errorf("set_color = %v %v", eng.controls, eng.reqs)
	}
	for _, m := range exttest.Exec(colorCommand(ctx, "default")) {
		f.Update(ctx, m)
	}
	if f.color != "" || eng.reqs[1].(proto.SetColorRequest).Color != "default" {
		t.Errorf("reset: %q %v", f.color, eng.reqs)
	}

	colorCommand(ctx, "mauve")
	colorCommand(ctx, "")
	if len(ctx.Notices) != 2 || len(eng.controls) != 2 {
		t.Errorf("bad/empty args: notices %+v, controls %v", ctx.Notices, eng.controls)
	}
	if got := colorCompletions(ctx, "p"); len(got) != 2 || got[0].Value != "purple" || got[1].Value != "pink" {
		t.Errorf("completions = %+v", got)
	}
}
