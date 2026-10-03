package enginefake

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// A Script is a list of steps run in order against the client, plus standing rules.
//
// The file format is JSONL: one step object per line. Blank lines and lines starting
// with "#" or "//" are comments. Step kinds (one per line):
//
//	{"emit": {...}}                         write one stdout line
//	{"expect": {...}, "timeout": 5000}      wait for a stdin message matching the pattern
//	{"respond": {...}}                      success control_response to the last matched control_request
//	{"respond_error": "msg"}                error control_response to the last matched control_request
//	{"request": {"subtype": ...}, "id": "x"} CLI → client control_request (id defaults to cli_<n>)
//	{"delay": 50}                           sleep, in milliseconds
//	{"stderr": "text"}                      write to stderr
//	{"exit": 0}                             exit now with this code
//	{"on": {...}, "respond": {...}}         standing rule (see below)
//
// "expect" may carry "respond"/"respond_error" to answer in the same step, and "save"
// ({"var": "path.in.message"}) to remember fields. Strings in emitted JSON may use
// ${var}; ${request_id} and ${uuid} are always set from the last matched message.
//
// Patterns match partially: every key in the pattern must be present and match;
// arrays match element-wise with equal length; "$any" matches any present value;
// a string starting with "re:" is a regular expression.
//
// "on" rules are active for the whole run, wherever they appear. A stdin message that
// matches a rule is answered (respond/respond_error, then optional emit) and is not
// offered to "expect". Built-in rules, which a script's own rules override: keep_alive
// is ignored, get_binary_version answers Version, and end_session is answered and
// ends the run with exit code 0.
//
// When the steps run out the fake keeps answering rules until stdin closes, then
// exits 0, like the real engine.
//
// Client mode (a {"client": true} line, or Script.Client) runs the same format from
// the other side, to drive a real engine: "emit" writes to the engine's stdin,
// "expect" skips engine output until a line matches, "respond" answers the engine's
// control requests, there are no built-in rules, and the run ends with the steps.
//
//	makes a fresh UUID and stores it as .
type Script struct {
	Steps  []Step
	Rules  []Step
	Client bool
}

// Step is one script line.
type Step struct {
	Emit         json.RawMessage   `json:"emit,omitempty"`
	Expect       json.RawMessage   `json:"expect,omitempty"`
	On           json.RawMessage   `json:"on,omitempty"`
	Respond      json.RawMessage   `json:"respond,omitempty"`
	RespondError string            `json:"respond_error,omitempty"`
	Request      json.RawMessage   `json:"request,omitempty"`
	ID           string            `json:"id,omitempty"`
	Save         map[string]string `json:"save,omitempty"`
	Delay        int               `json:"delay,omitempty"`
	Stderr       string            `json:"stderr,omitempty"`
	Exit         *int              `json:"exit,omitempty"`
	Timeout      int               `json:"timeout,omitempty"`
	Comment      string            `json:"#,omitempty"`
	Client       bool              `json:"client,omitempty"` // script-level directive

	line int
}

// Parse reads a script.
func Parse(r io.Reader) (*Script, error) {
	s := &Script{}
	br := bufio.NewReader(r)
	n := 0
	for {
		raw, err := br.ReadBytes('\n')
		n++
		line := bytes.TrimSpace(raw)
		if len(line) > 0 && line[0] != '#' && !bytes.HasPrefix(line, []byte("//")) {
			var st Step
			dec := json.NewDecoder(bytes.NewReader(line))
			dec.DisallowUnknownFields()
			if derr := dec.Decode(&st); derr != nil {
				return nil, fmt.Errorf("enginefake: line %d: %w", n, derr)
			}
			st.line = n
			if verr := st.validate(); verr != nil {
				return nil, fmt.Errorf("enginefake: line %d: %w", n, verr)
			}
			switch {
			case st.Client:
				s.Client = true
			case st.On != nil:
				s.Rules = append(s.Rules, st)
			default:
				s.Steps = append(s.Steps, st)
			}
		}
		if err == io.EOF {
			return s, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// ParseString parses a script held in a string (handy in tests).
func ParseString(src string) (*Script, error) { return Parse(strings.NewReader(src)) }

// MustParse parses a script or panics.
func MustParse(src string) *Script {
	s, err := ParseString(src)
	if err != nil {
		panic(err)
	}
	return s
}

// ParseFile reads a script file.
func ParseFile(path string) (*Script, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

func (s *Step) validate() error {
	kinds := 0
	for _, set := range []bool{s.Emit != nil && s.On == nil, s.Expect != nil, s.On != nil, s.Request != nil,
		s.Delay > 0, s.Stderr != "", s.Exit != nil, s.Expect == nil && s.On == nil && (s.Respond != nil || s.RespondError != "")} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		if kinds == 0 && (s.Comment != "" || s.Client) {
			return nil
		}
		return fmt.Errorf("step must have exactly one kind (emit, expect, on, request, respond, delay, stderr, exit)")
	}
	for _, raw := range []json.RawMessage{s.Emit, s.Expect, s.On, s.Request, s.Respond} {
		if raw != nil && !json.Valid(raw) {
			return fmt.Errorf("invalid JSON")
		}
	}
	return nil
}

// Builder helpers, for scripts written in Go.

// Emit returns an emit step for v (a JSON string, []byte or any marshalable value).
func Emit(v any) Step { return Step{Emit: toRaw(v)} }

// Expect returns an expect step for pattern v.
func Expect(v any) Step { return Step{Expect: toRaw(v)} }

// ExpectRespond expects a control request matching v and answers it with resp
// (nil sends a success with no payload).
func ExpectRespond(v, resp any) Step { return Step{Expect: toRaw(v), Respond: respRaw(resp)} }

// On returns a standing rule answering messages matching v with resp (nil sends a
// success with no payload).
func On(v, resp any) Step { return Step{On: toRaw(v), Respond: respRaw(resp)} }

func respRaw(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage("null")
	}
	return toRaw(v)
}

// Request returns a CLI → client control request step.
func Request(id string, body any) Step { return Step{Request: toRaw(body), ID: id} }

// Delay returns a delay step.
func Delay(ms int) Step { return Step{Delay: ms} }

// Exit returns an exit step.
func Exit(code int) Step { return Step{Exit: &code} }

// New builds a script from steps; "on" steps become rules.
func New(steps ...Step) *Script {
	s := &Script{}
	for _, st := range steps {
		if st.On != nil {
			s.Rules = append(s.Rules, st)
		} else {
			s.Steps = append(s.Steps, st)
		}
	}
	return s
}

func toRaw(v any) json.RawMessage {
	switch x := v.(type) {
	case nil:
		return nil
	case json.RawMessage:
		return x
	case []byte:
		return json.RawMessage(x)
	case string:
		return json.RawMessage(x)
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
