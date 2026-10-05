package dialogs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/KaitouKid1412/mantle/features/turn/mode"
)

// suggestionRow is the single "Yes, and …" option built from the engine's
// permission_suggestions, filtered the way Claude Code's prompts filter them:
//   - only the kinds a prompt displays: shell and other tools show rules for that tool
//     plus directory grants; file tools show an accept-edits switch, directory grants
//     and Read rules;
//   - only allow rules, and only project-local or session grants;
//   - when the request was blocked by a path check (blocked_path, or a working-dir or
//     safety decision), only directory grants.
//
// updates holds exactly the suggestions shown, so "always" never grants more than the
// label says.
type suggestionRow struct {
	label   string
	hint    string
	updates []PermissionUpdate
	// session marks a file tool's session grant (accept edits); shift+tab picks it.
	session bool
}

// displayedDestinations are the grant destinations a prompt offers.
var displayedDestinations = map[string]bool{"localSettings": true, "session": true}

// pathCheckReasons are decision reasons whose prompt only offers directory grants.
var pathCheckReasons = map[string]bool{"workingDir": true, "safetyCheck": true, "sandboxOverride": true}

func buildSuggestionRow(req ToolRequest, fileTool bool, cwd string) *suggestionRow {
	if req.SuppressAlwaysAllowRule {
		return nil
	}
	if fileTool && req.ToolName != "Read" {
		if row := claudeFolderRow(req, cwd); row != nil {
			return row
		}
	}
	pathOnly := !fileTool && (req.BlockedPath != "" || pathCheckReasons[req.DecisionReasonType])
	var kept []PermissionUpdate
	for _, s := range req.PermissionSuggestions {
		if s.Destination != "" && !displayedDestinations[s.Destination] {
			continue
		}
		switch s.Type {
		case "addDirectories":
			var dirs []string
			for _, d := range s.Directories {
				if strings.TrimSpace(d) != "" {
					dirs = append(dirs, d)
				}
			}
			if len(dirs) == 0 {
				continue
			}
			if len(dirs) != len(s.Directories) {
				s = PermissionUpdate{Type: s.Type, Directories: dirs, Destination: s.Destination}
			}
			kept = append(kept, s)
		case "addRules":
			if pathOnly || s.Behavior != "allow" {
				continue
			}
			var rules []PermissionRule
			for _, r := range s.Rules {
				switch {
				case r.ToolName == "Read" && r.RuleContent != "":
					rules = append(rules, r)
				case !fileTool && r.ToolName == req.ToolName:
					rules = append(rules, r)
				}
			}
			if len(rules) == 0 {
				continue
			}
			if len(rules) != len(s.Rules) {
				s = PermissionUpdate{Type: s.Type, Rules: rules, Behavior: s.Behavior, Destination: s.Destination}
			}
			kept = append(kept, s)
		case "setMode":
			if fileTool && !pathOnly && mode.Normalize(mode.Mode(s.Mode)) == mode.AcceptEdits {
				kept = append(kept, s)
			}
		}
	}
	if len(kept) == 0 {
		return nil
	}
	row := &suggestionRow{updates: kept}
	if fileTool {
		row.label, row.session = fileRowLabel(kept)
		if req.ToolName != "Read" {
			row.hint = "(shift+tab)"
		}
	} else {
		row.label = toolRowLabel(kept, req.ToolName, cwd)
	}
	return row
}

// claudeFolderRow is the grant for edits inside a .claude folder (the project's or
// ~/.claude), which accept-edits mode never covers. As in Claude Code it replaces the
// engine's suggestions: one session rule for edits anywhere in that folder.
func claudeFolderRow(req ToolRequest, cwd string) *suggestionRow {
	var in map[string]any
	_ = json.Unmarshal(req.Input, &in)
	path := str(in, "file_path")
	if path == "" {
		path = str(in, "notebook_path")
	}
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) && cwd != "" {
		path = filepath.Join(cwd, path)
	}
	rule, label := "", ""
	home, _ := os.UserHomeDir()
	switch {
	case home != "" && within(path, filepath.Join(home, ".claude")):
		rule, label = "~/.claude/**", "Yes, and allow edits in ~/.claude for this session"
	case cwd != "" && within(path, filepath.Join(cwd, ".claude")):
		rule, label = "/.claude/**", "Yes, and allow edits in this project's .claude folder for this session"
	default:
		return nil
	}
	return &suggestionRow{label: label, updates: []PermissionUpdate{{
		Type: "addRules", Rules: []PermissionRule{{ToolName: "Edit", RuleContent: rule}},
		Behavior: "allow", Destination: "session",
	}}}
}

