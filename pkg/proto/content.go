package proto

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Content block types.
const (
	BlockText             = "text"
	BlockThinking         = "thinking"
	BlockRedactedThinking = "redacted_thinking"
	BlockToolUse          = "tool_use"
	BlockServerToolUse    = "server_tool_use"
	BlockMCPToolUse       = "mcp_tool_use"
	BlockToolResult       = "tool_result"
	BlockImage            = "image"
	BlockDocument         = "document"
)

// ContentBlock is one block of message content: a flat union of the Anthropic content
// block types. Type says which fields are meaningful. Blocks of types this package
// doesn't know keep Raw and are written back unchanged.
type ContentBlock struct {
	Type string `json:"type"`

	// text
	Text      string          `json:"text,omitempty"`
	Citations json.RawMessage `json:"citations,omitempty"`

	// thinking; redacted_thinking uses Data
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`

	// tool_use, server_tool_use, mcp_tool_use
	ID         string          `json:"id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	ServerName string          `json:"server_name,omitempty"`

	// tool_result (and the server *_tool_result blocks)
	ToolUseID string   `json:"tool_use_id,omitempty"`
	Content   *Content `json:"content,omitempty"`
	IsError   bool     `json:"is_error,omitempty"`

	// image, document
	Source  *Source `json:"source,omitempty"`
	Title   string  `json:"title,omitempty"`
	Context string  `json:"context,omitempty"`

	CacheControl json.RawMessage `json:"cache_control,omitempty"`

	// Raw is the block as received, kept only for unknown types (written back as is).
	Raw json.RawMessage `json:"-"`
}

