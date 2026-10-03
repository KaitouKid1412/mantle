package chrome

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Task states.
const (
	TaskRunning   = "running"
	TaskCompleted = "completed"
	TaskFailed    = "failed"
	TaskStopped   = "stopped"
)

// subagentLinger is how long a finished subagent stays in the panel.
const subagentLinger = 30 * time.Second

// maxTokenSamples bounds the token history kept per task (subagentStatusLine).
const maxTokenSamples = 20

// taskInfo is what mantle knows about one subagent or background task.
type taskInfo struct {
	ID, ToolUseID string
	Description   string
	SubagentType  string
	TaskType      string
	Workflow      string
	Background    bool
	Ambient       bool
	Status        string
	LastTool      string
	Summary       string
	Error         string
	Tokens        int64
	ToolUses      int64
	DurationMS    int64
	Start, End    time.Time
	TokenSamples  []int
}

// Running reports whether the task has not finished.
func (t *taskInfo) Running() bool { return t.Status == TaskRunning || t.Status == "" }

// IsAgent reports whether the task is a subagent (or workflow) rather than a shell.
func (t *taskInfo) IsAgent() bool {
	return t.SubagentType != "" || t.Workflow != "" || strings.Contains(t.TaskType, "agent") ||
		strings.Contains(t.TaskType, "workflow")
}

// Name is a short label: the subagent type, the workflow or the task type.
func (t *taskInfo) Name() string {
	switch {
	case t.SubagentType != "":
		return t.SubagentType
	case t.Workflow != "":
		return t.Workflow
	case strings.Contains(t.TaskType, "bash") || strings.Contains(t.TaskType, "shell"):
		return "shell"
	case t.TaskType != "":
		return t.TaskType
	}
	return "task"
}

// Elapsed is the task's run time: the engine's figure when known, else wall clock.
func (t *taskInfo) Elapsed(now time.Time) time.Duration {
	if t.DurationMS > 0 && !t.Running() {
		return time.Duration(t.DurationMS) * time.Millisecond
	}
	end := now
	if !t.End.IsZero() {
		end = t.End
	}
	if t.Start.IsZero() || end.Before(t.Start) {
		return time.Duration(t.DurationMS) * time.Millisecond
	}
	return max(end.Sub(t.Start), time.Duration(t.DurationMS)*time.Millisecond)
}

// taskTracker follows task_* and background_tasks_changed events from one engine.
type taskTracker struct {
	tasks map[string]*taskInfo
	order []string
	// background is the engine's current background list (replace semantics).
	background []string
}

func newTaskTracker() *taskTracker { return &taskTracker{tasks: map[string]*taskInfo{}} }

func (t *taskTracker) get(id string, now time.Time) *taskInfo {
	ti, ok := t.tasks[id]
	if !ok {
		ti = &taskInfo{ID: id, Status: TaskRunning, Start: now}
		t.tasks[id] = ti
		t.order = append(t.order, id)
	}
	return ti
}

// observe folds an engine event in; it reports whether anything changed.
func (t *taskTracker) observe(now time.Time, ev proto.Event) bool {
	switch e := ev.(type) {
	case *proto.TaskStarted:
		ti := t.get(e.TaskID, now)
		ti.ToolUseID, ti.SubagentType, ti.TaskType = e.ToolUseID, e.SubagentType, e.TaskType
		ti.Workflow, ti.Background, ti.Ambient = e.WorkflowName, e.IsBackgrounded, e.Ambient
		set(&ti.Description, e.Description)
		ti.Status, ti.Start, ti.End = TaskRunning, now, time.Time{}
	case *proto.TaskProgress:
		ti := t.get(e.TaskID, now)
		set(&ti.Description, e.Description)
		set(&ti.LastTool, e.LastToolName)
		set(&ti.Summary, e.Summary)
		t.usage(ti, e.Usage)
	case *proto.TaskUpdated:
		ti := t.get(e.TaskID, now)
		var p struct {
			Status         string `json:"status"`
			Description    string `json:"description"`
			Error          string `json:"error"`
			IsBackgrounded *bool  `json:"is_backgrounded"`
		}
		if json.Unmarshal(e.Patch, &p) != nil {
			return false
		}
		set(&ti.Description, p.Description)
		set(&ti.Error, p.Error)
		if p.IsBackgrounded != nil {
			ti.Background = *p.IsBackgrounded
		}
		if p.Status != "" {
			t.finish(ti, p.Status, now)
		}
	case *proto.TaskNotification:
		ti := t.get(e.TaskID, now)
		set(&ti.Summary, e.Summary)
		t.usage(ti, e.Usage)
		t.finish(ti, e.Status, now)
	case *proto.BackgroundTasksChanged:
		t.background = t.background[:0]
		for _, b := range e.Tasks {
			ti := t.get(b.TaskID, now)
			set(&ti.TaskType, b.TaskType)
			set(&ti.Description, b.Description)
			ti.Ambient, ti.Background = b.Ambient, true
			t.background = append(t.background, b.TaskID)
		}
	case *proto.ConversationReset:
		*t = *newTaskTracker()
	default:
		return false
	}
	return true
}

