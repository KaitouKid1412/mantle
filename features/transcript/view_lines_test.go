package transcript

import (
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/render"
)

// linesView is how another package (plan 12's fullscreen) sees Store.Lines.
type linesView interface {
	Lines(ext.Ctx, int) ([]string, []ext.Block)
}

func fullscreenRig(t *testing.T) *rig {
	g := newRig(t, 80)
	g.c.LayoutMode = ext.Fullscreen
	return g
}

func blockText(b ext.Block) string { return render.Strip(strings.Join(b.Lines, "\n")) }

func TestStoreLinesNormal(t *testing.T) {
	g := fullscreenRig(t)
	g.send(focusTurn)
	var tr ext.Transcript = g.f.store
	lv, ok := tr.(linesView)
	if !ok {
		t.Fatal("the store does not offer Lines")
	}
	ids, blocks := lv.Lines(g.c, 80)
	if len(ids) != len(blocks) {
		t.Fatalf("%d ids for %d blocks", len(ids), len(blocks))
	}
	want := []string{"user:u1", "thk:a0:0", "t1", "t2", "txt:a3:0", "result:r1"}
	if strings.Join(ids, " ") != strings.Join(want, " ") {
		t.Fatalf("ids = %v", ids)
	}
	if !strings.Contains(blockText(blocks[4]), "All good.") {
		t.Fatalf("answer block = %q", blockText(blocks[4]))
	}
	if len(g.c.Printed) != 0 {
		t.Fatal("fullscreen layout printed to scrollback")
	}
}

func TestStoreLinesFocusAndBrief(t *testing.T) {
	g := fullscreenRig(t)
	g.c.SettingsV = exttest.NewSettings(map[string]any{"viewMode": "focus"})
	g.deliver(ext.SettingsMsg{})
	g.send(focusTurn)
	ids, blocks := g.f.store.Lines(g.c, 80)
	if strings.Join(ids, " ") != "user:u1 summary:t1 txt:a3:0 result:r1" {
		t.Fatalf("focus ids = %v", ids)
	}
	if !strings.Contains(blockText(blocks[1]), "Used 2 tools") {
		t.Fatalf("summary = %q", blockText(blocks[1]))
	}
	g.f.brief = true
	ids, _ = g.f.store.Lines(g.c, 80)
	if strings.Join(ids, " ") != "user:u1 txt:a3:0" {
		t.Fatalf("brief ids = %v", ids)
	}
}

func TestStoreLinesGroupsMCP(t *testing.T) {
	g := fullscreenRig(t)
	g.send(`
{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"tool_use","id":"t1","name":"mcp__slack__search","input":{}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
{"type":"assistant","uuid":"a2","message":{"id":"m","content":[{"type":"tool_use","id":"t2","name":"mcp__slack__post","input":{}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"ok"}]}}
`)
	// While the run may still grow, the calls show one by one.
	if ids, _ := g.f.store.Lines(g.c, 80); strings.Join(ids, " ") != "t1 t2" {
		t.Fatalf("open run ids = %v", ids)
	}
	g.send(`{"type":"assistant","uuid":"a3","message":{"id":"m","content":[{"type":"text","text":"Done."}]}}`)
	ids, blocks := g.f.store.Lines(g.c, 80)
	if strings.Join(ids, " ") != "group:t1 txt:a3:0" || !strings.Contains(blockText(blocks[0]), "Called slack 2 times") {
		t.Fatalf("ids = %v, first = %q", ids, blockText(blocks[0]))
	}
}

func TestStoreLinesCachesFinishedItems(t *testing.T) {
	g := fullscreenRig(t)
	calls := 0
	inner := g.c.Renderers[ext.KeyAssistantText]
	g.c.Renderers[ext.KeyAssistantText] = func(rc ext.RenderCtx, it *ext.Item) ext.Block {
		calls++
		return inner(rc, it)
	}
	g.send(focusTurn)
	g.f.store.Lines(g.c, 80)
	g.f.store.Lines(g.c, 80)
	if calls != 1 {
		t.Fatalf("finished text rendered %d times, want 1", calls)
	}
	g.f.store.Lines(g.c, 60)
	if calls != 2 {
		t.Fatalf("a new width did not re-render (%d calls)", calls)
	}
}

// TestTextCacheForgottenInFullscreen: with no inline commit to prune it, the
// streaming-markdown cache must not keep finished items.
func TestTextCacheForgottenInFullscreen(t *testing.T) {
	g := fullscreenRig(t)
	g.send(streamTwoParagraphs)
	g.f.store.Lines(g.c, 80)
	if len(g.f.texts) != 1 {
		t.Fatalf("streaming item not cached: %d entries", len(g.f.texts))
	}
	g.send(`
{"type":"stream_event","event":{"type":"content_block_stop","index":0}}
{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"text","text":"First paragraph.\n\nSecond"}]}}
`)
	g.f.store.Lines(g.c, 80)
	if len(g.f.texts) != 0 {
		t.Fatalf("finished item still cached: %d entries", len(g.f.texts))
	}
}
