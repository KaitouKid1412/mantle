package proto

import "encoding/json"

// Assistant is one finished assistant content block (several share Message.ID).
type Assistant struct {
	Envelope
	Message          Message         `json:"message"`
	ParentToolUseID  string          `json:"parent_tool_use_id,omitempty"`
	Error            string          `json:"error,omitempty"` // authentication_failed, rate_limit, overloaded, ...
	RequestID        string          `json:"request_id,omitempty"`
	UserMessageUUID  string          `json:"user_message_uuid,omitempty"`
	UserMessageUUIDs []string        `json:"user_message_uuids,omitempty"`
	Aborted          bool            `json:"aborted,omitempty"`
	Supersedes       []string        `json:"supersedes,omitempty"` // remove these earlier message uuids
	SubagentType     string          `json:"subagent_type,omitempty"`
	Timestamp        string          `json:"timestamp,omitempty"`
	ContextUsage     json.RawMessage `json:"context_usage,omitempty"` // /context
	UsageReport      json.RawMessage `json:"usage_report,omitempty"`  // /usage

	// Local slash command output (synthetic message).
	LocalCommandSource  string           `json:"local_command_source,omitempty"`
	LocalCommandRun     *LocalCommandRun `json:"local_command_run,omitempty"`
	LocalCommandOutcome json.RawMessage  `json:"local_command_outcome,omitempty"`

	// Since proto-v1 (seen on 2.1.288).
	ThinkingDurationMS int64           `json:"thinking_duration_ms,omitempty"`
	IsAPIErrorMessage  bool            `json:"is_api_error_message,omitempty"`
	TaskDescription    string          `json:"task_description,omitempty"` // subagent messages
	ToolUseMeta        json.RawMessage `json:"tool_use_meta,omitempty"`    // MCP tool calls
	WireToolInputs     json.RawMessage `json:"wire_tool_inputs,omitempty"`
}

// LocalCommandRun names the local slash command that produced a synthetic message.
type LocalCommandRun struct {
	Command string `json:"command"`
	Args    string `json:"args"`
}

// Assistant error categories.
const (
	AssistantErrAuthenticationFailed = "authentication_failed"
	AssistantErrBillingError         = "billing_error"
	AssistantErrRateLimit            = "rate_limit"
	AssistantErrOverloaded           = "overloaded"
	AssistantErrInvalidRequest       = "invalid_request"
	AssistantErrModelNotFound        = "model_not_found"
	AssistantErrServerError          = "server_error"
	AssistantErrMaxOutputTokens      = "max_output_tokens"
	AssistantErrUnknown              = "unknown"
)

// Message is an Anthropic (beta) assistant message.
type Message struct {
	ID                string          `json:"id,omitempty"`
	Type              string          `json:"type,omitempty"` // "message"
	Role              string          `json:"role,omitempty"`
	Model             string          `json:"model,omitempty"` // "<synthetic>" for local output
	Content           []ContentBlock  `json:"content"`
	StopReason        string          `json:"stop_reason,omitempty"`
	StopSequence      string          `json:"stop_sequence,omitempty"`
	Usage             *Usage          `json:"usage,omitempty"`
	Container         json.RawMessage `json:"container,omitempty"`
	ContextManagement json.RawMessage `json:"context_management,omitempty"`
}

// SyntheticModel is Message.Model for messages the engine made up locally.
const SyntheticModel = "<synthetic>"

