package proto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Envelope holds the fields shared by every stdout message. Every concrete Event type
// embeds it, so ev.Type, ev.Subtype and ev.Raw work on the concrete types and
// ev.Env() works through the Event interface.
type Envelope struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	UUID      string `json:"uuid,omitempty"`
	SessionID string `json:"session_id,omitempty"`

	// Raw is the line exactly as received. It is never marshalled.
	Raw json.RawMessage `json:"-"`
	// Mismatch is set when a known field had an unexpected JSON shape. The field is
	// left at its zero value and the rest of the message is still decoded.
	Mismatch error `json:"-"`
}

// Env returns the envelope; it makes every embedding type an Event.
func (e *Envelope) Env() *Envelope { return e }

// Event is one message read from the engine's stdout: *Assistant, *User, *StreamEvent,
// *Result, *SystemInit (and the other system subtypes), *ControlRequest,
// *ControlResponse, *ControlCancelRequest, the other top-level types, or *Unknown.
type Event interface {
	Env() *Envelope
}

// Unknown is a message whose type or subtype this version of mantle doesn't know, or
// whose shape it couldn't decode. It is never dropped: Raw holds the whole line.
type Unknown struct {
	Envelope
}

// MarshalJSON writes the original line back out.
func (u *Unknown) MarshalJSON() ([]byte, error) {
	if len(u.Raw) == 0 {
		return json.Marshal(struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype,omitempty"`
		}{u.Type, u.Subtype})
	}
	return u.Raw, nil
}

// ErrNotJSON is returned by Decode for lines that don't start with '{'. Callers should
// skip such lines (some builds print debug text to stdout).
var ErrNotJSON = errors.New("proto: not a JSON object line")

// Decode parses one stdout line into a typed Event.
//
// It fails only when the line is not a JSON object (ErrNotJSON, skip it) or is malformed
// JSON (a protocol error). Unknown types and subtypes become *Unknown. A known message
// whose fields have unexpected shapes is still returned, with Env().Mismatch set.
// The returned event keeps its own copy of line in Env().Raw.
func Decode(line []byte) (Event, error) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return nil, ErrNotJSON
	}
	var head struct {
		Type    any `json:"type"`
		Subtype any `json:"subtype"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return nil, fmt.Errorf("proto: malformed line: %w", err)
	}
	typ, _ := head.Type.(string)
	sub, _ := head.Subtype.(string)
	raw := bytes.Clone(line)

	ev := newEvent(typ, sub)
	if ev == nil {
		return &Unknown{Envelope{Type: typ, Subtype: sub, Raw: raw}}, nil
	}
	err := json.Unmarshal(raw, ev)
	env := ev.Env()
	env.Raw = raw
	if err != nil {
		var te *json.UnmarshalTypeError
		if !errors.As(err, &te) {
			return &Unknown{Envelope{Type: typ, Subtype: sub, Raw: raw, Mismatch: err}}, nil
		}
		env.Mismatch = err
	}
	if env.Type == "" {
		env.Type = typ
	}
	return ev, nil
}

// newEvent returns a zero value of the concrete type for (type, subtype), or nil.
func newEvent(typ, sub string) Event {
	switch typ {
	case TypeSystem:
		if f, ok := systemTypes[sub]; ok {
			return f()
		}
		return nil
	case TypeResult:
		return &Result{}
	}
	if f, ok := topTypes[typ]; ok {
		return f()
	}
	return nil
}

// Top-level message types on stdout.
const (
	TypeSystem               = "system"
	TypeAssistant            = "assistant"
	TypeUser                 = "user"
	TypeStreamEvent          = "stream_event"
	TypeResult               = "result"
	TypeToolProgress         = "tool_progress"
	TypeToolUseSummary       = "tool_use_summary"
	TypeAuthStatus           = "auth_status"
	TypeRateLimitEvent       = "rate_limit_event"
	TypePromptSuggestion     = "prompt_suggestion"
	TypeConversationReset    = "conversation_reset"
	TypeActiveGoal           = "active_goal"
	TypeKeepAlive            = "keep_alive"
	TypeCommandLifecycle     = "command_lifecycle"
	TypeControlRequest       = "control_request"
	TypeControlResponse      = "control_response"
	TypeControlCancelRequest = "control_cancel_request"
	TypeTranscriptMirror     = "transcript_mirror"

	// stdin only
	TypeUpdateEnvironmentVariables = "update_environment_variables"
)

var topTypes = map[string]func() Event{
	TypeAssistant:            func() Event { return &Assistant{} },
	TypeUser:                 func() Event { return &User{} },
	TypeStreamEvent:          func() Event { return &StreamEvent{} },
	TypeToolProgress:         func() Event { return &ToolProgress{} },
	TypeToolUseSummary:       func() Event { return &ToolUseSummary{} },
	TypeAuthStatus:           func() Event { return &AuthStatus{} },
	TypeRateLimitEvent:       func() Event { return &RateLimitEvent{} },
	TypePromptSuggestion:     func() Event { return &PromptSuggestion{} },
	TypeConversationReset:    func() Event { return &ConversationReset{} },
	TypeActiveGoal:           func() Event { return &ActiveGoal{} },
	TypeKeepAlive:            func() Event { return &KeepAlive{} },
	TypeCommandLifecycle:     func() Event { return &CommandLifecycle{} },
	TypeControlRequest:       func() Event { return &ControlRequest{} },
	TypeControlResponse:      func() Event { return &ControlResponse{} },
	TypeControlCancelRequest: func() Event { return &ControlCancelRequest{} },
}

// KnownTypes lists the top-level stdout types this package decodes (for drift checks).
func KnownTypes() []string {
	out := []string{TypeSystem, TypeResult}
	for t := range topTypes {
		out = append(out, t)
	}
	return sortStrings(out)
}

// KnownSystemSubtypes lists the system subtypes this package decodes (for drift checks).
func KnownSystemSubtypes() []string {
	out := make([]string, 0, len(systemTypes))
	for s := range systemTypes {
		out = append(out, s)
	}
	return sortStrings(out)
}
