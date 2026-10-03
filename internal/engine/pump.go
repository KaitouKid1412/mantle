package engine

import (
	"sync"

	tea "charm.land/bubbletea/v2"
)

// pump delivers messages to the UI in order on its own goroutine. Enqueue never
// blocks (the queue is unbounded), so the stdout reader is never held up by a busy
// UI; send may block (tea.Program.Send does until the event loop takes the message).
type pump struct {
	send func(tea.Msg)

	mu     sync.Mutex
	queue  []tea.Msg
	wake   chan struct{}
	closed bool
	done   chan struct{}
}

func newPump(send func(tea.Msg)) *pump {
	p := &pump{send: send, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go p.run()
	return p
}

// Enqueue appends messages; it is a no-op after Close.
func (p *pump) Enqueue(msgs []tea.Msg) {
	if len(msgs) == 0 {
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.queue = append(p.queue, msgs...)
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Close delivers what is queued, then stops. Done is closed afterwards.
func (p *pump) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Done is closed once the pump has delivered everything and stopped.
func (p *pump) Done() <-chan struct{} { return p.done }

func (p *pump) run() {
	defer close(p.done)
	for {
		p.mu.Lock()
		q := p.queue
		p.queue = nil
		closed := p.closed
		p.mu.Unlock()
		for _, m := range q {
			p.send(m)
		}
		if len(q) == 0 {
			if closed {
				return
			}
			<-p.wake
		}
	}
}
