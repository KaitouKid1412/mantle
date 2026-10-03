package transcript

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Content keys this package adds to pkg/ext's.
const (
	KeyResult           ext.ContentKey = "system.result"            // Data *proto.Result: turn duration, errors, denials
	KeyPermissionDenied ext.ContentKey = "system.permission_denied" // Data *proto.PermissionDenied
	KeyModelRefusal     ext.ContentKey = "system.model_refusal"     // Data *proto.ModelRefusal
	KeyToolUseSummary   ext.ContentKey = "system.tool_use_summary"  // Data *proto.ToolUseSummary
	KeyTaskNotification ext.ContentKey = "system.task_notification" // Data *proto.TaskNotification
	KeyNotification     ext.ContentKey = "system.notification"      // Data *proto.Notification
)

// Store is the transcript: ordered items with stable IDs and revisions. It is the
// single source for inline commit, the ctrl+o viewer and fullscreen. It implements
// ext.Transcript. Only the UI goroutine touches it.
//
// Top-level items are in Items(); subagent output (parent_tool_use_id) is kept as
// children of its Agent/Task tool item, in Children(id).
type Store struct {
	engineID string
	now      func() time.Time

	items     []*ext.Item
	byID      map[string]*ext.Item
	children  map[string][]*ext.Item
	committed int
	rev       int // bumped on every change

	// streaming state, per parent_tool_use_id ("" = main thread)
	curMsg     map[string]string           // parent → message.id of the message being streamed
	blocks     map[blockKey]*ext.Item      // (parent, message.id, index) → item
	pending    map[msgKey][]*ext.Item      // streamed items not yet matched by an assistant message
	inputs     map[string]*strings.Builder // tool_use id → partial input JSON
	uuids      map[string][]string         // assistant message uuid → item IDs (supersedes)
	tools      map[string]*ToolInfo        // tool_use id → progress
	tasks      map[string]string           // task_id → tool_use id
	retry      *ext.Item                   // the live api_retry item, if any
	hooks      map[string]*ext.Item        // hook_id → item
	notes      []string                    // pending markers for the commit policy
	rateStatus string                      // last rate_limit_event status
	_          struct{}
}

type blockKey struct {
	parent, msg string
	index       int
}

type msgKey struct{ parent, msg string }

// ToolInfo is live progress for a tool call that is not part of its Item.
type ToolInfo struct {
	Elapsed  time.Duration // from tool_progress heartbeats
	TaskID   string
	Task     *proto.TaskUsage // subagent totals (task_progress / task_notification)
	Summary  string           // task_progress summary or last tool
	Status   string           // task_notification status
	LastTool string
}

// NewStore returns an empty store for one engine's events. now is the clock.
func NewStore(engineID string, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	s := &Store{engineID: engineID, now: now}
	s.reset()
	return s
}

func (s *Store) reset() {
	s.items = nil
	s.byID = map[string]*ext.Item{}
	s.children = map[string][]*ext.Item{}
	s.committed = 0
	s.curMsg = map[string]string{}
	s.blocks = map[blockKey]*ext.Item{}
	s.pending = map[msgKey][]*ext.Item{}
	s.inputs = map[string]*strings.Builder{}
	s.uuids = map[string][]string{}
	s.tools = map[string]*ToolInfo{}
	s.tasks = map[string]string{}
	s.hooks = map[string]*ext.Item{}
	s.retry = nil
	s.rev++
}

var _ ext.Transcript = (*Store)(nil)

// Items returns the top-level items in order. Do not modify the slice.
func (s *Store) Items() []*ext.Item { return s.items }

// Get returns any item (top-level or nested) by ID.
func (s *Store) Get(id string) *ext.Item { return s.byID[id] }

// Committed returns the number of leading items already committed to scrollback.
func (s *Store) Committed() int { return s.committed }

// SetCommitted moves the commit watermark (commit policy only).
func (s *Store) SetCommitted(n int) { s.committed = max(0, min(n, len(s.items))) }

