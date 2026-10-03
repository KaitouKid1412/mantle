package proto

import "encoding/json"

// Documented control request subtypes, client → CLI.
const (
	SubInitialize                   = "initialize"
	SubInterrupt                    = "interrupt"
	SubSetPermissionMode            = "set_permission_mode"
	SubSetModel                     = "set_model"
	SubSetMaxThinkingTokens         = "set_max_thinking_tokens"
	SubApplyFlagSettings            = "apply_flag_settings"
	SubUpdateSettings               = "update_settings"
	SubGetSettings                  = "get_settings"
	SubGetHooksListing              = "get_hooks_listing"
	SubListPermissionRules          = "list_permission_rules"
	SubMCPStatus                    = "mcp_status"
	SubMCPReconnect                 = "mcp_reconnect"
	SubMCPToggle                    = "mcp_toggle"
	SubMCPSetServers                = "mcp_set_servers"
	SubMCPCall                      = "mcp_call"
	SubMCPReadResource              = "mcp_read_resource"
	SubSetMCPPermissionModeOverride = "set_mcp_permission_mode_override"
	SubGetContextUsage              = "get_context_usage"
	SubGetUsage                     = "get_usage"
	SubGetSessionCost               = "get_session_cost"
	SubListModels                   = "list_models"
	SubGetBinaryVersion             = "get_binary_version"
	SubRewindFiles                  = "rewind_files"
	SubCancelAsyncMessage           = "cancel_async_message"
	SubStopTask                     = "stop_task"
	SubBackgroundTasks              = "background_tasks"
	SubGetTaskOutput                = "get_task_output"
	SubRenameSession                = "rename_session"
	SubGenerateSessionTitle         = "generate_session_title"
	SubReloadPlugins                = "reload_plugins"
	SubReloadSkills                 = "reload_skills"
	SubReloadOutputStyles           = "reload_output_styles"
	SubReadFile                     = "read_file"
	SubFileSuggestions              = "file_suggestions"
	SubSeedReadState                = "seed_read_state"
	SubRegisterRepoRoot             = "register_repo_root"
	SubSetColor                     = "set_color"
	SubEndSession                   = "end_session"
)

// Control request subtypes, CLI → client.
const (
	SubCanUseTool           = "can_use_tool"
	SubHookCallback         = "hook_callback"
	SubMCPMessage           = "mcp_message"
	SubElicitation          = "elicitation"
	SubRequestUserDialog    = "request_user_dialog"
	SubOAuthTokenRefresh    = "oauth_token_refresh"
	SubHostAuthTokenRefresh = "host_auth_token_refresh"
)

// KnownRequestSubtypes lists the documented client → CLI subtypes (for drift checks).
func KnownRequestSubtypes() []string {
	return []string{
		SubInitialize, SubInterrupt, SubSetPermissionMode, SubSetModel, SubSetMaxThinkingTokens,
		SubApplyFlagSettings, SubUpdateSettings, SubGetSettings, SubGetHooksListing,
		SubListPermissionRules, SubMCPStatus, SubMCPReconnect, SubMCPToggle, SubMCPSetServers,
		SubMCPCall, SubMCPReadResource, SubSetMCPPermissionModeOverride, SubGetContextUsage,
		SubGetUsage, SubGetSessionCost, SubListModels, SubGetBinaryVersion, SubRewindFiles,
		SubCancelAsyncMessage, SubStopTask, SubBackgroundTasks, SubGetTaskOutput,
		SubRenameSession, SubGenerateSessionTitle, SubReloadPlugins, SubReloadSkills,
		SubReloadOutputStyles, SubReadFile, SubFileSuggestions, SubSeedReadState,
		SubRegisterRepoRoot, SubSetColor, SubEndSession,
	}
}

// ---- initialize ----

