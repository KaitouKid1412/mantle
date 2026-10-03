package perms

import (
	"errors"
	"testing"
)

func TestParseRoundTrip(t *testing.T) {
	tests := []struct {
		in      string
		tool    string
		spec    string
		hasSpec bool
	}{
		{"Bash", "Bash", "", false},
		{"Bash()", "Bash", "", true},
		{"Bash(*)", "Bash", "*", true},
		{"Bash(git *)", "Bash", "git *", true},
		{"Bash(npm run test:*)", "Bash", "npm run test:*", true},
		{"WebFetch(domain:example.com)", "WebFetch", "domain:example.com", true},
		{"mcp__github", "mcp__github", "", false},
		{"mcp__github__*", "mcp__github__*", "", false},
		{"mcp__my-server__create_issue", "mcp__my-server__create_issue", "", false},
		{"Read(./src/**)", "Read", "./src/**", true},
		{"Edit(//abs/path/**)", "Edit", "//abs/path/**", true},
		{"Read(~/x)", "Read", "~/x", true},
		{"Read(/docs/*.md)", "Read", "/docs/*.md", true},
		{"Agent(Explore)", "Agent", "Explore", true},
		{"Skill(code-review)", "Skill", "code-review", true},
		{"Bash(echo (nested) ok)", "Bash", "echo (nested) ok", true},
		{`Bash(echo \) and \( escaped)`, "Bash", `echo \) and \( escaped`, true},
		{`Bash(printf "a\\b")`, "Bash", `printf "a\\b"`, true},
		{"Bash(a(b(c)))", "Bash", "a(b(c))", true},
	}
	for _, tt := range tests {
		r, err := Parse(tt.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.in, err)
			continue
		}
		if r.Tool != tt.tool || r.Spec != tt.spec || r.HasSpec != tt.hasSpec {
			t.Errorf("Parse(%q) = %+v", tt.in, r)
		}
		if got := r.String(); got != tt.in {
			t.Errorf("round trip %q -> %q", tt.in, got)
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		in   string
		want error
	}{
		{"", ErrEmpty},
		{"(git *)", ErrNoTool},
		{"Bash(git", ErrUnbalanced},
		{"Bash(a(b)", ErrUnbalanced},
		{"Bash)", ErrUnbalanced},
		{"Bash(a)b", ErrTrailing},
		{"Bash(a)b)", ErrTrailing},
		{"Bash(a) ", ErrTrailing},
		{" Bash", ErrBadTool},
		{"Ba sh(x)", ErrBadTool},
		{`Bash(a\)`, ErrUnbalanced},
	}
	for _, tt := range tests {
		_, err := Parse(tt.in)
		if !errors.Is(err, tt.want) {
			t.Errorf("Parse(%q) err = %v, want %v", tt.in, err, tt.want)
		}
	}
}

func TestMCP(t *testing.T) {
	tests := []struct{ in, server, tool string }{
		{"mcp__github", "github", ""},
		{"mcp__github__*", "github", "*"},
		{"mcp__my_server__create_issue", "my_server", "create_issue"},
		{"Bash", "", ""},
	}
	for _, tt := range tests {
		r := MustParse(tt.in)
		if r.IsMCP() != (tt.server != "") || r.MCPServer() != tt.server || r.MCPTool() != tt.tool {
			t.Errorf("%q: IsMCP=%v server=%q tool=%q", tt.in, r.IsMCP(), r.MCPServer(), r.MCPTool())
		}
	}
}

func TestParam(t *testing.T) {
	name, value, ok := MustParse("WebFetch(domain:example.com)").Param()
	if !ok || name != "domain" || value != "example.com" {
		t.Errorf("WebFetch param: %q %q %v", name, value, ok)
	}
	if _, _, ok := MustParse("Bash(npm run test:*)").Param(); ok {
		t.Error("Bash spec treated as param")
	}
	if _, _, ok := MustParse("WebFetch(example.com)").Param(); ok {
		t.Error("bare WebFetch spec treated as param")
	}
	if _, _, ok := MustParse("WebFetch(host:example.com)").Param(); ok {
		t.Error("unknown param accepted")
	}
}

func TestUnescapedSpec(t *testing.T) {
	r := MustParse(`Bash(echo \(hi\) \\ done)`)
	if got := r.UnescapedSpec(); got != `echo (hi) \ done` {
		t.Errorf("got %q", got)
	}
}

func TestDescribe(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Bash", "Any shell command"},
		{"Bash(*)", "Any shell command"},
		{"Read", "Any use of Read"},
		{"Bash(npm run test:*)", `Commands that start with "npm run test"`},
		{"Bash(git *)", `Commands matching "git *"`},
		{"Bash(make build)", `The command "make build"`},
		{"WebFetch(domain:example.com)", "Fetching pages from example.com"},
		{"mcp__github", "Every tool from the github MCP server"},
		{"mcp__github__*", "Every tool from the github MCP server"},
		{"mcp__github__create_issue", "The create_issue tool from the github MCP server"},
		{"Read(./src/**)", "Reading ./src/**"},
		{"Edit(//etc/hosts)", "Editing //etc/hosts (absolute path)"},
		{"Read(~/notes/**)", "Reading ~/notes/** (in your home directory)"},
		{"Write(/docs/**)", "Writing /docs/** (relative to the settings file's project)"},
		{"Agent(Explore)", "The Explore subagent"},
		{"Skill(code-review)", "The code-review skill"},
		{"NotebookRead(x)", `NotebookRead matching "x"`},
	}
	for _, tt := range tests {
		if got := MustParse(tt.in).Describe(); got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.in, got, tt.want)
		}
	}
}
