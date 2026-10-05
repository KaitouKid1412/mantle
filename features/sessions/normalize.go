package sessions

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// keyResult is the transcript feature's turn-result key ("system.result", Data
// *proto.Result). History turn_duration records use it so finished turns render the
// same way live and on resume.
var keyResult = ext.SystemKey("result")

// NormalizeOptions controls Normalize.
type NormalizeOptions struct {
	EngineID string
	// AcrossCompaction includes the history that compact boundaries summarized.
	AcrossCompaction bool
	// SubagentsDir is the session's subagents directory (sessions.SubagentsDir); when
	// set, subagent transcripts are nested under their Agent tool call.
	SubagentsDir string
	// Leaf, when set, normalizes the branch ending at that message instead of the
	// active one (a rewound conversation before the engine has written to it).
	Leaf string
	// ToolResultsDir is the session's tool-results directory; when set, results the
	// engine cut to a preview (<persisted-output>) get their saved full output back.
	ToolResultsDir string
}

// maxPersisted caps how much of a saved tool output history loads.
const maxPersisted = 1 << 20

var persistedPathRE = regexp.MustCompile(`saved to:\s*(\S+)`)

// persistedOutput replaces a <persisted-output> preview with the full output the
// engine saved under the session's tool-results directory (only there, at most 1 MB).
func (n *normalizer) persistedOutput(c proto.Content) proto.Content {
	dir := n.opts.ToolResultsDir
	text := c.PlainText()
	if dir == "" || !strings.Contains(text, "<persisted-output>") {
		return c
	}
	m := persistedPathRE.FindStringSubmatch(text)
	if m == nil {
		return c
	}
	p := filepath.Clean(m[1])
	if rel, err := filepath.Rel(dir, p); err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return c
	}
	f, err := os.Open(p)
	if err != nil {
		return c
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxPersisted))
	if err != nil || len(b) == 0 {
		return c
	}
	return proto.TextContent(string(b))
}

// Normalize turns a transcript's active branch into transcript items, using the same
// content keys, IDs and Data types as the live transcript store, so history renders
// exactly like live output. Engine bookkeeping (meta messages, most attachments,
// retries) is left out. Subagent items have ParentID set to their Agent tool call.
func Normalize(t *sessions.Transcript, opts NormalizeOptions) []*ext.Item {
	n := &normalizer{opts: opts, byID: map[string]*ext.Item{}}
	if opts.SubagentsDir != "" {
		if subs, err := sessions.ListSubagents(opts.SubagentsDir); err == nil {
			n.subs = subs
		}
	}
	n.inline = sessions.Sidechains(t.Entries)
	bo := sessions.BranchOptions{AcrossCompaction: opts.AcrossCompaction}
	branch := t.Main(bo)
	if opts.Leaf != "" {
		if leaf := t.Tree.Node(opts.Leaf); leaf != nil {
			branch = t.Tree.Branch(leaf, bo)
		}
	}
	n.entries(branch, "", 0)
	return n.items
}

type normalizer struct {
	opts   NormalizeOptions
	items  []*ext.Item
	byID   map[string]*ext.Item
	subs   []sessions.Subagent
	inline map[string][]*sessions.Entry // legacy inline sidechains by agent id
	nested map[string]bool              // agent ids already nested
}

// maxDepth bounds subagent nesting (a subagent can spawn subagents).
const maxDepth = 4

func (n *normalizer) add(it *ext.Item) {
	if it.ID == "" || n.byID[it.ID] != nil {
		return
	}
	it.EngineID = n.opts.EngineID
	if it.End.IsZero() && it.State.Finished() {
		it.End = it.Start
	}
	n.byID[it.ID] = it
	n.items = append(n.items, it)
}

func (n *normalizer) entries(es []*sessions.Entry, parent string, depth int) {
	for _, e := range es {
		switch e.Kind() {
		case sessions.KindAssistant:
			n.assistant(e, parent)
		case sessions.KindUser:
			n.user(e, parent, depth)
		case sessions.KindSystem:
			if parent == "" {
				n.system(e)
			}
		case sessions.KindAttachment:
			if parent == "" {
				n.attachment(e)
			}
		}
	}
	// Tool calls that never got a result were cut off (interrupt, crash, or the
	// session ended mid-call).
	for _, it := range n.items {
		if it.ParentID == parent && it.State == ext.Running {
			it.State = ext.Interrupted
			it.End = it.Start
		}
	}
}

