package discovery

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill kinds.
const (
	KindSkill   = "skill"   // <dir>/SKILL.md
	KindCommand = "command" // commands/<name>.md
)

// syncedPrefix namespaces skills synced from claude.ai.
const syncedPrefix = "anthropic-skills"

// Skill is a skill or custom slash command found on disk.
type Skill struct {
	Name           string // as typed after "/": "deploy", "demo:status", "anthropic-skills:pdf"
	Kind           string // KindSkill or KindCommand
	Description    string
	ArgumentHint   string
	WhenToUse      string
	UserInvocable  bool // false hides it from the / menu
	ModelInvocable bool // false when disable-model-invocation is set
	Path           string
	Dir            string // the skill directory (skills only)
	Scope          Scope
	Plugin         string // plugin name for ScopePlugin
	Namespace      string // command subdirectory, e.g. "frontend" for commands/frontend/x.md
	Nested         bool   // found in a .claude directory below the cwd
	Frontmatter    Frontmatter
	Err            error // read or frontmatter error
}

// Skills scans every skill and command location: managed, user, claude.ai
// synced, project (cwd and its parents), nested project directories below the
// cwd (Roots.NestedDepth levels), and enabled plugins. Items come in that order;
// within a location they are sorted by path.
func Skills(r Roots, settings []SettingsFile) []Skill {
	var out []Skill
	if r.ManagedDir != "" {
		out = append(out, skillDir(filepath.Join(r.ManagedDir, ".claude", "skills"), ScopeManaged, "")...)
		out = append(out, commandDir(filepath.Join(r.ManagedDir, ".claude", "commands"), ScopeManaged, "")...)
	}
	if r.ConfigDir != "" {
		out = append(out, skillDir(filepath.Join(r.ConfigDir, "skills"), ScopeUser, "")...)
		out = append(out, commandDir(filepath.Join(r.ConfigDir, "commands"), ScopeUser, "")...)
		out = append(out, syncedSkills(filepath.Join(r.ConfigDir, "skills", "synced"))...)
	}
	for _, d := range projectDirs(r) {
		out = append(out, skillDir(filepath.Join(d, "skills"), ScopeProject, "")...)
		out = append(out, commandDir(filepath.Join(d, "commands"), ScopeProject, "")...)
	}
	for _, d := range nestedDotClaude(r) {
		for _, s := range append(skillDir(filepath.Join(d, "skills"), ScopeProject, ""),
			commandDir(filepath.Join(d, "commands"), ScopeProject, "")...) {
			s.Nested = true
			out = append(out, s)
		}
	}
	for _, p := range Plugins(r, settings) {
		m := readManifest(p.Path)
		dirs := append([]string{filepath.Join(p.Path, "skills")}, manifestPaths(p.Path, m.Skills)...)
		for _, d := range dirs {
			if isFile(filepath.Join(d, "SKILL.md")) {
				out = append(out, readSkill(filepath.Join(d, "SKILL.md"), filepath.Base(d), ScopePlugin, p.Name))
			} else {
				out = append(out, skillDir(d, ScopePlugin, p.Name)...)
			}
		}
		cmds := append([]string{filepath.Join(p.Path, "commands")}, manifestPaths(p.Path, m.Commands)...)
		for _, c := range cmds {
			if isFile(c) {
				out = append(out, readCommand(c, "", ScopePlugin, p.Name))
			} else {
				out = append(out, commandDir(c, ScopePlugin, p.Name)...)
			}
		}
	}
	return dedupeSkills(out)
}

// projectDirs returns <dir>/.claude for the cwd and each parent, nearest first,
// skipping the user config directory.
func projectDirs(r Roots) []string {
	var out []string
	for _, d := range ancestors(r.CWD) {
		dc := filepath.Join(d, ".claude")
		if isDir(dc) && !r.isUserConfig(dc) {
			out = append(out, dc)
		}
	}
	return out
}