// Children returns the items nested under a tool call, in order.
func (s *Store) Children(id string) []*ext.Item { return s.children[id] }

// Tool returns live progress for a tool call (nil if none arrived).
func (s *Store) Tool(id string) *ToolInfo { return s.tools[id] }

// Rev is bumped on every change to the store.
func (s *Store) Rev() int { return s.rev }

// EngineID is the engine whose events the store accepts.
func (s *Store) EngineID() string { return s.engineID }

// TakeNotes returns and clears markers for the commit policy (such as "a
// committed response was replaced").
func (s *Store) TakeNotes() []string {
	n := s.notes
	s.notes = nil
	return n
}

// Note markers.
const NoteReplaced = "replaced"

// Reset empties the store (conversation_reset, /clear, session switch).
func (s *Store) Reset() { s.reset() }

// AppendHistory adds finished items from an earlier session (plan 06's JSONL
// normalizer) at the end of the store. Items with a ParentID are nested.
func (s *Store) AppendHistory(items []*ext.Item) {
	for _, it := range items {
		if it == nil || it.ID == "" || s.byID[it.ID] != nil {
			continue
		}
		if !it.State.Finished() {
			it.State = ext.Done
		}
		s.add(it)
	}
}

// Add appends an item made by another feature (a `!` command, a local notice).
func (s *Store) Add(it *ext.Item) {
	if it == nil || it.ID == "" || s.byID[it.ID] != nil {
		return
	}
	s.add(it)
}

// Update bumps an item's revision after its owner changed it in place.
func (s *Store) Update(id string) {
	if it := s.byID[id]; it != nil {
		s.touch(it)
	}
}

func (s *Store) add(it *ext.Item) {
	if it.EngineID == "" {
		it.EngineID = s.engineID
	}
	if it.Start.IsZero() {
		it.Start = s.now()
	}
	it.Rev++
	s.byID[it.ID] = it
	if it.ParentID != "" {
		s.children[it.ParentID] = append(s.children[it.ParentID], it)
		if p := s.byID[it.ParentID]; p != nil {
			s.touch(p)
		}
	} else {
		s.items = append(s.items, it)
	}
	s.rev++
}

func (s *Store) touch(it *ext.Item) {
	it.Rev++
	s.rev++
	// A child changing changes how its parent renders.
	for p := s.byID[it.ParentID]; p != nil; p = s.byID[p.ParentID] {
		p.Rev++
	}
}

func (s *Store) finish(it *ext.Item, st ext.ItemState) {
	if it.State == st {
		return
	}
	it.State = st
	if st.Finished() {
		it.End = s.now()
	}
	s.touch(it)
}

// remove deletes an item (and its children) from the store.
func (s *Store) remove(id string) {
	it := s.byID[id]
	if it == nil {
		return
	}
	for _, c := range s.children[id] {
		s.remove(c.ID)
	}
	delete(s.children, id)
	delete(s.byID, id)
	if it.ParentID != "" {
		kids := s.children[it.ParentID]
		for i, c := range kids {
			if c == it {
				s.children[it.ParentID] = append(kids[:i:i], kids[i+1:]...)
				break
			}
		}
		if p := s.byID[it.ParentID]; p != nil {
			s.touch(p)
		}
	} else {
		for i, c := range s.items {
			if c == it {
				s.items = append(s.items[:i:i], s.items[i+1:]...)
				if i < s.committed {
					s.committed--
					s.notes = append(s.notes, NoteReplaced)
				}
				break
			}
		}
	}
	s.rev++
}

