package engine

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// DefaultCoalesceInterval is how long stream deltas may be held for merging.
const DefaultCoalesceInterval = 16 * time.Millisecond

// coalescer merges stream_event text/thinking/input_json deltas per stream and
// releases messages in order to out:
//
//   - a delta is held for at most interval, merged with later deltas of the same
//     (parent_tool_use_id, index, delta type);
//   - any other message flushes all held deltas first and is released immediately.
//
// Relative order between different streams' deltas may change inside one window;
// order within a stream, and relative to non-delta messages, never does.
//
// out is called with c.mu held (so batches from the reader and the flush timer can
// never pass each other); it must not block.
type coalescer struct {
	engineID string
	interval time.Duration
	out      func([]tea.Msg)

	mu      sync.Mutex
	held    []*ext.EngineEventMsg
	byKey   map[deltaKey]int // index into held
	timer   *time.Timer
	stopped bool
}

type deltaKey struct {
	parent string
	index  int
	typ    string
}

func newCoalescer(engineID string, interval time.Duration, out func([]tea.Msg)) *coalescer {
	if interval <= 0 {
		interval = DefaultCoalesceInterval
	}
	return &coalescer{engineID: engineID, interval: interval, out: out, byKey: map[deltaKey]int{}}
}

func mergeable(ev proto.Event) (*proto.StreamEvent, deltaKey, bool) {
	se, ok := ev.(*proto.StreamEvent)
	if !ok || !se.IsDelta() || se.Event.Delta == nil {
		return nil, deltaKey{}, false
	}
	switch se.Event.Delta.Type {
	case proto.DeltaText, proto.DeltaThinking, proto.DeltaInputJSON:
		return se, deltaKey{se.ParentToolUseID, se.Event.Index, se.Event.Delta.Type}, true
	}
	return nil, deltaKey{}, false
}

// Push adds an engine event in stdout order.
func (c *coalescer) Push(ev proto.Event) {
	se, key, ok := mergeable(ev)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return
	}
	if !ok {
		c.out(append(c.takeLocked(), ext.EngineEventMsg{EngineID: c.engineID, Event: ev}))
		return
	}
	if i, found := c.byKey[key]; found {
		mergeDelta(c.held[i].Event.(*proto.StreamEvent), se)
	} else {
		cp := *se
		d := *se.Event.Delta
		cp.Event.Delta = &d
		cp.Raw = nil // a merged event no longer matches one line
		c.byKey[key] = len(c.held)
		c.held = append(c.held, &ext.EngineEventMsg{EngineID: c.engineID, Event: &cp})
	}
	if c.timer == nil {
		c.timer = time.AfterFunc(c.interval, c.Flush)
	}
}

// PushMsg releases a non-event message in order (flushing held deltas first).
func (c *coalescer) PushMsg(m tea.Msg) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.stopped {
		c.out(append(c.takeLocked(), m))
	}
}

// Flush releases held deltas now.
func (c *coalescer) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if batch := c.takeLocked(); len(batch) > 0 {
		c.out(batch)
	}
}

// Stop flushes, then drops anything pushed later.
func (c *coalescer) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if batch := c.takeLocked(); len(batch) > 0 {
		c.out(batch)
	}
	c.stopped = true
}

func (c *coalescer) takeLocked() []tea.Msg {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if len(c.held) == 0 {
		return nil
	}
	out := make([]tea.Msg, 0, len(c.held)+1)
	for _, m := range c.held {
		out = append(out, *m)
	}
	c.held = c.held[:0]
	clear(c.byKey)
	return out
}

func mergeDelta(dst, src *proto.StreamEvent) {
	d, s := dst.Event.Delta, src.Event.Delta
	switch s.Type {
	case proto.DeltaText:
		d.Text += s.Text
	case proto.DeltaThinking:
		d.Thinking += s.Thinking
	case proto.DeltaInputJSON:
		d.PartialJSON += s.PartialJSON
	}
}
