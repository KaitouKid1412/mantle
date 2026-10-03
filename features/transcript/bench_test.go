package transcript

import (
	"fmt"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// turnEvents builds n small turns (prompt, text, tool call and result).
func turnEvents(t testing.TB, n int) []proto.Event {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, `{"type":"user","uuid":"u%d","isReplay":true,"message":{"role":"user","content":"question %d"}}`+"\n", i, i)
		fmt.Fprintf(&b, `{"type":"assistant","uuid":"a%d","message":{"id":"m%d","content":[{"type":"text","text":"Answer **%d** with `+"`code`"+` and a [link](https://example.com/%d)."}]}}`+"\n", i, i, i, i)
		fmt.Fprintf(&b, `{"type":"assistant","uuid":"b%d","message":{"id":"m%d","content":[{"type":"tool_use","id":"t%d","name":"Bash","input":{"command":"echo %d"}}]}}`+"\n", i, i, i, i)
		fmt.Fprintf(&b, `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t%d","content":"%d"}]},"tool_use_result":{"stdout":"%d","stderr":""}}`+"\n", i, i, i)
	}
	return decodeLines(t, b.String())
}

// BenchmarkReplay10k feeds ~10k items through the store and commit policy.
func BenchmarkReplay10k(b *testing.B) {
	evs := turnEvents(b, 3334) // 3 items per turn
	b.ReportAllocs()
	for b.Loop() {
		f := New(ext.MainEngine)
		r := exttest.NewRegistrar()
		_ = f.Setup(r)
		c := exttest.NewCtx()
		c.Renderers = r.Renderers
		for _, ev := range evs {
			f.onEvent(c, ext.EngineEventMsg{EngineID: ext.MainEngine, Event: ev})
		}
		c.Printed = nil
	}
}

// BenchmarkLiveViewSteadyState renders the live area of a 10k-item store with
// one streaming answer: only uncommitted items render.
func BenchmarkLiveViewSteadyState(b *testing.B) {
	f := New(ext.MainEngine)
	r := exttest.NewRegistrar()
	_ = f.Setup(r)
	c := exttest.NewCtx()
	c.Renderers = r.Renderers
	for _, ev := range turnEvents(b, 3334) {
		f.onEvent(c, ext.EngineEventMsg{EngineID: ext.MainEngine, Event: ev})
	}
	for _, ev := range decodeLines(b, streamTwoParagraphs) {
		f.onEvent(c, ext.EngineEventMsg{EngineID: ext.MainEngine, Event: ev})
	}
	if n := len(f.store.Items()); n < 10000 {
		b.Fatalf("only %d items", n)
	}
	area := ext.Area{Width: 100, MaxHeight: 20, Mode: ext.Inline}
	b.ReportAllocs()
	for b.Loop() {
		_ = f.live.View(c, area)
	}
}