// Apply folds one engine event into the store. It reports whether anything
// changed.
func (s *Store) Apply(ev proto.Event) bool {
	before := s.rev
	switch e := ev.(type) {
	case *proto.StreamEvent:
		s.applyStream(e)
	case *proto.Assistant:
		s.clearRetry()
		s.applyAssistant(e)
	case *proto.User:
		s.applyUser(e)
	case *proto.Result:
		s.clearRetry()
		s.applyResult(e)
	case *proto.ToolProgress:
		info := s.tool(e.ToolUseID)
		info.Elapsed = time.Duration(e.ElapsedTimeSeconds * float64(time.Second))
		if e.TaskID != "" {
			info.TaskID = e.TaskID
			s.tasks[e.TaskID] = e.ToolUseID
		}
		if it := s.byID[e.ToolUseID]; it != nil {
			s.touch(it)
		}
	case *proto.RateLimitEvent:
		// Warnings and rejections show inline, once per change of status.
		st := e.RateLimitInfo.Status
		if st != s.rateStatus {
			s.rateStatus = st
			if st != "" && st != "allowed" {
				id := "rate:" + e.UUID
				if e.UUID == "" {
					id = "rate:" + itoa(s.rev)
				}
				s.add(&ext.Item{ID: id, Key: ext.KeySystemRateLimit, Data: e, State: ext.Done})
			}
		}
	case *proto.ToolUseSummary:
		s.add(&ext.Item{ID: "summary:" + e.UUID, Key: KeyToolUseSummary, Data: e, State: ext.Done})
	case *proto.ConversationReset:
		s.reset()
	case *proto.CompactBoundary:
		s.add(&ext.Item{ID: "sys:" + e.UUID, Key: ext.KeySystemCompactBoundary, Data: e, State: ext.Done})
	case *proto.APIRetry:
		if s.retry != nil {
			s.retry.Data = e
			s.touch(s.retry)
		} else {
			s.retry = &ext.Item{ID: "retry:" + e.UUID, Key: ext.KeySystemAPIRetry, Data: e, State: ext.Running}
			s.add(s.retry)
		}
	case *proto.Informational:
		s.add(&ext.Item{ID: "sys:" + e.UUID, Key: ext.KeySystemInformational, Data: e, State: ext.Done})
	case *proto.LocalCommandOutput:
		s.add(&ext.Item{ID: "sys:" + e.UUID, Key: ext.KeySystemLocalCommand, Data: e, State: ext.Done})
	case *proto.Hook:
		s.applyHook(e)
	case *proto.PermissionDenied:
		s.add(&ext.Item{ID: "denied:" + e.ToolUseID + ":" + e.UUID, Key: KeyPermissionDenied, Data: e, State: ext.Done})
	case *proto.ModelRefusal:
		for _, u := range e.RetractedMessageUUIDs {
			s.removeMessage(u)
		}
		s.add(&ext.Item{ID: "sys:" + e.UUID, Key: KeyModelRefusal, Data: e, State: ext.Done})
	case *proto.TaskStarted:
		if e.ToolUseID != "" {
			s.tasks[e.TaskID] = e.ToolUseID
			s.tool(e.ToolUseID).TaskID = e.TaskID
			if it := s.byID[e.ToolUseID]; it != nil {
				s.touch(it)
			}
		}
	case *proto.TaskProgress:
		if id := s.tasks[e.TaskID]; id != "" {
			info := s.tool(id)
			info.Task = e.Usage
			info.LastTool = e.LastToolName
			if e.Summary != "" {
				info.Summary = e.Summary
			}
			if it := s.byID[id]; it != nil {
				s.touch(it)
			}
		}
	case *proto.TaskNotification:
		if id := s.tasks[e.TaskID]; id != "" && s.byID[id] != nil {
			info := s.tool(id)
			info.Status = e.Status
			if e.Usage != nil {
				info.Task = e.Usage
			}
			if e.Summary != "" {
				info.Summary = e.Summary
			}
			s.touch(s.byID[id])
		} else {
			s.add(&ext.Item{ID: "task:" + e.TaskID + ":" + e.UUID, Key: KeyTaskNotification, Data: e, State: ext.Done})
		}
	}
	return s.rev != before
}