// InitializeRequest configures the session. mantle never sets SystemPrompt, so the
// default Claude Code system prompt is kept.
type InitializeRequest struct {
	Hooks                  map[string][]HookMatcher   `json:"hooks,omitempty"`
	SDKMCPServers          []string                   `json:"sdkMcpServers,omitempty"`
	JSONSchema             json.RawMessage            `json:"jsonSchema,omitempty"`
	SystemPrompt           []string                   `json:"systemPrompt,omitempty"`
	AppendSystemPrompt     string                     `json:"appendSystemPrompt,omitempty"`
	PlanModeInstructions   string                     `json:"planModeInstructions,omitempty"`
	ExcludeDynamicSections bool                       `json:"excludeDynamicSections,omitempty"`
	Agents                 map[string]AgentDefinition `json:"agents,omitempty"`
	Title                  string                     `json:"title,omitempty"`
	Skills                 []string                   `json:"skills,omitempty"`
	PromptSuggestions      bool                       `json:"promptSuggestions,omitempty"`
	AgentProgressSummaries bool                       `json:"agentProgressSummaries,omitempty"`
	ForwardSubagentText    bool                       `json:"forwardSubagentText,omitempty"`
	SupportedDialogKinds   []string                   `json:"supportedDialogKinds,omitempty"`
	PerTaskStopAffordance  bool                       `json:"perTaskStopAffordance,omitempty"`
	Plugins                []PluginSpec               `json:"plugins,omitempty"` // needs --await-initialize
}

func (InitializeRequest) ControlSubtype() string { return SubInitialize }

// HookMatcher registers client hook callbacks for one hook event.
type HookMatcher struct {
	Matcher         string   `json:"matcher,omitempty"`
	HookCallbackIDs []string `json:"hookCallbackIds"`
	Timeout         int      `json:"timeout,omitempty"`
}

// AgentDefinition defines a custom subagent.
type AgentDefinition struct {
	Description     string   `json:"description"`
	Prompt          string   `json:"prompt"`
	Tools           []string `json:"tools,omitempty"`
	DisallowedTools []string `json:"disallowedTools,omitempty"`
	Model           string   `json:"model,omitempty"`
	Skills          []string `json:"skills,omitempty"`
	MaxTurns        int      `json:"maxTurns,omitempty"`
}

// PluginSpec is a local plugin to load.
type PluginSpec struct {
	Type             string `json:"type"` // "local"
	Path             string `json:"path"`
	SkipMCPDiscovery bool   `json:"skipMcpDiscovery,omitempty"`
}

// InitializeResponse describes the session after initialize. The full payload stays
// in ControlResponseBody.Response.
type InitializeResponse struct {
	Commands               []SlashCommand  `json:"commands"`
	Agents                 []AgentInfo     `json:"agents"`
	OutputStyle            string          `json:"output_style"`
	AvailableOutputStyles  []string        `json:"available_output_styles"`
	UserOutputStylesDir    string          `json:"user_output_styles_dir,omitempty"`
	Models                 []ModelInfo     `json:"models"`
	UnavailableModels      json.RawMessage `json:"unavailable_models,omitempty"`
	Account                Account         `json:"account"`
	PID                    int             `json:"pid"`
	CurrentPermissionMode  string          `json:"current_permission_mode"`
	FastModeState          string          `json:"fast_mode_state,omitempty"`
	FastModeDisabledReason string          `json:"fast_mode_disabled_reason,omitempty"`
	SessionState           string          `json:"session_state,omitempty"`
	Capabilities           []string        `json:"capabilities,omitempty"`
	AnalyticsDisabled      bool            `json:"analytics_disabled,omitempty"`
}

