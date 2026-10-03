package status

import (
	"strings"
	"testing"
)

func sample() Inputs {
	return Inputs{
		MantleVersion: "0.1.0",
		EngineVersion: "2.1.288",
		Model:         "opus",
		ModelDisplay:  "Opus",
		ResolvedModel: "claude-opus-5-5",
		Account: Account{Email: "user@example.com", Organization: "Example Org",
			SubscriptionType: "max", TokenSource: "claude.ai", APIProvider: "firstParty"},
		SessionID:      "0f8e6a2c-0000-4000-8000-000000000000",
		Cwd:            "/home/u/work/repo",
		AdditionalDirs: []string{"/home/u/work/shared", "/srv/data"},
		PermissionMode: "acceptEdits",
		OutputStyle:    "Explanatory",
		FastModeState:  "off",
		Tools:          []string{"Bash", "Read", "Edit"},
		MCP: []MCPServer{
			{Name: "zeta", Status: "failed", Error: "spawn ENOENT"},
			{Name: "alpha", Status: "connected"},
			{Name: "beta", Status: "connected"},
			{Name: "gamma", Status: "needs-auth"},
		},
		SettingsFiles: []SettingsFile{
			{Scope: "User", Path: "/home/u/.claude/settings.json", Exists: true},
			{Scope: "Project", Path: "/home/u/work/repo/.claude/settings.json", Exists: true, Err: "unknown key"},
			{Scope: "Local", Path: "/home/u/work/repo/.claude/settings.local.json"},
		},
		Memory:   []MemoryFile{{Path: "/home/u/.claude/CLAUDE.md", Scope: "user"}, {Path: "/home/u/work/repo/CLAUDE.md", Scope: "project"}},
		Settings: map[string]any{"sandbox": map[string]any{"enabled": true}},
		Home:     "/home/u",
	}
}

func TestBuild(t *testing.T) {
	s := Build(sample())
	checks := map[string]string{
		"model":    s.Model,
		"provider": s.Provider,
		"cwd":      s.Cwd,
		"dir0":     s.AdditionalDirs[0],
		"dir1":     s.AdditionalDirs[1],
		"mode":     s.PermissionMode,
		"fast":     s.FastMode,
		"sandbox":  s.Sandbox,
		"mcp":      s.MCPSummary,
		"mcp0":     s.MCP[0].Name,
		"settings": s.SettingsFiles[0].Path,
		"memory":   s.Memory[1].Path,
	}
	want := map[string]string{
		"model":    "Opus (claude-opus-5-5)",
		"provider": "Anthropic",
		"cwd":      "~/work/repo",
		"dir0":     "~/work/shared",
		"dir1":     "/srv/data",
		"mode":     "Accept edits",
		"fast":     "off",
		"sandbox":  "on, sandboxed commands run without asking",
		"mcp":      "2 connected, 1 needs-auth, 1 failed",
		"mcp0":     "alpha",
		"settings": "~/.claude/settings.json",
		"memory":   "~/work/repo/CLAUDE.md",
	}
	for k, w := range want {
		if checks[k] != w {
			t.Errorf("%s: got %q, want %q", k, checks[k], w)
		}
	}
	if s.Tools != 3 {
		t.Errorf("tools %d", s.Tools)
	}
}

func TestBuildEmpty(t *testing.T) {
	s := Build(Inputs{})
	if s.MantleVersion != "unknown" || s.Model != "Default" || s.MCPSummary != "none" ||
		s.Sandbox != "off" || s.PermissionMode != "Ask before acting" || s.OutputStyle != "Default" {
		t.Errorf("empty status %+v", s)
	}
	// Sections render without inputs.
	if len(s.Sections()) != 6 {
		t.Error("sections")
	}
}

func TestFastAndSandbox(t *testing.T) {
	if fastLine("on", "") != "on" || fastLine("cooldown", "") != "cooldown" ||
		!strings.HasPrefix(fastLine("off", "not on this plan"), "unavailable") {
		t.Error("fast line")
	}
	off := map[string]any{"sandbox": map[string]any{"enabled": true, "autoAllowBashIfSandboxed": false}}
	if sandboxLine(off) != "on, commands still ask for permission" {
		t.Error("sandbox no auto-allow")
	}
}

func TestSections(t *testing.T) {
	secs := Build(sample()).Sections()
	var titles []string
	problems := 0
	for _, s := range secs {
		titles = append(titles, s.Title)
		for _, l := range s.Lines {
			if l.Problem {
				problems++
			}
		}
	}
	if strings.Join(titles, "|") != "Session|Account|Model and modes|MCP servers|Settings files|Memory" {
		t.Errorf("titles %v", titles)
	}
	// zeta failed, gamma needs auth, project settings invalid.
	if problems != 3 {
		t.Errorf("%d problem lines", problems)
	}
	acct := secs[1]
	var plan string
	for _, l := range acct.Lines {
		if l.Label == "Plan" {
			plan = l.Value
		}
	}
	if plan != "Max" {
		t.Errorf("plan %q", plan)
	}
}

func TestProviderNames(t *testing.T) {
	for in, want := range map[string]string{"": "Anthropic", "bedrock": "Amazon Bedrock", "vertex": "Google Vertex AI", "new": "new"} {
		if got := ProviderName(in); got != want {
			t.Errorf("%q -> %q", in, got)
		}
	}
}
