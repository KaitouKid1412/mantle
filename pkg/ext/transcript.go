package ext

import (
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// ContentKey names what an Item holds, and so which Renderer draws it.
//
// Naming: "<source>.<kind>[.<detail>]".
//   - base: "user.prompt", "user.bash", "assistant.text", "assistant.thinking"
//   - tools: "tool.<ToolName>" ("tool.Bash", "tool.Edit", "tool.Agent"),
//     MCP tools "tool.mcp.<server>.<tool>"
//   - system events: "system.<subtype>" ("system.compact_boundary", "system.api_retry")
//
// A pattern ending in ".*" matches any key with that prefix ("tool.mcp.*"). Renderer
// resolution: exact key, then the longest matching wildcard, then "default".
type ContentKey string

// Base content keys.
const (
	KeyUserPrompt        ContentKey = "user.prompt"
	KeyUserBash          ContentKey = "user.bash"
	KeyAssistantText     ContentKey = "assistant.text"
	KeyAssistantThinking ContentKey = "assistant.thinking"
	KeyDefault           ContentKey = "default"
	KeyMCPTools          ContentKey = "tool.mcp.*"
)

// System event keys.
const (
	KeySystemCompactBoundary ContentKey = "system.compact_boundary"
	KeySystemAPIRetry        ContentKey = "system.api_retry"
	KeySystemInformational   ContentKey = "system.informational"
	KeySystemHook            ContentKey = "system.hook"
	KeySystemRateLimit       ContentKey = "system.rate_limit"
	KeySystemLocalCommand    ContentKey = "system.local_command"
	KeySystemError           ContentKey = "system.error"
)

// ToolKey is the content key for a tool call ("tool.Bash"). MCP tools named
// "mcp__<server>__<tool>" map to "tool.mcp.<server>.<tool>".
func ToolKey(toolName string) ContentKey {
	if rest, ok := strings.CutPrefix(toolName, "mcp__"); ok {
		if server, tool, ok := strings.Cut(rest, "__"); ok {
			return MCPToolKey(server, tool)
		}
	}
	return ContentKey("tool." + toolName)
}

// MCPToolKey is the content key for an MCP tool call.
func MCPToolKey(server, tool string) ContentKey {
	return ContentKey("tool.mcp." + server + "." + tool)
}

// SystemKey is the content key for a system event subtype.
func SystemKey(subtype string) ContentKey { return ContentKey("system." + subtype) }

// Wildcard reports whether k is a pattern ("tool.mcp.*").
func (k ContentKey) Wildcard() bool { return strings.HasSuffix(string(k), ".*") }

// Match reports whether k matches pattern p (exact, or a ".*" prefix pattern).
func (k ContentKey) Match(p ContentKey) bool {
	if p == k || p == KeyDefault {
		return true
	}
	if !p.Wildcard() {
		return false
	}
	prefix := strings.TrimSuffix(string(p), "*")
	return strings.HasPrefix(string(k), prefix)
}

// Candidates lists the keys to try for k, in resolution order: k itself, every
// wildcard from longest to shortest, then "default". "tool.mcp.gh.create" yields
// tool.mcp.gh.create, tool.mcp.gh.*, tool.mcp.*, tool.*, default.
func (k ContentKey) Candidates() []ContentKey {
	out := []ContentKey{k}
	s := string(k)
	for {
		i := strings.LastIndexByte(s, '.')
		if i < 0 {
			break
		}
		s = s[:i]
		out = append(out, ContentKey(s+".*"))
	}
	return append(out, KeyDefault)
}

// ItemState is an item's lifecycle state.
type ItemState int

const (
	Streaming ItemState = iota
	Running
	Done
	Failed
	Interrupted
)

// Finished reports whether the item will not change again.
func (s ItemState) Finished() bool { return s >= Done }

// Item is one transcript entry: a prompt, a block of assistant output, a tool call with
// its result, or a system event.
type Item struct {
	ID, ParentID string // ParentID = parent_tool_use_id
	EngineID     string
	Key          ContentKey
	// Data is the item's payload: *proto.ContentBlock for assistant text and thinking,
	// *proto.ToolUse for tool calls, the concrete proto event type for system events,
	// or a feature-defined type for mantle-generated items.
	Data   any
	Result *proto.ToolResult
	State  ItemState
	Rev    int // bumped on every change; renderers' caches key on (ID, Rev)
	Start  time.Time
	End    time.Time
}

// ViewMode is how much detail the transcript shows.
type ViewMode int

const (
	Normal ViewMode = iota
	Verbose
	Brief
	Focus
	FullTranscript // the ctrl+o viewer
)

// RenderCtx is everything a renderer may depend on. Renderers are pure: same inputs,
// same output.
type RenderCtx struct {
	Width    int
	Mode     ViewMode
	Theme    *theme.Theme
	Expanded bool
	Now      time.Time
	Children []*Item
}

// Block is a rendered item: styled lines, each at most RenderCtx.Width cells.
type Block struct {
	Lines       []string
	Collapsible bool // has more detail when Expanded
}

// Renderer draws an item.
type Renderer func(RenderCtx, *Item) Block

// Transcript is the read-only view of the transcript store (plan 03 implements it).
type Transcript interface {
	Items() []*Item
	Get(id string) *Item
	Committed() int // number of leading items already committed to scrollback
}