func (s *Store) tool(id string) *ToolInfo {
	info := s.tools[id]
	if info == nil {
		info = &ToolInfo{}
		s.tools[id] = info
	}
	return info
}

// clearRetry drops the transient api_retry item once the request went through.
func (s *Store) clearRetry() {
	if s.retry != nil {
		s.remove(s.retry.ID)
		s.retry = nil
	}
}

func (s *Store) applyStream(e *proto.StreamEvent) {
	p := e.Event
	parent := e.ParentToolUseID
	switch p.Type {
	case proto.StreamMessageStart:
		if p.Message != nil {
			s.curMsg[parent] = p.Message.ID
		}
	case proto.StreamContentBlockStart:
		if p.ContentBlock == nil {
			return
		}
		msg := s.curMsg[parent]
		b := *p.ContentBlock
		it := &ext.Item{ParentID: parent, State: ext.Streaming}
		switch {
		case b.Type == proto.BlockText:
			it.ID = streamID(msg, parent, p.Index)
			it.Key = ext.KeyAssistantText
			it.Data = &b
		case b.Type == proto.BlockThinking || b.Type == proto.BlockRedactedThinking:
			it.ID = streamID(msg, parent, p.Index)
			it.Key = ext.KeyAssistantThinking
			it.Data = &b
		case b.IsToolUse():
			tu, _ := b.ToolUse()
			if s.byID[tu.ID] != nil {
				return
			}
			tu.Input = nil // streamed as input_json_delta
			it.ID = tu.ID
			it.Key = toolKey(tu)
			it.Data = &tu
			s.inputs[tu.ID] = &strings.Builder{}
		default:
			return
		}
		if s.byID[it.ID] != nil {
			return
		}
		s.add(it)
		s.blocks[blockKey{parent, msg, p.Index}] = it
		mk := msgKey{parent, msg}
		s.pending[mk] = append(s.pending[mk], it)
	case proto.StreamContentBlockDelta:
		it := s.blocks[blockKey{parent, s.curMsg[parent], p.Index}]
		if it == nil || p.Delta == nil || it.State.Finished() {
			return
		}
		switch d := p.Delta; d.Type {
		case proto.DeltaText:
			if b, ok := it.Data.(*proto.ContentBlock); ok {
				b.Text += d.Text
				s.touch(it)
			}
		case proto.DeltaThinking:
			if b, ok := it.Data.(*proto.ContentBlock); ok {
				b.Thinking += d.Thinking
				s.touch(it)
			}
		case proto.DeltaInputJSON:
			if in := s.inputs[it.ID]; in != nil {
				in.WriteString(d.PartialJSON)
				s.touch(it)
			}
		}
	case proto.StreamContentBlockStop:
		it := s.blocks[blockKey{parent, s.curMsg[parent], p.Index}]
		if it == nil || it.State.Finished() {
			return
		}
		if tu, ok := it.Data.(*proto.ToolUse); ok {
			if in := s.inputs[it.ID]; in != nil && json.Valid([]byte(in.String())) {
				tu.Input = json.RawMessage(in.String())
			}
			delete(s.inputs, it.ID)
			it.State = ext.Running
			s.touch(it)
			return
		}
		s.finish(it, ext.Done)
	}
}

// PartialInput returns the tool input JSON streamed so far for a tool call
// whose input is still arriving ("" otherwise).
func (s *Store) PartialInput(id string) string {
	if in := s.inputs[id]; in != nil {
		return in.String()
	}
	return ""
}

func streamID(msg, parent string, index int) string {
	return "blk:" + parent + ":" + msg + ":" + itoa(index)
}

func toolKey(tu proto.ToolUse) ext.ContentKey {
	if tu.Type == proto.BlockMCPToolUse && tu.ServerName != "" {
		return ext.MCPToolKey(tu.ServerName, tu.Name)
	}
	return ext.ToolKey(tu.Name)
}

