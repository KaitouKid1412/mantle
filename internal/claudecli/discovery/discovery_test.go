package discovery

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeTree creates files under root; a trailing "/" makes a directory.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type tree struct {
	root  string
	roots Roots
	repo  string
}

func newTree(t *testing.T) *tree {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cfg := filepath.Join(home, ".claude")
	repo := filepath.Join(home, "work", "repo")
	managed := filepath.Join(root, "managed")
	cache := filepath.Join(home, "plugins-cache")
	slug := ProjectSlug(repo)

	writeTree(t, root, map[string]string{
		"home/.claude/settings.json": `{
  "enabledPlugins": {"demo@mk": true, "off@mk": false},
  "skillOverrides": {"deploy": "name-only"},
  "hooks": {
    "PreToolUse": [{"matcher": "Edit|Write", "hooks": [{"type": "command", "command": "./fmt.sh", "args": ["--fix"], "timeout": 10}]}],
    "Stop": [{"hooks": [{"type": "prompt", "prompt": "Check the goal"}]}]
  }
}`,
		"home/.claude/CLAUDE.md":                         "User memory.\nSee @~/notes/extra.md and `@not/an/import.md`.\n```\n@inside/fence.md\n```\n",
		"home/notes/extra.md":                            "Extra @./deeper.md and user@example.test\n",
		"home/notes/deeper.md":                           "Deeper.\n",
		"home/.claude/rules/style.md":                    "Use tabs.\n",
		"home/.claude/agents/helper.md":                  "---\nname: helper\ndescription: Helps.\ntools: Read, Grep\nmodel: haiku\n---\nBody\n",
		"home/.claude/skills/deploy/SKILL.md":            "---\ndescription: Deploy the app\nargument-hint: '[env]'\ndisable-model-invocation: true\n---\n",
		"home/.claude/skills/synced/acct-1/pdf/SKILL.md": "---\nname: pdf\ndescription: PDFs\n---\n",
		"home/.claude/commands/quick.md":                 "---\ndescription: Quick thing\n---\nDo it.\n",
		"home/.claude/commands/git/commit.md":            "# Commit\n\nCommit the staged changes.\n",
		"home/.claude/plugins/installed_plugins.json": `{"version": 2, "plugins": {
  "demo@mk": [{"scope": "user", "installPath": "` + filepath.ToSlash(filepath.Join(cache, "demo")) + `", "version": "1.0.0"}],
  "off@mk": [{"scope": "user", "installPath": "` + filepath.ToSlash(filepath.Join(cache, "off")) + `", "version": "1.0.0"}]
}}`,
		"home/.claude/projects/" + slug + "/memory/MEMORY.md": "- [topic](topic.md)\n",
		"home/.claude/projects/" + slug + "/memory/topic.md":  "A topic.\n",

		"home/plugins-cache/demo/.claude-plugin/plugin.json": `{"name": "demo", "version": "1.0.0"}`,
		"home/plugins-cache/demo/skills/status/SKILL.md":     "---\nname: status\ndescription: Show status\n---\n",
		"home/plugins-cache/demo/commands/run.md":            "---\ndescription: Run it\n---\n",
		"home/plugins-cache/demo/agents/reviewer.md":         "---\nname: reviewer\ndescription: Reviews\n---\n",
		"home/plugins-cache/demo/hooks/hooks.json":           `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/start"}]}]}}`,
		"home/plugins-cache/off/skills/hidden/SKILL.md":      "---\nname: hidden\n---\n",

		"home/work/CLAUDE.md":              "Parent memory.\n",
		"home/work/repo/.git/HEAD":         "ref: refs/heads/main\n",
		"home/work/repo/CLAUDE.md":         "Project memory.\n",
		"home/work/repo/CLAUDE.local.md":   "Local memory.\n",
		"home/work/repo/AGENTS.md":         "Agents file.\n",
		"home/work/repo/.claude/CLAUDE.md": "Alt project memory.\n",
		"home/work/repo/.claude/settings.json": `{
  "autoMemoryDirectory": "~/ignored-in-project",
  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "lint"}]}],
            "Bad": "not a list"}
}`,
		"home/work/repo/.claude/settings.local.json":                `{"oops": }`,
		"home/work/repo/.claude/rules/api.md":                       "---\npaths:\n  - src/api/**\n---\nAPI rules.\n",
		"home/work/repo/.claude/agents/planner.md":                  "---\nname: planner\ndescription: Plans\nno closing\n",
		"home/work/repo/.claude/skills/build/SKILL.md":              "---\nname: builder\ndescription: Build it\nuser-invocable: false\n---\n",
		"home/work/repo/.claude/commands/test.md":                   "Run the tests and report.\n",
		"home/work/repo/pkg/sub/.claude/skills/nested/SKILL.md":     "---\ndescription: Nested skill\n---\n",
		"home/work/repo/node_modules/x/.claude/skills/bad/SKILL.md": "---\ndescription: never\n---\n",

		"managed/CLAUDE.md":                      "Managed memory.\n",
		"managed/managed-settings.json":          `{"hooks": {"SessionStart": [{"hooks": [{"type": "http", "url": "https://hooks.example.test/start"}]}]}}`,
		"managed/.claude/skills/policy/SKILL.md": "---\ndescription: Policy skill\n---\n",
	})
	return &tree{root: root, repo: repo, roots: Roots{
		Home: home, ConfigDir: cfg, CWD: repo, ManagedDir: managed, NestedDepth: 3,
		Getenv: func(string) string { return "" },
	}}
}

