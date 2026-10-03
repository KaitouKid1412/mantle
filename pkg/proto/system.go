package proto

import "encoding/json"

// System message subtypes.
const (
	SysInit                   = "init"
	SysCompactBoundary        = "compact_boundary"
	SysStatus                 = "status"
	SysAPIRetry               = "api_retry"
	SysInformational          = "informational"
	SysLocalCommandOutput     = "local_command_output"
	SysHookStarted            = "hook_started"
	SysHookProgress           = "hook_progress"
	SysHookResponse           = "hook_response"
	SysTaskStarted            = "task_started"
	SysTaskProgress           = "task_progress"
	SysTaskUpdated            = "task_updated"
	SysTaskNotification       = "task_notification"
	SysBackgroundTasksChanged = "background_tasks_changed"
	SysSessionStateChanged    = "session_state_changed"
	SysCommandsChanged        = "commands_changed"
	SysNotification           = "notification"
	SysThinkingTokens         = "thinking_tokens"
	SysPermissionDenied       = "permission_denied"
	SysModelRefusalFallback   = "model_refusal_fallback"
	SysModelRefusalNoFallback = "model_refusal_no_fallback"
	SysMemoryRecall           = "memory_recall"
	SysFilesPersisted         = "files_persisted"
	SysElicitationComplete    = "elicitation_complete"
	SysPluginInstall          = "plugin_install"
	SysControlRequestProgress = "control_request_progress"
	SysWorkerShuttingDown     = "worker_shutting_down"

	// Internal or optional subtypes (may not be emitted on plain stdio).
	SysSessionTitleChanged = "session_title_changed"
	SysTurnDuration        = "turn_duration"
	SysAPIError            = "api_error"
	SysModelFallback       = "model_fallback"
	SysAwaySummary         = "away_summary"
)

var systemTypes = map[string]func() Event{
	SysInit:                   func() Event { return &SystemInit{} },
	SysCompactBoundary:        func() Event { return &CompactBoundary{} },
	SysStatus:                 func() Event { return &Status{} },
	SysAPIRetry:               func() Event { return &APIRetry{} },
	SysInformational:          func() Event { return &Informational{} },
	SysLocalCommandOutput:     func() Event { return &LocalCommandOutput{} },
	SysHookStarted:            func() Event { return &Hook{} },
	SysHookProgress:           func() Event { return &Hook{} },
	SysHookResponse:           func() Event { return &Hook{} },
	SysTaskStarted:            func() Event { return &TaskStarted{} },
	SysTaskProgress:           func() Event { return &TaskProgress{} },
	SysTaskUpdated:            func() Event { return &TaskUpdated{} },
	SysTaskNotification:       func() Event { return &TaskNotification{} },
	SysBackgroundTasksChanged: func() Event { return &BackgroundTasksChanged{} },
	SysSessionStateChanged:    func() Event { return &SessionStateChanged{} },
	SysCommandsChanged:        func() Event { return &CommandsChanged{} },
	SysNotification:           func() Event { return &Notification{} },
	SysThinkingTokens:         func() Event { return &ThinkingTokens{} },
	SysPermissionDenied:       func() Event { return &PermissionDenied{} },
	SysModelRefusalFallback:   func() Event { return &ModelRefusal{} },
	SysModelRefusalNoFallback: func() Event { return &ModelRefusal{} },
	SysMemoryRecall:           func() Event { return &MemoryRecall{} },
	SysFilesPersisted:         func() Event { return &SystemOther{} },
	SysElicitationComplete:    func() Event { return &ElicitationComplete{} },
	SysPluginInstall:          func() Event { return &PluginInstall{} },
	SysControlRequestProgress: func() Event { return &SystemOther{} },
	SysWorkerShuttingDown:     func() Event { return &SystemOther{} },
	SysSessionTitleChanged:    func() Event { return &SessionTitleChanged{} },
	SysTurnDuration:           func() Event { return &TurnDuration{} },
	SysAPIError:               func() Event { return &SystemOther{} },
	SysModelFallback:          func() Event { return &SystemOther{} },
	SysAwaySummary:            func() Event { return &SystemOther{} },
}

// SystemOther is a known system subtype with no typed fields yet; read Raw.
type SystemOther struct {
	Envelope
}

