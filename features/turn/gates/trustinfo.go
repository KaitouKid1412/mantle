package gates

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// TrustReport lists what a folder would be able to do once trusted. The trust dialog
// shows it so the user knows what they are agreeing to run. Only repository-controlled
// sources (project and local settings, .mcp.json, .claude/) are listed.
type TrustReport struct {
	Dir            string
	AllowRules     []SourcedValue // permissions.allow
	Hooks          []HookRef
	Env            []SourcedValue // env var names set by settings
	Helpers        []SourcedValue // commands run outside the tool system (apiKeyHelper, statusLine, …)
	AdditionalDirs []SourcedValue // permissions.additionalDirectories
	McpServers     []McpServer    // from .mcp.json
	// Extras lists other executable project content: custom commands, agents, skills
	// and hook scripts directories that exist under .claude/.
	Extras []string
}

// SourcedValue is a value with the settings file it came from.
type SourcedValue struct {
	Value  string
	Source string // path
	Detail string // e.g. the helper's settings key
}

// HookRef is one configured hook.
type HookRef struct {
	Event   string // "PreToolUse"
	Matcher string
	Kind    string // "command" | "prompt" | "http" | "mcp_tool" | …
	Detail  string // command, URL or server/tool
	Source  string
}

// Empty reports whether nothing would run.
func (r TrustReport) Empty() bool {
	return len(r.AllowRules)+len(r.Hooks)+len(r.Env)+len(r.Helpers)+len(r.AdditionalDirs)+
		len(r.McpServers)+len(r.Extras) == 0
}

// helperKeys are settings keys whose value runs a command.
var helperKeys = []string{
	"apiKeyHelper",
	"awsAuthRefresh",
	"awsCredentialExport",
	"gcpAuthRefresh",
	"otelHeadersHelper",
	"proxyAuthHelper",
	"processWrapper",
	"statusLine.command",
	"subagentStatusLine.command",
	"fileSuggestion.command",
}

// CollectTrustReport gathers the trust dialog's contents for cwd from the project and
// local settings layers and the .mcp.json result.
func CollectTrustReport(cwd string, layers Layers, mcp McpApprovalResult) TrustReport {
	r := TrustReport{Dir: absClean(cwd)}
	for _, f := range layers {
		if (f.Scope != ScopeProject && f.Scope != ScopeLocal) || f.Data == nil {
			continue
		}
		src := f.Path
		for _, s := range stringList(f.Data, "permissions.allow") {
			r.AllowRules = append(r.AllowRules, SourcedValue{Value: s, Source: src})
		}
		for _, s := range stringList(f.Data, "permissions.additionalDirectories") {
			r.AdditionalDirs = append(r.AdditionalDirs, SourcedValue{Value: s, Source: src})
		}
		if env, ok := f.Data["env"].(map[string]any); ok {
			for _, k := range sortedKeys(env) {
				r.Env = append(r.Env, SourcedValue{Value: k, Source: src})
			}
		}
		for _, key := range helperKeys {
			if v, ok := dig(f.Data, key); ok {
				if s, ok := v.(string); ok && s != "" {
					r.Helpers = append(r.Helpers, SourcedValue{Value: s, Source: src, Detail: key})
				}
			}
		}
		r.Hooks = append(r.Hooks, collectHooks(f.Data, src)...)
	}
	for _, s := range mcp.Servers {
		r.McpServers = append(r.McpServers, s.Server)
	}
	for _, sub := range []string{"commands", "agents", "skills", "hooks", "output-styles", "workflows"} {
		p := filepath.Join(r.Dir, ".claude", sub)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			r.Extras = append(r.Extras, filepath.Join(".claude", sub))
		}
	}
	return r
}

func collectHooks(data map[string]any, src string) []HookRef {
	events, ok := data["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	var out []HookRef
	for _, ev := range sortedKeys(events) {
		groups, _ := events[ev].([]any)
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				continue
			}
			matcher, _ := gm["matcher"].(string)
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				hm, ok := h.(map[string]any)
				if !ok {
					continue
				}
				ref := HookRef{Event: ev, Matcher: matcher, Source: src}
				ref.Kind, _ = hm["type"].(string)
				if ref.Kind == "" {
					ref.Kind = "command"
				}
				switch ref.Kind {
				case "command":
					ref.Detail, _ = hm["command"].(string)
				case "http":
					ref.Detail, _ = hm["url"].(string)
				case "prompt":
					ref.Detail, _ = hm["prompt"].(string)
				case "mcp_tool":
					server, _ := hm["server"].(string)
					tool, _ := hm["tool"].(string)
					ref.Detail = fmt.Sprintf("%s/%s", server, tool)
				}
				out = append(out, ref)
			}
		}
	}
	return out
}

func stringList(data map[string]any, key string) []string {
	v, ok := dig(data, key)
	if !ok {
		return nil
	}
	arr, _ := v.([]any)
	var out []string
	for _, it := range arr {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
