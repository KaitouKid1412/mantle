package chrome

import (
	"time"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// storyTracker is a fixed set of tasks relative to now.
func storyTracker(now time.Time) *taskTracker {
	tr := newTaskTracker()
	start := now.Add(-2 * time.Minute)
	tr.observe(start, &proto.TaskStarted{TaskID: "a1", ToolUseID: "tu1", SubagentType: "Explore",
		TaskType: "local_agent", Description: "Find every caller of parseExpr"})
	tr.observe(now.Add(-10*time.Second), &proto.TaskProgress{TaskID: "a1", LastToolName: "Grep",
		Usage: &proto.TaskUsage{TotalTokens: 12_345, ToolUses: 9, DurationMS: 110_000}})
	tr.observe(start, &proto.TaskStarted{TaskID: "a2", ToolUseID: "tu2", SubagentType: "reviewer",
		TaskType: "local_agent", Description: "Review the lexer change", IsBackgrounded: true})
	tr.observe(now.Add(-5*time.Second), &proto.TaskNotification{TaskID: "a2", Status: TaskCompleted,
		Usage: &proto.TaskUsage{TotalTokens: 48_100, DurationMS: 95_000}})
	tr.observe(start, &proto.TaskStarted{TaskID: "b1", TaskType: "local_bash",
		Description: "npm run dev", IsBackgrounded: true})
	tr.observe(start, &proto.BackgroundTasksChanged{Tasks: []proto.BackgroundTask{
		{TaskID: "b1", TaskType: "local_bash", Description: "npm run dev"},
		{TaskID: "a2", TaskType: "local_agent", Description: "Review the lexer change"}}})
	return tr
}

func subagentStories() []ext.Story {
	panel := func(id string, focused bool) ext.Story {
		return ext.Story{ID: SubagentsID + "/" + id, Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
			p := newSubagentPanel(storyTracker(ctx.Clock().Now()))
			p.focused, p.sel = focused, 1
			return p.View(ctx, a)
		}}
	}
	tasks := ext.Story{ID: TasksDialogID + "/list", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d := &tasksDialog{tr: storyTracker(ctx.Clock().Now())}
		return d.View(ctx, a)
	}}
	output := ext.Story{ID: TasksDialogID + "/output", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d := &tasksDialog{tr: storyTracker(ctx.Clock().Now()), viewing: "b1",
			output: "> app@1.0.0 dev\n> vite\n\n  VITE ready in 312 ms\n  ➜  Local: http://localhost:5173/\n"}
		return d.View(ctx, a)
	}}
	view := ext.Story{ID: SubagentViewID + "/empty", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		v := &subagentView{args: subagentViewArgs{TaskID: "a1", ToolUseID: "tu1",
			Title: "Explore · Find every caller of parseExpr"}, follow: true}
		a.MaxHeight = 6
		return v.View(ctx, a)
	}}
	return []ext.Story{panel("rows", false), panel("focused", true), tasks, output, view}
}