// skippedDirs are never searched for nested .claude directories.
var skippedDirs = map[string]bool{"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true}

// nestedDotClaude finds .claude directories below the cwd, at most
// Roots.NestedDepth levels down.
func nestedDotClaude(r Roots) []string {
	if r.NestedDepth <= 0 || r.CWD == "" {
		return nil
	}
	var out []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > r.NestedDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || skippedDirs[e.Name()] {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if e.Name() == ".claude" {
				if depth > 0 && !r.isUserConfig(p) {
					out = append(out, p)
				}
				continue
			}
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			walk(p, depth+1)
		}
	}
	walk(r.CWD, 0)
	sort.Strings(out)
	return out
}

// skillDir reads <dir>/<name>/SKILL.md for every subdirectory.
func skillDir(dir string, scope Scope, plugin string) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			continue
		}
		if scope == ScopeUser && e.Name() == "synced" {
			continue
		}
		path := filepath.Join(dir, e.Name(), "SKILL.md")
		if isFile(path) {
			out = append(out, readSkill(path, e.Name(), scope, plugin))
		}
	}
	return out
}

// syncedSkills reads claude.ai-synced skills: synced/<account>/<name>/SKILL.md.
func syncedSkills(dir string) []Skill {
	accounts, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, a := range accounts {
		if !a.IsDir() {
			continue
		}
		for _, s := range skillDir(filepath.Join(dir, a.Name()), ScopeClaudeAI, "") {
			s.Name = syncedPrefix + ":" + s.Name
			out = append(out, s)
		}
	}
	return out
}

func readSkill(path, dirName string, scope Scope, plugin string) Skill {
	s := Skill{Kind: KindSkill, Path: path, Dir: filepath.Dir(path), Scope: scope, Plugin: plugin,
		UserInvocable: true, ModelInvocable: true}
	s.Frontmatter, s.Err = readFrontmatter(path)
	s.Name = s.Frontmatter.String("name")
	if s.Name == "" {
		s.Name = dirName
	}
	s.fill()
	if plugin != "" {
		s.Name = plugin + ":" + s.Name
	}
	return s
}

// commandDir reads commands/**/*.md; subdirectories become the namespace.
func commandDir(dir string, scope Scope, plugin string) []Skill {
	var out []Skill
	for _, p := range mdFilesRecursive(dir) {
		ns := ""
		if rel, err := filepath.Rel(dir, filepath.Dir(p)); err == nil && rel != "." {
			ns = strings.ReplaceAll(filepath.ToSlash(rel), "/", ":")
		}
		out = append(out, readCommand(p, ns, scope, plugin))
	}
	return out
}

func readCommand(path, namespace string, scope Scope, plugin string) Skill {
	s := Skill{Kind: KindCommand, Path: path, Scope: scope, Plugin: plugin, Namespace: namespace,
		UserInvocable: true, ModelInvocable: true}
	s.Frontmatter, s.Err = readFrontmatter(path)
	s.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	s.fill()
	if s.Description == "" {
		s.Description = firstProseLine(s.Frontmatter.Body)
	}
	if plugin != "" {
		s.Name = plugin + ":" + s.Name
	}
	return s
}

func (s *Skill) fill() {
	fm := s.Frontmatter
	s.Description = fm.String("description")
	s.ArgumentHint = fm.String("argument-hint")
	s.WhenToUse = fm.String("when_to_use")
	if v, ok := fm.Bool("user-invocable"); ok {
		s.UserInvocable = v
	}
	if v, ok := fm.Bool("disable-model-invocation"); ok && v {
		s.ModelInvocable = false
	}
}

// dedupeSkills drops repeats of the same file (a parent directory reached twice
// through a symlink, or a plugin listed twice).
func dedupeSkills(in []Skill) []Skill {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		key := s.Name + "\x00" + s.Path
		if real, err := filepath.EvalSymlinks(s.Path); err == nil {
			key = s.Name + "\x00" + real
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func readFrontmatter(path string) (Frontmatter, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Frontmatter{Fields: map[string]any{}}, err
	}
	return ParseFrontmatter(string(b))
}

// firstProseLine returns the first non-heading, non-empty line of a body.
func firstProseLine(body string) string {
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "!`") {
			continue
		}
		if len(l) > 120 {
			l = l[:117] + "..."
		}
		return l
	}
	return ""
}
