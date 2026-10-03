package sessions

import (
	"encoding/json"
	"time"
)

// Record kinds (the "type" field of a transcript line).
const (
	KindUser       = "user"
	KindAssistant  = "assistant"
	KindAttachment = "attachment"
	KindSystem     = "system"
	KindProgress   = "progress"

	KindLastPrompt          = "last-prompt"
	KindAITitle             = "ai-title"
	KindCustomTitle         = "custom-title"
	KindSummary             = "summary"
	KindTag                 = "tag"
	KindAgentName           = "agent-name"
	KindCostState           = "cost-state"
	KindMode                = "mode"
	KindPermissionMode      = "permission-mode"
	KindQueueOperation      = "queue-operation"
	KindFileHistorySnapshot = "file-history-snapshot"
	KindFileHistoryDelta    = "file-history-delta"
	KindRelocated           = "relocated"
	KindPRLink              = "pr-link"
	KindWorktreeState       = "worktree-state"
)

// KnownKinds lists the record types the engine (2.1.288) is known to write. Kinds not in
// this set still decode (as *Unknown); the opt-in real-session test reports them so the
// list can be kept current.
var KnownKinds = map[string]bool{
	"user": true, "assistant": true, "attachment": true, "system": true, "progress": true,
	"last-prompt": true, "ai-title": true, "custom-title": true, "summary": true, "tag": true,
	"agent-name": true, "agent-color": true, "agent-setting": true, "cost-state": true,
	"mode": true, "permission-mode": true, "queue-operation": true,
	"file-history-snapshot": true, "file-history-delta": true, "relocated": true,
	"pr-link": true, "worktree-state": true, "continued-in": true,
	"content-replacement": true, "api-request-shape": true, "api-request-blob": true,
	"api-request": true, "fork-context-ref": true, "frame-link": true, "ended-by-model": true,
	"artifact-comment-monitor": true, "artifact-autoreact-ledger": true,
	"bridge-session": true, "history-suppression": true, "attribution-snapshot": true,
	"isolation-latch": true, "dev-mods": true, "memory-mode": true, "atis-latch": true,
	"observer-ref": true,
}

// Pos is where a record was read.
type Pos struct {
	Line   int   // 1-based line number
	Offset int64 // byte offset of the line start
}

// Record is one decoded transcript line. The concrete type is *Entry for message records
// and one of the metadata types below; anything else is *Unknown. Raw always holds the
// line verbatim (minus leading NULs and the newline).
type Record interface {
	Kind() string
	Raw() json.RawMessage
	Pos() Pos
}

type rec struct {
	kind string
	raw  json.RawMessage
	pos  Pos
}

func (r *rec) Kind() string         { return r.kind }
func (r *rec) Raw() json.RawMessage { return r.raw }
func (r *rec) Pos() Pos             { return r.pos }

// Entry is a message record (user, assistant, attachment, system, progress): a node of
// the parentUuid tree.
type Entry struct {
	rec

	UUID       string
	ParentUUID string // "" for a chain root
	// LogicalParentUUID is set on a compact boundary: the leaf the boundary replaces.
	LogicalParentUUID string
	IsSidechain       bool
	Timestamp         time.Time
	SessionID         string
	AgentID           string // set in subagent transcripts
	Cwd               string
	GitBranch         string
	Version           string
	Entrypoint        string
	UserType          string
	SessionKind       string
	Slug              string
	IsMeta            bool
	PromptID          string
	PermissionMode    string

	// Message is the API message for user and assistant records.
	Message *Message

	// User records.
	ToolUseResult             json.RawMessage
	SourceToolAssistantUUID   string
	SourceToolUseID           string
	IsCompactSummary          bool
	IsVisibleInTranscriptOnly bool

	// Assistant records.
	RequestID         string
	IsAPIErrorMessage bool

	// Attachment records.
	Attachment *Attachment

	// System records.
	Subtype         string
	Content         string
	Level           string
	Duration        time.Duration // turn_duration
	MessageCount    int           // turn_duration
	CompactMetadata *CompactMetadata
}

// IsCompactBoundary reports whether e is a system compact_boundary record.
func (e *Entry) IsCompactBoundary() bool {
	return e.kind == KindSystem && e.Subtype == "compact_boundary"
}

// Message is an Anthropic API message as stored in the transcript.
type Message struct {
	ID         string  `json:"id,omitempty"`
	Role       string  `json:"role,omitempty"`
	Model      string  `json:"model,omitempty"`
	Content    Content `json:"content"`
	StopReason string  `json:"stop_reason,omitempty"`
	Usage      *Usage  `json:"usage,omitempty"`
}

