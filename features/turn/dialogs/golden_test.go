package dialogs

import (
	"encoding/json"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/KaitouKid1412/mantle/features/turn/gates"
)

// samples builds one of each dialog in a representative state. Run with -update in this
// package to refresh testdata/.
func samples(t *testing.T) map[string]Model {
	t.Helper()
	m := map[string]Model{}

	m["permission_bash"] = NewPermission(toolReq(t, bashReq), PermissionContext{Index: 1, Total: 3})

	m["permission_edit"] = NewPermission(toolReq(t, `{"tool_name":"Edit","tool_use_id":"e",
	  "input":{"file_path":"/work/proj/internal/server/handler.go",
	    "old_string":"func handle(w http.ResponseWriter) {\n\tw.WriteHeader(200)\n}",
	    "new_string":"func handle(w http.ResponseWriter) {\n\tw.WriteHeader(http.StatusOK)\n\tw.Write(nil)\n}"},
	  "permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}]}`),
		PermissionContext{Cwd: "/work/proj"})

	m["permission_write"] = NewPermission(toolReq(t, `{"tool_name":"Write","tool_use_id":"w",
	  "input":{"file_path":"/work/proj/NOTES.md","content":"# Notes\n\n- one\n- two\n"}}`),
		PermissionContext{Cwd: "/work/proj"})

	m["permission_read_blocked"] = NewPermission(toolReq(t, `{"tool_name":"Read","tool_use_id":"r",
	  "input":{"file_path":"/etc/hosts","offset":10,"limit":20},
	  "blocked_path":"/etc/hosts","decision_reason":"Path is outside the working directory",
	  "title":"Claude wants to read hosts",
	  "permission_suggestions":[{"type":"addDirectories","directories":["/etc"],"destination":"session"}]}`),
		PermissionContext{Cwd: "/work/proj", Agent: "code-reviewer", Engine: "Builder mod-a3f"})

	m["permission_webfetch"] = NewPermission(toolReq(t, `{"tool_name":"WebFetch","tool_use_id":"f",
	  "input":{"url":"https://docs.example.com/api/v2/reference?q=1","prompt":"Summarise the auth section"},
	  "permission_suggestions":[{"type":"addRules","rules":[{"toolName":"WebFetch","ruleContent":"domain:docs.example.com"}],"behavior":"allow","destination":"userSettings"}]}`),
		PermissionContext{})

	m["permission_mcp"] = NewPermission(toolReq(t, `{"tool_name":"mcp__github__create_issue","tool_use_id":"m",
	  "input":{"repo":"acme/app","title":"Crash on start"},
	  "mcp_server":{"name":"github","source":"project"}}`), PermissionContext{OfferAuto: true})

	m["permission_generic"] = NewPermission(toolReq(t, `{"tool_name":"Workflow","tool_use_id":"x",
	  "display_name":"Run workflow","description":"Starts a multi-agent workflow",
	  "input":{"name":"review","agents":4},"default_to_no":true,"suppress_always_allow_rule":true}`),
		PermissionContext{})

	feedback := NewPermission(toolReq(t, bashReq), PermissionContext{})
	press(t, feedback, "tab", "'use pnpm instead'")
	m["permission_feedback"] = feedback

	ask, err := NewAskQuestion(toolReq(t, questionsReq), PermissionContext{})
	if err != nil {
		t.Fatal(err)
	}
	press(t, ask, "down")
	m["ask_question"] = ask

	askMulti, _ := NewAskQuestion(toolReq(t, questionsReq), PermissionContext{})
	press(t, askMulti, "1", "space", "down", "down", "down", "enter", "'docs'")
	m["ask_question_multi_other"] = askMulti

	askReview, _ := NewAskQuestion(toolReq(t, questionsReq), PermissionContext{})
	press(t, askReview, "2", "tab")
	m["ask_question_review"] = askReview

	m["plan_approval"] = NewPlanApproval(toolReq(t, `{"tool_name":"ExitPlanMode","tool_use_id":"p",
	  "input":{"plan":"## Plan\n\n1. Add the handler in internal/server.\n2. Cover it with a table test.\n3. Update the README."}}`),
		PlanContext{AutoAvailable: true})

	m["elicitation_form"] = NewElicitation(ElicitationRequest{McpServerName: "crm", Title: "New contact",
		Message: "Fill in the contact details.", RequestedSchema: json.RawMessage(formSchema)}, PermissionContext{})

	m["elicitation_url"] = NewElicitation(ElicitationRequest{McpServerName: "auth", Mode: "url",
		Message: "Sign in to continue.", URL: "https://auth.example.com/device?code=ABCD-1234"}, PermissionContext{})

	m["trust"] = NewTrust(gates.TrustReport{
		Dir:        "/work/proj",
		AllowRules: []gates.SourcedValue{{Value: "Bash(npm test:*)"}, {Value: "Read"}},
		Hooks:      []gates.HookRef{{Event: "PreToolUse", Matcher: "Bash", Kind: "command", Detail: "./scripts/check.sh"}},
		Env:        []gates.SourcedValue{{Value: "NODE_ENV"}},
		Helpers:    []gates.SourcedValue{{Value: "./get-key.sh", Detail: "apiKeyHelper"}},
		McpServers: []gates.McpServer{{Name: "db", Transport: "stdio", Command: "npx", Args: []string{"db-mcp"}}},
		Extras:     []string{".claude/commands"},
	}, false)
	m["trust_empty_home"] = NewTrust(gates.TrustReport{Dir: "/home/user"}, true)
	m["bypass_warning"] = NewBypassWarning()
	m["api_key"] = NewAPIKeyPrompt("ABCDEFGHIJKLMNOPQRST")
	m["auto_mode"] = NewAutoModePrompt()
	m["engine_check"] = NewEngineCheck("2.1.300", []string{"skills", "hooks"}, "2.1.288")
	m["usage_limit"] = NewUsageLimit("3:00 PM")
	m["mcp_approval_one"] = NewMcpApproval([]gates.McpServer{{Name: "db", Transport: "stdio", Command: "npx", Args: []string{"db-mcp"}, EnvKeys: []string{"TOKEN"}}})
	m["mcp_approval_many"] = NewMcpApproval([]gates.McpServer{
		{Name: "db", Transport: "stdio", Command: "npx", Args: []string{"db-mcp"}},
		{Name: "docs", Transport: "http", URL: "https://example.test/mcp"},
	})
	return m
}

