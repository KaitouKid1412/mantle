package discovery

import (
	"path/filepath"
	"strings"
)

// Agent is a subagent definition file.
type Agent struct {
	Name            string // plugin agents are "<plugin>:<name>"
	Description     string
	Tools           []string // empty means every tool
	DisallowedTools []string
	Model           string
	Color           string
	Path            string
	Scope           Scope
	Plugin          string
	Frontmatter     Frontmatter
	Err             error
}

// Agents scans agent definitions: managed, user, project (cwd and parents,
// nearest first) and enabled plugins. Built-in agents have no file and are not
// listed; the engine reports them in initialize.agents.
func Agents(r Roots, settings []SettingsFile) []Agent {
	var out []Agent
	if r.ManagedDir != "" {
		out = append(out, agentDir(filepath.Join(r.ManagedDir, ".claude", "agents"), ScopeManaged, "")...)
	}
	if r.ConfigDir != "" {
		out = append(out, agentDir(filepath.Join(r.ConfigDir, "agents"), ScopeUser, "")...)
	}
	for _, d := range projectDirs(r) {
		out = append(out, agentDir(filepath.Join(d, "agents"), ScopeProject, "")...)
	}
	for _, p := range Plugins(r, settings) {
		m := readManifest(p.Path)
		for _, d := range append([]string{filepath.Join(p.Path, "agents")}, manifestPaths(p.Path, m.Agents)...) {
			if isFile(d) {
				out = append(out, readAgent(d, ScopePlugin, p.Name))
			} else {
				out = append(out, agentDir(d, ScopePlugin, p.Name)...)
			}
		}
	}
	return out
}

func agentDir(dir string, scope Scope, plugin string) []Agent {
	var out []Agent
	for _, p := range mdFilesRecursive(dir) {
		out = append(out, readAgent(p, scope, plugin))
	}
	return out
}

func readAgent(path string, scope Scope, plugin string) Agent {
	a := Agent{Path: path, Scope: scope, Plugin: plugin}
	a.Frontmatter, a.Err = readFrontmatter(path)
	fm := a.Frontmatter
	a.Name = fm.String("name")
	if a.Name == "" {
		a.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if plugin != "" {
		a.Name = plugin + ":" + a.Name
	}
	a.Description = fm.String("description")
	a.Tools = fm.List("tools")
	a.DisallowedTools = fm.List("disallowedTools")
	if len(a.DisallowedTools) == 0 {
		a.DisallowedTools = fm.List("disallowed-tools")
	}
	a.Model = fm.String("model")
	a.Color = fm.String("color")
	return a
}

// AgentTemplate is the starting text mantle writes for a new agent before
// opening it in $EDITOR.
func AgentTemplate(name string) string {
	return "---\nname: " + name + "\ndescription: When Claude should use this agent, in one or two sentences.\n" +
		"# tools: Read, Grep, Glob   (omit to allow every tool)\n# model: sonnet\n---\n\n" +
		"You are a focused assistant for ... Describe the role, the steps to follow and what to return.\n"
}

// AgentPath returns where a new agent named name is created for scope (user or
// project).
func AgentPath(r Roots, scope Scope, name string) string {
	file := name + ".md"
	if scope == ScopeProject {
		return filepath.Join(r.CWD, ".claude", "agents", file)
	}
	return filepath.Join(r.ConfigDir, "agents", file)
}
