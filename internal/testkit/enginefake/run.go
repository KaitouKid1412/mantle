package enginefake

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultExpectTimeout bounds each expect step unless the step sets "timeout".
const DefaultExpectTimeout = 5 * time.Second

// ExitError reports a script failure; Code is the exit code fakeclaude uses (3).
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// FailCode is the exit code for a script failure (mismatch, timeout, early EOF).
const FailCode = 3

type runner struct {
	s      *Script
	stdout io.Writer
	stderr io.Writer

	outMu sync.Mutex

	varMu sync.Mutex
	vars  map[string]string

	recvMu   sync.Mutex
	received [][]byte

	inbox  chan []byte // stdin lines not taken by rules
	eof    chan struct{}
	ended  chan int // end_session or rule-driven exit
	nextID int
}

// Run executes the script, reading the client's stdin lines from stdin and writing
// stdout lines to stdout. It returns the process exit code; err is non-nil (an
// *ExitError) when the script failed.
func (s *Script) Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	code, err, _ := s.run(ctx, stdin, stdout, stderr)
	return code, err
}

func (s *Script) run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) (int, error, [][]byte) {
	if stderr == nil {
		stderr = io.Discard
	}
	r := &runner{
		s:      s,
		stdout: stdout,
		stderr: stderr,
		vars:   map[string]string{},
		inbox:  make(chan []byte, 1024),
		eof:    make(chan struct{}),
		ended:  make(chan int, 1),
	}
	go r.readStdin(stdin)
	code, err := r.steps(ctx)
	if err == nil && code < 0 {
		// Steps ran out: keep serving rules until stdin closes, the session ends or
		// the context is cancelled.
		select {
		case <-r.eof:
			code = 0
		case c := <-r.ended:
			code = c
		case <-ctx.Done():
			code = 0
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "fakeclaude: %v\n", err)
	}
	r.recvMu.Lock()
	recv := r.received
	r.recvMu.Unlock()
	return code, err, recv
}

func (r *runner) fail(st Step, format string, args ...any) error {
	return &ExitError{Code: FailCode, Err: fmt.Errorf("script line %d: %s", st.line, fmt.Sprintf(format, args...))}
}

// steps runs the step list. It returns code -1 when the steps ran out normally.
func (r *runner) steps(ctx context.Context) (int, error) {
	for _, st := range r.s.Steps {
		select {
		case c := <-r.ended:
			return c, nil
		default:
		}
		switch {
		case st.Emit != nil:
			r.write(r.subst(st.Emit))
		case st.Expect != nil:
			msg, err := r.expect(ctx, st)
			if err != nil {
				return FailCode, err
			}
			r.capture(msg, st.Save)
			if err := r.answer(st); err != nil {
				return FailCode, err
			}
		case st.Respond != nil || st.RespondError != "":
			if err := r.answer(st); err != nil {
				return FailCode, err
			}
		case st.Request != nil:
			id := st.ID
			if id == "" {
				r.nextID++
				id = "cli_" + strconv.Itoa(r.nextID)
			}
			body := r.subst(st.Request)
			line, _ := json.Marshal(struct {
				Type      string          `json:"type"`
				RequestID string          `json:"request_id"`
				Request   json.RawMessage `json:"request"`
			}{"control_request", id, body})
			r.write(line)
		case st.Delay > 0:
			select {
			case <-time.After(time.Duration(st.Delay) * time.Millisecond):
			case <-ctx.Done():
				return 0, nil
			}
		case st.Stderr != "":
			fmt.Fprint(r.stderr, st.Stderr)
		case st.Exit != nil:
			return *st.Exit, nil
		}
	}
	return -1, nil
}

func (r *runner) expect(ctx context.Context, st Step) ([]byte, error) {
	var pat any
	if err := json.Unmarshal(st.Expect, &pat); err != nil {
		return nil, r.fail(st, "bad pattern: %v", err)
	}
	timeout := DefaultExpectTimeout
	if st.Timeout > 0 {
		timeout = time.Duration(st.Timeout) * time.Millisecond
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case line := <-r.inbox:
		return r.check(st, pat, line)
	default:
	}
	select {
	case line := <-r.inbox:
		return r.check(st, pat, line)
	case <-r.eof:
		// Drain anything that raced with EOF.
		select {
		case line := <-r.inbox:
			return r.check(st, pat, line)
		default:
		}
		return nil, r.fail(st, "stdin closed while expecting %s", st.Expect)
	case <-timer.C:
		return nil, r.fail(st, "timed out after %v expecting %s", timeout, st.Expect)
	case <-ctx.Done():
		return nil, r.fail(st, "cancelled while expecting %s", st.Expect)
	}
}

func (r *runner) check(st Step, pat any, line []byte) ([]byte, error) {
	var got any
	if err := json.Unmarshal(line, &got); err != nil {
		return nil, r.fail(st, "client sent invalid JSON: %s", line)
	}
	if why := match(pat, got, ""); why != "" {
		return nil, r.fail(st, "expected %s\n  got %s\n  (%s)", st.Expect, line, why)
	}
	return line, nil
}

// answer writes the step's respond/respond_error to ${request_id}.
func (r *runner) answer(st Step) error {
	if st.Respond == nil && st.RespondError == "" {
		return nil
	}
	id := r.get("request_id")
	if id == "" {
		return r.fail(st, "respond without a matched control_request")
	}
	r.write(controlResponse(id, r.subst(st.Respond), st.RespondError))
	return nil
}

