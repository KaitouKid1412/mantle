package fakeapi

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultChunkRunes = 12

// Kind says how a request was classified and answered.
type Kind string

const (
	KindMain        Kind = "main"         // main conversation: consumes scripted turns
	KindSide        Kind = "side"         // side call (title, helper, suggestion): SideReply
	KindCountTokens Kind = "count_tokens" // POST /v1/messages/count_tokens
	KindOther       Kind = "other"        // any other path: 200 for "/", 404 otherwise
)

// Request is one recorded HTTP request.
type Request struct {
	Time   time.Time   `json:"time"`
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Query  string      `json:"query,omitempty"`
	Header http.Header `json:"header"` // secrets replaced by "REDACTED"
	Body   []byte      `json:"-"`
	Kind   Kind        `json:"kind"`
	// Reason explains the classification (e.g. "side: no tools").
	Reason string `json:"reason,omitempty"`
	// Turn is the index of the scripted turn that answered, or -1.
	Turn int `json:"turn"`
	// Status is the HTTP status fakeapi answered with.
	Status int `json:"status"`
}

// MarshalJSON includes the body as JSON when it is JSON, else as a string.
func (r Request) MarshalJSON() ([]byte, error) {
	type plain Request
	var body any = string(r.Body)
	if json.Valid(r.Body) {
		body = json.RawMessage(r.Body)
	}
	return json.Marshal(struct {
		plain
		Body any `json:"body,omitempty"`
	}{plain(r), body})
}

// Messages decodes the body as a Messages API request.
func (r Request) Messages() (*MessagesRequest, error) {
	var m MessagesRequest
	if err := json.Unmarshal(r.Body, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// An Option configures a Server.
type Option func(*Server)

// WithLogf logs one line per request (and unknown paths) through f.
func WithLogf(f func(format string, args ...any)) Option {
	return func(s *Server) { s.logf = f }
}

// WithChunkRunes sets the maximum runes per text or thinking delta (default 12).
func WithChunkRunes(n int) Option {
	return func(s *Server) { s.chunkRunes = n }
}

// WithChunkDelay sleeps between streamed events of every reply (default 0).
func WithChunkDelay(d time.Duration) Option {
	return func(s *Server) { s.chunkDelay = d }
}

// WithRecorder calls f with every request after it was answered.
func WithRecorder(f func(Request)) Option {
	return func(s *Server) { s.onRequest = f }
}

// WithClassifier replaces Classify.
func WithClassifier(f func(h http.Header, m *MessagesRequest) (Kind, string)) Option {
	return func(s *Server) { s.classify = f }
}

// Server is the fake Messages API. It is an http.Handler; serve it with httptest.NewServer
// or net/http.
type Server struct {
	logf       func(string, ...any)
	chunkRunes int
	chunkDelay time.Duration
	onRequest  func(Request)
	classify   func(http.Header, *MessagesRequest) (Kind, string)

	mu        sync.Mutex
	script    *Script
	next      int
	msgSeq    int
	toolSeq   int
	reqs      []Request
	unmatched []string
}

// New returns a server that plays script. It panics on an invalid script (use Parse to
// get an error instead). A nil script answers every main request with "OK".
func New(script *Script, opts ...Option) *Server {
	if script == nil {
		script = &Script{}
	}
	if err := script.Validate(); err != nil {
		panic(err)
	}
	s := &Server{script: script, chunkRunes: defaultChunkRunes, classify: Classify}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Requests returns a copy of every request so far, in arrival order.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.reqs...)
}

// RequestsOf returns the recorded requests of one kind.
func (s *Server) RequestsOf(k Kind) []Request {
	var out []Request
	for _, r := range s.Requests() {
		if r.Kind == k {
			out = append(out, r)
		}
	}
	return out
}

// Consumed is the number of scripted turns played so far.
func (s *Server) Consumed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next
}

// Remaining is the number of scripted turns not played yet.
func (s *Server) Remaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.script.Turns) - s.next
}

// Unmatched describes main requests that did not match the next turn.
func (s *Server) Unmatched() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.unmatched...)
}

func (s *Server) log(format string, args ...any) {
	if s.logf != nil {
		s.logf(format, args...)
	}
}

// ServeHTTP answers one request.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	rec := Request{
		Time: time.Now(), Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
		Header: redact(r.Header), Body: body, Kind: KindOther, Turn: -1,
	}
	switch {
	case err != nil:
		rec.Status = http.StatusBadRequest
		writeError(w, rec.Status, "invalid_request_error", "fakeapi: read body: "+err.Error())
	case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
		s.serveMessages(w, &rec)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/messages/count_tokens":
		rec.Kind = KindCountTokens
		rec.Status = http.StatusOK
		writeJSON(w, rec.Status, map[string]int{"input_tokens": max(1, len(body)/4)})
	case (r.URL.Path == "/" || r.URL.Path == "/api/hello") &&
		(r.Method == http.MethodGet || r.Method == http.MethodHead):
		// Connectivity probes: claude sends HEAD /api/hello at startup.
		rec.Status = http.StatusOK
		w.WriteHeader(rec.Status)
	case r.Method == http.MethodConnect:
		// claude was given fakeapi as its HTTPS proxy: refuse every outside host.
		rec.Path = r.Host
		rec.Status = http.StatusForbidden
		s.log("fakeapi: refused proxy CONNECT %s", r.Host)
		http.Error(w, "fakeapi: offline", rec.Status)
	case r.URL.Host != "" && r.URL.Host != r.Host:
		// A proxied plain-HTTP request for another host.
		rec.Path = r.URL.String()
		rec.Status = http.StatusForbidden
		s.log("fakeapi: refused proxy request %s %s", r.Method, r.URL)
		http.Error(w, "fakeapi: offline", rec.Status)
	default:
		rec.Status = http.StatusNotFound
		s.log("fakeapi: unknown path %s %s", r.Method, r.URL.RequestURI())
		writeError(w, rec.Status, "not_found_error", "fakeapi: no handler for "+r.Method+" "+r.URL.Path)
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, rec)
	s.mu.Unlock()
	s.log("fakeapi: %s %s -> %d %s %s", r.Method, r.URL.Path, rec.Status, rec.Kind, rec.Reason)
	if s.onRequest != nil {
		s.onRequest(rec)
	}
}

