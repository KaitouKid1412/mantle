// Package perms models Claude Code permission rules for the /permissions panel.
//
// It parses and prints rule strings in the engine's syntax, merges the rules of every
// settings scope into one view with a source label on each entry, and turns edits
// (add or remove a rule, a working directory, the default mode, an auto-mode rule) into
// patch.Patch values for the config writer. Managed and flag rules are shown but never
// edited.
package perms

import (
	"errors"
	"fmt"
	"strings"
)

// Rule is one permission rule: a tool name with an optional specifier, e.g. "Bash",
// "Bash(git *)", "WebFetch(domain:example.com)" or "mcp__github__*".
//
// Spec is kept exactly as written (escapes included), so String reproduces the input.
type Rule struct {
	Tool    string
	Spec    string
	HasSpec bool
}

// Parse errors.
var (
	ErrEmpty      = errors.New("empty rule")
	ErrNoTool     = errors.New("rule has no tool name")
	ErrBadTool    = errors.New("tool name has invalid characters")
	ErrUnbalanced = errors.New("unbalanced parentheses")
	ErrTrailing   = errors.New("text after the closing parenthesis")
)

// Parse reads a rule string. It does not trim: callers that take user input should
// strings.TrimSpace first. For a valid rule, Parse(s).String() == s.
func Parse(s string) (Rule, error) {
	if s == "" {
		return Rule{}, ErrEmpty
	}
	open := strings.IndexByte(s, '(')
	if open < 0 {
		if strings.IndexByte(s, ')') >= 0 {
			return Rule{}, fmt.Errorf("%q: %w", s, ErrUnbalanced)
		}
		if err := checkTool(s); err != nil {
			return Rule{}, fmt.Errorf("%q: %w", s, err)
		}
		return Rule{Tool: s}, nil
	}
	tool := s[:open]
	if tool == "" {
		return Rule{}, fmt.Errorf("%q: %w", s, ErrNoTool)
	}
	if err := checkTool(tool); err != nil {
		return Rule{}, fmt.Errorf("%q: %w", s, err)
	}
	end, err := matchParen(s, open)
	if err != nil {
		return Rule{}, fmt.Errorf("%q: %w", s, err)
	}
	if end != len(s)-1 {
		return Rule{}, fmt.Errorf("%q: %w", s, ErrTrailing)
	}
	return Rule{Tool: tool, Spec: s[open+1 : end], HasSpec: true}, nil
}

// MustParse is Parse for literals known to be valid; it panics otherwise.
func MustParse(s string) Rule {
	r, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return r
}

// checkTool accepts tool names such as Bash, mcp__my-server__tool and mcp__srv__*.
func checkTool(t string) error {
	if t == "" {
		return ErrNoTool
	}
	for _, c := range t {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_', c == '-', c == '.', c == '*':
		default:
			return ErrBadTool
		}
	}
	return nil
}

// matchParen returns the index of the ')' that closes the '(' at open. A backslash
// escapes the next character, so "\(" and "\)" do not count.
func matchParen(s string, open int) (int, error) {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++ // skip the escaped character
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return -1, ErrUnbalanced
}

// String prints the rule in engine syntax.
func (r Rule) String() string {
	if !r.HasSpec {
		return r.Tool
	}
	return r.Tool + "(" + r.Spec + ")"
}