// Usage is the token usage of an assistant message.
type Usage struct {
	InputTokens              int64          `json:"input_tokens"`
	OutputTokens             int64          `json:"output_tokens"`
	CacheCreationInputTokens int64          `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64          `json:"cache_read_input_tokens"`
	ServiceTier              string         `json:"service_tier,omitempty"`
	ServerToolUse            *ServerToolUse `json:"server_tool_use,omitempty"`
}

// ServerToolUse counts server-side tool calls in Usage.
type ServerToolUse struct {
	WebSearchRequests int64 `json:"web_search_requests"`
	WebFetchRequests  int64 `json:"web_fetch_requests"`
}

// Attachment is the payload of an attachment record. Its fields vary by Type; Raw holds
// the whole object.
type Attachment struct {
	Type string
	Raw  json.RawMessage
}

// CompactMetadata describes a compact boundary.
type CompactMetadata struct {
	Trigger            string            `json:"trigger"` // manual or auto
	PreTokens          int64             `json:"preTokens"`
	MessagesSummarized int               `json:"messagesSummarized"`
	DurationMs         int64             `json:"durationMs"`
	PreservedSegment   *PreservedSegment `json:"preservedSegment,omitempty"`
}

// PreservedSegment names the messages a partial compaction kept verbatim.
type PreservedSegment struct {
	HeadUUID   string `json:"headUuid"`
	AnchorUUID string `json:"anchorUuid"`
	TailUUID   string `json:"tailUuid"`
}

// LastPrompt is the last-prompt record: the latest prompt text and the resume leaf.
type LastPrompt struct {
	rec
	SessionID  string
	LastPrompt string
	LeafUUID   string
}

// AITitle is an ai-title record (auto-generated session title).
type AITitle struct {
	rec
	SessionID string
	Title     string
}

// CustomTitle is a custom-title record (a name the user gave the session).
type CustomTitle struct {
	rec
	SessionID string
	Title     string
}

// AgentName is an agent-name record (the session's display name).
type AgentName struct {
	rec
	SessionID string
	Name      string
}

// Summary is a summary record (older engines' session title).
type Summary struct {
	rec
	Summary  string
	LeafUUID string
}

// Tag is a tag record.
type Tag struct {
	rec
	SessionID string
	Tag       string
}

// Mode is a mode record (normal, plan, …; engine-defined).
type Mode struct {
	rec
	SessionID string
	Mode      string
}

// PermissionMode is a permission-mode record.
type PermissionMode struct {
	rec
	SessionID      string
	PermissionMode string
}

// Relocated records that the session moved to another cwd.
type Relocated struct {
	rec
	SessionID    string
	RelocatedCwd string
}

// PRLink links the session to a pull request.
type PRLink struct {
	rec
	SessionID    string
	PRNumber     int
	PRURL        string
	PRRepository string
	Timestamp    time.Time
}

// WorktreeState records the worktree the session runs in (engine-defined object).
type WorktreeState struct {
	rec
	SessionID       string
	WorktreeSession json.RawMessage
}

// QueueOperation is a queue-operation record (prompt queue enqueue/dequeue/…).
type QueueOperation struct {
	rec
	SessionID string
	Operation string
	Content   string
	Timestamp time.Time
}

// ModelUsage is one model's token and cost totals in a cost-state record.
type ModelUsage struct {
	InputTokens              int64   `json:"inputTokens"`
	OutputTokens             int64   `json:"outputTokens"`
	ThinkingTokens           int64   `json:"thinkingTokens,omitempty"`
	CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
	WebSearchRequests        int64   `json:"webSearchRequests"`
	CostUSD                  float64 `json:"costUSD"`
}

// CostState is the cost-state record: the session's running totals.
type CostState struct {
	rec
	SessionID                 string
	TotalCostUSD              float64
	APIDuration               time.Duration
	APIDurationWithoutRetries time.Duration
	ToolDuration              time.Duration
	Duration                  time.Duration
	LinesAdded                int64
	LinesRemoved              int64
	StartTime                 time.Time
	ModelUsage                map[string]ModelUsage
	HasUnknownModelCost       bool
}

// FileBackup is one tracked file in a file-history snapshot.
type FileBackup struct {
	BackupFileName string // "" when the file did not exist yet
	Version        int
	BackupTime     time.Time
}

// FileHistorySnapshot is a file-history-snapshot record: the tracked-file backups taken
// before the user message MessageID. Rewind restores from these.
type FileHistorySnapshot struct {
	rec
	MessageID        string
	IsSnapshotUpdate bool
	Timestamp        time.Time
	Backups          map[string]FileBackup // by tracked path
}

// FileHistoryDelta is a file-history-delta record (one backup added to a snapshot).
type FileHistoryDelta struct {
	rec
	MessageID         string
	SnapshotMessageID string
	TrackingPath      string
	Backup            json.RawMessage
	Timestamp         time.Time
}

// Unknown is a record of a type the reader has no struct for. Kind() is its "type"
// field ("" if absent).
type Unknown struct {
	rec
}
