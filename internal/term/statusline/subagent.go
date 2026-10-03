package statusline

import (
	"bytes"
	"encoding/json"
	"strings"
)

// SubagentPayload is the stdin of a subagentStatusLine command: one object per refresh
// carrying every visible subagent row (not one run per row).
type SubagentPayload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	PermissionMode string `json:"permission_mode,omitempty"`
	PromptID       string `json:"prompt_id,omitempty"`
	// Columns is the usable row width.
	Columns int            `json:"columns"`
	Tasks   []SubagentTask `json:"tasks"` // [] when none, never null
}

// SubagentTask is one row of the subagent panel.
type SubagentTask struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Description string `json:"description"`
	Label       string `json:"label"`
	StartTime   int64  `json:"startTime"` // Unix milliseconds
	Model       string `json:"model,omitempty"`
	// Effort is a level string or a numeric token budget, as configured; absent when
	// the subagent inherits the session's effort.
	Effort            any    `json:"effort,omitempty"`
	ContextWindowSize int    `json:"contextWindowSize,omitempty"`
	TokenCount        int    `json:"tokenCount"`
	TokenSamples      []int  `json:"tokenSamples"`
	Cwd               string `json:"cwd"`
}

// Marshal encodes the payload, normalising nil slices to [].
func (p SubagentPayload) Marshal() ([]byte, error) {
	if p.Tasks == nil {
		p.Tasks = []SubagentTask{}
	}
	for i := range p.Tasks {
		if p.Tasks[i].TokenSamples == nil {
			p.Tasks[i].TokenSamples = []int{}
		}
	}
	return json.Marshal(p)
}

// RowOverride is one line of subagentStatusLine output.
type RowOverride struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// ParseRows reads subagentStatusLine output: one JSON object per line,
// {"id": "<task id>", "content": "<row body>"}. Rows missing from the result keep the
// default rendering; an empty content hides the row. Content is sanitized like status
// line output. Lines that are not valid objects with an id are ignored; a later line
// for the same id wins.
func ParseRows(out []byte) map[string]string {
	rows := map[string]string{}
	for _, line := range bytes.Split(out, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var o struct {
			ID      *string `json:"id"`
			Content *string `json:"content"`
		}
		if err := json.Unmarshal(line, &o); err != nil || o.ID == nil || *o.ID == "" || o.Content == nil {
			continue
		}
		rows[*o.ID] = Sanitize(firstLine(*o.Content))
	}
	return rows
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