// UnescapedSpec is Spec with "\(", "\)" and "\\" unescaped.
func (r Rule) UnescapedSpec() string {
	if !strings.Contains(r.Spec, `\`) {
		return r.Spec
	}
	var b strings.Builder
	for i := 0; i < len(r.Spec); i++ {
		if r.Spec[i] == '\\' && i+1 < len(r.Spec) {
			i++
		}
		b.WriteByte(r.Spec[i])
	}
	return b.String()
}

// WholeTool reports whether the rule covers every use of the tool ("Bash", "Bash()",
// "Bash(*)").
func (r Rule) WholeTool() bool {
	return !r.HasSpec || r.Spec == "" || r.Spec == "*"
}

const mcpPrefix = "mcp__"

// IsMCP reports whether the rule targets MCP tools.
func (r Rule) IsMCP() bool { return strings.HasPrefix(r.Tool, mcpPrefix) }

// MCPServer is the server part of an MCP rule ("github" for "mcp__github__create_issue").
func (r Rule) MCPServer() string {
	if !r.IsMCP() {
		return ""
	}
	server, _, _ := strings.Cut(strings.TrimPrefix(r.Tool, mcpPrefix), "__")
	return server
}

// MCPTool is the tool part of an MCP rule: a tool name, "*", or "" for a whole server.
func (r Rule) MCPTool() string {
	if !r.IsMCP() {
		return ""
	}
	_, tool, _ := strings.Cut(strings.TrimPrefix(r.Tool, mcpPrefix), "__")
	return tool
}

// paramTools lists tools whose specifier is "param:value".
var paramTools = map[string][]string{
	"WebFetch": {"domain"},
}

// Param splits a "param:value" specifier for tools that use that form
// (WebFetch(domain:example.com)). ok is false for every other rule.
func (r Rule) Param() (name, value string, ok bool) {
	params, known := paramTools[r.Tool]
	if !known || !r.HasSpec {
		return "", "", false
	}
	name, value, found := strings.Cut(r.Spec, ":")
	if !found {
		return "", "", false
	}
	for _, p := range params {
		if p == name {
			return name, value, true
		}
	}
	return "", "", false
}

// pathTools take a path pattern as their specifier.
var pathTools = map[string]string{
	"Read":         "Reading",
	"Edit":         "Editing",
	"Write":        "Writing",
	"MultiEdit":    "Editing",
	"NotebookEdit": "Editing notebooks",
	"Glob":         "Searching file names",
	"Grep":         "Searching file contents",
}

// Describe is a short plain-language summary of what the rule matches.
func (r Rule) Describe() string {
	if r.IsMCP() {
		server, tool := r.MCPServer(), r.MCPTool()
		if tool == "" || tool == "*" {
			return fmt.Sprintf("Every tool from the %s MCP server", server)
		}
		return fmt.Sprintf("The %s tool from the %s MCP server", tool, server)
	}
	if r.WholeTool() {
		if r.Tool == "Bash" {
			return "Any shell command"
		}
		return fmt.Sprintf("Any use of %s", r.Tool)
	}
	spec := r.UnescapedSpec()
	if name, value, ok := r.Param(); ok {
		if name == "domain" {
			return fmt.Sprintf("Fetching pages from %s", value)
		}
		return fmt.Sprintf("%s with %s %s", r.Tool, name, value)
	}
	switch r.Tool {
	case "Bash", "PowerShell":
		if prefix, ok := strings.CutSuffix(spec, ":*"); ok {
			return fmt.Sprintf("Commands that start with %q", prefix)
		}
		if strings.Contains(spec, "*") {
			return fmt.Sprintf("Commands matching %q", spec)
		}
		return fmt.Sprintf("The command %q", spec)
	case "Agent", "Task":
		return fmt.Sprintf("The %s subagent", spec)
	case "Skill":
		return fmt.Sprintf("The %s skill", spec)
	}
	if verb, ok := pathTools[r.Tool]; ok {
		return fmt.Sprintf("%s %s%s", verb, spec, pathAnchor(spec))
	}
	return fmt.Sprintf("%s matching %q", r.Tool, spec)
}

// pathAnchor explains how a path specifier is anchored.
func pathAnchor(spec string) string {
	switch {
	case strings.HasPrefix(spec, "//"):
		return " (absolute path)"
	case strings.HasPrefix(spec, "~/"):
		return " (in your home directory)"
	case strings.HasPrefix(spec, "/"):
		return " (relative to the settings file's project)"
	}
	return ""
}
