package fakeapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// A Script is the ordered list of replies for the main conversation, plus the replies
// used for side calls and for requests that arrive after the turns run out.
//
// JSON form (a bare array of turns is accepted too):
//
//	{
//	  "turns": [
//	    {"match": {"contains": "hello"},
//	     "reply": [{"type": "text", "text": "Hello from fakeapi."}]},
//	    {"reply": [{"type": "thinking", "thinking": "Need a file."},
//	               {"type": "tool_use", "name": "Bash",
//	                "input": {"command": "echo hi > out.txt"}}]},
//	    {"match": {"tool_result": "Bash"},
//	     "reply": [{"type": "text", "text": "Done."}]}
//	  ],
//	  "side_reply": "OK",
//	  "default_reply": "OK"
//	}
type Script struct {
	Turns []Turn `json:"turns"`
	// SideReply answers side calls (titles, helper calls, suggestions). Default "OK".
	SideReply string `json:"side_reply,omitempty"`
	// DefaultReply answers main requests after the turns run out. Default "OK".
	DefaultReply string `json:"default_reply,omitempty"`
}

// A Turn answers one main-conversation request. Turns are consumed in order; a turn whose
// Match fails is not consumed (the request gets an "unmatched" text reply instead, and the
// miss is recorded, see Server.Unmatched).
type Turn struct {
	Match *Match  `json:"match,omitempty"`
	Reply []Block `json:"reply,omitempty"`
	// StopReason overrides the computed stop reason (tool_use when Reply has a tool_use,
	// else end_turn), e.g. "max_tokens" or "refusal".
	StopReason string `json:"stop_reason,omitempty"`
	// Error, when set, makes the turn answer with an HTTP error instead of a message.
	Error *APIError `json:"error,omitempty"`
	// Headers are extra response headers (e.g. anthropic-ratelimit-unified-*).
	Headers map[string]string `json:"headers,omitempty"`
	// ChunkDelayMS sleeps between streamed events of this turn (for interrupt tests).
	ChunkDelayMS int `json:"chunk_delay_ms,omitempty"`
	// DelayMS waits before answering at all, like a slow API (so a test can see the
	// screen between the prompt and the reply).
	DelayMS int `json:"delay_ms,omitempty"`
}

// Match tests the latest user message of a request. All set fields must hold.
type Match struct {
	// Contains is a substring of the message text (text blocks and tool_result text).
	Contains string `json:"contains,omitempty"`
	// Regex is a Go regular expression matched against the same text.
	Regex string `json:"regex,omitempty"`
	// ToolResult is a tool name: the message must carry a tool_result answering a
	// tool_use of that name from an earlier assistant message.
	ToolResult string `json:"tool_result,omitempty"`

	re *regexp.Regexp
}

// A Block is one content block of a reply.
type Block struct {
	Type string `json:"type"` // text | thinking | tool_use

	Text string `json:"text,omitempty"` // text

	Thinking  string `json:"thinking,omitempty"`  // thinking
	Signature string `json:"signature,omitempty"` // thinking; generated when empty

	ID    string          `json:"id,omitempty"`    // tool_use; toolu_fake_N when empty
	Name  string          `json:"name,omitempty"`  // tool_use
	Input json.RawMessage `json:"input,omitempty"` // tool_use; {} when empty
}

// APIError is an error answer: HTTP Status with an Anthropic error body.
type APIError struct {
	Status  int    `json:"status"`            // e.g. 529, 429, 500
	Type    string `json:"type,omitempty"`    // e.g. overloaded_error
	Message string `json:"message,omitempty"` // human text
}

// Parse decodes and validates a script.
func Parse(data []byte) (*Script, error) {
	data = bytes.TrimSpace(data)
	var s Script
	if len(data) > 0 && data[0] == '[' {
		if err := json.Unmarshal(data, &s.Turns); err != nil {
			return nil, fmt.Errorf("fakeapi: parse script: %w", err)
		}
	} else {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&s); err != nil {
			return nil, fmt.Errorf("fakeapi: parse script: %w", err)
		}
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// ParseFile reads and parses a script file.
func ParseFile(path string) (*Script, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fakeapi: %w", err)
	}
	return Parse(data)
}

