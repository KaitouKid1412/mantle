package engine

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// MaxLineBytes caps one stdout line. Lines carry tool results and base64 images, so
// this is far above any real message; it only stops a runaway engine from eating RAM.
const MaxLineBytes = 512 << 20

// ErrClosed is returned by Send after the input side was closed.
var ErrClosed = errors.New("engine: transport closed")

// Transport speaks NDJSON to the engine: one reader goroutine decodes stdout lines,
// one writer goroutine drains an unbounded queue into stdin, so callers never block
// on a pipe.
type Transport struct {
	r io.Reader
	w io.WriteCloser

	mu       sync.Mutex
	queue    [][]byte
	wake     chan struct{} // capacity 1: "queue changed"
	closing  bool          // CloseInput called: flush, then close w
	detached bool          // Detach called: flush, then stop without closing w
	holding  bool          // Hold called: Send keeps lines in held until Release
	held     [][]byte
	closed   bool // writer finished
	werr     error

	readDone  chan struct{}
	writeDone chan struct{}
	rerr      error
	leftover  []byte

	// Tap, if set before Start, sees every line read (stdout) and written (stdin),
	// without the trailing newline. Used by the fixture recorder and debug logs.
	Tap func(dir Direction, line []byte)
}

// Direction is the side of a tapped line.
type Direction int

const (
	FromEngine Direction = iota // engine stdout
	ToEngine                    // engine stdin
)

// NewTransport returns a transport reading from stdout r and writing to stdin w.
func NewTransport(r io.Reader, w io.WriteCloser) *Transport {
	return &Transport{
		r:         r,
		w:         w,
		wake:      make(chan struct{}, 1),
		readDone:  make(chan struct{}),
		writeDone: make(chan struct{}),
	}
}

// Start launches the reader and writer goroutines. onEvent runs on the reader
// goroutine in stdout order and must not block for long. onError (may be nil) gets
// malformed lines; reading continues after them.
func (t *Transport) Start(onEvent func(proto.Event), onError func(error)) {
	if onError == nil {
		onError = func(error) {}
	}
	go t.readLoop(onEvent, onError)
	go t.writeLoop()
}

func (t *Transport) readLoop(onEvent func(proto.Event), onError func(error)) {
	defer close(t.readDone)
	br := bufio.NewReaderSize(t.r, 64<<10)
	var acc []byte
	for {
		line, err := readLine(br, &acc)
		if errors.Is(err, os.ErrDeadlineExceeded) {
			// Stopped for a hand-off: keep the partial line for the next owner.
			t.leftover = append(bytes.Clone(line), readAll(br)...)
			return
		}
		if len(line) > 0 {
			if t.Tap != nil {
				t.Tap(FromEngine, line)
			}
			ev, derr := proto.Decode(line)
			switch {
			case derr == nil:
				onEvent(ev)
			case errors.Is(derr, proto.ErrNotJSON):
				// debug noise on stdout; skip
			default:
				onError(derr)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.rerr = err
			}
			return
		}
	}
}

// readAll returns what br has buffered, without reading more.
func readAll(br *bufio.Reader) []byte {
	b, _ := br.Peek(br.Buffered())
	return bytes.Clone(b)
}

// Leftover returns stdout bytes read but not yet processed when the reader stopped
// at a read deadline (see Engine.PrepareHandoff). Valid after ReadDone.
func (t *Transport) Leftover() []byte { return t.leftover }

// Detach flushes queued lines, then stops the writer without closing stdin, so the
// pipe can be handed to another process. WriteDone is closed afterwards.
func (t *Transport) Detach() {
	t.mu.Lock()
	t.detached = true
	t.mu.Unlock()
	t.signal()
}