// within reports whether path is inside dir, ignoring case where the file system
// usually does (macOS, Windows).
func within(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		path, dir = strings.ToLower(path), strings.ToLower(dir)
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}

// fileRowLabel phrases a file prompt's session grant.
func fileRowLabel(kept []PermissionUpdate) (string, bool) {
	var parts []string
	session := false
	for _, s := range kept {
		switch s.Type {
		case "setMode":
			parts = append(parts, "switch to accept edits for this session")
			session = true
		case "addDirectories":
			parts = append(parts, "allow access to "+joinList(displayDirs(s.Directories))+" for this session")
		case "addRules":
			var dirs []string
			for _, r := range s.Rules {
				dirs = append(dirs, readRuleDir(r.RuleContent))
			}
			parts = append(parts, "allow reading "+joinList(dirs)+" for this session")
		}
	}
	return "Yes, and " + strings.Join(parts, " and "), session
}

// toolRowLabel phrases a shell or other tool prompt's grant: directories, commands or
// rules, or both.
func toolRowLabel(kept []PermissionUpdate, tool, cwd string) string {
	var dirs, readDirs, cmds, rules []string
	shell := tool == "Bash" || tool == "PowerShell"
	for _, s := range kept {
		switch s.Type {
		case "addDirectories":
			dirs = append(dirs, displayDirs(s.Directories)...)
		case "addRules":
			for _, r := range s.Rules {
				switch {
				case r.ToolName == "Read":
					readDirs = append(readDirs, readRuleDir(r.RuleContent))
				case shell && r.RuleContent != "":
					cmds = append(cmds, commandPrefix(r.RuleContent))
				default:
					rules = append(rules, RuleString(r))
				}
			}
		}
	}
	allDirs := append(append([]string(nil), dirs...), readDirs...)
	where := filepath.Base(cwd)
	if where == "." || where == "/" || cwd == "" {
		where = "this project"
	}
	switch {
	case len(cmds) == 0 && len(rules) == 0 && len(dirs) == 0:
		return "Yes, and allow reading " + joinList(readDirs) + " in this project"
	case len(cmds) == 0 && len(rules) == 0:
		return "Yes, and always allow access to " + joinList(allDirs) + " in this project"
	case len(allDirs) == 0 && len(rules) == 0:
		return "Yes, and don't ask again for " + joinList(quote(cmds)) + " commands in " + where
	case len(allDirs) == 0:
		return "Yes, and don't ask again for " + joinList(append(quote(cmds), rules...))
	}
	return "Yes, and allow " + joinList(allDirs) + " and " + joinList(append(quote(cmds), rules...))
}

func quote(cmds []string) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = "`" + SanitizeLine(c) + "`"
	}
	return out
}

// commandPrefix drops a rule's trailing wildcard: "npm test:*" → "npm test".
func commandPrefix(rule string) string {
	for _, suf := range []string{":*", " *"} {
		if strings.HasSuffix(rule, suf) {
			return strings.TrimSuffix(rule, suf)
		}
	}
	return rule
}

// readRuleDir turns a Read rule's path pattern into a directory: "./src/**" → "src".
func readRuleDir(rule string) string {
	d := strings.TrimSuffix(rule, "/**")
	d = strings.TrimPrefix(d, "./")
	if strings.HasPrefix(d, "//") {
		d = d[1:]
	}
	return SanitizeLine(d)
}

// displayDirs shows directories with the home directory as "~".
func displayDirs(dirs []string) []string {
	home, _ := os.UserHomeDir()
	out := make([]string, len(dirs))
	for i, d := range dirs {
		d = SanitizeLine(d)
		if home != "" && (d == home || strings.HasPrefix(d, home+string(filepath.Separator))) {
			d = "~" + strings.TrimPrefix(d, home)
		}
		out[i] = d
	}
	return out
}
