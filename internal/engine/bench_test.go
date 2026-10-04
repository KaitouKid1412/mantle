package engine

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// deltaLine is a typical text_delta line as the engine prints it.
var deltaLine = []byte(`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"word "}},"session_id":"00000000-0000-4000-8000-000000000001","parent_tool_use_id":null,"uuid":"00000000-0000-4000-8000-000000000002"}`)

// BenchmarkDecodeDelta: cost of decoding one delta line (reader goroutine).
func BenchmarkDecodeDelta(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := proto.Decode(deltaLine); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecodeAndCoalesce: decode plus coalescing, per delta.
func BenchmarkDecodeAndCoalesce(b *testing.B) {
	b.ReportAllocs()
	released := 0
	c := newCoalescer("main", DefaultCoalesceInterval, func(ms []tea.Msg) { released += len(ms) })
	for i := 0; i < b.N; i++ {
		ev, _ := proto.Decode(deltaLine)
		c.Push(ev)
	}
	c.Stop()
}

// TestCoalescingRate shows how many UI messages a burst of deltas becomes.
func TestCoalescingRate(t *testing.T) {
	for _, interval := range []time.Duration{0, 8 * time.Millisecond, 16 * time.Millisecond, 33 * time.Millisecond} {
		released := 0
		var c *coalescer
		push := func(ev proto.Event) { c.Push(ev) }
		if interval == 0 {
			push = func(proto.Event) { released++ }
		} else {
			c = newCoalescer("main", interval, func(ms []tea.Msg) { released += len(ms) })
		}
		// 2000 deltas/s for 0.5 s (well above the engine's observed peak).
		start := time.Now()
		for i := 0; i < 1000; i++ {
			ev, _ := proto.Decode(deltaLine)
			push(ev)
			time.Sleep(time.Until(start.Add(time.Duration(i+1) * 500 * time.Microsecond)))
		}
		if c != nil {
			c.Stop()
		}
		t.Logf("interval %-5v: 1000 deltas in %v -> %d UI messages", interval, time.Since(start).Round(time.Millisecond), released)
		if interval >= 16*time.Millisecond && released > 1000/int(interval/(500*time.Microsecond))*3 {
			t.Errorf("coalescing too weak: %d", released)
		}
	}
	_ = fmt.Sprint
}
