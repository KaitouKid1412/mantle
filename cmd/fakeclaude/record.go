package main

import (
	"bytes"
	"io"
	"strings"
	"sync"
)

// recordingReader copies everything read from stdin so it can be logged at exit.
type recordingReader struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (r *recordingReader) wrap(in io.Reader) io.Reader { return io.TeeReader(in, r) }

func (r *recordingReader) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *recordingReader) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, l := range strings.Split(r.buf.String(), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