func (n *normalizer) assistant(e *sessions.Entry, parent string) {
	var w struct {
		Message proto.Message `json:"message"`
		Error   string        `json:"error"`
	}
	_ = json.Unmarshal(e.Raw(), &w)
	a := &proto.Assistant{
		Envelope:        proto.Envelope{Type: proto.TypeAssistant, UUID: e.UUID, SessionID: e.SessionID, Raw: e.Raw()},
		Message:         w.Message,
		ParentToolUseID: parent,
		Error:           w.Error,
		RequestID:       e.RequestID,
		Timestamp:       stamp(e.Timestamp),
	}
	if e.IsAPIErrorMessage || a.Error != "" {
		if a.Error == "" {
			a.Error = proto.AssistantErrUnknown
		}
		n.add(&ext.Item{ID: "err:" + e.UUID, ParentID: parent, Key: ext.KeySystemError, Data: a, State: ext.Failed, Start: e.Timestamp})
		return
	}
	if a.Message.Model == proto.SyntheticModel {
		n.add(&ext.Item{ID: "local:" + e.UUID, ParentID: parent, Key: ext.KeySystemLocalCommand, Data: a, State: ext.Done, Start: e.Timestamp})
		return
	}
	for bi := range a.Message.Content {
		b := a.Message.Content[bi]
		it := &ext.Item{ParentID: parent, State: ext.Done, Start: e.Timestamp}
		switch {
		case b.Type == proto.BlockText:
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			// Keep this ID form: the transcript's turn ledger finds a turn's final
			// answer by it to restore the turn line for headless sessions.
			it.ID, it.Key, it.Data = "txt:"+e.UUID+":"+itoa(bi), ext.KeyAssistantText, &b
		case b.Type == proto.BlockThinking || b.Type == proto.BlockRedactedThinking:
			it.ID, it.Key, it.Data = "thk:"+e.UUID+":"+itoa(bi), ext.KeyAssistantThinking, &b
		case b.IsToolUse():
			tu, _ := b.ToolUse()
			it.ID, it.Key, it.Data, it.State = tu.ID, toolKey(tu), &tu, ext.Running
		case b.ToolUseID != "" && strings.HasSuffix(b.Type, "_tool_result"):
			// A server tool's result arrives inside the assistant message.
			if tool := n.byID[b.ToolUseID]; tool != nil {
				res := proto.ToolResult{ToolUseID: b.ToolUseID, ParentToolUseID: parent}
				if b.Content != nil {
					res.Content = *b.Content
				} else if len(b.Raw) > 0 {
					res.Content = proto.Content{Raw: b.Raw}
				}
				n.finishTool(tool, &res, e.Timestamp)
			}
			continue
		default:
			continue
		}
		n.add(it)
	}
}

func toolKey(tu proto.ToolUse) ext.ContentKey {
	if tu.Type == proto.BlockMCPToolUse && tu.ServerName != "" {
		return ext.MCPToolKey(tu.ServerName, tu.Name)
	}
	return ext.ToolKey(tu.Name)
}

func (n *normalizer) finishTool(tool *ext.Item, res *proto.ToolResult, at time.Time) {
	tool.Result = res
	tool.State = ext.Done
	if res.IsError {
		tool.State = ext.Failed
	}
	tool.End = at
}

var (
	commandNameRE = regexp.MustCompile(`(?s)<command-name>(.*?)</command-name>`)
	commandArgsRE = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
	localOutRE    = regexp.MustCompile(`(?s)^<local-command-(stdout|stderr)>(.*?)</local-command-(?:stdout|stderr)>`)
	interruptRE   = regexp.MustCompile(`^\[Request interrupted by user[^\]]*\]$`)
)

