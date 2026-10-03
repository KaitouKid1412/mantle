package proto

import "encoding/json"

// CanUseTool asks the client whether a tool call may run (permission prompts,
// AskUserQuestion, ExitPlanMode). Answer with exactly one PermissionResult.
type CanUseTool struct {
	ToolName                string             `json:"tool_name"`
	Input                   json.RawMessage    `json:"input"`
	ToolUseID               string             `json:"tool_use_id"`
	PermissionSuggestions   []PermissionUpdate `json:"permission_suggestions,omitempty"`
	BlockedPath             string             `json:"blocked_path,omitempty"`
	DecisionReason          string             `json:"decision_reason,omitempty"`
	DecisionReasonType      string             `json:"decision_reason_type,omitempty"`
	ClassifierApprovable    bool               `json:"classifier_approvable,omitempty"`
	SuppressAlwaysAllowRule bool               `json:"suppress_always_allow_rule,omitempty"`
	DefaultToNo             bool               `json:"default_to_no,omitempty"`
	MatchedAskRule          *MatchedAskRule    `json:"matched_ask_rule,omitempty"`
	Title                   string             `json:"title,omitempty"`
	DisplayName             string             `json:"display_name,omitempty"`
	Description             string             `json:"description,omitempty"`
	AgentID                 string             `json:"agent_id,omitempty"`
	MCPServer               *MCPServerRef      `json:"mcp_server,omitempty"`
	RequiresUserInteraction bool               `json:"requires_user_interaction,omitempty"`
}

// Tool names that arrive through can_use_tool but are dialogs, not permissions.
const (
	ToolAskUserQuestion = "AskUserQuestion"
	ToolExitPlanMode    = "ExitPlanMode"
)

// MatchedAskRule is the "ask" rule that triggered a prompt.
type MatchedAskRule struct {
	Source      string `json:"source"`
	ToolName    string `json:"tool_name"`
	RuleContent string `json:"rule_content,omitempty"`
}

// MCPServerRef names the MCP server behind a tool.
type MCPServerRef struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
}

// PermissionResult answers can_use_tool.
type PermissionResult struct {
	Behavior               string             `json:"behavior"` // allow | deny
	UpdatedInput           json.RawMessage    `json:"updatedInput,omitempty"`
	UpdatedPermissions     []PermissionUpdate `json:"updatedPermissions,omitempty"`
	Message                string             `json:"message,omitempty"`
	Interrupt              bool               `json:"interrupt,omitempty"`
	ToolUseID              string             `json:"toolUseID,omitempty"`
	DecisionClassification string             `json:"decisionClassification,omitempty"`
}

// Permission behaviours and decision classifications.
const (
	BehaviorAllow = "allow"
	BehaviorDeny  = "deny"
	BehaviorAsk   = "ask"

	DecisionUserTemporary = "user_temporary"
	DecisionUserPermanent = "user_permanent"
	DecisionUserReject    = "user_reject"
)

// Allow allows the call once. Pass updatedInput to change the input (for example
// AskUserQuestion answers), or nil to keep it.
func (c *CanUseTool) Allow(updatedInput json.RawMessage) PermissionResult {
	return PermissionResult{Behavior: BehaviorAllow, UpdatedInput: updatedInput, ToolUseID: c.ToolUseID,
		DecisionClassification: DecisionUserTemporary}
}

// AllowAlways allows the call and applies permission updates (usually one of
// PermissionSuggestions).
func (c *CanUseTool) AllowAlways(updates ...PermissionUpdate) PermissionResult {
	return PermissionResult{Behavior: BehaviorAllow, UpdatedPermissions: updates, ToolUseID: c.ToolUseID,
		DecisionClassification: DecisionUserPermanent}
}

// Deny denies the call; interrupt also stops the turn.
func (c *CanUseTool) Deny(message string, interrupt bool) PermissionResult {
	return PermissionResult{Behavior: BehaviorDeny, Message: message, Interrupt: interrupt, ToolUseID: c.ToolUseID,
		DecisionClassification: DecisionUserReject}
}