func (s *Store) applyAssistant(e *proto.Assistant) {
	for _, u := range e.Supersedes {
		s.removeMessage(u)
	}
	parent := e.ParentToolUseID
	msg := e.Message.ID

	// Synthetic messages: local command output and API errors.
	if e.LocalCommandRun != nil || e.LocalCommandSource != "" ||
		(e.Message.Model == proto.SyntheticModel && e.Error == "") {
		s.addFor(e, &ext.Item{ID: "local:" + e.UUID, ParentID: parent, Key: ext.KeySystemLocalCommand, Data: e, State: ext.Done})
		return
	}
	if e.Error != "" {
		s.addFor(e, &ext.Item{ID: "err:" + e.UUID, ParentID: parent, Key: ext.KeySystemError, Data: e, State: ext.Failed})
		return
	}

	for bi := range e.Message.Content {
		b := e.Message.Content[bi]
		it := s.match(parent, msg, b)
		state := ext.Done
		if b.IsToolUse() {
			state = ext.Running
		}
		if it == nil {
			it = &ext.Item{ParentID: parent}
			switch {
			case b.Type == proto.BlockText:
				if strings.TrimSpace(b.Text) == "" {
					continue
				}
				it.Key, it.Data = ext.KeyAssistantText, &b
				it.ID = "txt:" + e.UUID + ":" + itoa(bi)
			case b.Type == proto.BlockThinking || b.Type == proto.BlockRedactedThinking:
				it.Key, it.Data = ext.KeyAssistantThinking, &b
				it.ID = "thk:" + e.UUID + ":" + itoa(bi)
			case b.IsToolUse():
				tu, _ := b.ToolUse()
				if s.byID[tu.ID] != nil {
					continue
				}
				it.Key, it.Data, it.ID = toolKey(tu), &tu, tu.ID
			default:
				continue
			}
			it.State = state
			s.addFor(e, it)
		} else {
			// The finished block is authoritative.
			if b.IsToolUse() {
				tu, _ := b.ToolUse()
				it.Data = &tu
				delete(s.inputs, it.ID)
			} else {
				bb := b
				it.Data = &bb
			}
			s.uuids[e.UUID] = append(s.uuids[e.UUID], it.ID)
			if it.State == ext.Streaming || (it.State == ext.Running && !b.IsToolUse()) {
				it.State = state
			}
			if state.Finished() && it.End.IsZero() {
				it.End = s.now()
			}
			s.touch(it)
		}
		if e.Aborted && !it.State.Finished() {
			s.finish(it, ext.Interrupted)
		}
	}
}

// match finds the streamed item an assistant block finalises.
func (s *Store) match(parent, msg string, b proto.ContentBlock) *ext.Item {
	mk := msgKey{parent, msg}
	list := s.pending[mk]
	for i, it := range list {
		ok := false
		switch {
		case b.IsToolUse():
			ok = it.ID == b.ID
		case b.Type == proto.BlockText:
			ok = it.Key == ext.KeyAssistantText
		default:
			ok = it.Key == ext.KeyAssistantThinking
		}
		if ok {
			s.pending[mk] = append(list[:i:i], list[i+1:]...)
			if len(s.pending[mk]) == 0 {
				delete(s.pending, mk)
			}
			return it
		}
	}
	if b.IsToolUse() {
		return s.byID[b.ID]
	}
	return nil
}

func (s *Store) addFor(e *proto.Assistant, it *ext.Item) {
	if s.byID[it.ID] != nil {
		return
	}
	s.add(it)
	if e.UUID != "" {
		s.uuids[e.UUID] = append(s.uuids[e.UUID], it.ID)
	}
	if it.State.Finished() && it.End.IsZero() {
		it.End = s.now()
	}
}

// removeMessage drops the items an assistant message produced (supersedes,
// model refusal retractions).
func (s *Store) removeMessage(uuid string) {
	for _, id := range s.uuids[uuid] {
		s.remove(id)
	}
	delete(s.uuids, uuid)
}

