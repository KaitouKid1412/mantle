package engine

import "github.com/KaitouKid1412/mantle/pkg/proto"

// Undocumented control subtypes. They exist in the engine binary but have no public
// schema, so they may change in any release. They live only here, never in pkg/proto.
// Each is supported only on versions it was verified on (and turns off when the
// engine rejects it); callers must use the documented fallback otherwise.
type unstable struct {
	verifiedOn string
	fallback   string
}

var unstableSubtypes = map[string]unstable{
	SubRewindConversation: {"2.1.288", "restart with --resume-session-at=<uuid>"},
	SubForkConversation:   {"2.1.288", "restart with --resume=<id> --fork-session"},
	SubExportConversation: {"2.1.288", "read the session JSONL (plan 06)"},
	SubGetStatus:          {"2.1.288", "compose /status from init data and get_usage"},
	SubSideQuestion:       {"2.1.288", "ask in a forked engine (/btw)"},
	SubSetCwd:             {"2.1.288", "restart the engine in the new cwd"},
	SubAddDirectory:       {"2.1.288", "send /add-dir as a prompt, or restart with --add-dir"},
	SubMCPAuthenticate:    {"2.1.288", "hand off to `claude mcp`"},
	SubMCPClearAuth:       {"2.1.288", "hand off to `claude mcp`"},
	SubClaudeAuthenticate: {"2.1.288", "hand off to `claude auth login`"},
	SubGetMemoryDialog:    {"2.1.288", "list memory files from get_context_usage"},
	SubGetSkillsDialog:    {"2.1.288", "list skills from initialize/init"},
	SubGetSandboxDialog:   {"2.1.288", "read sandbox settings with get_settings"},
	SubGetWorkspaceDiff:   {"2.1.288", "run git diff"},
	SubListDirectory:      {"2.1.288", "read the directory locally"},
}

// Unstable control subtypes.
const (
	SubRewindConversation = "rewind_conversation"
	SubForkConversation   = "fork_conversation"
	SubExportConversation = "export_conversation"
	SubGetStatus          = "get_status"
	SubSideQuestion       = "side_question"
	SubSetCwd             = "set_cwd"
	SubAddDirectory       = "add_directory"
	SubMCPAuthenticate    = "mcp_authenticate"
	SubMCPClearAuth       = "mcp_clear_auth"
	SubClaudeAuthenticate = "claude_authenticate"
	SubGetMemoryDialog    = "get_memory_dialog"
	SubGetSkillsDialog    = "get_skills_dialog"
	SubGetSandboxDialog   = "get_sandbox_dialog"
	SubGetWorkspaceDiff   = "get_workspace_diff"
	SubListDirectory      = "list_directory"
)

// UnstableFallback returns the documented fallback for an unstable subtype.
func UnstableFallback(subtype string) string { return unstableSubtypes[subtype].fallback }

// RewindConversationRequest rewinds the conversation to a message (unstable; field
// names from the 2.1.288 binary).
type RewindConversationRequest struct {
	TargetMessageUUID       string `json:"target_message_uuid"`
	InterruptIfRunning      bool   `json:"interrupt_if_running,omitempty"`
	LastSeenUserMessageUUID string `json:"last_seen_user_message_uuid,omitempty"`
}

// ControlSubtype implements proto.Request.
func (RewindConversationRequest) ControlSubtype() string { return SubRewindConversation }

var _ proto.Request = RewindConversationRequest{}