// PermissionUpdate changes permission rules, mode or directories.
type PermissionUpdate struct {
	Type        string           `json:"type"` // addRules | replaceRules | removeRules | setMode | addDirectories | removeDirectories
	Rules       []PermissionRule `json:"rules,omitempty"`
	Behavior    string           `json:"behavior,omitempty"` // allow | deny | ask
	Mode        string           `json:"mode,omitempty"`
	Directories []string         `json:"directories,omitempty"`
	Destination string           `json:"destination,omitempty"` // userSettings | projectSettings | localSettings | session | cliArg
}

// PermissionUpdate types and destinations.
const (
	UpdateAddRules          = "addRules"
	UpdateReplaceRules      = "replaceRules"
	UpdateRemoveRules       = "removeRules"
	UpdateSetMode           = "setMode"
	UpdateAddDirectories    = "addDirectories"
	UpdateRemoveDirectories = "removeDirectories"

	DestUserSettings    = "userSettings"
	DestProjectSettings = "projectSettings"
	DestLocalSettings   = "localSettings"
	DestSession         = "session"
	DestCLIArg          = "cliArg"
)

// PermissionRule is a tool rule such as Bash(npm test:*).
type PermissionRule struct {
	ToolName    string `json:"toolName"`
	RuleContent string `json:"ruleContent,omitempty"`
}

// HookCallback runs a hook callback the client registered in initialize.
// Answer with a HookOutput (an empty one means "no opinion").
type HookCallback struct {
	CallbackID string          `json:"callback_id"`
	Input      json.RawMessage `json:"input"`
	ToolUseID  string          `json:"tool_use_id,omitempty"`
}

// HookInput returns the fields every hook input has.
func (h *HookCallback) HookInput() HookInput {
	var in HookInput
	_ = json.Unmarshal(h.Input, &in)
	return in
}

// HookInput is the common part of a hook's input.
type HookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	PermissionMode string `json:"permission_mode,omitempty"`
}

// HookOutput answers a hook callback.
type HookOutput struct {
	Continue           *bool           `json:"continue,omitempty"`
	SuppressOutput     bool            `json:"suppressOutput,omitempty"`
	StopReason         string          `json:"stopReason,omitempty"`
	Decision           string          `json:"decision,omitempty"` // approve | block
	SystemMessage      string          `json:"systemMessage,omitempty"`
	Reason             string          `json:"reason,omitempty"`
	HookSpecificOutput json.RawMessage `json:"hookSpecificOutput,omitempty"`
	Async              bool            `json:"async,omitempty"`
	AsyncTimeout       int             `json:"asyncTimeout,omitempty"`
}

// MCPMessage forwards a JSON-RPC message to an in-process (SDK) MCP server.
// Answer {"mcp_response": <JSON-RPC response>}.
type MCPMessage struct {
	ServerName string          `json:"server_name"`
	Message    json.RawMessage `json:"message"`
}

// Elicitation asks the user for input on behalf of an MCP server.
type Elicitation struct {
	MCPServerName   string          `json:"mcp_server_name"`
	Message         string          `json:"message"`
	Mode            string          `json:"mode,omitempty"` // form | url
	URL             string          `json:"url,omitempty"`
	ElicitationID   string          `json:"elicitation_id,omitempty"`
	RequestedSchema json.RawMessage `json:"requested_schema,omitempty"`
	Title           string          `json:"title,omitempty"`
}

// ElicitationResult answers an elicitation.
type ElicitationResult struct {
	Action  string          `json:"action"` // accept | decline | cancel
	Content json.RawMessage `json:"content,omitempty"`
}

// RequestUserDialog asks the client to show a dialog of a kind it declared in
// initialize.supportedDialogKinds. Shapes vary by kind; read the request's Raw.
type RequestUserDialog struct {
	Kind string `json:"kind,omitempty"`
}