func (n *normalizer) user(e *sessions.Entry, parent string, depth int) {
	var w struct {
		Message proto.UserMessage `json:"message"`
	}
	_ = json.Unmarshal(e.Raw(), &w)
	u := &proto.User{
		Envelope:        proto.Envelope{Type: proto.TypeUser, UUID: e.UUID, SessionID: e.SessionID, Raw: e.Raw()},
		Message:         w.Message,
		ParentToolUseID: parent,
		ToolUseResult:   e.ToolUseResult,
		IsReplay:        true,
		Timestamp:       stamp(e.Timestamp),
	}

	for _, r := range u.ToolResults() {
		tool := n.byID[r.ToolUseID]
		if tool == nil {
			continue
		}
		res := r
		res.Content = n.persistedOutput(res.Content)
		n.finishTool(tool, &res, e.Timestamp)
		if depth < maxDepth && isAgentTool(tool.Key) {
			n.nestSubagent(tool, e, depth)
		}
	}
	if parent != "" || e.IsCompactSummary || e.IsVisibleInTranscriptOnly || e.IsToolResult() {
		return
	}

	text := strings.TrimSpace(u.Message.Content.PlainText())
	switch {
	case localOutRE.MatchString(text):
		m := localOutRE.FindStringSubmatch(text)
		out := &proto.LocalCommandOutput{
			Envelope: proto.Envelope{Type: proto.TypeSystem, Subtype: proto.SysLocalCommandOutput, UUID: e.UUID, SessionID: e.SessionID},
			Content:  strings.TrimSpace(m[2]),
		}
		if out.Content != "" {
			n.add(&ext.Item{ID: "sys:" + e.UUID, Key: ext.KeySystemLocalCommand, Data: out, State: ext.Done, Start: e.Timestamp})
		}
		return
	case e.IsMeta:
		return
	case strings.HasPrefix(text, "<local-command-caveat>"):
		return
	case commandNameRE.MatchString(text):
		// A slash command: show it the way it was typed.
		cmd := strings.TrimSpace(commandNameRE.FindStringSubmatch(text)[1])
		if !strings.HasPrefix(cmd, "/") {
			cmd = "/" + cmd
		}
		if m := commandArgsRE.FindStringSubmatch(text); m != nil && strings.TrimSpace(m[1]) != "" {
			cmd += " " + strings.TrimSpace(m[1])
		}
		u.Message.Content = proto.TextContent(cmd)
	case interruptRE.MatchString(text):
		info := &proto.Informational{
			Envelope: proto.Envelope{Type: proto.TypeSystem, Subtype: proto.SysInformational, UUID: e.UUID, SessionID: e.SessionID},
			Content:  "Interrupted by user",
			Level:    "info",
		}
		n.add(&ext.Item{ID: "sys:" + e.UUID, Key: ext.KeySystemInformational, Data: info, State: ext.Done, Start: e.Timestamp})
		return
	case strings.HasPrefix(text, "<bash-stdout>"), strings.HasPrefix(text, "<bash-stderr>"):
		// Output of the preceding `!` command belongs to that item.
		if k := len(n.items); k > 0 && n.items[k-1].Key == ext.KeyUserBash {
			last := n.items[k-1]
			if prev, ok := last.Data.(*proto.User); ok {
				merged := *prev
				merged.Message.Content = proto.TextContent(prev.Message.Content.PlainText() + "\n" + text)
				last.Data = &merged
			}
		}
		return
	case text == "" && !hasImage(u.Message.Content):
		return
	}
	key := ext.KeyUserPrompt
	if strings.HasPrefix(text, "<bash-input>") {
		key = ext.KeyUserBash
	}
	n.add(&ext.Item{ID: "user:" + e.UUID, Key: key, Data: u, State: ext.Done, Start: e.Timestamp})
}

func hasImage(c proto.Content) bool {
	for _, b := range c.Blocks {
		if b.Type == proto.BlockImage {
			return true
		}
	}
	return false
}

func isAgentTool(k ext.ContentKey) bool { return k == ext.ToolKey("Agent") || k == ext.ToolKey("Task") }

// nestSubagent adds the transcript of the subagent an Agent call ran, as children of
// the tool item: from subagents/agent-<id>.jsonl (matched by the meta file's toolUseId,
// or by the agentId in the tool result), or from inline sidechain entries.
func (n *normalizer) nestSubagent(tool *ext.Item, carrier *sessions.Entry, depth int) {
	agentID := ""
	if len(carrier.ToolUseResult) > 0 && carrier.ToolUseResult[0] == '{' {
		var r struct {
			AgentID string `json:"agentId"`
		}
		_ = json.Unmarshal(carrier.ToolUseResult, &r)
		agentID = r.AgentID
	}
	if n.nested == nil {
		n.nested = map[string]bool{}
	}
	for _, s := range n.subs {
		if (s.Meta.ToolUseID == tool.ID || (agentID != "" && s.AgentID == agentID)) && !n.nested[s.AgentID] {
			n.nested[s.AgentID] = true
			sub, err := s.Load()
			if err != nil {
				return
			}
			if leaf := sub.Tree.ActiveLeaf(""); leaf != nil {
				n.entries(sub.Tree.Branch(leaf, sessions.BranchOptions{Sidechains: true}), tool.ID, depth+1)
			}
			return
		}
	}
	if es := n.inline[agentID]; agentID != "" && len(es) > 0 && !n.nested[agentID] {
		n.nested[agentID] = true
		n.entries(es, tool.ID, depth+1)
	}
}