func (tr *tree) rel(p string) string {
	r, err := filepath.Rel(tr.root, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}

func TestSettingsFiles(t *testing.T) {
	tr := newTree(t)
	files := SettingsFiles(tr.roots)
	var scopes []Scope
	for _, f := range files {
		scopes = append(scopes, f.Scope)
	}
	if want := []Scope{ScopeUser, ScopeProject, ScopeLocal, ScopeManaged}; !reflect.DeepEqual(scopes, want) {
		t.Fatalf("scopes = %v, want %v", scopes, want)
	}
	local := files[2]
	if !local.Exists || local.Err == nil || !strings.Contains(local.Err.Error(), "line 1") {
		t.Errorf("invalid local settings: exists=%v err=%v", local.Exists, local.Err)
	}
	if files[0].Err != nil {
		t.Errorf("user settings: %v", files[0].Err)
	}
	if _, err := ParseJSONObject([]byte(`[1]`)); err == nil {
		t.Error("array accepted as settings")
	}
	if _, err := ParseJSONObject([]byte(`{} {}`)); err == nil {
		t.Error("trailing data accepted")
	}
	if _, err := ParseJSONObject([]byte("  ")); err == nil {
		t.Error("empty file accepted")
	}
	if _, err := ParseJSONObject([]byte("null")); err == nil {
		t.Error("null accepted")
	}
	ov := SkillOverrides(files)
	if ov["deploy"].Value != VisibilityNameOnly || ov["deploy"].Scope != ScopeUser {
		t.Errorf("overrides = %+v", ov)
	}
	if VisibilityOn.Next() != VisibilityNameOnly || VisibilityOff.Next() != VisibilityOn || Visibility("x").Next() != VisibilityOn {
		t.Error("visibility cycle")
	}
}

func TestPlugins(t *testing.T) {
	tr := newTree(t)
	ps := Plugins(tr.roots, SettingsFiles(tr.roots))
	if len(ps) != 1 || ps[0].Name != "demo" || ps[0].ID != "demo@mk" || ps[0].Scope != ScopeUser || ps[0].Version != "1.0.0" {
		t.Fatalf("plugins = %+v", ps)
	}
	extra := filepath.Join(tr.root, "session-plugin")
	writeTree(t, tr.root, map[string]string{"session-plugin/skills/s/SKILL.md": "---\nname: s\n---\n"})
	tr.roots.Plugins = []PluginRoot{{Path: extra}, {Path: ps[0].Path}}
	ps = Plugins(tr.roots, SettingsFiles(tr.roots))
	if len(ps) != 2 || ps[1].Name != "session-plugin" {
		t.Errorf("with session plugin = %+v", ps)
	}
}

func TestSkills(t *testing.T) {
	tr := newTree(t)
	skills := Skills(tr.roots, SettingsFiles(tr.roots))
	got := map[string]Skill{}
	var names []string
	for _, s := range skills {
		got[s.Name] = s
		names = append(names, s.Name)
	}
	expect := map[string]struct {
		kind  string
		scope Scope
	}{
		"policy":               {KindSkill, ScopeManaged},
		"deploy":               {KindSkill, ScopeUser},
		"quick":                {KindCommand, ScopeUser},
		"commit":               {KindCommand, ScopeUser},
		"anthropic-skills:pdf": {KindSkill, ScopeClaudeAI},
		"builder":              {KindSkill, ScopeProject},
		"test":                 {KindCommand, ScopeProject},
		"nested":               {KindSkill, ScopeProject},
		"demo:status":          {KindSkill, ScopePlugin},
		"demo:run":             {KindCommand, ScopePlugin},
	}
	if len(skills) != len(expect) {
		t.Errorf("got %d skills: %q", len(skills), names)
	}
	for name, w := range expect {
		s, ok := got[name]
		if !ok {
			t.Errorf("missing %s (have %q)", name, names)
			continue
		}
		if s.Kind != w.kind || s.Scope != w.scope {
			t.Errorf("%s: kind=%s scope=%s, want %s %s", name, s.Kind, s.Scope, w.kind, w.scope)
		}
	}
	if s := got["deploy"]; s.ModelInvocable || !s.UserInvocable || s.ArgumentHint != "[env]" || s.Description != "Deploy the app" {
		t.Errorf("deploy = %+v", s)
	}
	if s := got["builder"]; s.UserInvocable || filepath.Base(s.Dir) != "build" {
		t.Errorf("builder = %+v", s)
	}
	if s := got["commit"]; s.Namespace != "git" || s.Description != "Commit the staged changes." {
		t.Errorf("commit = %+v", s)
	}
	if s := got["test"]; s.Description != "Run the tests and report." {
		t.Errorf("test description = %q", s.Description)
	}
	if s := got["nested"]; !s.Nested {
		t.Errorf("nested flag not set")
	}
	if s := got["demo:status"]; s.Plugin != "demo" {
		t.Errorf("plugin skill = %+v", s)
	}
	for _, s := range skills {
		if strings.Contains(s.Path, "node_modules") || strings.Contains(s.Path, "/off/") {
			t.Errorf("should be skipped: %s", tr.rel(s.Path))
		}
	}

	tr.roots.NestedDepth = 0
	for _, s := range Skills(tr.roots, SettingsFiles(tr.roots)) {
		if s.Nested {
			t.Errorf("nested search should be off: %s", s.Name)
		}
	}
}

func TestAgents(t *testing.T) {
	tr := newTree(t)
	agents := Agents(tr.roots, SettingsFiles(tr.roots))
	got := map[string]Agent{}
	for _, a := range agents {
		got[a.Name] = a
	}
	if len(agents) != 3 {
		t.Fatalf("agents = %+v", agents)
	}
	if a := got["helper"]; a.Scope != ScopeUser || a.Model != "haiku" || !reflect.DeepEqual(a.Tools, []string{"Read", "Grep"}) {
		t.Errorf("helper = %+v", a)
	}
	if a := got["planner"]; a.Scope != ScopeProject || a.Err == nil {
		t.Errorf("planner should carry its frontmatter error: %+v", a)
	}
	if a := got["demo:reviewer"]; a.Scope != ScopePlugin || a.Plugin != "demo" {
		t.Errorf("plugin agent = %+v", a)
	}
	if p := AgentPath(tr.roots, ScopeProject, "x"); p != filepath.Join(tr.repo, ".claude", "agents", "x.md") {
		t.Errorf("AgentPath project = %s", p)
	}
	if p := AgentPath(tr.roots, ScopeUser, "x"); p != filepath.Join(tr.roots.ConfigDir, "agents", "x.md") {
		t.Errorf("AgentPath user = %s", p)
	}
	fm, err := ParseFrontmatter(AgentTemplate("x"))
	if err != nil || fm.String("name") != "x" || fm.String("description") == "" {
		t.Errorf("template frontmatter = %+v, %v", fm.Fields, err)
	}
}

func TestMemory(t *testing.T) {
	tr := newTree(t)
	info := Memory(tr.roots, SettingsFiles(tr.roots))
	var got []string
	for _, f := range info.Files {
		got = append(got, string(f.Scope)+" "+string(f.Kind)+" "+tr.rel(f.Path))
	}
	slug := ProjectSlug(tr.repo)
	want := []string{
		"managed CLAUDE.md managed/CLAUDE.md",
		"user CLAUDE.md home/.claude/CLAUDE.md",
		"user import home/notes/extra.md",
		"user import home/notes/deeper.md",
		"user rule home/.claude/rules/style.md",
		"project CLAUDE.md home/work/CLAUDE.md",
		"project CLAUDE.md home/work/repo/CLAUDE.md",
		"project CLAUDE.md home/work/repo/.claude/CLAUDE.md",
		"project rule home/work/repo/.claude/rules/api.md",
		"project AGENTS.md home/work/repo/AGENTS.md",
		"local CLAUDE.local.md home/work/repo/CLAUDE.local.md",
		"auto auto home/.claude/projects/" + slug + "/memory/MEMORY.md",
		"auto auto home/.claude/projects/" + slug + "/memory/topic.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("memory files:\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(want, "\n     "))
	}
	for _, f := range info.Files {
		switch {
		case f.Kind == MemoryImport && strings.HasSuffix(f.Path, "deeper.md"):
			if f.Depth != 2 || !strings.HasSuffix(f.ImportedFrom, "extra.md") {
				t.Errorf("deeper import = %+v", f)
			}
		case f.Kind == MemoryRule && strings.HasSuffix(f.Path, "api.md"):
			if !reflect.DeepEqual(f.Paths, []string{"src/api/**"}) {
				t.Errorf("rule paths = %q", f.Paths)
			}
		}
		if !f.Exists {
			t.Errorf("%s should exist", tr.rel(f.Path))
		}
	}
	// The project setting for autoMemoryDirectory is ignored.
	if !info.AutoEnabled || info.AutoDirFrom != "" || !strings.HasSuffix(info.AutoDir, filepath.Join(slug, "memory")) {
		t.Errorf("auto memory = %v %q %q", info.AutoEnabled, info.AutoDir, info.AutoDirFrom)
	}
}

