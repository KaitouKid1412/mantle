package dialogs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// toolView is the per-tool header of a permission prompt.
type toolView struct {
	title    string
	question string
	kind     string // frame kind for BorderColor
	body     func(width int, st Styles) []string
}

// Line budgets for previews inside an inline dialog.
const (
	maxCommandLines = 12
	maxDiffLines    = 24
	maxInputLines   = 10
)

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func boolean(m map[string]any, k string) bool {
	b, _ := m[k].(bool)
	return b
}

func number(m map[string]any, k string) (int, bool) {
	f, ok := m[k].(float64)
	return int(f), ok
}

// prettyJSON indents raw JSON for display; invalid JSON is shown as-is.
func prettyJSON(raw []byte) string {
	var buf bytes.Buffer
	if json.Indent(&buf, raw, "", "  ") != nil {
		return string(raw)
	}
	return buf.String()
}

// describeTool picks the header variant for a request.
func describeTool(req ToolRequest, in map[string]any, cwd string) toolView {
	name := req.ToolName
	v := toolView{kind: "permission", question: "Do you want to proceed?"}
	switch {
	case name == "Bash" || name == "PowerShell":
		v.title = "Bash command"
		if name == "PowerShell" {
			v.title = "PowerShell command"
		}
		if boolean(in, "dangerouslyDisableSandbox") {
			v.title += " (unsandboxed)"
		}
		if boolean(in, "run_in_background") {
			v.title += " (background)"
		}
		cmd, desc := str(in, "command"), str(in, "description")
		v.body = func(w int, st Styles) []string {
			out := styleLines(st.Code, truncateLines(wrapIndent("  ", Sanitize(cmd), w), maxCommandLines, st))
			if desc != "" {
				out = append(out, styleLines(st.Dim, wrapIndent("  ", Sanitize(desc), w))...)
			}
			return out
		}
	case name == "Edit" || name == "MultiEdit":
		path := str(in, "file_path")
		v.title = "Edit file"
		v.question = "Do you want to make this edit to " + baseName(path) + "?"
		var lines []diffLine
		if name == "MultiEdit" {
			edits, _ := in["edits"].([]any)
			for i, e := range edits {
				em, _ := e.(map[string]any)
				if i > 0 {
					lines = append(lines, diffLine{kind: '~'})
				}
				lines = append(lines, diffPreview(str(em, "old_string"), str(em, "new_string"), 2)...)
			}
		} else {
			lines = diffPreview(str(in, "old_string"), str(in, "new_string"), 2)
		}
		replaceAll := boolean(in, "replace_all")
		v.body = func(w int, st Styles) []string {
			out := styleLines(st.Code, wrapIndent("  ", displayPath(path, cwd), w))
			if replaceAll {
				out = append(out, render(st.Dim, "  (every occurrence)"))
			}
			return append(out, renderDiff(lines, w, maxDiffLines, st)...)
		}
	case name == "Write":
		path := str(in, "file_path")
		v.title = "Write file"
		v.question = "Do you want to write " + baseName(path) + "?"
		lines := addedPreview(str(in, "content"))
		v.body = func(w int, st Styles) []string {
			out := styleLines(st.Code, wrapIndent("  ", displayPath(path, cwd), w))
			return append(out, renderDiff(lines, w, maxDiffLines, st)...)
		}
	case name == "NotebookEdit":
		path := str(in, "notebook_path")
		v.title = "Edit notebook"
		v.question = "Do you want to make this edit to " + baseName(path) + "?"
		cell, editMode := str(in, "cell_id"), str(in, "edit_mode")
		lines := addedPreview(str(in, "new_source"))
		v.body = func(w int, st Styles) []string {
			out := styleLines(st.Code, wrapIndent("  ", displayPath(path, cwd), w))
			meta := strings.TrimSpace(strings.Join([]string{editMode, cellLabel(cell)}, " "))
			if meta != "" {
				out = append(out, render(st.Dim, "  "+SanitizeLine(meta)))
			}
			return append(out, renderDiff(lines, w, maxDiffLines, st)...)
		}
	case name == "Read":
		path := str(in, "file_path")
		v.title = "Read file"
		rng := ""
		if off, ok := number(in, "offset"); ok {
			if lim, ok := number(in, "limit"); ok {
				rng = fmt.Sprintf("lines %d–%d", off, off+lim-1)
			} else {
				rng = fmt.Sprintf("from line %d", off)
			}
		} else if lim, ok := number(in, "limit"); ok {
			rng = fmt.Sprintf("first %d lines", lim)
		}
		v.body = func(w int, st Styles) []string {
			out := styleLines(st.Code, wrapIndent("  ", displayPath(path, cwd), w))
			if rng != "" {
				out = append(out, render(st.Dim, "  "+rng))
			}
			return out
		}
	case name == "Glob" || name == "Grep":
		v.title = "Search files"
		pattern, path := str(in, "pattern"), str(in, "path")
		v.body = func(w int, st Styles) []string {
			out := styleLines(st.Code, wrapIndent("  ", SanitizeLine(pattern), w))
			if path != "" {
				out = append(out, styleLines(st.Dim, wrapIndent("  in ", displayPath(path, cwd), w))...)
			}
			return out
		}
	case name == "WebFetch":
		raw := str(in, "url")
		v.title = "Fetch"
		host := raw
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			host = u.Hostname()
		}
		v.question = "Do you want to allow Claude to fetch this content?"
		prompt := str(in, "prompt")
		v.body = func(w int, st Styles) []string {
			out := styleLines(st.Title, wrapIndent("  ", SanitizeLine(host), w))
			out = append(out, styleLines(st.Code, wrapIndent("  ", SanitizeLine(raw), w))...)
			if prompt != "" {
				out = append(out, styleLines(st.Dim, truncateLines(wrapIndent("  ", Sanitize(prompt), w), 3, st))...)
			}
			return out
		}
	case name == "WebSearch":
		v.title = "Web search"
		q := str(in, "query")
		v.body = func(w int, st Styles) []string {
			return styleLines(st.Code, wrapIndent("  ", SanitizeLine(q), w))
		}
	case strings.HasPrefix(name, "mcp__"):
		server, tool := splitMcpName(name)
		if req.McpServer != nil && req.McpServer.Name != "" {
			server = req.McpServer.Name
		}
		source := ""
		if req.McpServer != nil {
			source = req.McpServer.Source
		}
		v.title = "MCP tool"
		args := prettyJSON(req.Input)
		v.body = func(w int, st Styles) []string {
			srv := SanitizeLine(server)
			if source != "" {
				srv += " (" + SanitizeLine(source) + ")"
			}
			out := wrapIndent("  server ", srv, w)
			out = append(out, wrapIndent("  tool   ", SanitizeLine(tool), w)...)
			if args != "" && args != "{}" && args != "null" {
				out = append(out, styleLines(st.Code, truncateLines(wrapIndent("  ", Sanitize(args), w), maxInputLines, st))...)
			}
			return out
		}
	default:
		v.title = req.DisplayName
		if v.title == "" {
			v.title = name
		}
		v.title = SanitizeLine(v.title)
		raw := prettyJSON(req.Input)
		desc := req.Description
		v.body = func(w int, st Styles) []string {
			var out []string
			if desc != "" {
				out = append(out, styleLines(st.Dim, wrapIndent("  ", Sanitize(desc), w))...)
			}
			if raw != "" && raw != "{}" && raw != "null" {
				out = append(out, styleLines(st.Code, truncateLines(wrapIndent("  ", Sanitize(raw), w), maxInputLines, st))...)
			}
			return out
		}
	}
	return v
}

func cellLabel(id string) string {
	if id == "" {
		return ""
	}
	return "cell " + id
}

func baseName(p string) string {
	p = SanitizeLine(p)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	if p == "" {
		return "this file"
	}
	return p
}

// splitMcpName splits "mcp__server__tool" into its parts.
func splitMcpName(name string) (server, tool string) {
	rest := strings.TrimPrefix(name, "mcp__")
	if i := strings.Index(rest, "__"); i >= 0 {
		return rest[:i], rest[i+2:]
	}
	return rest, ""
}
