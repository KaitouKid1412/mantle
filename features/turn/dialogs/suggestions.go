package dialogs

import (
	"path/filepath"
	"strings"

	"github.com/KaitouKid1412/mantle/features/turn/mode"
)

// RuleString formats a rule the way settings files spell it: "Bash(npm test:*)" or
// "WebFetch".
func RuleString(r PermissionRule) string {
	if r.RuleContent == "" {
		return r.ToolName
	}
	return r.ToolName + "(" + r.RuleContent + ")"
}

// destinationSuffix says where a grant lives, in words.
func destinationSuffix(dest string) string {
	switch dest {
	case "session":
		return " for this session"
	case "localSettings":
		return " in this project"
	case "projectSettings":
		return " in this project (shared)"
	case "userSettings":
		return " in every project"
	}
	return ""
}

// suggestionPhrase describes one suggestion as the tail of "Yes, and …". Only
// suggestions sent by the engine are ever offered; mantle never invents rules.
func suggestionPhrase(u PermissionUpdate, cwd string) string {
	suffix := destinationSuffix(u.Destination)
	switch u.Type {
	case "addRules", "replaceRules":
		rules := make([]string, len(u.Rules))
		for i, r := range u.Rules {
			rules[i] = RuleString(r)
		}
		verb := "don't ask again for "
		switch u.Behavior {
		case "deny":
			verb = "always deny "
		case "ask":
			verb = "always ask for "
		}
		return verb + joinList(rules) + suffix
	case "addDirectories":
		dirs := make([]string, len(u.Directories))
		for i, d := range u.Directories {
			dirs[i] = displayPath(d, cwd) + string(filepath.Separator)
		}
		return "allow access to " + joinList(dirs) + suffix
	case "setMode":
		m, _ := mode.Parse(u.Mode)
		switch m {
		case mode.AcceptEdits:
			return "allow all edits" + suffix
		case mode.BypassPermissions:
			return "bypass permissions" + suffix
		}
		return "switch to " + strings.ToLower(mode.Label(m)) + " mode" + suffix
	case "removeRules":
		rules := make([]string, len(u.Rules))
		for i, r := range u.Rules {
			rules[i] = RuleString(r)
		}
		return "remove the rule " + joinList(rules) + suffix
	case "removeDirectories":
		return "remove " + joinList(u.Directories) + " from allowed directories" + suffix
	}
	return "apply the suggested permission update" + suffix
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
