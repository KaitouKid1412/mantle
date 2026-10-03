// Package dialogs holds the view-models of plan 05's dialogs: permission prompts,
// AskUserQuestion, plan approval, MCP elicitation and the startup gates (trust,
// .mcp.json approval, bypass warning, API key, auto mode).
//
// Each view-model is pure state plus key handling: keys go in through HandleKey (or a
// Claude Code `confirm:*` action through Action), and when the dialog is answered its
// Response is the exact JSON the engine expects. Side effects a view-model can't perform
// itself (switching the permission mode, opening $EDITOR or a browser) are returned as an
// Effect for the host to carry out. Rendering takes a width and a Styles value, so the
// same models serve inline dialogs, stories and goldens.
//
// Wire types here mirror the protocol's JSON (docs/research/protocol-2.1.288.md §6) so
// the package doesn't depend on pkg/proto; Part B adapts proto values to them.
//
// Parity: PD-01..PD-14, PD-21..PD-28, PD-30..PD-35, PD-41, PD-45.
package dialogs

import (
	"bytes"
	"encoding/json"
)

// Text is a string field that also accepts non-string JSON (kept as compact JSON text),
// so an engine that changes a field's type doesn't break decoding.
type Text string

// UnmarshalJSON implements json.Unmarshaler.
func (t *Text) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = Text(s)
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		*t = ""
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return err
	}
	*t = Text(buf.String())
	return nil
}

// ToolRequest is the body of a `can_use_tool` control request.
type ToolRequest struct {
	ToolName                string             `json:"tool_name"`
	Input                   json.RawMessage    `json:"input"`
	ToolUseID               string             `json:"tool_use_id"`
	PermissionSuggestions   []PermissionUpdate `json:"permission_suggestions,omitempty"`
	BlockedPath             string             `json:"blocked_path,omitempty"`
	DecisionReason          Text               `json:"decision_reason,omitempty"`
	DecisionReasonType      string             `json:"decision_reason_type,omitempty"`
	ClassifierApprovable    bool               `json:"classifier_approvable,omitempty"`
	SuppressAlwaysAllowRule bool               `json:"suppress_always_allow_rule,omitempty"`
	DefaultToNo             bool               `json:"default_to_no,omitempty"`
	MatchedAskRule          *MatchedRule       `json:"matched_ask_rule,omitempty"`
	Title                   string             `json:"title,omitempty"`
	DisplayName             string             `json:"display_name,omitempty"`
	Description             string             `json:"description,omitempty"`
	AgentID                 string             `json:"agent_id,omitempty"`
	McpServer               *McpServerRef      `json:"mcp_server,omitempty"`
	RequiresUserInteraction bool               `json:"requires_user_interaction,omitempty"`
}

// MatchedRule is the ask rule that caused a prompt.
type MatchedRule struct {
	Source      string `json:"source"`
	ToolName    string `json:"tool_name"`
	RuleContent string `json:"rule_content,omitempty"`
}

// McpServerRef names the MCP server that owns a tool.
type McpServerRef struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
}

// PermissionUpdate is one entry of permission_suggestions / updatedPermissions. The
// engine's own JSON is kept in Raw and sent back byte for byte, so suggestions survive
// fields mantle doesn't know.
type PermissionUpdate struct {
	Type        string           `json:"type"` // addRules|replaceRules|removeRules|setMode|addDirectories|removeDirectories
	Rules       []PermissionRule `json:"rules,omitempty"`
	Behavior    string           `json:"behavior,omitempty"` // allow|deny|ask
	Mode        string           `json:"mode,omitempty"`
	Directories []string         `json:"directories,omitempty"`
	Destination string           `json:"destination,omitempty"` // userSettings|projectSettings|localSettings|session|cliArg
	Raw         json.RawMessage  `json:"-"`
}

// PermissionRule is {toolName, ruleContent?}.
type PermissionRule struct {
	ToolName    string `json:"toolName"`
	RuleContent string `json:"ruleContent,omitempty"`
}

// UnmarshalJSON keeps the raw bytes alongside the parsed fields.
func (u *PermissionUpdate) UnmarshalJSON(b []byte) error {
	type plain PermissionUpdate
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*u = PermissionUpdate(p)
	u.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// MarshalJSON sends the engine's original JSON when there is one.
func (u PermissionUpdate) MarshalJSON() ([]byte, error) {
	if len(u.Raw) > 0 {
		return u.Raw, nil
	}
	type plain PermissionUpdate
	return json.Marshal(plain(u))
}

// SetModeUpdate builds a client-side setMode update (plan approval, "switch to auto").
func SetModeUpdate(mode, destination string) PermissionUpdate {
	return PermissionUpdate{Type: "setMode", Mode: mode, Destination: destination}
}

// Decision classifications reported with a permission result.
const (
	ClassUserTemporary = "user_temporary"
	ClassUserPermanent = "user_permanent"
	ClassUserReject    = "user_reject"
)

// PermissionResult is the response to `can_use_tool`.
type PermissionResult struct {
	Behavior               string // "allow" | "deny"
	UpdatedInput           json.RawMessage
	UpdatedPermissions     []PermissionUpdate
	Message                string
	Interrupt              bool
	ToolUseID              string
	DecisionClassification string
}

// Allowed reports whether the result allows the tool call.
func (r PermissionResult) Allowed() bool { return r.Behavior == "allow" }

// MarshalJSON writes the exact shape the engine validates: allow carries updatedInput
// and updatedPermissions, deny always carries message.
func (r PermissionResult) MarshalJSON() ([]byte, error) {
	if r.Behavior == "deny" {
		return json.Marshal(struct {
			Behavior               string `json:"behavior"`
			Message                string `json:"message"`
			Interrupt              bool   `json:"interrupt,omitempty"`
			ToolUseID              string `json:"toolUseID,omitempty"`
			DecisionClassification string `json:"decisionClassification,omitempty"`
		}{"deny", r.Message, r.Interrupt, r.ToolUseID, r.DecisionClassification})
	}
	return json.Marshal(struct {
		Behavior               string             `json:"behavior"`
		UpdatedInput           json.RawMessage    `json:"updatedInput,omitempty"`
		UpdatedPermissions     []PermissionUpdate `json:"updatedPermissions,omitempty"`
		ToolUseID              string             `json:"toolUseID,omitempty"`
		DecisionClassification string             `json:"decisionClassification,omitempty"`
	}{"allow", r.UpdatedInput, r.UpdatedPermissions, r.ToolUseID, r.DecisionClassification})
}

// ElicitationRequest is the body of an `elicitation` control request.
type ElicitationRequest struct {
	McpServerName   string          `json:"mcp_server_name"`
	Message         string          `json:"message"`
	Mode            string          `json:"mode,omitempty"` // "form" (default) | "url"
	URL             string          `json:"url,omitempty"`
	ElicitationID   string          `json:"elicitation_id,omitempty"`
	RequestedSchema json.RawMessage `json:"requested_schema,omitempty"`
	Title           string          `json:"title,omitempty"`
}

// ElicitationResult is the response to `elicitation`.
type ElicitationResult struct {
	Action  string         `json:"action"` // accept | decline | cancel
	Content map[string]any `json:"content,omitempty"`
}