func (t *taskTracker) usage(ti *taskInfo, u *proto.TaskUsage) {
	if u == nil {
		return
	}
	ti.Tokens, ti.ToolUses, ti.DurationMS = u.TotalTokens, u.ToolUses, u.DurationMS
	ti.TokenSamples = append(ti.TokenSamples, int(u.TotalTokens))
	if len(ti.TokenSamples) > maxTokenSamples {
		ti.TokenSamples = ti.TokenSamples[len(ti.TokenSamples)-maxTokenSamples:]
	}
}

func (t *taskTracker) finish(ti *taskInfo, status string, now time.Time) {
	switch status {
	case TaskCompleted, TaskFailed, TaskStopped, "killed", "cancelled", "error":
		if status == "killed" || status == "cancelled" {
			status = TaskStopped
		}
		if status == "error" {
			status = TaskFailed
		}
		ti.Status = status
		if ti.End.IsZero() {
			ti.End = now
		}
	default:
		ti.Status = status
	}
}

// agents returns the subagent rows to show: running ones and those finished less than
// linger ago, in start order.
func (t *taskTracker) agents(now time.Time, linger time.Duration) []*taskInfo {
	var out []*taskInfo
	for _, id := range t.order {
		ti := t.tasks[id]
		if ti.Ambient || !ti.IsAgent() {
			continue
		}
		if ti.Running() || now.Sub(ti.End) < linger {
			out = append(out, ti)
		}
	}
	return out
}

// backgroundTasks returns the engine's background list plus backgrounded agents and
// recently finished tasks, for /tasks.
func (t *taskTracker) backgroundTasks(now time.Time, linger time.Duration) []*taskInfo {
	var out []*taskInfo
	for _, id := range t.order {
		ti := t.tasks[id]
		if ti.Ambient {
			continue
		}
		inList := slices.Contains(t.background, id)
		recent := !ti.Running() && now.Sub(ti.End) < linger && (ti.Background || inList)
		if inList || (ti.Background && ti.Running()) || recent {
			out = append(out, ti)
		}
	}
	return out
}

// prune forgets tasks that finished more than linger ago and are not listed by the
// engine; it reports whether any were removed.
func (t *taskTracker) prune(now time.Time, linger time.Duration) bool {
	kept := t.order[:0]
	removed := false
	for _, id := range t.order {
		ti := t.tasks[id]
		if !ti.Running() && now.Sub(ti.End) >= linger && !slices.Contains(t.background, id) {
			delete(t.tasks, id)
			removed = true
			continue
		}
		kept = append(kept, id)
	}
	t.order = kept
	return removed
}

// dismiss removes one finished task.
func (t *taskTracker) dismiss(id string) {
	if ti, ok := t.tasks[id]; ok && !ti.Running() {
		delete(t.tasks, id)
		t.order = slices.DeleteFunc(t.order, func(s string) bool { return s == id })
		t.background = slices.DeleteFunc(t.background, func(s string) bool { return s == id })
	}
}

// formatTokens prints a token count compactly: 950, 12.3k, 1.2M.
func formatTokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0") + "M"
}