// SystemInit starts every turn and carries the session's current configuration.
// The newest one wins.
type SystemInit struct {
	Envelope
	CWD                     string          `json:"cwd"`
	Tools                   []string        `json:"tools"`
	MCPServers              []MCPServerInfo `json:"mcp_servers"`
	Model                   string          `json:"model"`
	PermissionMode          string          `json:"permissionMode"`
	SlashCommands           []string        `json:"slash_commands"`
	TerminalSlashCommands   []string        `json:"terminal_slash_commands,omitempty"`
	APIKeySource            string          `json:"apiKeySource,omitempty"`
	Betas                   []string        `json:"betas,omitempty"`
	ClaudeCodeVersion       string          `json:"claude_code_version"`
	OutputStyle             string          `json:"output_style"`
	Agents                  []string        `json:"agents,omitempty"`
	Skills                  []string        `json:"skills,omitempty"`
	Plugins                 []PluginInfo    `json:"plugins,omitempty"`
	PluginErrors            json.RawMessage `json:"plugin_errors,omitempty"`
	MCPServerErrors         json.RawMessage `json:"mcp_server_errors,omitempty"`
	Capabilities            []string        `json:"capabilities,omitempty"`
	AnalyticsDisabled       bool            `json:"analytics_disabled,omitempty"`
	ProductFeedbackDisabled bool            `json:"product_feedback_disabled,omitempty"`
	MemoryPaths             json.RawMessage `json:"memory_paths,omitempty"` // {"auto": dir, ...}
	ScratchpadPath          string          `json:"scratchpad_path,omitempty"`
	MessagingSocketPath     string          `json:"messaging_socket_path,omitempty"`
	FastModeState           string          `json:"fast_mode_state,omitempty"`
	FastModeDisabledReason  string          `json:"fast_mode_disabled_reason,omitempty"`
	Effort                  string          `json:"effort,omitempty"`
	PerTurnEffortActive     bool            `json:"per_turn_effort_active,omitempty"`
	ViewMode                string          `json:"view_mode,omitempty"`
}

// HasCapability reports whether the init capabilities list contains c.
func (s *SystemInit) HasCapability(c string) bool {
	for _, x := range s.Capabilities {
		if x == c {
			return true
		}
	}
	return false
}

// Capabilities seen in SystemInit.Capabilities.
const (
	CapInterruptReceipt     = "interrupt_receipt_v1"
	CapInterruptCancelQueue = "interrupt_cancel_queued_v1"
	CapInterruptSendNow     = "interrupt_send_now_v1"
	CapMsgLifecycle         = "msg_lifecycle_v1"
	CapMCPReadResource      = "mcp_read_resource_v1"
	CapMCPToolUIMeta        = "mcp_tool_ui_meta_v1"
	CapUISurface            = "ui_surface_v1"
)

// MCPServerInfo is an MCP server's name and connection status.
type MCPServerInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"` // connected | failed | needs-auth | pending | disabled
	Source string `json:"source,omitempty"`
}

// PluginInfo is a loaded plugin.
type PluginInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Source  string `json:"source,omitempty"`
	Version string `json:"version,omitempty"`
}

// CompactBoundary marks where the conversation was compacted.
type CompactBoundary struct {
	Envelope
	CompactMetadata CompactMetadata `json:"compact_metadata"`
}

// CompactMetadata describes a compaction.
type CompactMetadata struct {
	Trigger           string          `json:"trigger"` // manual | auto
	PreTokens         int64           `json:"pre_tokens"`
	PostTokens        int64           `json:"post_tokens,omitempty"`
	DurationMS        int64           `json:"duration_ms,omitempty"`
	PreservedSegment  json.RawMessage `json:"preserved_segment,omitempty"`
	PreservedMessages json.RawMessage `json:"preserved_messages,omitempty"`
}

// Status reports compacting/requesting status and permission mode changes.
type Status struct {
	Envelope
	Status         string          `json:"status,omitempty"` // compacting | requesting | "" (null)
	PermissionMode string          `json:"permissionMode,omitempty"`
	CompactResult  json.RawMessage `json:"compact_result,omitempty"`
	CompactError   json.RawMessage `json:"compact_error,omitempty"`
}

// APIRetry reports a retried API request.
type APIRetry struct {
	Envelope
	Attempt      int             `json:"attempt"`
	MaxRetries   int             `json:"max_retries"`
	RetryDelayMS int64           `json:"retry_delay_ms"`
	ErrorStatus  json.RawMessage `json:"error_status,omitempty"`
	Error        json.RawMessage `json:"error,omitempty"`
	NoResponse   bool            `json:"no_response,omitempty"`
}

// Informational is an informational system message.
type Informational struct {
	Envelope
	Content             string `json:"content"`
	Level               string `json:"level,omitempty"` // info | notice | suggestion | warning
	ToolUseID           string `json:"tool_use_id,omitempty"`
	PreventContinuation bool   `json:"prevent_continuation,omitempty"`
	Tag                 string `json:"tag,omitempty"`
}

// LocalCommandOutput is output of a local slash command (schema form; most output
// arrives as a synthetic Assistant message instead).
type LocalCommandOutput struct {
	Envelope
	Content string `json:"content"`
}

