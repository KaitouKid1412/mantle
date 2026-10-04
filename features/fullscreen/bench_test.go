package fullscreen

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// bigTranscript is a ctx with n finished items of a few lines each.
func bigTranscript(n int) *exttest.Ctx {
	ctx := exttest.NewCtx()
	ctx.LayoutMode = ext.Fullscreen
	ctx.Renderers[ext.KeyDefault] = func(rc ext.RenderCtx, it *ext.Item) ext.Block {
		s, _ := it.Data.(string)
		return ext.Block{Lines: strings.Split(s, "\n")}
	}
	st := &store{}
	for i := range n {
		st.items = append(st.items, textItem(fmt.Sprint("i", i), ext.KeyAssistantText,
			fmt.Sprintf("⏺ item %d\n  detail line one\n  detail line two", i)))
	}
	ctx.TranscriptV = st
	return ctx
}

// BenchmarkViewSteady10k is the steady-state frame cost with 10,000 items: every item
// is cached, so a frame lays out block heights and paints the visible window.
// Target (plan 12): well under 1 ms.
func BenchmarkViewSteady10k(b *testing.B) {
	ctx := bigTranscript(10_000)
	v := newTranscriptView(terminal.Map{})
	v.Init(ctx)
	a := ext.Area{Width: 120, MaxHeight: 40, Mode: ext.Fullscreen}
	v.View(ctx, a) // warm the cache
	b.ResetTimer()
	for range b.N {
		v.View(ctx, a)
	}
}

// TestViewSteadyUnderBudget fails if a steady frame with 10k items takes over 5 ms on
// average (a loose bound so slow CI machines pass; the benchmark shows the real cost).
func TestViewSteadyUnderBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	ctx := bigTranscript(10_000)
	v := newTranscriptView(terminal.Map{})
	v.Init(ctx)
	a := ext.Area{Width: 120, MaxHeight: 40, Mode: ext.Fullscreen}
	v.View(ctx, a)
	const n = 50
	start := time.Now()
	for range n {
		v.View(ctx, a)
	}
	if per := time.Since(start) / n; per > 5*time.Millisecond {
		t.Errorf("steady View with 10k items = %v per frame", per)
	} else {
		t.Logf("steady View with 10k items = %v per frame", per)
	}
}
