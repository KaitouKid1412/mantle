package fakeapi

import (
	"encoding/json"
	"strings"
)

// MessagesRequest is the part of a /v1/messages body that fakeapi looks at.
type MessagesRequest struct {
	Model     string            `json:"model"`
	MaxTokens int               `json:"max_tokens"`
	Stream    bool              `json:"stream"`
	System    json.RawMessage   `json:"system,omitempty"` // string or text blocks
	Messages  []RequestMessage  `json:"messages"`
	Tools     []json.RawMessage `json:"tools,omitempty"`
	Thinking  json.RawMessage   `json:"thinking,omitempty"`
	Metadata  json.RawMessage   `json:"metadata,omitempty"`
}

// RequestMessage is one conversation message of a request.
type RequestMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or blocks
}

type reqBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"` // tool_result: string or blocks
}

// UserMessage summarises the latest user message of a request.
type UserMessage struct {
	// Text joins the text blocks and the text of tool_result blocks, one per line.
	Text string
	// ToolResults names the tool of each tool_result block (by its tool_use_id, looked up
	// in earlier assistant messages; "" when not found).
	ToolResults []string
}

// SystemText joins the system prompt (string or text blocks).
func (r *MessagesRequest) SystemText() string {
	return joinText(r.System)
}

// LatestUser summarises the last message with role "user".
func (r *MessagesRequest) LatestUser() UserMessage {
	names := map[string]string{} // tool_use id → name
	var last *RequestMessage
	for i := range r.Messages {
		m := &r.Messages[i]
		if m.Role == "assistant" {
			for _, b := range blocks(m.Content) {
				if b.Type == "tool_use" || b.Type == "server_tool_use" {
					names[b.ID] = b.Name
				}
			}
		}
		if m.Role == "user" {
			last = m
		}
	}
	var u UserMessage
	if last == nil {
		return u
	}
	var texts []string
	if s, ok := asString(last.Content); ok {
		texts = append(texts, s)
	}
	for _, b := range blocks(last.Content) {
		switch b.Type {
		case "text":
			texts = append(texts, b.Text)
		case "tool_result":
			u.ToolResults = append(u.ToolResults, names[b.ToolUseID])
			if t := joinText(b.Content); t != "" {
				texts = append(texts, t)
			}
		}
	}
	u.Text = strings.Join(texts, "\n")
	return u
}

func asString(raw json.RawMessage) (string, bool) {
	var s string
	if len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	return "", false
}

func blocks(raw json.RawMessage) []reqBlock {
	var bs []reqBlock
	if len(raw) > 0 && raw[0] == '[' {
		_ = json.Unmarshal(raw, &bs)
	}
	return bs
}

// joinText returns a string value, or the text blocks of a block array joined by newlines.
func joinText(raw json.RawMessage) string {
	if s, ok := asString(raw); ok {
		return s
	}
	var texts []string
	for _, b := range blocks(raw) {
		if b.Type == "text" {
			texts = append(texts, b.Text)
		}
	}
	return strings.Join(texts, "\n")
}