// Hook is hook_started, hook_progress or hook_response (see Subtype).
type Hook struct {
	Envelope
	HookID    string `json:"hook_id"`
	HookName  string `json:"hook_name"`
	HookEvent string `json:"hook_event"`
	Stdout    string `json:"stdout,omitempty"`
	Stderr    string `json:"stderr,omitempty"`
	Output    string `json:"output,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Outcome   string `json:"outcome,omitempty"` // success | error | cancelled ...
}

// TaskStarted reports a new subagent or background task.
type TaskStarted struct {
	Envelope
	TaskID         string `json:"task_id"`
	ToolUseID      string `json:"tool_use_id,omitempty"`
	Description    string `json:"description"`
	SubagentType   string `json:"subagent_type,omitempty"`
	TaskType       string `json:"task_type,omitempty"`
	IsBackgrounded bool   `json:"is_backgrounded,omitempty"`
	SpawnDepth     int    `json:"spawn_depth,omitempty"`
	WorkflowName   string `json:"workflow_name,omitempty"`
	Prompt         string `json:"prompt,omitempty"`
	SkipTranscript bool   `json:"skip_transcript,omitempty"`
	Ambient        bool   `json:"ambient,omitempty"`
}

// TaskProgress reports a task's progress.
type TaskProgress struct {
	Envelope
	TaskID       string     `json:"task_id"`
	Description  string     `json:"description"`
	Usage        *TaskUsage `json:"usage,omitempty"`
	LastToolName string     `json:"last_tool_name,omitempty"`
	Summary      string     `json:"summary,omitempty"`
}

// TaskUsage is a task's running totals.
type TaskUsage struct {
	TotalTokens int64 `json:"total_tokens"`
	ToolUses    int64 `json:"tool_uses"`
	DurationMS  int64 `json:"duration_ms"`
}

// TaskUpdated patches a task's state.
type TaskUpdated struct {
	Envelope
	TaskID string          `json:"task_id"`
	Patch  json.RawMessage `json:"patch"` // {status?, description?, end_time?, error?, is_backgrounded?, ...}
}

// TaskNotification reports a finished task.
type TaskNotification struct {
	Envelope
	TaskID     string     `json:"task_id"`
	ToolUseID  string     `json:"tool_use_id,omitempty"`
	Status     string     `json:"status"` // completed | failed | stopped
	OutputFile string     `json:"output_file,omitempty"`
	Summary    string     `json:"summary,omitempty"`
	Usage      *TaskUsage `json:"usage,omitempty"`
	Reason     string     `json:"reason,omitempty"`
}

// BackgroundTasksChanged replaces the list of background tasks.
type BackgroundTasksChanged struct {
	Envelope
	Tasks []BackgroundTask `json:"tasks"`
}

// BackgroundTask is one entry of BackgroundTasksChanged.
type BackgroundTask struct {
	TaskID      string `json:"task_id"`
	TaskType    string `json:"task_type,omitempty"`
	Description string `json:"description,omitempty"`
	Ambient     bool   `json:"ambient,omitempty"`
}

// SessionStateChanged reports idle / running / requires_action. idle means truly done,
// including background work (needs CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS=1).
type SessionStateChanged struct {
	Envelope
	State string `json:"state"`
}

// Session states.
const (
	StateIdle           = "idle"
	StateRunning        = "running"
	StateRequiresAction = "requires_action"
)

// CommandsChanged replaces the cached slash-command list.
type CommandsChanged struct {
	Envelope
	Commands []SlashCommand `json:"commands"`
}

// Notification is a transient notice from the engine.
type Notification struct {
	Envelope
	Key       string `json:"key,omitempty"`
	Text      string `json:"text"`
	Priority  string `json:"priority,omitempty"`
	Color     string `json:"color,omitempty"`
	TimeoutMS int64  `json:"timeout_ms,omitempty"`
}

// ThinkingTokens estimates thinking tokens so far.
type ThinkingTokens struct {
	Envelope
	EstimatedTokens      int64  `json:"estimated_tokens"`
	EstimatedTokensDelta int64  `json:"estimated_tokens_delta"`
	UserMessageUUID      string `json:"user_message_uuid,omitempty"`
}

// PermissionDenied reports a tool call denied without asking.
type PermissionDenied struct {
	Envelope
	ToolName           string `json:"tool_name"`
	ToolUseID          string `json:"tool_use_id"`
	AgentID            string `json:"agent_id,omitempty"`
	DecisionReasonType string `json:"decision_reason_type,omitempty"`
	DecisionReason     string `json:"decision_reason,omitempty"`
	Message            string `json:"message,omitempty"`
}

// ModelRefusal is model_refusal_fallback or model_refusal_no_fallback.
type ModelRefusal struct {
	Envelope
	OriginalModel         string   `json:"original_model,omitempty"`
	FallbackModel         string   `json:"fallback_model,omitempty"`
	RetractedMessageUUIDs []string `json:"retracted_message_uuids,omitempty"`
	Content               string   `json:"content,omitempty"`
}

// MemoryRecall reports recalled memories.
type MemoryRecall struct {
	Envelope
	Mode     string          `json:"mode,omitempty"`
	Memories json.RawMessage `json:"memories,omitempty"`
}

// ElicitationComplete reports a finished MCP elicitation.
type ElicitationComplete struct {
	Envelope
	ElicitationID string `json:"elicitation_id,omitempty"`
}

// PluginInstall reports plugin installation progress.
type PluginInstall struct {
	Envelope
	Status string `json:"status"`
	Name   string `json:"name,omitempty"`
	Error  string `json:"error,omitempty"`
}

// SessionTitleChanged reports a new session title.
type SessionTitleChanged struct {
	Envelope
	Title string `json:"title"`
}

// TurnDuration reports how long a turn took.
type TurnDuration struct {
	Envelope
	DurationMS int64 `json:"duration_ms"`
}