// Validate checks block types and compiles regular expressions. New calls it too.
func (s *Script) Validate() error {
	for i := range s.Turns {
		t := &s.Turns[i]
		if t.Match != nil && t.Match.Regex != "" && t.Match.re == nil {
			re, err := regexp.Compile(t.Match.Regex)
			if err != nil {
				return fmt.Errorf("fakeapi: turn %d: bad regex: %w", i, err)
			}
			t.Match.re = re
		}
		if t.Error != nil {
			if t.Error.Status < 400 || t.Error.Status > 599 {
				return fmt.Errorf("fakeapi: turn %d: error status %d is not 4xx/5xx", i, t.Error.Status)
			}
			continue
		}
		if len(t.Reply) == 0 {
			return fmt.Errorf("fakeapi: turn %d: empty reply", i)
		}
		for j, b := range t.Reply {
			switch b.Type {
			case "text", "thinking":
			case "tool_use":
				if b.Name == "" {
					return fmt.Errorf("fakeapi: turn %d block %d: tool_use needs a name", i, j)
				}
				if len(b.Input) > 0 && !json.Valid(b.Input) {
					return fmt.Errorf("fakeapi: turn %d block %d: invalid input JSON", i, j)
				}
			default:
				return fmt.Errorf("fakeapi: turn %d block %d: unknown block type %q", i, j, b.Type)
			}
		}
	}
	return nil
}

// Matches reports whether the latest user message satisfies m. A nil Match matches.
func (m *Match) Matches(u UserMessage) bool {
	if m == nil {
		return true
	}
	if m.Contains != "" && !strings.Contains(u.Text, m.Contains) {
		return false
	}
	if m.Regex != "" {
		re := m.re
		if re == nil {
			var err error
			if re, err = regexp.Compile(m.Regex); err != nil {
				return false
			}
		}
		if !re.MatchString(u.Text) {
			return false
		}
	}
	if m.ToolResult != "" {
		found := false
		for _, name := range u.ToolResults {
			if name == m.ToolResult {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// String describes the match for logs.
func (m *Match) String() string {
	if m == nil {
		return "anything"
	}
	var parts []string
	if m.Contains != "" {
		parts = append(parts, fmt.Sprintf("contains %q", m.Contains))
	}
	if m.Regex != "" {
		parts = append(parts, fmt.Sprintf("regex %q", m.Regex))
	}
	if m.ToolResult != "" {
		parts = append(parts, fmt.Sprintf("tool_result for %s", m.ToolResult))
	}
	if len(parts) == 0 {
		return "anything"
	}
	return strings.Join(parts, " and ")
}

// stopReason is the turn's stop reason: the override, else tool_use or end_turn.
func (t *Turn) stopReason() string {
	if t.StopReason != "" {
		return t.StopReason
	}
	for _, b := range t.Reply {
		if b.Type == "tool_use" {
			return "tool_use"
		}
	}
	return "end_turn"
}

// TextTurn is a turn that answers with one text block.
func TextTurn(text string) Turn {
	return Turn{Reply: []Block{{Type: "text", Text: text}}}
}

// ToolTurn is a turn that answers with an optional text block and one tool_use.
func ToolTurn(text, name string, input any) Turn {
	raw, err := json.Marshal(input)
	if err != nil {
		panic(fmt.Sprintf("fakeapi: ToolTurn input: %v", err))
	}
	var t Turn
	if text != "" {
		t.Reply = append(t.Reply, Block{Type: "text", Text: text})
	}
	t.Reply = append(t.Reply, Block{Type: "tool_use", Name: name, Input: raw})
	return t
}
