package enginefake

import (
	"io"
	"sync"
)

// bufPipe is an in-memory pipe whose writes never block (unbounded buffer), so a
// script never stalls on a client that reads slowly or not at all. Reads block until
// data arrives or the write side closes.
type bufPipe struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool // write side closed: EOF after the buffer drains
	broken bool // read side closed: writes fail
}

func newBufPipe() *bufPipe {
	p := &bufPipe{}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *bufPipe) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.broken || p.closed {
		return 0, io.ErrClosedPipe
	}
	p.buf = append(p.buf, b...)
	p.cond.Broadcast()
	return len(b), nil
}

func (p *bufPipe) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.buf) == 0 && !p.closed && !p.broken {
		p.cond.Wait()
	}
	if p.broken {
		return 0, io.ErrClosedPipe
	}
	if len(p.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(b, p.buf)
	p.buf = p.buf[n:]
	if len(p.buf) == 0 {
		p.buf = nil
	}
	return n, nil
}

// closeWrite ends the stream; readers get EOF after the buffer drains.
func (p *bufPipe) closeWrite() {
	p.mu.Lock()
	p.closed = true
	p.cond.Broadcast()
	p.mu.Unlock()
}

// Close closes the read side; buffered data is dropped and later writes fail.
func (p *bufPipe) Close() error {
	p.mu.Lock()
	p.broken = true
	p.buf = nil
	p.cond.Broadcast()
	p.mu.Unlock()
	return nil
}
