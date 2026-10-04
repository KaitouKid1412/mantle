package chrome

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// TodoStatus is a checklist item's state.
type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
)

// Todo is one checklist item.
type Todo struct {
	ID         string // TaskCreate id; TodoWrite items get their index
	Subject    string
	ActiveForm string // present-tense label shown while in progress
	Status     TodoStatus
}

// Label is what the panel shows: the active form while in progress, else the subject.
func (t Todo) Label() string {
	if t.Status == TodoInProgress && t.ActiveForm != "" {
		return t.ActiveForm
	}
	return t.Subject
}

// Todos is the main conversation's checklist, built from TodoWrite (replaces the
// list) and TaskCreate / TaskUpdate (incremental) tool calls. A call takes effect
// only when its result arrives without an error (the engine rejects tools a model
// doesn't have). Feed it only tool calls without a parent_tool_use_id: subagents keep
// their own lists.
type Todos struct {
	items []Todo
	// Checklist calls waiting for their result.
	pending map[string]pendingCall
}

type pendingCall struct {
	name  string
	input json.RawMessage
}

// OnToolUse records a checklist tool call; nothing changes until its result. It
// reports whether the list changed (always false).
func (t *Todos) OnToolUse(toolUseID, name string, input json.RawMessage) bool {
	switch name {
	case "TodoWrite", "TaskCreate", "TaskUpdate":
		if t.pending == nil {
			t.pending = map[string]pendingCall{}
		}
		t.pending[toolUseID] = pendingCall{name: name, input: input}
	}
	return false
}

var taskNumber = regexp.MustCompile(`#(\d+)`)

// OnToolResult applies a pending call when its result succeeded. A TaskCreate takes its
// id from the structured tool_use_result ({"task":{"id":…}} or {"taskId":…}) or the
// text ("Task #3 created …"). It reports whether the list changed.
func (t *Todos) OnToolResult(toolUseID string, isError bool, text string, structured json.RawMessage) bool {
	call, ok := t.pending[toolUseID]
	if !ok {
		return false
	}
	delete(t.pending, toolUseID)
	if isError {
		return false
	}
	switch call.name {
	case "TodoWrite":
		return t.applyTodoWrite(call.input)
	case "TaskCreate":
		return t.applyCreate(toolUseID, call.input, text, structured)
	case "TaskUpdate":
		return t.applyUpdate(call.input)
	}
	return false
}

func (t *Todos) applyTodoWrite(input json.RawMessage) bool {
	var in struct {
		Todos []struct {
			ID         string `json:"id"`
			Content    string `json:"content"`
			Status     string `json:"status"`
			ActiveForm string `json:"activeForm"`
		} `json:"todos"`
	}
	if json.Unmarshal(input, &in) != nil {
		return false
	}
	items := make([]Todo, 0, len(in.Todos))
	for i, td := range in.Todos {
		id := td.ID
		if id == "" {
			id = strconv.Itoa(i + 1)
		}
		items = append(items, Todo{ID: id, Subject: td.Content, ActiveForm: td.ActiveForm,
			Status: parseStatus(td.Status)})
	}
	t.items = items
	return true
}

func (t *Todos) applyCreate(toolUseID string, input json.RawMessage, text string, structured json.RawMessage) bool {
	var in struct {
		Subject    string `json:"subject"`
		ActiveForm string `json:"activeForm"`
	}
	if json.Unmarshal(input, &in) != nil || in.Subject == "" {
		return false
	}
	td := Todo{Subject: in.Subject, ActiveForm: in.ActiveForm, Status: TodoPending}
	td.ID = structuredID(structured)
	if td.ID == "" {
		if m := taskNumber.FindStringSubmatch(text); m != nil {
			td.ID = m[1]
		}
	}
	if td.ID == "" {
		td.ID = "new-" + toolUseID
	}
	t.items = append(t.items, td)
	return true
}

func (t *Todos) applyUpdate(input json.RawMessage) bool {
	var in struct {
		TaskID     json.RawMessage `json:"taskId"`
		Status     string          `json:"status"`
		Subject    string          `json:"subject"`
		ActiveForm string          `json:"activeForm"`
	}
	if json.Unmarshal(input, &in) != nil {
		return false
	}
	id := rawID(in.TaskID)
	for i := range t.items {
		if t.items[i].ID != id {
			continue
		}
		if in.Status == "deleted" {
			t.items = append(t.items[:i], t.items[i+1:]...)
			return true
		}
		if in.Status != "" {
			t.items[i].Status = parseStatus(in.Status)
		}
		if in.Subject != "" {
			t.items[i].Subject = in.Subject
		}
		if in.ActiveForm != "" {
			t.items[i].ActiveForm = in.ActiveForm
		}
		return true
	}
	return false
}

// Reset empties the list (conversation reset, /clear).
func (t *Todos) Reset() {
	t.items, t.pending = nil, nil
}

// Items returns the checklist in order.
func (t *Todos) Items() []Todo { return t.items }

// Counts returns completed and total items.
func (t *Todos) Counts() (done, total int) {
	for _, it := range t.items {
		if it.Status == TodoCompleted {
			done++
		}
	}
	return done, len(t.items)
}

// Current returns the first in-progress item.
func (t *Todos) Current() (Todo, bool) {
	for _, it := range t.items {
		if it.Status == TodoInProgress {
			return it, true
		}
	}
	return Todo{}, false
}

// AllDone reports whether a non-empty list is fully completed.
func (t *Todos) AllDone() bool {
	done, total := t.Counts()
	return total > 0 && done == total
}

// Window picks at most limit items to show, keeping the work in focus: it starts one
// item before the first unfinished one (for context) and slides back so the window is
// full. hiddenBefore and hiddenAfter count the items left out on each side.
func (t *Todos) Window(limit int) (shown []Todo, hiddenBefore, hiddenAfter int) {
	n := len(t.items)
	if limit <= 0 || n <= limit {
		return t.items, 0, 0
	}
	first := n - 1
	for i, it := range t.items {
		if it.Status != TodoCompleted {
			first = i
			break
		}
	}
	start := min(max(first-1, 0), n-limit)
	return t.items[start : start+limit], start, n - start - limit
}

func parseStatus(s string) TodoStatus {
	switch TodoStatus(strings.ToLower(s)) {
	case TodoInProgress:
		return TodoInProgress
	case TodoCompleted:
		return TodoCompleted
	}
	return TodoPending
}

// rawID accepts a string or number id.
func rawID(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimPrefix(s, "#")
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

func structuredID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v struct {
		Task struct {
			ID json.RawMessage `json:"id"`
		} `json:"task"`
		TaskID json.RawMessage `json:"taskId"`
		ID     json.RawMessage `json:"id"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	for _, r := range []json.RawMessage{v.Task.ID, v.TaskID, v.ID} {
		if id := rawID(r); id != "" {
			return id
		}
	}
	return ""
}