// Source is the source of an image or document block.
type Source struct {
	Type      string          `json:"type"` // base64 | url | text | content | file
	MediaType string          `json:"media_type,omitempty"`
	Data      string          `json:"data,omitempty"`
	URL       string          `json:"url,omitempty"`
	FileID    string          `json:"file_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

// Text returns a text content block.
func Text(text string) ContentBlock { return ContentBlock{Type: BlockText, Text: text} }

// Image returns a base64 image content block.
func Image(mediaType, base64Data string) ContentBlock {
	return ContentBlock{Type: BlockImage, Source: &Source{Type: "base64", MediaType: mediaType, Data: base64Data}}
}

// Document returns a base64 document content block (for example a PDF).
func Document(mediaType, base64Data, title string) ContentBlock {
	return ContentBlock{Type: BlockDocument, Title: title, Source: &Source{Type: "base64", MediaType: mediaType, Data: base64Data}}
}

// IsToolUse reports whether b is a tool call (client, server or MCP).
func (b ContentBlock) IsToolUse() bool {
	return b.Type == BlockToolUse || b.Type == BlockServerToolUse || b.Type == BlockMCPToolUse
}

// ToolUse returns the tool call in b; ok is false if b isn't one.
func (b ContentBlock) ToolUse() (t ToolUse, ok bool) {
	if !b.IsToolUse() {
		return ToolUse{}, false
	}
	return ToolUse{Type: b.Type, ID: b.ID, Name: b.Name, Input: b.Input, ServerName: b.ServerName}, true
}

// ToolResult returns the tool result in b; ok is false if b isn't a tool_result.
func (b ContentBlock) ToolResult() (r ToolResult, ok bool) {
	if b.Type != BlockToolResult {
		return ToolResult{}, false
	}
	r = ToolResult{ToolUseID: b.ToolUseID, IsError: b.IsError}
	if b.Content != nil {
		r.Content = *b.Content
	}
	return r, true
}

// UnmarshalJSON decodes any block shape. Blocks of unknown types keep Raw (and never
// fail); known types don't, so big blocks aren't held twice.
func (b *ContentBlock) UnmarshalJSON(data []byte) error {
	type plain ContentBlock
	var p plain
	err := json.Unmarshal(data, &p)
	*b = ContentBlock(p)
	if !knownBlock(b.Type) {
		b.Raw = bytes.Clone(data)
		return nil
	}
	return err
}

func knownBlock(t string) bool {
	switch t {
	case BlockText, BlockThinking, BlockRedactedThinking, BlockToolUse, BlockServerToolUse,
		BlockMCPToolUse, BlockToolResult, BlockImage, BlockDocument:
		return true
	}
	return false
}

// MarshalJSON writes the fields each known block type requires. Unknown block types
// are written back from Raw.
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	type plain ContentBlock
	switch b.Type {
	case BlockText:
		return json.Marshal(struct {
			Type         string          `json:"type"`
			Text         string          `json:"text"`
			Citations    json.RawMessage `json:"citations,omitempty"`
			CacheControl json.RawMessage `json:"cache_control,omitempty"`
		}{b.Type, b.Text, b.Citations, b.CacheControl})
	case BlockThinking:
		return json.Marshal(struct {
			Type      string `json:"type"`
			Thinking  string `json:"thinking"`
			Signature string `json:"signature"`
		}{b.Type, b.Thinking, b.Signature})
	case BlockRedactedThinking:
		return json.Marshal(struct {
			Type string `json:"type"`
			Data string `json:"data"`
		}{b.Type, b.Data})
	case BlockToolUse, BlockServerToolUse, BlockMCPToolUse:
		input := b.Input
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		return json.Marshal(struct {
			Type         string          `json:"type"`
			ID           string          `json:"id"`
			Name         string          `json:"name"`
			Input        json.RawMessage `json:"input"`
			ServerName   string          `json:"server_name,omitempty"`
			CacheControl json.RawMessage `json:"cache_control,omitempty"`
		}{b.Type, b.ID, b.Name, input, b.ServerName, b.CacheControl})
	case BlockToolResult:
		return json.Marshal(struct {
			Type         string          `json:"type"`
			ToolUseID    string          `json:"tool_use_id"`
			Content      *Content        `json:"content,omitempty"`
			IsError      bool            `json:"is_error,omitempty"`
			CacheControl json.RawMessage `json:"cache_control,omitempty"`
		}{b.Type, b.ToolUseID, b.Content, b.IsError, b.CacheControl})
	case BlockImage, BlockDocument:
		return json.Marshal(plain(b))
	}
	if len(b.Raw) > 0 {
		return b.Raw, nil
	}
	return json.Marshal(plain(b))
}

// Content is message or tool-result content: either a plain string or a list of
// blocks. When Blocks is nil the content is the string Text.
type Content struct {
	Text   string
	Blocks []ContentBlock
	// Raw holds content of any other JSON shape (written back unchanged).
	Raw json.RawMessage
}

// TextContent returns string content.
func TextContent(s string) Content { return Content{Text: s} }

// BlockContent returns block content.
func BlockContent(blocks ...ContentBlock) Content {
	if blocks == nil {
		blocks = []ContentBlock{}
	}
	return Content{Blocks: blocks}
}

// IsString reports whether the content is a plain string.
func (c Content) IsString() bool { return c.Blocks == nil && len(c.Raw) == 0 }

// PlainText returns the string content, or the text blocks joined by newlines.
func (c Content) PlainText() string {
	if c.Blocks == nil {
		return c.Text
	}
	var parts []string
	for _, b := range c.Blocks {
		if b.Type == BlockText {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// MarshalJSON writes a string, a block array, or the original raw value.
func (c Content) MarshalJSON() ([]byte, error) {
	switch {
	case len(c.Raw) > 0:
		return c.Raw, nil
	case c.Blocks != nil:
		return json.Marshal(c.Blocks)
	default:
		return json.Marshal(c.Text)
	}
}

// UnmarshalJSON accepts a string, a block array, or (kept raw) anything else.
func (c *Content) UnmarshalJSON(data []byte) error {
	*c = Content{}
	d := bytes.TrimSpace(data)
	if len(d) == 0 || bytes.Equal(d, []byte("null")) {
		return nil
	}
	switch d[0] {
	case '"':
		return json.Unmarshal(d, &c.Text)
	case '[':
		var blocks []ContentBlock
		if err := json.Unmarshal(d, &blocks); err == nil {
			if blocks == nil {
				blocks = []ContentBlock{}
			}
			c.Blocks = blocks
			return nil
		}
	}
	c.Raw = bytes.Clone(d)
	return nil
}

// ToolUse is a tool call from an assistant message (tool_use, server_tool_use or
// mcp_tool_use block).
type ToolUse struct {
	Type       string          `json:"type"`
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
	ServerName string          `json:"server_name,omitempty"`
}

// DecodeInput unmarshals the tool input into v (for example a map or a tool-specific
// struct).
func (t ToolUse) DecodeInput(v any) error {
	if len(t.Input) == 0 {
		return nil
	}
	return json.Unmarshal(t.Input, v)
}

// ToolResult is the result of a tool call: a tool_result block, plus the structured
// tool_use_result the engine attaches to the carrying user message.
type ToolResult struct {
	ToolUseID string  `json:"tool_use_id"`
	Content   Content `json:"content"`
	IsError   bool    `json:"is_error,omitempty"`

	// Structured is the user message's tool_use_result (tool-specific shape; see the
	// typed helpers on User). Not part of the block's JSON.
	Structured json.RawMessage `json:"-"`
	// ParentToolUseID is the carrying message's parent_tool_use_id (subagent nesting).
	ParentToolUseID string `json:"-"`
}