func TestMemoryMissingFilesAndOverrides(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cwd := filepath.Join(root, "proj")
	writeTree(t, root, map[string]string{
		"proj/":                      "",
		"home/.claude/settings.json": `{"autoMemoryEnabled": false, "autoMemoryDirectory": "~/mem"}`,
	})
	r := Roots{Home: home, ConfigDir: filepath.Join(home, ".claude"), CWD: cwd}
	info := Memory(r, SettingsFiles(r))
	if len(info.Files) < 2 || info.Files[0].Exists || info.Files[0].Scope != ScopeUser {
		t.Fatalf("user CLAUDE.md should be listed as missing: %+v", info.Files)
	}
	var cwdListed bool
	for _, f := range info.Files {
		if f.Path == filepath.Join(cwd, "CLAUDE.md") && !f.Exists {
			cwdListed = true
		}
	}
	if !cwdListed {
		t.Error("cwd CLAUDE.md should be listed as missing")
	}
	if info.AutoEnabled || info.AutoDir != filepath.Join(home, "mem") || info.AutoDirFrom != ScopeUser {
		t.Errorf("auto = %v %q %q", info.AutoEnabled, info.AutoDir, info.AutoDirFrom)
	}

	r.Getenv = func(k string) string {
		if k == "CLAUDE_CODE_DISABLE_AUTO_MEMORY" {
			return "1"
		}
		return ""
	}
	writeTree(t, root, map[string]string{"home/.claude/settings.json": `{"autoMemoryEnabled": true}`})
	if on, _, _ := AutoMemory(r, SettingsFiles(r)); on {
		t.Error("env should disable auto memory")
	}
}