// SlashCommand is a command the engine accepts headlessly.
type SlashCommand struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ArgumentHint string   `json:"argumentHint,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
	Builtin      bool     `json:"builtin,omitempty"`
}

// AgentInfo is an available subagent.
type AgentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Model       string `json:"model,omitempty"`
}

// ModelInfo is a selectable model.
type ModelInfo struct {
	Value                    string   `json:"value"`
	ResolvedModel            string   `json:"resolvedModel,omitempty"`
	DisplayName              string   `json:"displayName"`
	Description              string   `json:"description,omitempty"`
	SupportsEffort           bool     `json:"supportsEffort,omitempty"`
	SupportedEffortLevels    []string `json:"supportedEffortLevels,omitempty"`
	SupportsAdaptiveThinking bool     `json:"supportsAdaptiveThinking,omitempty"`
	SupportsFastMode         bool     `json:"supportsFastMode,omitempty"`
	SupportsAutoMode         bool     `json:"supportsAutoMode,omitempty"`
}

// Account is the logged-in account.
type Account struct {
	Email            string `json:"email,omitempty"`
	Organization     string `json:"organization,omitempty"`
	SubscriptionType string `json:"subscriptionType,omitempty"`
	TokenSource      string `json:"tokenSource,omitempty"`
	APIKeySource     string `json:"apiKeySource,omitempty"`
	APIProvider      string `json:"apiProvider,omitempty"`
}

// ---- turn and settings control ----

// InterruptRequest stops the running turn. The interrupted turn's Result follows.
type InterruptRequest struct {
	CancelQueued bool `json:"cancel_queued,omitempty"`
}

func (InterruptRequest) ControlSubtype() string { return SubInterrupt }

// InterruptResponse lists queued messages that survived or were cancelled.
type InterruptResponse struct {
	StillQueued []string `json:"still_queued,omitempty"`
	Cancelled   []string `json:"cancelled,omitempty"`
}

// SetPermissionModeRequest changes the permission mode.
type SetPermissionModeRequest struct {
	Mode string `json:"mode"`
}

func (SetPermissionModeRequest) ControlSubtype() string { return SubSetPermissionMode }

// Permission modes.
const (
	ModeDefault           = "default"
	ModeAcceptEdits       = "acceptEdits"
	ModePlan              = "plan"
	ModeBypassPermissions = "bypassPermissions"
	ModeDontAsk           = "dontAsk"
	ModeAuto              = "auto"
)

// SetModelRequest switches model; "default" resets.
type SetModelRequest struct {
	Model string `json:"model"`
}

func (SetModelRequest) ControlSubtype() string { return SubSetModel }

// SetMaxThinkingTokensRequest changes the thinking budget; nil clears it.
type SetMaxThinkingTokensRequest struct {
	MaxThinkingTokens *int   `json:"max_thinking_tokens"`
	ThinkingDisplay   string `json:"thinking_display,omitempty"` // summarized | omitted | highlights
}

func (SetMaxThinkingTokensRequest) ControlSubtype() string { return SubSetMaxThinkingTokens }

// ApplyFlagSettingsRequest merges flag-tier settings; a nil value clears a key.
type ApplyFlagSettingsRequest struct {
	Settings map[string]any `json:"settings"`
}

func (ApplyFlagSettingsRequest) ControlSubtype() string { return SubApplyFlagSettings }

// UpdateSettingsRequest writes allowlisted keys (outputStyle, effortLevel) to a
// settings file.
type UpdateSettingsRequest struct {
	Source   string         `json:"source"` // localSettings | userSettings
	Settings map[string]any `json:"settings"`
}

func (UpdateSettingsRequest) ControlSubtype() string { return SubUpdateSettings }

// GetSettingsRequest reads the merged settings.
type GetSettingsRequest struct{}

func (GetSettingsRequest) ControlSubtype() string { return SubGetSettings }

// SettingsResponse is the get_settings reply.
type SettingsResponse struct {
	Effective json.RawMessage  `json:"effective"`
	Sources   []SettingsSource `json:"sources,omitempty"`
	Applied   json.RawMessage  `json:"applied,omitempty"`
}

// SettingsSource is one settings scope's contents.
type SettingsSource struct {
	Source   string          `json:"source"`
	Settings json.RawMessage `json:"settings"`
}

// GetHooksListingRequest lists configured hooks.
type GetHooksListingRequest struct{}

func (GetHooksListingRequest) ControlSubtype() string { return SubGetHooksListing }

// HooksListing is the get_hooks_listing reply.
type HooksListing struct {
	Events []HookEventInfo  `json:"events"`
	Hooks  []HookConfigInfo `json:"hooks"`
}

// HookEventInfo summarises one hook event.
type HookEventInfo struct {
	Name            string `json:"name"`
	Summary         string `json:"summary,omitempty"`
	SupportsMatcher bool   `json:"supportsMatcher,omitempty"`
	HookCount       int    `json:"hookCount"`
}

// HookConfigInfo is one configured hook.
type HookConfigInfo struct {
	Event       string `json:"event"`
	Matcher     string `json:"matcher,omitempty"`
	Source      string `json:"source,omitempty"`
	SourceLabel string `json:"sourceLabel,omitempty"`
	PluginName  string `json:"pluginName,omitempty"`
	Type        string `json:"type,omitempty"`
	DisplayText string `json:"displayText,omitempty"`
	Timeout     int    `json:"timeout,omitempty"`
}

// ListPermissionRulesRequest lists permission rules.
type ListPermissionRulesRequest struct{}

func (ListPermissionRulesRequest) ControlSubtype() string { return SubListPermissionRules }

// PermissionRulesResponse is the list_permission_rules reply.
type PermissionRulesResponse struct {
	State struct {
		Rules                json.RawMessage `json:"rules"`
		WorkspaceDirectories []string        `json:"workspaceDirectories"`
		OriginalCWD          string          `json:"originalCwd"`
		ManagedOnly          bool            `json:"managedOnly"`
	} `json:"state"`
}

// ---- MCP ----

// MCPStatusRequest lists MCP servers.
type MCPStatusRequest struct{}

func (MCPStatusRequest) ControlSubtype() string { return SubMCPStatus }

// MCPStatusResponse is the mcp_status reply.
type MCPStatusResponse struct {
	MCPServers []MCPServerStatus `json:"mcpServers"`
}

// MCPServerStatus is an MCP server's full status.
type MCPServerStatus struct {
	Name       string          `json:"name"`
	Status     string          `json:"status"`
	ServerInfo *MCPServerID    `json:"serverInfo,omitempty"`
	Config     json.RawMessage `json:"config,omitempty"`
	Scope      string          `json:"scope,omitempty"`
	Source     string          `json:"source,omitempty"`
	Tools      []MCPToolInfo   `json:"tools,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// MCPServerID is a server's self-reported name and version.
