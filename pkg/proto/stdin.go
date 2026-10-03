package proto

import (
	"encoding/json"
	"sort"
)

// UserInput is a user message written to the engine's stdin.
type UserInput struct {
	Type            string          `json:"type"` // "user"
	SessionID       string          `json:"session_id"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	UUID            string          `json:"uuid,omitempty"`
	Message         UserMessage     `json:"message"`
	Priority        string          `json:"priority,omitempty"` // now | next | later
	ShouldQuery     *bool           `json:"shouldQuery,omitempty"`
	ClientComposed  bool            `json:"client_composed,omitempty"`
	Origin          *Origin         `json:"origin,omitempty"`
	PastedContent   json.RawMessage `json:"pasted_content,omitempty"`
	InlinePastes    []string        `json:"inline_pastes,omitempty"`
	Timestamp       string          `json:"timestamp,omitempty"`
}

// Message priorities for input sent while a turn runs.
const (
	PriorityNow   = "now"   // end the current turn and deliver immediately
	PriorityNext  = "next"  // fold into the running turn between tool rounds
	PriorityLater = "later" // queue as its own turn
)

// OriginHuman marks input typed by a person.
const OriginHuman = "human"

// NewUserInput returns a user message with the given content. Plain text is sent as a
// string; anything else as blocks. uuid should be a fresh UUID (it correlates acks,
// results and cancel_async_message).
func NewUserInput(uuid string, blocks ...ContentBlock) UserInput {
	var c Content
	if len(blocks) == 1 && blocks[0].Type == BlockText && blocks[0].CacheControl == nil && blocks[0].Citations == nil {
		c = TextContent(blocks[0].Text)
	} else {
		c = BlockContent(blocks...)
	}
	return UserInput{
		Type:    TypeUser,
		UUID:    uuid,
		Message: UserMessage{Role: "user", Content: c},
	}
}

// MarshalLine encodes the message as one stdin line (without the newline).
func (u UserInput) MarshalLine() ([]byte, error) {
	if u.Type == "" {
		u.Type = TypeUser
	}
	if u.Message.Role == "" {
		u.Message.Role = "user"
	}
	return json.Marshal(u)
}

// KeepAliveLine is the keep_alive frame.
var KeepAliveLine = []byte(`{"type":"keep_alive"}`)

// UpdateEnvironmentVariables changes the engine's environment.
type UpdateEnvironmentVariables struct {
	Type      string            `json:"type"` // update_environment_variables
	Variables map[string]string `json:"variables"`
	RequestID string            `json:"request_id,omitempty"`
}

// MarshalLine encodes the frame as one stdin line.
func (u UpdateEnvironmentVariables) MarshalLine() ([]byte, error) {
	u.Type = TypeUpdateEnvironmentVariables
	return json.Marshal(u)
}

func sortStrings(s []string) []string {
	sort.Strings(s)
	return s
}
