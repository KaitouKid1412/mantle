package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Default control request timeouts.
const (
	DefaultControlTimeout = 30 * time.Second
	InitializeTimeout     = 60 * time.Second
)

// slowSubtypes may legitimately take longer than the default timeout.
var slowSubtypes = map[string]time.Duration{
	proto.SubInitialize:           InitializeTimeout,
	proto.SubGenerateSessionTitle: 2 * time.Minute,
	proto.SubMCPCall:              5 * time.Minute,
	proto.SubMCPReconnect:         2 * time.Minute,
	proto.SubReloadPlugins:        2 * time.Minute,
	proto.SubEndSession:           10 * time.Second,
	SubSideQuestion:               2 * time.Minute,
	SubGetWorkspaceDiff:           time.Minute,
}

// TimeoutFor returns the timeout used for a control request subtype.
func TimeoutFor(subtype string) time.Duration {
	if d, ok := slowSubtypes[subtype]; ok {
		return d
	}
	return DefaultControlTimeout
}

// Errors from control requests.
var (
	ErrTimeout = errors.New("engine: control request timed out")
	ErrExited  = errors.New("engine: engine exited")
)

// sender writes one stdin line; Transport.Send in production.
type sender func(line []byte) error

type result struct {
	body proto.ControlResponseBody
	err  error
}

type call struct {
	subtype string
	ch      chan result // capacity 1
}

// correlator matches control responses to our requests by request_id.
type correlator struct {
	send    sender
	sendNow sender // bypasses the transport's hold (handshake); nil = send

	mu      sync.Mutex
	n       uint64
	pending map[string]*call
	closed  error
}

func newCorrelator(send sender) *correlator {
	return &correlator{send: send, pending: map[string]*call{}}
}

func newRequestID(n uint64) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("req_%d_%s", n, hex.EncodeToString(b[:]))
}

// Request sends req and waits for its response, the timeout or ctx. On timeout or
// cancellation it sends control_cancel_request. Error responses become
// *proto.ControlError. It returns the request id too, for messages.
func (c *correlator) Request(ctx context.Context, req proto.Request, timeout time.Duration) (string, json.RawMessage, error) {
	id, body, err := c.RequestBody(ctx, req, timeout)
	return id, body.Response, err
}

// RequestBody is Request returning the whole response body (initialize also carries
// pending_permission_requests and pending_user_dialog_requests there).
func (c *correlator) RequestBody(ctx context.Context, req proto.Request, timeout time.Duration) (string, proto.ControlResponseBody, error) {
	return c.request(ctx, req, timeout, c.send)
}

// RequestBodyNow is RequestBody sent ahead of held lines (the initialize handshake).
func (c *correlator) RequestBodyNow(ctx context.Context, req proto.Request, timeout time.Duration) (string, proto.ControlResponseBody, error) {
	send := c.sendNow
	if send == nil {
		send = c.send
	}
	return c.request(ctx, req, timeout, send)
}

func (c *correlator) request(ctx context.Context, req proto.Request, timeout time.Duration, send sender) (string, proto.ControlResponseBody, error) {
	id, wait, err := c.begin(req, timeout, send)
	if err != nil {
		return id, proto.ControlResponseBody{}, err
	}
	body, err := wait(ctx)
	return id, body, err
}

// begin sends req and returns a function that waits for its reply. Sending and
// waiting are split so a request can take its place on stdin now and be awaited later.
func (c *correlator) begin(req proto.Request, timeout time.Duration, send sender) (string, func(context.Context) (proto.ControlResponseBody, error), error) {
	var none proto.ControlResponseBody
	c.mu.Lock()
	if c.closed != nil {
		err := c.closed
		c.mu.Unlock()
		return "", nil, err
	}
	c.n++
	id := newRequestID(c.n)
	cl := &call{subtype: req.ControlSubtype(), ch: make(chan result, 1)}
	c.pending[id] = cl
	c.mu.Unlock()

	line, err := proto.MarshalControlRequest(id, req)
	if err == nil {
		err = send(line)
	}
	if err != nil {
		c.drop(id)
		return id, nil, err
	}
	if timeout <= 0 {
		timeout = TimeoutFor(cl.subtype)
	}
	wait := func(ctx context.Context) (proto.ControlResponseBody, error) {
		t := time.NewTimer(timeout)
		defer t.Stop()
		select {
		case r := <-cl.ch:
			return r.body, r.err
		case <-t.C:
			c.cancel(id)
			return none, fmt.Errorf("%w: %s after %v", ErrTimeout, cl.subtype, timeout)
		case <-ctx.Done():
			c.cancel(id)
			return none, ctx.Err()
		}
	}
	return id, wait, nil
}

// BeginNow sends req ahead of held lines and returns a function that waits for the
// reply (the handshake's version request: its stdin position matters, not its reply).
func (c *correlator) BeginNow(req proto.Request, timeout time.Duration) (func(context.Context) (proto.ControlResponseBody, error), error) {
	send := c.sendNow
	if send == nil {
		send = c.send
	}
	_, wait, err := c.begin(req, timeout, send)
	return wait, err
}

// Complete delivers a control_response. It reports whether the id was pending.
func (c *correlator) Complete(body proto.ControlResponseBody) bool {
	c.mu.Lock()
	cl, ok := c.pending[body.RequestID]
	delete(c.pending, body.RequestID)
	c.mu.Unlock()
	if !ok {
		return false
	}
	if err := body.Err(); err != nil {
		cl.ch <- result{err: err}
	} else {
		cl.ch <- result{body: body}
	}
	return true
}

// Fail completes a pending request with err (its line was never sent).
func (c *correlator) Fail(id string, err error) {
	c.mu.Lock()
	cl, ok := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ok {
		cl.ch <- result{err: err}
	}
}

// Subtype returns the subtype of a pending request.
func (c *correlator) Subtype(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cl, ok := c.pending[id]; ok {
		return cl.subtype
	}
	return ""
}

func (c *correlator) drop(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *correlator) cancel(id string) {
	c.mu.Lock()
	_, ok := c.pending[id]
	delete(c.pending, id)
	closed := c.closed != nil
	c.mu.Unlock()
	if ok && !closed {
		if line, err := proto.MarshalControlCancel(id); err == nil {
			_ = c.send(line)
		}
	}
}

// Close fails every pending request with err and refuses new ones.
func (c *correlator) Close(err error) {
	c.mu.Lock()
	if c.closed == nil {
		c.closed = err
	}
	p := c.pending
	c.pending = map[string]*call{}
	c.mu.Unlock()
	for _, cl := range p {
		cl.ch <- result{err: err}
	}
}

// Len returns the number of pending requests.
func (c *correlator) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}