func (s *Server) serveMessages(w http.ResponseWriter, rec *Request) {
	var m MessagesRequest
	if err := json.Unmarshal(rec.Body, &m); err != nil {
		rec.Status = http.StatusBadRequest
		writeError(w, rec.Status, "invalid_request_error", "fakeapi: bad JSON body: "+err.Error())
		return
	}
	kind, reason := s.classify(rec.Header, &m)
	rec.Kind, rec.Reason = kind, reason

	var turn Turn
	s.mu.Lock()
	switch {
	case kind != KindMain:
		turn = TextTurn(orDefault(s.script.SideReply, "OK"))
	case s.next >= len(s.script.Turns):
		turn = TextTurn(orDefault(s.script.DefaultReply, "OK"))
		rec.Reason += "; script exhausted"
	default:
		t := s.script.Turns[s.next]
		if u := m.LatestUser(); !t.Match.Matches(u) {
			miss := fmt.Sprintf("turn %d wants %s; latest user message: %q", s.next, t.Match, truncate(u.Text, 200))
			s.unmatched = append(s.unmatched, miss)
			turn = TextTurn("fakeapi: unmatched request (" + miss + ")")
			rec.Reason += "; unmatched"
		} else {
			turn = t
			rec.Turn = s.next
			s.next++
		}
	}
	rep := s.resolve(&m, &turn, len(rec.Body))
	s.mu.Unlock()

	for k, v := range turn.Headers {
		w.Header().Set(k, v)
	}
	w.Header().Set("request-id", "req_fake_"+strings.TrimPrefix(rep.ID, "msg_fake_"))
	if turn.Error != nil {
		rec.Status = turn.Error.Status
		writeError(w, rec.Status, orDefault(turn.Error.Type, "api_error"), orDefault(turn.Error.Message, "fakeapi scripted error"))
		return
	}
	rec.Status = http.StatusOK
	if !m.Stream {
		writeJSON(w, rec.Status, rep.message())
		return
	}
	delay := s.chunkDelay
	if turn.ChunkDelayMS > 0 {
		delay = time.Duration(turn.ChunkDelayMS) * time.Millisecond
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(rec.Status)
	flusher, _ := w.(http.Flusher)
	for i, ev := range rep.events(s.chunkRunes) {
		if i > 0 && delay > 0 {
			time.Sleep(delay)
		}
		if _, err := ev.WriteTo(w); err != nil {
			return // client went away (e.g. an interrupt)
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// resolve fills ids, signatures, inputs and usage for a turn. Callers hold s.mu.
func (s *Server) resolve(m *MessagesRequest, t *Turn, bodyLen int) *reply {
	s.msgSeq++
	rep := &reply{
		ID:         fmt.Sprintf("msg_fake_%d", s.msgSeq),
		Model:      orDefault(m.Model, "claude-fake"),
		StopReason: t.stopReason(),
	}
	for _, b := range t.Reply {
		switch b.Type {
		case "thinking":
			if b.Signature == "" {
				b.Signature = fakeSignature(b.Thinking)
			}
		case "tool_use":
			if b.ID == "" {
				s.toolSeq++
				b.ID = fmt.Sprintf("toolu_fake_%d", s.toolSeq)
			}
			if len(b.Input) == 0 {
				b.Input = json.RawMessage("{}")
			} else {
				var buf bytes.Buffer
				if json.Compact(&buf, b.Input) == nil {
					b.Input = buf.Bytes()
				}
			}
		}
		rep.Blocks = append(rep.Blocks, b)
	}
	rep.Usage = Usage{
		InputTokens:  max(1, bodyLen/4),
		OutputTokens: outputTokens(rep.Blocks),
		ServiceTier:  "standard",
	}
	return rep
}

// Classify decides whether a /v1/messages request belongs to the main conversation.
//
// Claude Code makes side calls besides the main agent loop: session titles, prompt
// suggestions and other small helper requests. Those carry no tools, so a request
// without tools is a side call; everything else is the main conversation.
func Classify(h http.Header, m *MessagesRequest) (Kind, string) {
	if len(m.Tools) == 0 {
		return KindSide, "side: no tools"
	}
	return KindMain, "main"
}

func readBody(r *http.Request) ([]byte, error) {
	var rd io.Reader = r.Body
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		rd = gz
	}
	return io.ReadAll(rd)
}

// redact copies h with credential-looking headers replaced by "REDACTED".
func redact(h http.Header) http.Header {
	out := h.Clone()
	for k := range out {
		lk := strings.ToLower(k)
		for _, s := range []string{"key", "auth", "token", "cookie", "secret"} {
			if strings.Contains(lk, s) {
				out[k] = []string{"REDACTED"}
				break
			}
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, typ, msg string) {
	writeJSON(w, status, map[string]any{
		"type":  "error",
		"error": map[string]string{"type": typ, "message": msg},
	})
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