// Usage is token usage. Fields the engine sends as null decode as zero.
type Usage struct {
	InputTokens              int64               `json:"input_tokens"`
	OutputTokens             int64               `json:"output_tokens"`
	CacheCreationInputTokens int64               `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64               `json:"cache_read_input_tokens,omitempty"`
	ServerToolUse            *ServerToolUseUsage `json:"server_tool_use,omitempty"`
	ServiceTier              string              `json:"service_tier,omitempty"`
	CacheCreation            *CacheCreation      `json:"cache_creation,omitempty"`
	OutputTokensDetails      json.RawMessage     `json:"output_tokens_details,omitempty"`
	Speed                    string              `json:"speed,omitempty"`
	InferenceGeo             string              `json:"inference_geo,omitempty"`
	Iterations               json.RawMessage     `json:"iterations,omitempty"`
}

// ServerToolUseUsage counts server-side tool requests.
type ServerToolUseUsage struct {
	WebSearchRequests int64 `json:"web_search_requests"`
	WebFetchRequests  int64 `json:"web_fetch_requests"`
}

// CacheCreation splits cache writes by TTL.
type CacheCreation struct {
	Ephemeral1hInputTokens int64 `json:"ephemeral_1h_input_tokens"`
	Ephemeral5mInputTokens int64 `json:"ephemeral_5m_input_tokens"`
}

// User is a user message on stdout: tool results, or a replay of what we sent.
type User struct {
	Envelope
	Message         UserMessage     `json:"message"`
	ParentToolUseID string          `json:"parent_tool_use_id,omitempty"`
	ToolUseResult   json.RawMessage `json:"tool_use_result,omitempty"`
	IsSynthetic     bool            `json:"isSynthetic,omitempty"`
	IsReplay        bool            `json:"isReplay,omitempty"`
	FileAttachments json.RawMessage `json:"file_attachments,omitempty"`
	Timestamp       string          `json:"timestamp,omitempty"`
	Priority        string          `json:"priority,omitempty"`
	Origin          *Origin         `json:"origin,omitempty"`
	ToolResultMeta  json.RawMessage `json:"tool_result_meta,omitempty"`
}

// UserMessage is a user message param.
type UserMessage struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

// ToolResults returns the message's tool_result blocks, each carrying the message's
// tool_use_result and parent_tool_use_id.
func (u *User) ToolResults() []ToolResult {
	var out []ToolResult
	for _, b := range u.Message.Content.Blocks {
		if r, ok := b.ToolResult(); ok {
			r.Structured = u.ToolUseResult
			r.ParentToolUseID = u.ParentToolUseID
			out = append(out, r)
		}
	}
	return out
}

// Origin says who wrote a user message.
type Origin struct {
	Kind     string `json:"kind"`               // "human", ...
	Producer string `json:"producer,omitempty"` // since proto-v1
}

// StreamEvent is a raw Messages API stream event (with --include-partial-messages).
// The finished Assistant message still follows.
type StreamEvent struct {
	Envelope
	Event            StreamPayload `json:"event"`
	ParentToolUseID  string        `json:"parent_tool_use_id,omitempty"`
	TTFTMS           int64         `json:"ttft_ms,omitempty"`
	UserMessageUUID  string        `json:"user_message_uuid,omitempty"`
	UserMessageUUIDs []string      `json:"user_message_uuids,omitempty"`
	ThinkingDisplay  string        `json:"thinking_display,omitempty"`
}

// Stream event types (StreamPayload.Type).
const (
	StreamMessageStart      = "message_start"
	StreamContentBlockStart = "content_block_start"
	StreamContentBlockDelta = "content_block_delta"
	StreamContentBlockStop  = "content_block_stop"
	StreamMessageDelta      = "message_delta"
	StreamMessageStop       = "message_stop"
)

// Delta types (Delta.Type).
const (
	DeltaText      = "text_delta"
	DeltaThinking  = "thinking_delta"
	DeltaInputJSON = "input_json_delta"
	DeltaSignature = "signature_delta"
	DeltaCitations = "citations_delta"
)

// StreamPayload is a flat union of the stream event shapes.
type StreamPayload struct {
	Type         string        `json:"type"`
	Message      *Message      `json:"message,omitempty"`       // message_start
	Index        int           `json:"index"`                   // content_block_*
	ContentBlock *ContentBlock `json:"content_block,omitempty"` // content_block_start
	Delta        *Delta        `json:"delta,omitempty"`         // content_block_delta, message_delta
	Usage        *Usage        `json:"usage,omitempty"`         // message_delta

	ContextManagement json.RawMessage `json:"context_management,omitempty"`
}

// Delta is a content_block_delta delta or a message_delta delta.
type Delta struct {
	Type            string          `json:"type,omitempty"`
	Text            string          `json:"text,omitempty"`
	Thinking        string          `json:"thinking,omitempty"`
	PartialJSON     string          `json:"partial_json,omitempty"`
	Signature       string          `json:"signature,omitempty"`
	Citation        json.RawMessage `json:"citation,omitempty"`
	EstimatedTokens *int64          `json:"estimated_tokens,omitempty"` // thinking_delta

	// message_delta
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}

// IsDelta reports whether the event is a content_block_delta (coalescable).
func (s *StreamEvent) IsDelta() bool { return s.Event.Type == StreamContentBlockDelta }

// Result ends every turn (exactly one per turn).
type Result struct {
	Envelope
	DurationMS        int64                 `json:"duration_ms"`
	DurationAPIMS     int64                 `json:"duration_api_ms"`
	IsError           bool                  `json:"is_error"`
	NumTurns          int                   `json:"num_turns"`
	StopReason        string                `json:"stop_reason,omitempty"`
	TerminalReason    string                `json:"terminal_reason,omitempty"`
	ResultIndex       int                   `json:"result_index,omitempty"`
	QueuedTurnCount   int                   `json:"queued_turn_count,omitempty"`
	TotalCostUSD      float64               `json:"total_cost_usd"`
	Usage             *Usage                `json:"usage,omitempty"`
	ModelUsage        map[string]ModelUsage `json:"modelUsage,omitempty"`
	PermissionDenials []PermissionDenial    `json:"permission_denials,omitempty"`
	UserMessageUUID   string                `json:"user_message_uuid,omitempty"`
	UserMessageUUIDs  []string              `json:"user_message_uuids,omitempty"`
	FastModeState     string                `json:"fast_mode_state,omitempty"`
	Origin            *Origin               `json:"origin,omitempty"`
	SubagentStats     json.RawMessage       `json:"subagent_stats,omitempty"`

	// subtype success
	Result           string          `json:"result,omitempty"`
	APIErrorStatus   json.RawMessage `json:"api_error_status,omitempty"`
	StructuredOutput json.RawMessage `json:"structured_output,omitempty"`
	DeferredToolUse  json.RawMessage `json:"deferred_tool_use,omitempty"`
	TTFTMS           int64           `json:"ttft_ms,omitempty"`
	LocalCommand     string          `json:"local_command,omitempty"`

	// subtype error_*
	Errors               []string `json:"errors,omitempty"`
	StartupFailureReason string   `json:"startup_failure_reason,omitempty"`

	// Since proto-v1 (seen on 2.1.288): timing and fast mode.
	FastModeDisabledReason  string `json:"fast_mode_disabled_reason,omitempty"`
	FirstContentFrameMS     int64  `json:"first_content_frame_ms,omitempty"`
	FirstRequestInputTokens int64  `json:"first_request_input_tokens,omitempty"`
	RequestSentWallMS       int64  `json:"request_sent_wall_ms,omitempty"`
	TimeToRequestMS         int64  `json:"time_to_request_ms,omitempty"`
	TTFTStreamMS            int64  `json:"ttft_stream_ms,omitempty"`
}

// Result subtypes.
const (
	ResultSuccess                         = "success"
	ResultErrorDuringExecution            = "error_during_execution"
	ResultErrorMaxTurns                   = "error_max_turns"
	ResultErrorMaxBudgetUSD               = "error_max_budget_usd"
	ResultErrorMaxStructuredOutputRetries = "error_max_structured_output_retries"
)

// Terminal reasons seen on Result.TerminalReason.
const (
	TerminalCompleted        = "completed"
	TerminalAbortedStreaming = "aborted_streaming"
	TerminalAbortedTools     = "aborted_tools"
	TerminalPromptTooLong    = "prompt_too_long"
	TerminalAPIError         = "api_error"
	TerminalMaxTurns         = "max_turns"
	TerminalBudgetExhausted  = "budget_exhausted"
)

// Interrupted reports whether the turn ended because it was interrupted.
func (r *Result) Interrupted() bool {
	return r.TerminalReason == TerminalAbortedStreaming || r.TerminalReason == TerminalAbortedTools
}

// ModelUsage is per-model usage and cost in a Result.
type ModelUsage struct {
	InputTokens              int64   `json:"inputTokens"`
	OutputTokens             int64   `json:"outputTokens"`
	CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
	WebSearchRequests        int64   `json:"webSearchRequests"`
	CostUSD                  float64 `json:"costUSD"`
	ContextWindow            int64   `json:"contextWindow"`
	MaxOutputTokens          int64   `json:"maxOutputTokens"`
	ThinkingTokens           int64   `json:"thinkingTokens,omitempty"`
	CanonicalModel           string  `json:"canonicalModel,omitempty"`
	CostBasis                string  `json:"costBasis,omitempty"`
	Provider                 string  `json:"provider,omitempty"`
}

// PermissionDenial is a tool call that was denied during the turn.
type PermissionDenial struct {
	ToolName  string          `json:"tool_name"`
	ToolUseID string          `json:"tool_use_id"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"`
}