// readLine returns the next line without its newline. It reuses *acc for lines
// longer than the bufio buffer; the result is only valid until the next call.
func readLine(br *bufio.Reader, acc *[]byte) ([]byte, error) {
	frag, err := br.ReadSlice('\n')
	if err == nil {
		return bytes.TrimRight(frag, "\r\n"), nil
	}
	if !errors.Is(err, bufio.ErrBufferFull) {
		return bytes.TrimRight(frag, "\r\n"), err
	}
	buf := append((*acc)[:0], frag...)
	for {
		frag, err = br.ReadSlice('\n')
		buf = append(buf, frag...)
		if len(buf) > MaxLineBytes {
			// Drop the rest of the line, then report.
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = br.ReadSlice('\n')
			}
			*acc = nil
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("engine: stdout line over %d bytes", MaxLineBytes)
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			break
		}
	}
	if cap(buf) <= 4<<20 {
		*acc = buf // keep moderate buffers for reuse
	} else {
		*acc = nil
	}
	return bytes.TrimRight(buf, "\r\n"), err
}

// Send queues one line (without newline) for stdin. It never blocks.
func (t *Transport) Send(line []byte) error { return t.send(line, false) }

// SendNow queues one line ahead of held lines (see Hold): the initialize handshake
// and answers to the engine's own requests.
func (t *Transport) SendNow(line []byte) error { return t.send(line, true) }

func (t *Transport) send(line []byte, now bool) error {
	t.mu.Lock()
	if t.closing || t.closed || t.detached {
		t.mu.Unlock()
		return ErrClosed
	}
	if t.werr != nil {
		err := t.werr
		t.mu.Unlock()
		return err
	}
	b := make([]byte, len(line)+1)
	copy(b, line)
	b[len(line)] = '\n'
	if t.holding && !now {
		t.held = append(t.held, b)
		t.mu.Unlock()
		return nil
	}
	t.queue = append(t.queue, b)
	t.mu.Unlock()
	t.signal()
	return nil
}

// Hold makes Send keep lines back until Release (SendNow still goes out). The engine
// holds everything until the initialize handshake has completed, as the SDK does.
func (t *Transport) Hold() {
	t.mu.Lock()
	t.holding = true
	t.mu.Unlock()
}

// Release sends the held lines, in order, and stops holding.
func (t *Transport) Release() {
	t.mu.Lock()
	t.holding = false
	t.queue = append(t.queue, t.held...)
	t.held = nil
	t.mu.Unlock()
	t.signal()
}

// DropHeld discards the held lines (returned without their newline) and stops
// holding.
func (t *Transport) DropHeld() [][]byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.holding = false
	out := make([][]byte, 0, len(t.held))
	for _, b := range t.held {
		out = append(out, b[:len(b)-1])
	}
	t.held = nil
	return out
}

func (t *Transport) signal() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

func (t *Transport) writeLoop() {
	defer close(t.writeDone)
	for {
		t.mu.Lock()
		q := t.queue
		t.queue = nil
		closing := t.closing
		detached := t.detached
		t.mu.Unlock()

		for _, b := range q {
			if t.Tap != nil {
				t.Tap(ToEngine, b[:len(b)-1])
			}
			if _, err := t.w.Write(b); err != nil {
				t.finishWrite(err)
				return
			}
		}
		if len(q) == 0 && detached {
			return
		}
		if len(q) == 0 && closing {
			t.finishWrite(nil)
			return
		}
		if len(q) == 0 {
			<-t.wake
		}
	}
}

func (t *Transport) finishWrite(err error) {
	cerr := t.w.Close()
	t.mu.Lock()
	t.closed = true
	if err == nil {
		err = cerr
	}
	if err != nil && t.werr == nil {
		t.werr = err
	}
	t.queue = nil
	t.mu.Unlock()
}

// CloseInput flushes queued lines, then closes stdin (the engine exits at EOF).
func (t *Transport) CloseInput() {
	t.mu.Lock()
	t.closing = true
	t.mu.Unlock()
	t.signal()
}

// ReadDone is closed when stdout reaches EOF or fails.
func (t *Transport) ReadDone() <-chan struct{} { return t.readDone }

// WriteDone is closed when the writer has closed stdin (after CloseInput or a write
// error).
func (t *Transport) WriteDone() <-chan struct{} { return t.writeDone }

// ReadErr is the stdout read error (nil on clean EOF). Valid after ReadDone.
func (t *Transport) ReadErr() error { return t.rerr }

// WriteErr is the first stdin write error, if any.
func (t *Transport) WriteErr() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.werr
}

// Pending returns the number of queued, unwritten lines.
func (t *Transport) Pending() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.queue)
}
