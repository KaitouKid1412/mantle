package transcript

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/render"
)

// tools2_1_288 is the built-in tool list of Claude Code 2.1.288 (current and
// legacy names), from docs/research/inventory-binary-2.1.288.md §6.
var tools2_1_288 = []string{
	"Bash", "PowerShell", "Read", "Write", "Edit", "Glob", "Grep", "NotebookEdit", "LSP",
	"Agent", "Task", "SendMessage", "ListAgents", "ListPeers", "SubagentHandback",
	"WebFetch", "WebSearch",
	"TodoWrite", "TaskCreate", "TaskUpdate", "TaskGet", "TaskList", "TaskStop", "KillShell",
	"KillBash", "GetTask", "Monitor",
	"EnterPlanMode", "ExitPlanMode", "EnterWorktree", "ExitWorktree",
	"AskUserQuestion", "Skill", "ToolSearch", "SendUserMessage", "Brief", "SendUserFile",
	"SendFile", "PushNotification",
	"CronCreate", "CronDelete", "CronList", "ScheduleWakeup", "RemoteTrigger",
	"Workflow", "StructuredOutput", "ReportFindings", "ProposeGoal", "EndConversation", "Poll",
	"ReadNotifications", "FetchInboxMessage",
	"ListMcpResourcesTool", "ReadMcpResourceTool", "ReadMcpResourceDirTool",
	"Artifact", "AppifactRepl", "Projects", "ClaudeDesign", "DesignSync", "SuggestSkills",
	"SuggestConnectors", "SearchMcpRegistry", "ListConnectors", "SuggestPluginInstall",
	"SendFeedback", "ShareOnboardingGuide", "ShowOnboardingRolePicker", "REPL",
	"memory_list", "memory_read", "memory_write",
	"MultiEdit", "NotebookRead", "LS", "TaskOutput", "BashOutput",
}

// sampleInput gives each tool an input with the fields its renderer reads.
var sampleInput = map[string]any{
	"Bash":            map[string]any{"command": "ls -la", "description": "List"},
	"Read":            map[string]any{"file_path": "/w/a.go", "offset": 10, "limit": 20},
	"Write":           map[string]any{"file_path": "/w/new.go", "content": "package w\n"},
	"Edit":            map[string]any{"file_path": "/w/a.go", "old_string": "a", "new_string": "b"},
	"MultiEdit":       map[string]any{"file_path": "/w/a.go", "edits": []any{map[string]any{"old_string": "a", "new_string": "b"}}},
	"Glob":            map[string]any{"pattern": "**/*.go"},
	"Grep":            map[string]any{"pattern": "TODO", "path": "/w"},
	"WebFetch":        map[string]any{"url": "https://example.com", "prompt": "summarise"},
	"WebSearch":       map[string]any{"query": "go generics"},
	"Agent":           map[string]any{"description": "look", "subagent_type": "Explore"},
	"Task":            map[string]any{"description": "look"},
	"TodoWrite":       map[string]any{"todos": []any{map[string]any{"content": "a", "status": "pending"}}},
	"TaskCreate":      map[string]any{"subject": "Write tests"},
	"TaskUpdate":      map[string]any{"taskId": "3", "status": "completed"},
	"ExitPlanMode":    map[string]any{"plan": "1. Do it"},
	"AskUserQuestion": map[string]any{"questions": []any{map[string]any{"question": "Which?", "header": "H"}}},
	"NotebookEdit":    map[string]any{"notebook_path": "/w/n.ipynb", "cell_id": "c1", "new_source": "x = 1"},
	"Skill":           map[string]any{"skill": "review"},
	"SendUserMessage": map[string]any{"message": "Done **now**."},
}

func TestEveryToolRenders(t *testing.T) {
	f, _ := setupFeature(t)
	f.renderers = f.rendererTable()
	names := append(append([]string{}, tools2_1_288...), "mcp__srv__do_thing", "TotallyNewTool")
	for _, name := range names {
		in := sampleInput[name]
		if in == nil {
			in = map[string]any{"arg": "value"}
		}
		raw, _ := json.Marshal(in)
		tu := &proto.ToolUse{Type: proto.BlockToolUse, ID: "t_" + name, Name: name, Input: raw}
		for _, state := range []ext.ItemState{ext.Running, ext.Done, ext.Failed, ext.Interrupted} {
			it := &ext.Item{ID: tu.ID, Key: ext.ToolKey(name), Data: tu, State: state}
			switch state {
			case ext.Done:
				it.Result = &proto.ToolResult{ToolUseID: tu.ID, Content: proto.TextContent("result line 1\nresult line 2")}
			case ext.Failed:
				it.Result = &proto.ToolResult{ToolUseID: tu.ID, Content: proto.TextContent("it broke"), IsError: true}
			}
			for _, w := range []int{20, 40, 100} {
				rc := ext.RenderCtx{Width: w}
				lines := f.rendererFor(it.Key)(rc, it).Lines
				if len(lines) == 0 {
					t.Fatalf("%s (%v) rendered nothing at %d", name, state, w)
				}
				checkLines(t, lines, w)
				if state == ext.Failed && !strings.Contains(render.Strip(strings.Join(lines, " ")), "it broke") &&
					!strings.Contains(render.Strip(strings.Join(lines, " ")), "Done") {
					t.Errorf("%s: error text missing: %q", name, lines)
				}
			}
		}
	}
}
