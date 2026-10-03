package engine

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/proto"
)

type collector struct {
	mu     sync.Mutex
	events []proto.Event
	errs   []error
}

func (c *collector) event(e proto.Event) { c.mu.Lock(); c.events = append(c.events, e); c.mu.Unlock() }
func (c *collector) err(e error)         { c.mu.Lock(); c.errs = append(c.errs, e); c.mu.Unlock() }

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func TestTransportReadsHugeLinesAndSkipsNoise(t *testing.T) {
	big := strings.Repeat("x", 9<<20) // 9 MB tool result
	input := "[SandboxDebug] noise\n" +
		`{"type":"keep_alive"}` + "\n\n" +
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"` + big + `"}]}}` + "\r\n" +
		`{"type":` + "\n" +
		`{"type":"result","subtype":"success","is_error":false,"result":"done"}` // no trailing newline
	tr := NewTransport(strings.NewReader(input), nopWriteCloser{io.Discard})
	var c collector
	tr.Start(c.event, c.err)
	<-tr.ReadDone()
	if tr.ReadErr() != nil {
		t.Fatal(tr.ReadErr())
	}
	if len(c.events) != 3 {
		t.Fatalf("got %d events, want 3", len(c.events))
	}
	u := c.events[1].(*proto.User)
	if got := u.ToolResults()[0].Content.PlainText(); len(got) != len(big) {
		t.Errorf("big line truncated: %d", len(got))
	}
	if r := c.events[2].(*proto.Result); r.Result != "done" {
		t.Errorf("last line: %+v", r)
	}
	if len(c.errs) != 1 {
		t.Errorf("want 1 malformed-line error, got %v", c.errs)
	}
}

func TestTransportLongLinesReuseDoesNotCorrupt(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&sb, `{"type":"prompt_suggestion","suggestion":"%d-%s"}`+"\n", i, strings.Repeat("ab", 40000+i*1000))
	}
	tr := NewTransport(strings.NewReader(sb.String()), nopWriteCloser{io.Discard})
	var c collector
	tr.Start(c.event, c.err)
	<-tr.ReadDone()
	if len(c.events) != 50 {
		t.Fatalf("got %d", len(c.events))
	}
	for i, e := range c.events {
		s := e.(*proto.PromptSuggestion).Suggestion
		if !strings.HasPrefix(s, fmt.Sprintf("%d-", i)) || len(s) != len(fmt.Sprint(i))+1+2*(40000+i*1000) {
			t.Fatalf("event %d corrupted (len %d)", i, len(s))
		}
	}
}

// slowPipe blocks writes until released, like a full pipe.
type slowPipe struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	release chan struct{}
	closed  bool
}

func (p *slowPipe) Write(b []byte) (int, error) {
	<-p.release
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.Write(b)
}
func (p *slowPipe) Close() error { p.mu.Lock(); p.closed = true; p.mu.Unlock(); return nil }

func TestTransportSendNeverBlocksAndKeepsOrder(t *testing.T) {
	p := &slowPipe{release: make(chan struct{})}
	tr := NewTransport(strings.NewReader(""), p)
	tr.Start(func(proto.Event) {}, nil)

	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		// Concurrent senders: per-sender order must hold.
		for s := 0; s < 4; s++ {
			wg.Add(1)
			go func(s int) {
				defer wg.Done()
				for i := 0; i < 500; i++ {
					if err := tr.Send([]byte(fmt.Sprintf(`{"s":%d,"i":%d}`, s, i))); err != nil {
						t.Error(err)
					}
				}
			}(s)
		}
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Send blocked on a stuck pipe")
	}
	close(p.release)
	tr.CloseInput()
	<-tr.WriteDone()
	if err := tr.Send([]byte("x")); !errors.Is(err, ErrClosed) {
		t.Errorf("Send after close: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(p.buf.String()), "\n")
	if len(lines) != 2000 {
		t.Fatalf("wrote %d lines", len(lines))
	}
	next := map[int]int{}
	for _, l := range lines {
		var s, i int
		fmt.Sscanf(l, `{"s":%d,"i":%d}`, &s, &i)
		if i != next[s] {
			t.Fatalf("sender %d out of order: got %d want %d", s, i, next[s])
		}
		next[s]++
	}
	if !p.closed {
		t.Error("stdin not closed")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }
func (failWriter) Close() error              { return nil }

func TestTransportWriteErrorSurfaces(t *testing.T) {
	tr := NewTransport(strings.NewReader(""), failWriter{})
	tr.Start(func(proto.Event) {}, nil)
	_ = tr.Send([]byte("{}"))
	<-tr.WriteDone()
	if tr.WriteErr() == nil {
		t.Fatal("no write error")
	}
	if err := tr.Send([]byte("{}")); err == nil {
		t.Error("Send after failure should error")
	}
}

func TestTransportTap(t *testing.T) {
	var mu sync.Mutex
	var got []string
	pr, pw := io.Pipe()
	var out bytes.Buffer
	tr := NewTransport(pr, nopWriteCloser{&out})
	tr.Tap = func(d Direction, l []byte) {
		mu.Lock()
		got = append(got, fmt.Sprintf("%d:%s", d, l))
		mu.Unlock()
	}
	tr.Start(func(proto.Event) {}, nil)
	_ = tr.Send([]byte(`{"type":"keep_alive"}`))
	tr.CloseInput()
	<-tr.WriteDone()
	fmt.Fprintln(pw, `{"type":"keep_alive"}`)
	pw.Close()
	<-tr.ReadDone()
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got, "|") != `1:{"type":"keep_alive"}|0:{"type":"keep_alive"}` {
		t.Errorf("tap: %v", got)
	}
}

func TestRing(t *testing.T) {
	r := NewRing(10)
	fmt.Fprint(r, "abc")
	if r.String() != "abc" {
		t.Errorf("got %q", r.String())
	}
	fmt.Fprint(r, "defghij")
	if r.String() != "abcdefghij" {
		t.Errorf("got %q", r.String())
	}
	fmt.Fprint(r, "kl")
	if r.String() != "cdefghijkl" {
		t.Errorf("got %q", r.String())
	}
	fmt.Fprint(r, "0123456789XYZ")
	if r.String() != "3456789XYZ" {
		t.Errorf("got %q", r.String())
	}
	if r.Total() != 25 {
		t.Errorf("total %d", r.Total())
	}

	r = NewRing(StderrRingSize)
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(r, "line %d\n", i)
	}
	if got := r.Tail(2); got != "line 9998\nline 9999" {
		t.Errorf("tail %q", got)
	}
	if len(r.Bytes()) != StderrRingSize {
		t.Errorf("size %d", len(r.Bytes()))
	}
	if NewRing(8).Tail(3) != "" {
		t.Error("empty tail")
	}
}
