package dialogs

import (
	"path/filepath"
	"strings"
)

// RuleString formats a rule the way settings files spell it: "Bash(npm test:*)" or
// "WebFetch".
func RuleString(r PermissionRule) string {
	if r.RuleContent == "" {
		return r.ToolName
	}
	return r.ToolName + "(" + r.RuleContent + ")"
}

func joinList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// displayPath shows p relative to cwd when it is inside it.
func displayPath(p, cwd string) string {
	p = SanitizeLine(p)
	if cwd == "" || !filepath.IsAbs(p) {
		return p
	}
	rel, err := filepath.Rel(cwd, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p
	}
	if rel == "." {
		return "."
	}
	return rel
}
