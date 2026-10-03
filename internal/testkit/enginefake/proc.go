package enginefake

import (
	"context"
	"io"
	"sync"
)

// Proc is a script running in-process on pipes, standing in for a claude process.
// Write the client's stdin to Stdin, read its stdout from Stdout.
type Proc struct {
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser

	cancel context.CancelFunc
	done   chan struct{}

	mu       sync.Mutex
	code     int
	err      error
	received [][]byte
}

// Start runs s in a goroutine. Stdout and Stderr close when the script exits.
func Start(s *Script) *Proc {
	inR, inW := io.Pipe()
	out, errp := newBufPipe(), newBufPipe()
	ctx, cancel := context.WithCancel(context.Background())
	p := &Proc{Stdin: inW, Stdout: out, Stderr: errp, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		code, err, recv := s.run(ctx, inR, out, errp)
		p.mu.Lock()
		p.code, p.err, p.received = code, err, recv
		p.mu.Unlock()
		out.closeWrite()
		errp.closeWrite()
		// Unblock a client still writing to a process that has exited.
		_ = inR.CloseWithError(io.ErrClosedPipe)
	}()
	return p
}

// Wait blocks until the script exits and returns its exit code and failure, if any.
func (p *Proc) Wait() (int, error) {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code, p.err
}

// Done is closed when the script has exited.
func (p *Proc) Done() <-chan struct{} { return p.done }

// Kill stops the script as if the process were killed.
func (p *Proc) Kill() {
	p.cancel()
	_ = p.Stdin.Close()
	_ = p.Stdout.Close() // unblock a script writing to a reader that went away
	_ = p.Stderr.Close()
}

// Received returns every line the client wrote to stdin (valid after Wait).
func (p *Proc) Received() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.received
}