// ToolProgress is a heartbeat for a long-running tool.
type ToolProgress struct {
	Envelope
	ToolUseID          string          `json:"tool_use_id"`
	ToolName           string          `json:"tool_name"`
	ParentToolUseID    string          `json:"parent_tool_use_id,omitempty"`
	ElapsedTimeSeconds float64         `json:"elapsed_time_seconds"`
	TaskID             string          `json:"task_id,omitempty"`
	Heartbeat          bool            `json:"heartbeat,omitempty"`
	SubagentRetry      json.RawMessage `json:"subagent_retry,omitempty"`
}

// ToolUseSummary summarises a group of earlier tool calls.
type ToolUseSummary struct {
	Envelope
	Summary             string   `json:"summary"`
	PrecedingToolUseIDs []string `json:"preceding_tool_use_ids,omitempty"`
}

// AuthStatus reports login progress (with --enable-auth-status).
type AuthStatus struct {
	Envelope
	IsAuthenticating bool     `json:"isAuthenticating"`
	Output           []string `json:"output,omitempty"`
	Error            string   `json:"error,omitempty"`
}

// RateLimitEvent reports rate-limit status.
type RateLimitEvent struct {
	Envelope
	RateLimitInfo RateLimitInfo `json:"rate_limit_info"`
}