func (s *Store) applyUser(e *proto.User) {
	// Tool results.
	for _, r := range e.ToolResults() {
		it := s.byID[r.ToolUseID]
		if it == nil {
			continue
		}
		res := r
		it.Result = &res
		st := ext.Done
		if r.IsError {
			st = ext.Failed
		}
		if it.State.Finished() {
			s.touch(it)
		} else {
			s.finish(it, st)
		}
	}
	if !e.IsReplay || e.IsSynthetic || e.ParentToolUseID != "" {
		return
	}
	text := e.Message.Content.PlainText()
	trimmed := strings.TrimSpace(text)
	switch {
	case trimmed == "" && !hasImage(e.Message.Content):
		return
	case strings.HasPrefix(trimmed, "<local-command-stdout>"), strings.HasPrefix(trimmed, "<local-command-stderr>"),
		strings.HasPrefix(trimmed, "<local-command-caveat>"):
		return
	case strings.HasPrefix(trimmed, "<bash-stdout>"), strings.HasPrefix(trimmed, "<bash-stderr>"):
		// Output of the preceding `!` command: attach it to that item.
		if n := len(s.items); n > 0 && s.items[n-1].Key == ext.KeyUserBash {
			last := s.items[n-1]
			if prev, ok := last.Data.(*proto.User); ok {
				merged := *prev
				merged.Message.Content = proto.TextContent(prev.Message.Content.PlainText() + "\n" + text)
				last.Data = &merged
				s.touch(last)
				return
			}
		}
	}
	key := ext.KeyUserPrompt
	if strings.HasPrefix(trimmed, "<bash-input>") {
		key = ext.KeyUserBash
	}
	id := "user:" + e.UUID
	if e.UUID == "" {
		id = "user:" + itoa(s.rev)
	}
	s.add(&ext.Item{ID: id, Key: key, Data: e, State: ext.Done, End: s.now()})
}

func hasImage(c proto.Content) bool {
	for _, b := range c.Blocks {
		if b.Type == proto.BlockImage {
			return true
		}
	}
	return false
}

func (s *Store) applyHook(e *proto.Hook) {
	it := s.hooks[e.HookID]
	if it == nil {
		it = &ext.Item{ID: "hook:" + e.HookID, Key: ext.KeySystemHook, Data: e, State: ext.Running}
		if e.HookID == "" {
			it.ID = "hook:" + e.UUID
		}
		s.hooks[e.HookID] = it
		s.add(it)
	} else {
		it.Data = e
		s.touch(it)
	}
	if e.Subtype == proto.SysHookResponse {
		st := ext.Done
		if e.Outcome != "" && e.Outcome != "success" {
			st = ext.Failed
		}
		delete(s.hooks, e.HookID)
		s.finish(it, st)
	}
}

func (s *Store) applyResult(e *proto.Result) {
	st := ext.Done
	if e.Interrupted() {
		st = ext.Interrupted
	}
	// Nothing still running survives the end of a turn.
	var walk func(items []*ext.Item)
	walk = func(items []*ext.Item) {
		for _, it := range items {
			if !it.State.Finished() {
				if st == ext.Interrupted || it.Result == nil {
					s.finish(it, ext.Interrupted)
				} else {
					s.finish(it, ext.Done)
				}
			}
			walk(s.children[it.ID])
		}
	}
	if e.Interrupted() || e.IsError {
		walk(s.items)
	}
	for k := range s.pending {
		delete(s.pending, k)
	}
	for id := range s.inputs {
		delete(s.inputs, id)
	}
	id := "result:" + e.UUID
	if e.UUID == "" {
		id = "result:" + itoa(s.rev)
	}
	s.add(&ext.Item{ID: id, Key: KeyResult, Data: e, State: ext.Done, End: s.now()})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