func (n *normalizer) system(e *sessions.Entry) {
	env := proto.Envelope{Type: proto.TypeSystem, Subtype: e.Subtype, UUID: e.UUID, SessionID: e.SessionID, Raw: e.Raw()}
	switch e.Subtype {
	case proto.SysCompactBoundary:
		cb := &proto.CompactBoundary{Envelope: env}
		if m := e.CompactMetadata; m != nil {
			cb.CompactMetadata = proto.CompactMetadata{Trigger: m.Trigger, PreTokens: m.PreTokens, DurationMS: m.DurationMs}
		}
		n.add(&ext.Item{ID: "sys:" + e.UUID, Key: ext.KeySystemCompactBoundary, Data: cb, State: ext.Done, Start: e.Timestamp})
	case proto.SysTurnDuration:
		res := &proto.Result{
			Envelope:   proto.Envelope{Type: proto.TypeResult, Subtype: proto.ResultSuccess, UUID: e.UUID, SessionID: e.SessionID},
			DurationMS: e.Duration.Milliseconds(),
			NumTurns:   1, // the entry marks a finished turn; the renderer skips results with none
		}
		start := e.Timestamp.Add(-e.Duration)
		n.add(&ext.Item{ID: "result:" + e.UUID, Key: keyResult, Data: res, State: ext.Done, Start: start, End: e.Timestamp})
	case "local_command":
		content := e.Content
		if m := localOutRE.FindStringSubmatch(strings.TrimSpace(content)); m != nil {
			content = m[2]
		}
		if content = strings.TrimSpace(content); content != "" {
			out := &proto.LocalCommandOutput{Envelope: env, Content: content}
			n.add(&ext.Item{ID: "sys:" + e.UUID, Key: ext.KeySystemLocalCommand, Data: out, State: ext.Done, Start: e.Timestamp})
		}
	case proto.SysAPIError, "stop_hook_summary":
		// Transient retry notices and hook bookkeeping.
	case proto.SysModelRefusalFallback, proto.SysModelRefusalNoFallback:
		mr := &proto.ModelRefusal{Envelope: env, Content: e.Content}
		n.add(&ext.Item{ID: "sys:" + e.UUID, Key: ext.SystemKey("model_refusal"), Data: mr, State: ext.Done, Start: e.Timestamp})
	default:
		if e.IsMeta || strings.TrimSpace(e.Content) == "" {
			return
		}
		info := &proto.Informational{Envelope: env, Content: e.Content, Level: e.Level}
		n.add(&ext.Item{ID: "sys:" + e.UUID, Key: ext.KeySystemInformational, Data: info, State: ext.Done, Start: e.Timestamp})
	}
}

func (n *normalizer) attachment(e *sessions.Entry) {
	a := e.Attachment
	if a == nil {
		return
	}
	switch a.Type {
	case "queued_command":
		var q struct {
			Prompt      proto.Content `json:"prompt"`
			CommandMode string        `json:"commandMode"`
			IsMeta      bool          `json:"isMeta"`
		}
		if json.Unmarshal(a.Raw, &q) != nil || q.IsMeta {
			return
		}
		u := &proto.User{
			Envelope:  proto.Envelope{Type: proto.TypeUser, UUID: e.UUID, SessionID: e.SessionID},
			Message:   proto.UserMessage{Role: "user", Content: q.Prompt},
			IsReplay:  true,
			Timestamp: stamp(e.Timestamp),
		}
		key := ext.KeyUserPrompt
		if q.CommandMode == "bash" {
			key = ext.KeyUserBash
			u.Message.Content = proto.TextContent("<bash-input>" + q.Prompt.PlainText() + "</bash-input>")
		}
		if strings.TrimSpace(u.Message.Content.PlainText()) == "" && !hasImage(u.Message.Content) {
			return
		}
		n.add(&ext.Item{ID: "user:" + e.UUID, Key: key, Data: u, State: ext.Done, Start: e.Timestamp})
	case "hook_blocking_error", "hook_system_message":
		var h struct {
			Content       json.RawMessage `json:"content"`
			BlockingError json.RawMessage `json:"blockingError"`
			HookName      string          `json:"hookName"`
		}
		if json.Unmarshal(a.Raw, &h) != nil {
			return
		}
		text := looseText(h.Content)
		level := "info"
		if a.Type == "hook_blocking_error" {
			text, level = looseText(h.BlockingError), "warning"
		}
		if text == "" {
			return
		}
		if h.HookName != "" {
			text = h.HookName + ": " + text
		}
		info := &proto.Informational{
			Envelope: proto.Envelope{Type: proto.TypeSystem, Subtype: proto.SysInformational, UUID: e.UUID, SessionID: e.SessionID},
			Content:  text,
			Level:    level,
		}
		n.add(&ext.Item{ID: "sys:" + e.UUID, Key: ext.KeySystemInformational, Data: info, State: ext.Done, Start: e.Timestamp})
	}
}

// looseText returns a JSON string's value, or the "blockingError"/"content" string of
// an object, or "".
func looseText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var o struct {
		BlockingError string `json:"blockingError"`
		Content       string `json:"content"`
	}
	if json.Unmarshal(raw, &o) == nil {
		return strings.TrimSpace(o.BlockingError + o.Content)
	}
	return ""
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