type MCPServerID struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// MCPToolInfo is a tool an MCP server offers.
type MCPToolInfo struct {
	Name        string          `json:"name"`
	Annotations json.RawMessage `json:"annotations,omitempty"`
}

// MCPReconnectRequest reconnects one server.
type MCPReconnectRequest struct {
	ServerName string `json:"serverName"`
}

func (MCPReconnectRequest) ControlSubtype() string { return SubMCPReconnect }

// MCPToggleRequest enables or disables one server.
type MCPToggleRequest struct {
	ServerName string `json:"serverName"`
	Enabled    bool   `json:"enabled"`
}

func (MCPToggleRequest) ControlSubtype() string { return SubMCPToggle }

// MCPSetServersRequest replaces the dynamic server set.
type MCPSetServersRequest struct {
	Servers map[string]json.RawMessage `json:"servers"`
}

func (MCPSetServersRequest) ControlSubtype() string { return SubMCPSetServers }

// MCPCallRequest calls an MCP tool directly (no model turn, no permission check).
type MCPCallRequest struct {
	Tool      string          `json:"tool"` // mcp__server__tool
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

func (MCPCallRequest) ControlSubtype() string { return SubMCPCall }

// MCPReadResourceRequest reads an MCP resource.
type MCPReadResourceRequest struct {
	ServerName string `json:"serverName"`
	URI        string `json:"uri"`
}

func (MCPReadResourceRequest) ControlSubtype() string { return SubMCPReadResource }

// SetMCPPermissionModeOverrideRequest overrides the permission mode for one server.
type SetMCPPermissionModeOverrideRequest struct {
	ServerName string  `json:"serverName"`
	Mode       *string `json:"mode"` // default | auto | nil
}

func (SetMCPPermissionModeOverrideRequest) ControlSubtype() string {
	return SubSetMCPPermissionModeOverride
}

// ---- usage and info ----

// GetContextUsageRequest asks for context window usage.
type GetContextUsageRequest struct {
	Detail string `json:"detail,omitempty"` // summary | full
}

func (GetContextUsageRequest) ControlSubtype() string { return SubGetContextUsage }

// ContextUsage is the get_context_usage reply (main fields; the rest stays raw).
type ContextUsage struct {
	Categories        []ContextCategory `json:"categories"`
	TotalTokens       int64             `json:"totalTokens"`
	MaxTokens         int64             `json:"maxTokens"`
	RawMaxTokens      int64             `json:"rawMaxTokens,omitempty"`
	Percentage        float64           `json:"percentage"`
	AutocompactSource string            `json:"autocompactSource,omitempty"`
	MemoryFiles       json.RawMessage   `json:"memoryFiles,omitempty"`
}

// ContextCategory is one row of context usage.
type ContextCategory struct {
	Name       string `json:"name"`
	Tokens     int64  `json:"tokens"`
	Color      string `json:"color,omitempty"`
	Kind       string `json:"kind,omitempty"` // used | deferred | buffer | free
	IsDeferred bool   `json:"isDeferred,omitempty"`
}

// GetUsageRequest asks for session cost and rate limits.
type GetUsageRequest struct {
	SkipBehaviors bool `json:"skip_behaviors,omitempty"`
}

func (GetUsageRequest) ControlSubtype() string { return SubGetUsage }

// UsageResponse is the get_usage reply (main fields; the rest stays raw).
type UsageResponse struct {
	Session struct {
		TotalCostUSD       float64         `json:"total_cost_usd"`
		TotalAPIDurationMS int64           `json:"total_api_duration_ms"`
		TotalDurationMS    int64           `json:"total_duration_ms"`
		TotalLinesAdded    int64           `json:"total_lines_added"`
		TotalLinesRemoved  int64           `json:"total_lines_removed"`
		ModelUsage         json.RawMessage `json:"model_usage,omitempty"`
	} `json:"session"`
	SubscriptionType    string          `json:"subscription_type,omitempty"`
	RateLimitsAvailable bool            `json:"rate_limits_available,omitempty"`
	RateLimits          json.RawMessage `json:"rate_limits,omitempty"`
}

// GetSessionCostRequest asks for the /cost text.
type GetSessionCostRequest struct{}

func (GetSessionCostRequest) ControlSubtype() string { return SubGetSessionCost }

// TextResponse is a reply carrying one text field ({text}).
type TextResponse struct {
	Text string `json:"text"`
}

// ListModelsRequest lists models.
type ListModelsRequest struct{}

func (ListModelsRequest) ControlSubtype() string { return SubListModels }

// ModelsResponse is the list_models reply.
type ModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

// GetBinaryVersionRequest asks for the engine version.
type GetBinaryVersionRequest struct{}

func (GetBinaryVersionRequest) ControlSubtype() string { return SubGetBinaryVersion }

// BinaryVersion is the get_binary_version reply.
type BinaryVersion struct {
	Version   string `json:"version"`
	BuildTime string `json:"buildTime,omitempty"`
}

// ---- tasks and messages ----

// RewindFilesRequest restores files to their state at a user message.
type RewindFilesRequest struct {
	UserMessageID string `json:"user_message_id"`
	DryRun        bool   `json:"dry_run,omitempty"`
}

func (RewindFilesRequest) ControlSubtype() string { return SubRewindFiles }

// RewindFilesResponse is the rewind_files reply.
type RewindFilesResponse struct {
	CanRewind    bool            `json:"canRewind"`
	Error        string          `json:"error,omitempty"`
	FilesChanged json.RawMessage `json:"filesChanged,omitempty"`
	Insertions   int             `json:"insertions,omitempty"`
	Deletions    int             `json:"deletions,omitempty"`
	SkippedLinks json.RawMessage `json:"skippedLinks,omitempty"`
}

// CancelAsyncMessageRequest takes back a queued user message.
type CancelAsyncMessageRequest struct {
	MessageUUID string `json:"message_uuid"`
}

func (CancelAsyncMessageRequest) ControlSubtype() string { return SubCancelAsyncMessage }

// CancelAsyncMessageResponse says whether the message was taken back.
type CancelAsyncMessageResponse struct {
	Cancelled bool `json:"cancelled"`
}

// StopTaskRequest stops a background task.
type StopTaskRequest struct {
	TaskID string `json:"task_id"`
}

func (StopTaskRequest) ControlSubtype() string { return SubStopTask }

// BackgroundTasksRequest backgrounds running work (like ctrl+b).
type BackgroundTasksRequest struct {
	ToolUseID string `json:"tool_use_id,omitempty"`
}

func (BackgroundTasksRequest) ControlSubtype() string { return SubBackgroundTasks }

// GetTaskOutputRequest reads a background task's output.
type GetTaskOutputRequest struct {
	TaskID string `json:"task_id"`
}

func (GetTaskOutputRequest) ControlSubtype() string { return SubGetTaskOutput }

// TaskOutput is the get_task_output reply.
type TaskOutput struct {
	Output     string `json:"output"`
	TotalBytes int64  `json:"total_bytes"`
	Truncated  bool   `json:"truncated"`
}

// ---- session ----

// RenameSessionRequest sets the session title.
type RenameSessionRequest struct {
	Title     string `json:"title"`
	Source    string `json:"source,omitempty"` // remote | host
	SessionID string `json:"session_id,omitempty"`
}

func (RenameSessionRequest) ControlSubtype() string { return SubRenameSession }

// GenerateSessionTitleRequest asks the engine to title a session.
type GenerateSessionTitleRequest struct {
	Description string `json:"description"`
	Persist     bool   `json:"persist,omitempty"`
}

func (GenerateSessionTitleRequest) ControlSubtype() string { return SubGenerateSessionTitle }

// TitleResponse is the generate_session_title reply.
type TitleResponse struct {
	Title string `json:"title"`
}

// ReloadPluginsRequest reloads plugins.
type ReloadPluginsRequest struct {
	HoldOnCacheImpact bool `json:"hold_on_cache_impact,omitempty"`
}

func (ReloadPluginsRequest) ControlSubtype() string { return SubReloadPlugins }

// ReloadPluginsResponse is the reload_plugins reply.
type ReloadPluginsResponse struct {
	Commands    []SlashCommand  `json:"commands,omitempty"`
	Agents      []AgentInfo     `json:"agents,omitempty"`
	Plugins     []PluginInfo    `json:"plugins,omitempty"`
	MCPServers  json.RawMessage `json:"mcpServers,omitempty"`
	ErrorCount  int             `json:"error_count"`
	Held        bool            `json:"held,omitempty"`
	CacheImpact json.RawMessage `json:"cache_impact,omitempty"`
}

// ReloadSkillsRequest reloads skills.
type ReloadSkillsRequest struct{}

func (ReloadSkillsRequest) ControlSubtype() string { return SubReloadSkills }

// ReloadSkillsResponse is the reload_skills reply.
type ReloadSkillsResponse struct {
	Skills []SlashCommand `json:"skills"`
}

// ReloadOutputStylesRequest reloads output styles.
type ReloadOutputStylesRequest struct{}

func (ReloadOutputStylesRequest) ControlSubtype() string { return SubReloadOutputStyles }

// OutputStylesResponse is the reload_output_styles reply.
type OutputStylesResponse struct {
	AvailableOutputStyles []string `json:"available_output_styles"`
}

// ---- files and misc ----

// ReadFileRequest reads a file through the engine.
type ReadFileRequest struct {
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

func (ReadFileRequest) ControlSubtype() string { return SubReadFile }

// ReadFileResponse is the read_file reply.
type ReadFileResponse struct {
	Contents  string `json:"contents"`
	AbsPath   string `json:"absPath"`
	Truncated bool   `json:"truncated,omitempty"`
	Encoding  string `json:"encoding,omitempty"`
}

// FileSuggestionsRequest asks for @-mention path suggestions.
type FileSuggestionsRequest struct {
	Query string `json:"query"`
}

func (FileSuggestionsRequest) ControlSubtype() string { return SubFileSuggestions }

// FileSuggestionsResponse is the file_suggestions reply.
type FileSuggestionsResponse struct {
	Suggestions []FileSuggestion `json:"suggestions"`
	CWD         string           `json:"cwd,omitempty"`
}

// FileSuggestion is one suggested path.
type FileSuggestion struct {
	Path string `json:"path"`
}

// SeedReadStateRequest marks a file as already read at mtime.
type SeedReadStateRequest struct {
	Path  string `json:"path"`
	Mtime int64  `json:"mtime"`
}

func (SeedReadStateRequest) ControlSubtype() string { return SubSeedReadState }

// RegisterRepoRootRequest registers another repository root.
type RegisterRepoRootRequest struct {
	Directory      string `json:"directory"`
	ReloadClaudeMD bool   `json:"reload_claude_md,omitempty"`
	ReloadPlugins  bool   `json:"reload_plugins,omitempty"`
	ReloadSkills   bool   `json:"reload_skills,omitempty"`
}

func (RegisterRepoRootRequest) ControlSubtype() string { return SubRegisterRepoRoot }

// SetColorRequest sets the session color.
type SetColorRequest struct {
	Color string `json:"color"`
}

func (SetColorRequest) ControlSubtype() string { return SubSetColor }

// EndSessionRequest ends the session; the engine exits after replying.
type EndSessionRequest struct {
	Reason string `json:"reason,omitempty"`
}

func (EndSessionRequest) ControlSubtype() string { return SubEndSession }

// RawRequest sends any subtype with a pre-built payload (for unstable subtypes).
type RawRequest struct {
	Subtype string
	Fields  json.RawMessage // a JSON object, or empty
}

func (r RawRequest) ControlSubtype() string { return r.Subtype }

// MarshalJSON writes Fields (or {}).
func (r RawRequest) MarshalJSON() ([]byte, error) {
	if len(r.Fields) == 0 {
		return []byte("{}"), nil
	}
	return r.Fields, nil
}
