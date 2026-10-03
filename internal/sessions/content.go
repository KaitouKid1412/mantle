package sessions

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Content is a message's content. A plain-string content decodes as one text block, so
// callers only deal with blocks; WasString remembers the original form.
type Content struct {
	Blocks    []Block
	WasString bool
}

// UnmarshalJSON accepts a string or an array of blocks. It never fails: anything else
// decodes as empty content, and a block field of the wrong type is dropped. (An error here
// would abort decoding of the whole record.)
func (c *Content) UnmarshalJSON(b []byte) error {
	*c = Content{}
	b = bytes.TrimSpace(b)
	switch {
	case len(b) > 0 && b[0] == '"':
		var s string
		_ = json.Unmarshal(b, &s)
		*c = Content{Blocks: []Block{{Type: "text", Text: s}}, WasString: true}
	case len(b) > 0 && b[0] == '[':
		_ = json.Unmarshal(b, &c.Blocks)
	}
	return nil
}

// MarshalJSON writes the original form back.
func (c Content) MarshalJSON() ([]byte, error) {
	if c.WasString && len(c.Blocks) == 1 && c.Blocks[0].Type == "text" {
		return json.Marshal(c.Blocks[0].Text)
	}
	if c.Blocks == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(c.Blocks)
}

// Block is one content block. Only the fields of the block's Type are set; unknown block
// types keep their Type and can be re-read from the record's Raw.
type Block struct {
	Type string `json:"type"`

	Text      string `json:"text,omitempty"`      // text
	Thinking  string `json:"thinking,omitempty"`  // thinking
	Signature string `json:"signature,omitempty"` // thinking
	Data      string `json:"data,omitempty"`      // redacted_thinking

	ID    string          `json:"id,omitempty"`    // tool_use, server_tool_use
	Name  string          `json:"name,omitempty"`  // tool_use, server_tool_use
	Input json.RawMessage `json:"input,omitempty"` // tool_use, server_tool_use

	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result
	Content   json.RawMessage `json:"content,omitempty"`     // tool_result: string or blocks
	IsError   bool            `json:"is_error,omitempty"`    // tool_result

	Source json.RawMessage `json:"source,omitempty"` // image, document
}

// ResultBlocks decodes a tool_result block's content (string or blocks) into blocks.
func (b Block) ResultBlocks() []Block {
	if len(b.Content) == 0 {
		return nil
	}
	var c Content
	_ = c.UnmarshalJSON(b.Content)
	return c.Blocks
}

// ResultText is the concatenated text of a tool_result block's content.
func (b Block) ResultText() string {
	var parts []string
	for _, rb := range b.ResultBlocks() {
		if rb.Type == "text" {
			parts = append(parts, rb.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// Text is the concatenated text blocks of c.
func (c Content) Text() string {
	var parts []string
	for _, b := range c.Blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// Has reports whether c has a block of the given type.
func (c Content) Has(blockType string) bool {
	for _, b := range c.Blocks {
		if b.Type == blockType {
			return true
		}
	}
	return false
}

// IsToolResult reports whether e is a user record carrying tool results (not a prompt).
func (e *Entry) IsToolResult() bool {
	return e.kind == KindUser && e.Message != nil && e.Message.Content.Has("tool_result")
}

// IsPrompt reports whether e is a prompt the user typed: a user record that is not meta,
// not a compact summary and not a tool result.
func (e *Entry) IsPrompt() bool {
	return e.kind == KindUser && !e.IsMeta && !e.IsCompactSummary && e.Message != nil &&
		!e.IsToolResult()
}

// MaxPromptPreview is the length (in characters) prompt previews are cut to.
const MaxPromptPreview = 200

var (
	commandNameRE = regexp.MustCompile(`<command-name>(.*?)</command-name>`)
	bashInputRE   = regexp.MustCompile(`(?s)<bash-input>(.*?)</bash-input>`)
	// Engine-generated user text: a leading XML-ish tag or an interruption marker.
	syntheticRE = regexp.MustCompile(`^(?:\s*<[a-z][\w-]*[\s>]|\[Request interrupted by user[^\]]*\])`)
)

// PromptPreview returns a one-line preview of a prompt entry for pickers and titles, and
// ok=false when e is not a prompt or its text is engine-generated. A slash command
// yields ok=false with command set to the command name, which callers may use as a
// fallback.
func PromptPreview(e *Entry) (preview, command string, ok bool) {
	if !e.IsPrompt() {
		return "", "", false
	}
	for _, b := range e.Message.Content.Blocks {
		if b.Type != "text" {
			continue
		}
		t := strings.TrimSpace(strings.ReplaceAll(b.Text, "\n", " "))
		if t == "" {
			continue
		}
		if m := commandNameRE.FindStringSubmatch(t); m != nil {
			if command == "" {
				command = m[1]
			}
			continue
		}
		if m := bashInputRE.FindStringSubmatch(t); m != nil {
			return "! " + strings.TrimSpace(m[1]), command, true
		}
		if syntheticRE.MatchString(t) {
			continue
		}
		return truncate(t, MaxPromptPreview), command, true
	}
	return "", command, false
}

// truncate cuts s to n characters, appending an ellipsis when it cut.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n])) + "…"
}