func TestGolden(t *testing.T) {
	for name, m := range samples(t) {
		for _, w := range []int{60, 100} {
			t.Run(name+"_"+itoa(w), func(t *testing.T) {
				out := m.View(w, PlainStyles())
				for i, l := range strings.Split(out, "\n") {
					if got := ansi.StringWidth(l); got > w {
						t.Fatalf("line %d is %d cells wide, more than %d: %q", i, got, w, l)
					}
				}
				golden.RequireEqual(t, out+"\n")
			})
		}
	}
}

func testStyles() Styles {
	accent := lipgloss.Color("#d77757")
	return Styles{
		Border:   lipgloss.NewStyle().Foreground(accent),
		Title:    lipgloss.NewStyle().Bold(true),
		Dim:      lipgloss.NewStyle().Faint(true),
		Code:     lipgloss.NewStyle().Foreground(lipgloss.Color("#87afff")),
		Selected: lipgloss.NewStyle().Foreground(accent).Bold(true),
		Accent:   lipgloss.NewStyle().Foreground(accent),
		Error:    lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f5f")),
		Warning:  lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")),
		DiffAdd:  lipgloss.NewStyle().Background(lipgloss.Color("#225c2a")),
		DiffDel:  lipgloss.NewStyle().Background(lipgloss.Color("#7a2a2a")),
		Cursor:   lipgloss.NewStyle().Reverse(true),
	}
}

// Styled rendering must keep the same cell widths as the plain one.
func TestStyledWidths(t *testing.T) {
	st := testStyles()
	for name, m := range samples(t) {
		for _, w := range []int{40, 60, 100, 160} {
			for i, l := range strings.Split(m.View(w, st), "\n") {
				if got := ansi.StringWidth(l); got > w {
					t.Fatalf("%s@%d line %d is %d cells: %q", name, w, i, got, l)
				}
			}
		}
	}
}
