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
	// header lines under the title (description, path); block is shown between dashed
	// rules (command, diff, arguments). Either may be nil.
	header, block func(width int, st Styles) []string
	// fileTool marks Edit/Write/NotebookEdit prompts: their suggestions are session
	// grants (accept edits) picked with shift+tab, and they offer no auto-mode switch.
	fileTool bool
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

// editPreview renders one edit with the context's Diff hook, or the plain fallback.
func editPreview(ctx PermissionContext, old, new, path string, w int, st Styles) []string {
	if ctx.Diff != nil {
		return ctx.Diff(SanitizeText(old), SanitizeText(new), path, w, maxDiffLines)
	}
	if old == "" {
		return renderDiff(addedPreview(new), w, maxDiffLines, st)
	}
	return renderDiff(diffPreview(old, new, 2), w, maxDiffLines, st)
}

// describeTool picks the header variant for a request: title, header lines (description,
// path), and the block shown between dashed rules (the command, diff or arguments).
func describeTool(req ToolRequest, in map[string]any, ctx PermissionContext) toolView {
	name := req.ToolName
	cwd := ctx.Cwd
	v := toolView{kind: "permission", question: "Do you want to proceed?"}
	desc := func(s string) func(int, Styles) []string {
		return func(w int, st Styles) []string {
			if s == "" {
				return nil
			}
			return wrap(Sanitize(s), w)
		}
	}
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
		cmd := str(in, "command")
		v.header = desc(str(in, "description"))
		v.block = func(w int, st Styles) []string {
			return styleLines(st.Code, truncateLines(wrap(Sanitize(cmd), w), maxCommandLines, st))
		}
	case name == "Edit" || name == "MultiEdit":
		path := str(in, "file_path")
		v.title, v.fileTool = "Edit file", true
		v.question = "Do you want to make this edit to " + baseName(path) + "?"
		type pair struct{ old, new string }
		var edits []pair
		if name == "MultiEdit" {
			list, _ := in["edits"].([]any)
			for _, e := range list {
				em, _ := e.(map[string]any)
				edits = append(edits, pair{str(em, "old_string"), str(em, "new_string")})
			}
		} else {
			edits = append(edits, pair{str(in, "old_string"), str(in, "new_string")})
		}
		replaceAll := boolean(in, "replace_all")
		v.header = func(w int, st Styles) []string {
			out := wrap(displayPath(path, cwd), w)
			if replaceAll {
				out = append(out, render(st.Dim, "(every occurrence)"))
			}
			return out
		}
		v.block = func(w int, st Styles) []string {
			var diff []string
			for i, e := range edits {
				if i > 0 {
					diff = append(diff, render(st.Dim, "⋯"))
				}
				diff = append(diff, editPreview(ctx, e.old, e.new, path, w, st)...)
			}
			return truncateLines(diff, maxDiffLines, st)
		}
	case name == "Write":
		path := str(in, "file_path")
		v.title, v.fileTool = "Write file", true
		v.question = "Do you want to write " + baseName(path) + "?"
		content := str(in, "content")
		v.header = func(w int, st Styles) []string { return wrap(displayPath(path, cwd), w) }
		v.block = func(w int, st Styles) []string { return editPreview(ctx, "", content, path, w, st) }
	case name == "NotebookEdit":
		path := str(in, "notebook_path")
		v.title, v.fileTool = "Edit notebook", true
		v.question = "Do you want to make this edit to " + baseName(path) + "?"
		cell, editMode := str(in, "cell_id"), str(in, "edit_mode")
		source := str(in, "new_source")
		v.header = func(w int, st Styles) []string {
			out := wrap(displayPath(path, cwd), w)
			if meta := strings.TrimSpace(strings.Join([]string{editMode, cellLabel(cell)}, " ")); meta != "" {
				out = append(out, render(st.Dim, SanitizeLine(meta)))
			}
			return out
		}
		v.block = func(w int, st Styles) []string { return editPreview(ctx, "", source, path, w, st) }
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
		v.header = func(w int, st Styles) []string {
			out := wrap(displayPath(path, cwd), w)
			if rng != "" {
				out = append(out, render(st.Dim, rng))
			}
			return out
		}
	case name == "Glob" || name == "Grep":
		v.title = "Search files"
		pattern, path := str(in, "pattern"), str(in, "path")
		v.header = func(w int, st Styles) []string {
			out := styleLines(st.Code, wrap(SanitizeLine(pattern), w))
			if path != "" {
				out = append(out, styleLines(st.Dim, wrapIndent("in ", displayPath(path, cwd), w))...)
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
		v.header = func(w int, st Styles) []string { return styleLines(st.Title, wrap(SanitizeLine(host), w)) }
		v.block = func(w int, st Styles) []string {
			out := styleLines(st.Code, wrap(SanitizeLine(raw), w))
			if prompt != "" {
				out = append(out, styleLines(st.Dim, truncateLines(wrap(Sanitize(prompt), w), 3, st))...)
			}
			return out
		}
	case name == "WebSearch":
		v.title = "Web search"
		q := str(in, "query")
		v.block = func(w int, st Styles) []string { return styleLines(st.Code, wrap(SanitizeLine(q), w)) }
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
		v.header = func(w int, st Styles) []string {
			srv := SanitizeLine(server)
			if source != "" {
				srv += " (" + SanitizeLine(source) + ")"
			}
			return wrap(srv+" · "+SanitizeLine(tool), w)
		}
		if args != "" && args != "{}" && args != "null" {
			v.block = func(w int, st Styles) []string {
				return styleLines(st.Code, truncateLines(wrap(Sanitize(args), w), maxInputLines, st))
			}
		}
	default:
		v.title = req.DisplayName
		if v.title == "" {
			v.title = name
		}
		v.title = SanitizeLine(v.title)
		raw := prettyJSON(req.Input)
		v.header = desc(req.Description)
		if raw != "" && raw != "{}" && raw != "null" {
			v.block = func(w int, st Styles) []string {
				return styleLines(st.Code, truncateLines(wrap(Sanitize(raw), w), maxInputLines, st))
			}
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
