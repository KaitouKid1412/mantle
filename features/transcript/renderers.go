package transcript

import (
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Tools without a dedicated renderer fall to tool.* (name, input summary,
// result text), and anything else to "default".

// oneLinerTools render as a header plus the first line of their result.
var oneLinerTools = []string{
	"TaskStop", "TaskOutput", "BashOutput", "KillShell", "KillBash", "Monitor",
	"EnterWorktree", "ExitWorktree", "SendMessage", "ToolSearch", "ListMcpResources",
	"ReadMcpResource", "LSP", "CronCreate", "CronDelete", "CronList", "ScheduleWakeup",
	"RemoteTrigger", "PushNotification", "ListAgents", "SendUserMessage", "SendUserFile",
	"StructuredOutput", "ProposeGoal", "EndConversation", "Workflow", "ReportFindings",
	"PowerShell", "NotebookRead", "LS",
}

// rendererTable lists the built-in renderers by content key.
func (f *Feature) rendererTable() map[ext.ContentKey]ext.Renderer {
	t := map[ext.ContentKey]ext.Renderer{
		ext.KeyUserPrompt:        f.renderPrompt,
		ext.KeyUserBash:          f.renderBash,
		ext.KeyAssistantText:     f.renderText,
		ext.KeyAssistantThinking: f.renderThinking,

		ext.ToolKey("Bash"):            f.renderBashTool,
		ext.ToolKey("Read"):            f.renderRead,
		ext.ToolKey("Write"):           f.renderWrite,
		ext.ToolKey("Edit"):            f.renderEdit,
		ext.ToolKey("MultiEdit"):       f.renderEdit,
		ext.ToolKey("Glob"):            f.renderSearch,
		ext.ToolKey("Grep"):            f.renderSearch,
		ext.ToolKey("WebFetch"):        f.renderFetch,
		ext.ToolKey("WebSearch"):       f.renderWebSearch,
		ext.ToolKey("Agent"):           f.renderAgent,
		ext.ToolKey("Task"):            f.renderAgent,
		ext.ToolKey("TodoWrite"):       f.renderTodos,
		ext.ToolKey("TaskCreate"):      f.renderTaskTool,
		ext.ToolKey("TaskUpdate"):      f.renderTaskTool,
		ext.ToolKey("TaskList"):        f.renderTaskTool,
		ext.ToolKey("TaskGet"):         f.renderTaskTool,
		ext.ToolKey("ExitPlanMode"):    f.renderExitPlan,
		ext.ToolKey("EnterPlanMode"):   f.renderEnterPlan,
		ext.ToolKey("AskUserQuestion"): f.renderAskUser,
		ext.ToolKey("NotebookEdit"):    f.renderNotebook,
		ext.ToolKey("Skill"):           f.renderSkill,
		ext.KeyMCPTools:                f.renderMCP,
		"tool.*":                       f.renderDefaultTool,

		KeyResult:                    f.renderResult,
		ext.KeySystemError:           f.renderError,
		ext.KeySystemLocalCommand:    f.renderLocalCommand,
		ext.KeySystemCompactBoundary: f.renderCompact,
		ext.KeySystemAPIRetry:        f.renderRetry,
		ext.KeySystemInformational:   f.renderInformational,
		ext.KeySystemHook:            f.renderHook,
		ext.KeySystemRateLimit:       f.renderRateLimit,
		KeyModelRefusal:              f.renderRefusal,
		KeyPermissionDenied:          f.renderDenied,
		KeyToolUseSummary:            f.renderToolSummary,
		KeyTaskNotification:          f.renderTaskNotification,
		KeyNotification:              f.renderNotification,
		ext.KeyDefault:               f.renderUnknown,
	}
	for _, name := range oneLinerTools {
		t[ext.ToolKey(name)] = f.renderOneLiner
	}
	return t
}

func (f *Feature) registerRenderers(r ext.Registrar) {
	f.renderers = f.rendererTable()
	for k, fn := range f.renderers {
		r.AddRenderer(k, fn)
	}
}

// rendererFor resolves a renderer: through the host (mods applied) once it is
// known, else from the built-in table.
func (f *Feature) rendererFor(k ext.ContentKey) ext.Renderer {
	if f.resolve != nil {
		return f.resolve(k)
	}
	for _, cand := range k.Candidates() {
		if r := f.renderers[cand]; r != nil {
			return r
		}
	}
	return f.renderUnknown
}