func controlResponse(id string, resp json.RawMessage, errMsg string) []byte {
	body := map[string]any{"request_id": id}
	if errMsg != "" {
		body["subtype"] = "error"
		body["error"] = errMsg
	} else {
		body["subtype"] = "success"
		if len(resp) > 0 && !bytes.Equal(bytes.TrimSpace(resp), []byte("null")) {
			body["response"] = resp
		}
	}
	line, _ := json.Marshal(map[string]any{"type": "control_response", "response": body})
	return line
}

func (r *runner) readStdin(stdin io.Reader) {
	defer close(r.eof)
	br := bufio.NewReaderSize(stdin, 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			r.recvMu.Lock()
			r.received = append(r.received, line)
			r.recvMu.Unlock()
			if !r.applyRules(line) {
				select {
				case r.inbox <- line:
				default: // nobody is expecting this many lines; keep reading to EOF
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// applyRules answers line if a standing rule matches; it reports whether it did.
func (r *runner) applyRules(line []byte) bool {
	var got any
	if json.Unmarshal(line, &got) != nil {
		return false
	}
	for _, rule := range r.s.Rules {
		var pat any
		if json.Unmarshal(rule.On, &pat) != nil || match(pat, got, "") != "" {
			continue
		}
		vars := r.capture(line, rule.Save)
		if rule.Respond != nil || rule.RespondError != "" {
			r.write(controlResponse(vars["request_id"], r.subst(rule.Respond), rule.RespondError))
		}
		if rule.Emit != nil {
			r.write(r.subst(rule.Emit))
		}
		return true
	}
	m, _ := got.(map[string]any)
	switch m["type"] {
	case "keep_alive":
		return true
	case "control_request":
		if req, _ := m["request"].(map[string]any); req["subtype"] == "end_session" {
			id, _ := m["request_id"].(string)
			r.write(controlResponse(id, nil, ""))
			select {
			case r.ended <- 0:
			default:
			}
			return true
		}
	}
	return false
}

// capture sets ${request_id}/${uuid} from line plus the step's save paths, and
// returns a snapshot of the variables.
func (r *runner) capture(line []byte, save map[string]string) map[string]string {
	var got any
	_ = json.Unmarshal(line, &got)
	r.varMu.Lock()
	defer r.varMu.Unlock()
	if m, ok := got.(map[string]any); ok {
		if id, ok := m["request_id"].(string); ok && m["type"] == "control_request" {
			r.vars["request_id"] = id
		}
		if u, ok := m["uuid"].(string); ok {
			r.vars["uuid"] = u
		}
	}
	for name, path := range save {
		if v, ok := lookup(got, path); ok {
			switch x := v.(type) {
			case string:
				r.vars[name] = x
			default:
				b, _ := json.Marshal(x)
				r.vars[name] = string(b)
			}
		}
	}
	snap := make(map[string]string, len(r.vars))
	for k, v := range r.vars {
		snap[k] = v
	}
	return snap
}

func (r *runner) get(name string) string {
	r.varMu.Lock()
	defer r.varMu.Unlock()
	return r.vars[name]
}

var varRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// subst replaces ${var} inside JSON string values. Values are JSON-escaped.
func (r *runner) subst(raw json.RawMessage) json.RawMessage {
	if !bytes.Contains(raw, []byte("${")) {
		return raw
	}
	r.varMu.Lock()
	defer r.varMu.Unlock()
	return varRe.ReplaceAllFunc(raw, func(m []byte) []byte {
		name := string(m[2 : len(m)-1])
		v, ok := r.vars[name]
		if !ok {
			return m
		}
		q, _ := json.Marshal(v)
		return q[1 : len(q)-1]
	})
}

func (r *runner) write(line []byte) {
	r.outMu.Lock()
	defer r.outMu.Unlock()
	b := make([]byte, 0, len(line)+1)
	b = append(b, bytes.TrimSpace(line)...)
	b = append(b, '\n')
	_, _ = r.stdout.Write(b)
}

// match returns "" if got matches pattern pat, else a reason.
func match(pat, got any, path string) string {
	switch p := pat.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: want object", where(path))
		}
		for k, pv := range p {
			gv, ok := g[k]
			if !ok {
				return fmt.Sprintf("%s: missing", where(join(path, k)))
			}
			if why := match(pv, gv, join(path, k)); why != "" {
				return why
			}
		}
		return ""
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(p) {
			return fmt.Sprintf("%s: want array of %d", where(path), len(p))
		}
		for i := range p {
			if why := match(p[i], g[i], fmt.Sprintf("%s[%d]", path, i)); why != "" {
				return why
			}
		}
		return ""
	case string:
		if p == "$any" {
			return ""
		}
		if strings.HasPrefix(p, "re:") {
			s, ok := got.(string)
			if !ok {
				return fmt.Sprintf("%s: want string", where(path))
			}
			re, err := regexp.Compile(p[3:])
			if err != nil || !re.MatchString(s) {
				return fmt.Sprintf("%s: %q !~ %s", where(path), s, p[3:])
			}
			return ""
		}
	}
	if !equalJSON(pat, got) {
		pb, _ := json.Marshal(pat)
		gb, _ := json.Marshal(got)
		return fmt.Sprintf("%s: want %s, got %s", where(path), pb, gb)
	}
	return ""
}

func equalJSON(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(ab, bb)
}

func join(path, k string) string {
	if path == "" {
		return k
	}
	return path + "." + k
}

func where(path string) string {
	if path == "" {
		return "(root)"
	}
	return path
}

// lookup follows a dotted path ("request.subtype", "message.content.0.text").
func lookup(v any, path string) (any, bool) {
	if path == "" {
		return v, true
	}
	for _, part := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			nv, ok := x[part]
			if !ok {
				return nil, false
			}
			v = nv
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}
