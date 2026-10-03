package discovery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Hook is one hook handler.
type Hook struct {
	Event   string // PreToolUse, SessionStart, …
	Matcher string // tool or source matcher; empty matches everything
	Type    string // command, prompt, http, mcp_tool, agent, …
	Command string
	Args    []string
	URL     string
	Prompt  string
	Server  string // mcp_tool
	Tool    string // mcp_tool
	Timeout float64
	Async   bool
	Scope   Scope
	Path    string // the file that declares it
	Plugin  string
	Raw     json.RawMessage
}

// HookProblem is a hooks block that could not be read.
type HookProblem struct {
	Path string
	Err  error
}

// HooksInfo is everything the /hooks browser lists.
type HooksInfo struct {
	Hooks    []Hook
	Disabled bool // disableAllHooks is set
	Problems []HookProblem
}

// Hooks collects hook handlers from every settings file and enabled plugin, in
// settings order (user, project, local, managed) and then plugins.
func Hooks(r Roots, settings []SettingsFile) HooksInfo {
	var info HooksInfo
	info.Disabled = effectiveBool(settings, "disableAllHooks", false)
	for _, f := range settings {
		raw, ok := f.Data["hooks"]
		if !ok {
			continue
		}
		hs, err := ParseHooksConfig(raw, f.Scope, f.Path, "")
		info.Hooks = append(info.Hooks, hs...)
		if err != nil {
			info.Problems = append(info.Problems, HookProblem{Path: f.Path, Err: err})
		}
	}
	for _, p := range Plugins(r, settings) {
		m := readManifest(p.Path)
		files := []string{filepath.Join(p.Path, "hooks", "hooks.json")}
		var inline map[string]json.RawMessage
		if len(m.Hooks) > 0 && json.Unmarshal(m.Hooks, &inline) == nil {
			hs, err := ParseHooksConfig(m.Hooks, ScopePlugin, filepath.Join(p.Path, ".claude-plugin", "plugin.json"), p.Name)
			info.Hooks = append(info.Hooks, hs...)
			if err != nil {
				info.Problems = append(info.Problems, HookProblem{Path: p.Path, Err: err})
			}
		} else {
			files = append(files, manifestPaths(p.Path, m.Hooks)...)
		}
		seen := map[string]bool{}
		for _, file := range files {
			if seen[file] {
				continue
			}
			seen[file] = true
			b, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			var doc struct {
				Hooks json.RawMessage `json:"hooks"`
			}
			if err := json.Unmarshal(b, &doc); err != nil {
				info.Problems = append(info.Problems, HookProblem{Path: file, Err: err})
				continue
			}
			hs, err := ParseHooksConfig(doc.Hooks, ScopePlugin, file, p.Name)
			info.Hooks = append(info.Hooks, hs...)
			if err != nil {
				info.Problems = append(info.Problems, HookProblem{Path: file, Err: err})
			}
		}
	}
	return info
}

// ParseHooksConfig parses a "hooks" object: event → [{matcher, hooks: [handler]}].
// Malformed entries are skipped and reported in the error; the rest are kept.
func ParseHooksConfig(raw json.RawMessage, scope Scope, path, plugin string) ([]Hook, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var events map[string]json.RawMessage
	if err := json.Unmarshal(raw, &events); err != nil {
		return nil, fmt.Errorf("hooks: %w", err)
	}
	var out []Hook
	var firstErr error
	fail := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	for _, event := range sortedKeys(events) {
		var groups []struct {
			Matcher string            `json:"matcher"`
			Hooks   []json.RawMessage `json:"hooks"`
		}
		if err := json.Unmarshal(events[event], &groups); err != nil {
			fail(fmt.Errorf("hooks.%s: %w", event, err))
			continue
		}
		for _, g := range groups {
			for _, h := range g.Hooks {
				var spec struct {
					Type    string   `json:"type"`
					Command string   `json:"command"`
					Args    []string `json:"args"`
					URL     string   `json:"url"`
					Prompt  string   `json:"prompt"`
					Server  string   `json:"server"`
					Tool    string   `json:"tool"`
					Timeout float64  `json:"timeout"`
					Async   bool     `json:"async"`
				}
				if err := json.Unmarshal(h, &spec); err != nil {
					fail(fmt.Errorf("hooks.%s: %w", event, err))
					continue
				}
				out = append(out, Hook{
					Event: event, Matcher: g.Matcher, Type: spec.Type, Command: spec.Command, Args: spec.Args,
					URL: spec.URL, Prompt: spec.Prompt, Server: spec.Server, Tool: spec.Tool,
					Timeout: spec.Timeout, Async: spec.Async, Scope: scope, Path: path, Plugin: plugin,
					Raw: append(json.RawMessage(nil), h...),
				})
			}
		}
	}
	return out, firstErr
}

// Summary is a one-line description of the handler: its command, URL, prompt or
// MCP tool.
func (h Hook) Summary() string {
	switch {
	case h.Command != "":
		s := h.Command
		for _, a := range h.Args {
			s += " " + a
		}
		return s
	case h.URL != "":
		return h.URL
	case h.Server != "" || h.Tool != "":
		return h.Server + "/" + h.Tool
	case h.Prompt != "":
		return h.Prompt
	}
	return h.Type
}
