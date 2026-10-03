package engine

import (
	"bytes"
	"strings"
	"sync"
)

// StderrRingSize is how much of the engine's stderr is kept.
const StderrRingSize = 64 << 10

// Ring is a fixed-size byte ring buffer that keeps the most recent writes. It is an
// io.Writer, safe for concurrent use. The engine's stderr drains into one, so it
// never reaches the terminal.
type Ring struct {
	mu    sync.Mutex
	buf   []byte
	start int // index of the oldest byte when full
	full  bool
	total int64
}

// NewRing returns a ring that keeps the last size bytes.
func NewRing(size int) *Ring { return &Ring{buf: make([]byte, 0, size)} }

// Write appends p, dropping the oldest bytes when full. It never fails.
func (r *Ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(p)
	r.total += int64(n)
	size := cap(r.buf)
	if size == 0 {
		return n, nil
	}
	if len(p) >= size {
		r.buf = append(r.buf[:0], p[len(p)-size:]...)
		r.start, r.full = 0, true
		return n, nil
	}
	if !r.full {
		room := size - len(r.buf)
		if len(p) <= room {
			r.buf = append(r.buf, p...)
			if len(r.buf) == size {
				r.full = true
			}
			return n, nil
		}
		r.buf = append(r.buf, p[:room]...)
		p = p[room:]
		r.full = true
	}
	for len(p) > 0 {
		c := copy(r.buf[r.start:], p)
		p = p[c:]
		r.start = (r.start + c) % size
	}
	return n, nil
}

// Bytes returns a copy of the kept bytes, oldest first.
func (r *Ring) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return bytes.Clone(r.buf)
	}
	out := make([]byte, 0, len(r.buf))
	out = append(out, r.buf[r.start:]...)
	return append(out, r.buf[:r.start]...)
}

// String returns the kept bytes as a string.
func (r *Ring) String() string { return string(r.Bytes()) }

// Total returns how many bytes were ever written.
func (r *Ring) Total() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.total
}

// Tail returns up to the last n lines (a partial first line is dropped when the ring
// has wrapped).
func (r *Ring) Tail(n int) string {
	b := r.Bytes()
	r.mu.Lock()
	wrapped := r.full && r.total > int64(cap(r.buf))
	r.mu.Unlock()
	s := strings.TrimRight(string(b), "\n")
	lines := strings.Split(s, "\n")
	if wrapped && len(lines) > 1 {
		lines = lines[1:]
	}
	if s == "" {
		return ""
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
