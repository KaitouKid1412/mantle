package gates

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// McpServer is one server declared in a project .mcp.json.
type McpServer struct {
	Name      string
	Transport string // "stdio" (default), "http", "sse", "ws", …
	Command   string
	Args      []string
	URL       string
	EnvKeys   []string // names only; values are never shown
	Source    string   // the .mcp.json that declares it
}

// McpDecision is a server's approval state.
type McpDecision string

const (
	McpPending  McpDecision = "pending"
	McpApproved McpDecision = "approved"
	McpRejected McpDecision = "rejected"
)

// McpServerState pairs a server with its current decision and the setting that made it.
type McpServerState struct {
	Server   McpServer
	Decision McpDecision
	Reason   string // the settings key that decided it, "" when pending
}

// McpApprovalResult is the .mcp.json gate's view of a directory.
type McpApprovalResult struct {
	Files   []string         // .mcp.json files read, nearest first
	Servers []McpServerState // sorted by name
	Errors  []error          // unreadable or malformed .mcp.json files
}

// McpApproval reads every .mcp.json from cwd up to the filesystem root (a nearer file
// overrides a farther one by server name) and classifies each server with the approval
// settings, the way Claude Code does once a folder is trusted:
//   - rejected if listed in disabledMcpjsonServers;
//   - approved if listed in enabledMcpjsonServers or enableAllProjectMcpServers is set;
//   - pending otherwise. Headless claude would connect pending servers without asking,
//     so the caller must ask, and pass the ones not approved as disabledMcpjsonServers.
//
// The settings keys merge across every scope plus the legacy per-project entries in
// ~/.claude.json and the answers mantle recorded (store). gc and store may be nil.
func McpApproval(env Env, cwd string, layers Layers, gc *GlobalConfig, store *GateStore) McpApprovalResult {
	if gc == nil {
		gc = LoadGlobalConfig(env)
	}
	cwd = absClean(cwd)
	var res McpApprovalResult
	servers := map[string]McpServer{}
	for _, dir := range ancestors(cwd, "") {
		path := filepath.Join(dir, ".mcp.json")
		found, err := readMcpJSON(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		res.Files = append(res.Files, path)
		if err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		for name, s := range found {
			if _, nearer := servers[name]; !nearer {
				servers[name] = s
			}
		}
	}

	disabled := layers.Strings("disabledMcpjsonServers")
	enabled := layers.Strings("enabledMcpjsonServers")
	enableAll := layers.Bool("enableAllProjectMcpServers")
	_, key := projectRoots(cwd)
	if key == "" {
		key = cwd
	}
	for _, k := range variants(key) {
		if p, ok := gc.Projects[k]; ok {
			disabled = append(disabled, p.DisabledMcpjsonServers...)
			enabled = append(enabled, p.EnabledMcpjsonServers...)
			enableAll = enableAll || p.EnableAllProjectMcpServers
		}
		if store != nil {
			if c, ok := store.Mcp[k]; ok {
				disabled = append(disabled, c.Rejected...)
				enabled = append(enabled, c.Approved...)
				enableAll = enableAll || c.EnableAll
			}
		}
	}

	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		st := McpServerState{Server: servers[n], Decision: McpPending}
		switch {
		case listHas(disabled, n):
			st.Decision, st.Reason = McpRejected, "disabledMcpjsonServers"
		case listHas(enabled, n):
			st.Decision, st.Reason = McpApproved, "enabledMcpjsonServers"
		case enableAll:
			st.Decision, st.Reason = McpApproved, "enableAllProjectMcpServers"
		}
		res.Servers = append(res.Servers, st)
	}
	return res
}

// Pending returns the servers that need the user's decision.
func (r McpApprovalResult) Pending() []McpServer {
	var out []McpServer
	for _, s := range r.Servers {
		if s.Decision == McpPending {
			out = append(out, s.Server)
		}
	}
	return out
}

// Disabled returns the names to pass to the engine as disabledMcpjsonServers: every
// server that is rejected, plus every pending server not in approved (the user's answers
// from the approval dialog).
func (r McpApprovalResult) Disabled(approved map[string]bool) []string {
	var out []string
	for _, s := range r.Servers {
		switch s.Decision {
		case McpRejected:
			out = append(out, s.Server.Name)
		case McpPending:
			if !approved[s.Server.Name] {
				out = append(out, s.Server.Name)
			}
		}
	}
	return out
}

func readMcpJSON(path string) (map[string]McpServer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var top struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("%s: %w", path, describeJSONError(data, err))
	}
	out := map[string]McpServer{}
	for name, raw := range top.McpServers {
		var cfg struct {
			Type    string            `json:"type"`
			Command string            `json:"command"`
			Args    []json.RawMessage `json:"args"`
			URL     string            `json:"url"`
			Env     map[string]any    `json:"env"`
		}
		_ = json.Unmarshal(raw, &cfg) // tolerate odd shapes; the name alone is enough to gate
		s := McpServer{Name: name, Transport: cfg.Type, Command: cfg.Command, URL: cfg.URL, Source: path}
		if s.Transport == "" {
			s.Transport = "stdio"
			if s.URL != "" {
				s.Transport = "http"
			}
		}
		for _, a := range cfg.Args {
			var str string
			if json.Unmarshal(a, &str) == nil {
				s.Args = append(s.Args, str)
			} else {
				s.Args = append(s.Args, string(a))
			}
		}
		for k := range cfg.Env {
			s.EnvKeys = append(s.EnvKeys, k)
		}
		sort.Strings(s.EnvKeys)
		out[name] = s
	}
	return out, nil
}

// listHas matches server names exactly or after normalizing characters outside
// [A-Za-z0-9_-] to "_" (Claude Code compares normalized names). Plugin-provided names
// ("plugin:…") only match exactly.
func listHas(list []string, name string) bool {
	for _, x := range list {
		if x == name {
			return true
		}
		if !strings.HasPrefix(x, "plugin:") && !strings.HasPrefix(name, "plugin:") &&
			normalizeServerName(x) == normalizeServerName(name) {
			return true
		}
	}
	return false
}

func normalizeServerName(s string) string {
	b := []byte(s)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			b[i] = '_'
		}
	}
	return string(b)
}

// FlagSettings builds the inline JSON for the engine's --settings flag: the user's own
// --settings value (inline JSON or a file path relative to cwd, may be "") merged with
// the gates' additions. Array values are unioned, other keys from add win.
func FlagSettings(user, cwd string, add map[string]any) (string, error) {
	base := map[string]any{}
	if u := strings.TrimSpace(user); u != "" {
		var data []byte
		if strings.HasPrefix(u, "{") {
			data = []byte(u)
		} else {
			p := u
			if !filepath.IsAbs(p) {
				p = filepath.Join(cwd, p)
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return "", err
			}
			data = b
		}
		m, err := parseSettings(data)
		if err != nil {
			return "", fmt.Errorf("--settings: %w", err)
		}
		base = m
	}
	for k, v := range add {
		if arr, ok := v.([]string); ok {
			seen := map[string]bool{}
			var merged []any
			if prev, ok := base[k].([]any); ok {
				for _, x := range prev {
					if s, ok := x.(string); ok && !seen[s] {
						seen[s] = true
						merged = append(merged, s)
					}
				}
			}
			for _, s := range arr {
				if !seen[s] {
					seen[s] = true
					merged = append(merged, s)
				}
			}
			base[k] = merged
			continue
		}
		base[k] = v
	}
	if len(base) == 0 {
		return "", nil
	}
	out, err := json.Marshal(base)
	return string(out), err
}