// RateLimitInfo describes the current rate-limit state. Extra fields stay in the
// event's Raw.
type RateLimitInfo struct {
	Status        string          `json:"status"`
	ResetsAt      json.RawMessage `json:"resetsAt,omitempty"`
	RateLimitType string          `json:"rateLimitType,omitempty"`
	Utilization   float64         `json:"utilization,omitempty"`

	// Since proto-v1 (seen on 2.1.288).
	OverageStatus         string          `json:"overageStatus,omitempty"`
	OverageDisabledReason string          `json:"overageDisabledReason,omitempty"`
	IsUsingOverage        bool            `json:"isUsingOverage,omitempty"`
	SurpassedThreshold    json.RawMessage `json:"surpassedThreshold,omitempty"`
	UnifiedWindows        json.RawMessage `json:"unifiedWindows,omitempty"`
}

// PromptSuggestion is a suggested next prompt (ghost text).
type PromptSuggestion struct {
	Envelope
	Suggestion string `json:"suggestion"`
}

// ConversationReset starts a fresh transcript (/clear). The session id changes, but
// NewConversationID is not the new session id: the next system/init reports that
// (verified on 2.1.288 and 2.1.289).
type ConversationReset struct {
	Envelope
	NewConversationID string `json:"new_conversation_id"`
	Trigger           string `json:"trigger,omitempty"` // clear | plan_mode_exit | fresh_session | onboarding
	UserMessageUUID   string `json:"user_message_uuid,omitempty"`
	Timestamp         string `json:"timestamp,omitempty"`
}

// ActiveGoal reports the active /goal (shape not documented; see Raw).
type ActiveGoal struct {
	Envelope
	Goal json.RawMessage `json:"goal,omitempty"`
	// Value is the goal ({condition, iterations, set_at, ...}; null when cleared). The
	// 2.1.288 schema names it "value" (since proto-v1; Goal is kept for compatibility).
	Value json.RawMessage `json:"value,omitempty"`
}

// KeepAlive is a no-op frame (both directions).
type KeepAlive struct {
	Envelope
}

// CommandLifecycle tracks a submitted user message by its uuid (msg_lifecycle_v1).
type CommandLifecycle struct {
	Envelope
	CommandUUID string `json:"command_uuid"`
	State       string `json:"state"` // queued | started | completed | cancelled | discarded | refused
}

// Command lifecycle states.
const (
	LifecycleQueued    = "queued"
	LifecycleStarted   = "started"
	LifecycleCompleted = "completed"
	LifecycleCancelled = "cancelled"
	LifecycleDiscarded = "discarded"
	LifecycleRefused   = "refused"
)