func TestHooks(t *testing.T) {
	tr := newTree(t)
	info := Hooks(tr.roots, SettingsFiles(tr.roots))
	var got []string
	for _, h := range info.Hooks {
		got = append(got, string(h.Scope)+" "+h.Event+" "+h.Matcher+" "+h.Type+" "+h.Summary())
	}
	want := []string{
		"user PreToolUse Edit|Write command ./fmt.sh --fix",
		"user Stop  prompt Check the goal",
		"project PreToolUse Bash command lint",
		"managed SessionStart  http https://hooks.example.test/start",
		"plugin SessionStart  command ${CLAUDE_PLUGIN_ROOT}/start",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hooks:\n got %q\nwant %q", got, want)
	}
	if len(info.Problems) != 1 || !strings.Contains(info.Problems[0].Err.Error(), "hooks.Bad") {
		t.Errorf("problems = %+v", info.Problems)
	}
	if info.Disabled {
		t.Error("disableAllHooks not set")
	}
	if info.Hooks[0].Timeout != 10 || info.Hooks[4].Plugin != "demo" {
		t.Errorf("hook details = %+v / %+v", info.Hooks[0], info.Hooks[4])
	}
	if _, err := ParseHooksConfig([]byte(`[]`), ScopeUser, "x", ""); err == nil {
		t.Error("non-object hooks accepted")
	}
	if hs, err := ParseHooksConfig(nil, ScopeUser, "x", ""); hs != nil || err != nil {
		t.Error("empty hooks")
	}
}

func TestCanonicalRootAndSlug(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "repo")
	wt := filepath.Join(main, ".claude", "worktrees", "feature")
	writeTree(t, root, map[string]string{
		"repo/.git/HEAD":                        "ref: refs/heads/main\n",
		"repo/.git/worktrees/feature/commondir": "../..\n",
		"repo/.claude/worktrees/feature/.git":   "gitdir: " + filepath.Join(main, ".git", "worktrees", "feature") + "\n",
		"repo/.claude/worktrees/feature/sub/":   "",
		"plain/":                                "",
	})
	if got := CanonicalRoot(filepath.Join(wt, "sub")); got != main {
		t.Errorf("worktree root = %s, want %s", got, main)
	}
	if got := CanonicalRoot(filepath.Join(main)); got != main {
		t.Errorf("main root = %s", got)
	}
	plain := filepath.Join(root, "plain")
	if got := CanonicalRoot(plain); got != plain {
		t.Errorf("plain dir root = %s", got)
	}
	if got := ProjectSlug("/Users/me/my.repo_x"); got != "-Users-me-my-repo-x" {
		t.Errorf("slug = %s", got)
	}
}
