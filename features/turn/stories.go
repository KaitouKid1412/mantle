package turn

import (
	"encoding/json"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/turn/dialogs"
	"github.com/KaitouKid1412/mantle/features/turn/gates"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// storyRequest decodes a sample can_use_tool body.
func storyRequest(raw string) dialogs.ToolRequest {
	var r dialogs.ToolRequest
	_ = json.Unmarshal([]byte(raw), &r)
	return r
}

// storyKeys presses keys on a view-model to put it in an interesting state.
func storyKeys(m dialogs.Model, keys ...tea.KeyPressMsg) dialogs.Model {
	for _, k := range keys {
		m.HandleKey(k)
	}
	return m
}

var storyDialogs = []struct {
	id    string
	build func() dialogs.Model
}{
	{DialogPermission + "/bash", func() dialogs.Model {
		return dialogs.NewPermission(storyRequest(`{"tool_name":"Bash","tool_use_id":"s","input":{"command":"npm test -- --watch=false","description":"Run the unit tests"},
		  "permission_suggestions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"npm test:*"}],"behavior":"allow","destination":"localSettings"}]}`),
			dialogs.PermissionContext{Index: 1, Total: 2})
	}},
	{DialogPermission + "/edit", func() dialogs.Model {
		return dialogs.NewPermission(storyRequest(`{"tool_name":"Edit","tool_use_id":"s",
		  "input":{"file_path":"/work/app/main.go","old_string":"fmt.Println(\"hi\")","new_string":"fmt.Println(\"hello\")\nos.Exit(0)"},
		  "permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}]}`),
			dialogs.PermissionContext{Cwd: "/work/app"})
	}},
	{DialogPermission + "/subagent-read", func() dialogs.Model {
		return dialogs.NewPermission(storyRequest(`{"tool_name":"Read","tool_use_id":"s","input":{"file_path":"/etc/hosts"},
		  "blocked_path":"/etc/hosts","title":"Claude wants to read hosts",
		  "permission_suggestions":[{"type":"addDirectories","directories":["/etc"],"destination":"session"}]}`),
			dialogs.PermissionContext{Agent: "code-reviewer"})
	}},
	{DialogPermission + "/feedback", func() dialogs.Model {
		p := dialogs.NewPermission(storyRequest(`{"tool_name":"Bash","tool_use_id":"s","input":{"command":"rm -rf build"}}`), dialogs.PermissionContext{})
		storyKeys(p, tea.KeyPressMsg{Code: '2', Text: "2"})
		p.HandlePaste("clean with make clean instead")
		return p
	}},
	{DialogAskUserQuestion + "/two-questions", func() dialogs.Model {
		a, _ := dialogs.NewAskQuestion(storyRequest(`{"tool_name":"AskUserQuestion","tool_use_id":"s","input":{"questions":[
		  {"question":"Which database should the service use?","header":"Database","multiSelect":false,
		   "options":[{"label":"Postgres","description":"Relational, already in the stack"},{"label":"SQLite","description":"Single file, simplest to run","preview":"db, _ := sql.Open(\"sqlite\", \"app.db\")"}]},
		  {"question":"Which checks should CI run?","header":"CI","multiSelect":true,
		   "options":[{"label":"lint","description":""},{"label":"test","description":""}]}]}}`), dialogs.PermissionContext{})
		return storyKeys(a, tea.KeyPressMsg{Code: tea.KeyDown})
	}},
	{DialogPlanApproval + "/plan", func() dialogs.Model {
		return dialogs.NewPlanApproval(storyRequest(`{"tool_name":"ExitPlanMode","tool_use_id":"s",
		  "input":{"plan":"## Plan\n\n1. Add a /health endpoint.\n2. Cover it with a handler test.\n3. Document it in the README."}}`),
			dialogs.PlanContext{AutoAvailable: true})
	}},
	{DialogElicitation + "/form", func() dialogs.Model {
		return dialogs.NewElicitation(dialogs.ElicitationRequest{McpServerName: "tickets", Title: "New ticket", Message: "Describe the bug.",
			RequestedSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","title":"Title"},
			  "severity":{"type":"string","enum":["low","high"]},"urgent":{"type":"boolean"}},"required":["title"]}`)},
			dialogs.PermissionContext{})
	}},
	{DialogElicitation + "/url", func() dialogs.Model {
		return dialogs.NewElicitation(dialogs.ElicitationRequest{McpServerName: "auth", Mode: "url", Message: "Sign in to continue.",
			URL: "https://auth.example.com/device"}, dialogs.PermissionContext{})
	}},
	{DialogTrust + "/project", func() dialogs.Model {
		return dialogs.NewTrust(gates.TrustReport{
			Dir:        "/work/app",
			AllowRules: []gates.SourcedValue{{Value: "Bash(npm test:*)"}},
			Hooks:      []gates.HookRef{{Event: "PostToolUse", Matcher: "Edit", Kind: "command", Detail: "./scripts/format.sh"}},
			McpServers: []gates.McpServer{{Name: "db", Transport: "stdio", Command: "npx", Args: []string{"db-mcp"}}},
		}, false)
	}},
	{DialogBypassWarning + "/default", func() dialogs.Model { return dialogs.NewBypassWarning() }},
	{DialogMcpApproval + "/one", func() dialogs.Model {
		return dialogs.NewMcpApproval([]gates.McpServer{{Name: "db", Transport: "stdio", Command: "npx", Args: []string{"db-mcp"}}})
	}},
	{DialogMcpApproval + "/many", func() dialogs.Model {
		return dialogs.NewMcpApproval([]gates.McpServer{
			{Name: "db", Transport: "stdio", Command: "npx", Args: []string{"db-mcp"}},
			{Name: "docs", Transport: "http", URL: "https://docs.example.com/mcp"},
		})
	}},
	{DialogAPIKey + "/default", func() dialogs.Model { return dialogs.NewAPIKeyPrompt("0123456789abcdefABCD") }},
	{DialogAutoMode + "/default", func() dialogs.Model { return dialogs.NewAutoModePrompt() }},
	{DialogUsageLimit + "/default", func() dialogs.Model { return dialogs.NewUsageLimit("3:00 PM") }},
	{DialogEngineCheck + "/pin", func() dialogs.Model {
		return dialogs.NewEngineCheck("2.1.300", []string{"skills", "hooks"}, "2.1.288")
	}},
}

func (st *state) setupStories(r ext.Registrar) {
	for _, s := range storyDialogs {
		build := s.build
		r.AddStory(ext.Story{
			ID: s.id,
			Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
				return ext.Rendered{Text: build().View(a.Width, stylesFor(c.Theme()))}
			},
		})
	}
	r.AddStory(ext.Story{
		ID: QueueComponentID + "/two",
		Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			return ext.Rendered{Text: renderQueue([]queuedPrompt{
				{UUID: "1", Text: "also update the changelog"},
				{UUID: "2", Text: "and run the linter\nafterwards"},
			}, a.Width, c.Theme())}
		},
	})
}
