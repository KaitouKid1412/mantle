// Package status assembles what /status shows from plain inputs: the engine's
// initialize response and system/init message, MCP status, the settings files in
// effect and the memory files loaded. It is pure; the panel gathers the inputs.
package status

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Account is initialize.account.
type Account = proto.Account

// MCPServer is one entry of system/init mcp_servers or the mcp_status response.
type MCPServer struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Scope  string `json:"scope,omitempty"`
	Error  string `json:"error,omitempty"`
}

// SettingsFile is one settings source and whether it was usable.
type SettingsFile struct {
	Scope  string // "User", "Project", "Local", "Managed", "Flag"
	Path   string
	Exists bool
	Err    string // parse or schema problem; the engine ignores invalid files in -p
}

// MemoryFile is a CLAUDE.md-style file loaded into context.
type MemoryFile struct {
	Path  string
	Scope string // "user", "project", "local", "managed", "auto"
}

// Inputs is everything /status reads.
type Inputs struct {
	MantleVersion string
	EngineVersion string

	// From initialize / system/init.
	Model                  string // session model as reported (alias or ID)
	ModelDisplay           string // display name from the models list, if known
	ResolvedModel          string
	Account                Account
	SessionID              string
	SessionName            string
	Cwd                    string
	AdditionalDirs         []string
	PermissionMode         string
	OutputStyle            string
	FastModeState          string // fast_mode_state
	FastModeDisabledReason string
	Tools                  []string
	MCP                    []MCPServer

	SettingsFiles []SettingsFile
	Memory        []MemoryFile
	// Settings is the merged Claude Code settings document (for sandbox and friends).
	Settings map[string]any

	Home string // for shortening paths to ~
}

// Status is the assembled view.
type Status struct {
	MantleVersion, EngineVersion string

	Model          string
	Account        Account
	Provider       string
	SessionID      string
	SessionName    string
	Cwd            string
	AdditionalDirs []string

	PermissionMode string
	OutputStyle    string
	FastMode       string
	Sandbox        string
	Tools          int

	MCP           []MCPServer
	MCPSummary    string
	SettingsFiles []SettingsFile
	Memory        []MemoryFile
}

// Build assembles a Status. Missing inputs show as empty strings or "unknown".
func Build(in Inputs) Status {
	s := Status{
		MantleVersion:  orUnknown(in.MantleVersion),
		EngineVersion:  orUnknown(in.EngineVersion),
		Model:          modelLine(in),
		Account:        in.Account,
		Provider:       ProviderName(in.Account.APIProvider),
		SessionID:      in.SessionID,
		SessionName:    in.SessionName,
		Cwd:            shorten(in.Cwd, in.Home),
		PermissionMode: ModeName(in.PermissionMode),
		OutputStyle:    styleName(in.OutputStyle),
		FastMode:       fastLine(in.FastModeState, in.FastModeDisabledReason),
		Sandbox:        sandboxLine(in.Settings),
		Tools:          len(in.Tools),
	}
	for _, d := range in.AdditionalDirs {
		s.AdditionalDirs = append(s.AdditionalDirs, shorten(d, in.Home))
	}
	s.MCP = append([]MCPServer(nil), in.MCP...)
	sort.SliceStable(s.MCP, func(i, j int) bool { return s.MCP[i].Name < s.MCP[j].Name })
	s.MCPSummary = mcpSummary(s.MCP)
	for _, f := range in.SettingsFiles {
		f.Path = shorten(f.Path, in.Home)
		s.SettingsFiles = append(s.SettingsFiles, f)
	}
	for _, m := range in.Memory {
		m.Path = shorten(m.Path, in.Home)
		s.Memory = append(s.Memory, m)
	}
	return s
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func modelLine(in Inputs) string {
	name := in.ModelDisplay
	if name == "" {
		name = in.Model
	}
	if name == "" || name == "default" {
		name = "Default"
	}
	if in.ResolvedModel != "" && !strings.EqualFold(in.ResolvedModel, name) {
		return name + " (" + in.ResolvedModel + ")"
	}
	return name
}

// ProviderName turns account.apiProvider into a readable name.
func ProviderName(p string) string {
	switch p {
	case "", "firstParty":
		return "Anthropic"
	case "bedrock":
		return "Amazon Bedrock"
	case "vertex":
		return "Google Vertex AI"
	case "foundry":
		return "Microsoft Foundry"
	case "anthropicAws":
		return "Anthropic on AWS"
	case "anthropicGoogleCloud":
		return "Anthropic on Google Cloud"
	case "gateway":
		return "Gateway"
	}
	return p
}

// ModeName is the readable name of a permission mode.
func ModeName(m string) string {
	switch m {
	case "", "default", "manual":
		return "Ask before acting"
	case "acceptEdits":
		return "Accept edits"
	case "plan":
		return "Plan"
	case "auto":
		return "Auto"
	case "dontAsk":
		return "Don't ask (deny unless allowed)"
	case "bypassPermissions":
		return "Bypass permissions"
	}
	return m
}

func styleName(s string) string {
	if s == "" || s == "default" {
		return "Default"
	}
	return s
}

func fastLine(state, reason string) string {
	switch {
	case reason != "":
		return "unavailable: " + reason
	case state == "" || state == "off":
		return "off"
	case state == "on":
		return "on"
	}
	return state
}

func sandboxLine(settings map[string]any) string {
	sb, _ := settings["sandbox"].(map[string]any)
	if on, _ := sb["enabled"].(bool); !on {
		return "off"
	}
	if auto, ok := sb["autoAllowBashIfSandboxed"].(bool); !ok || auto {
		return "on, sandboxed commands run without asking"
	}
	return "on, commands still ask for permission"
}

func mcpSummary(servers []MCPServer) string {
	if len(servers) == 0 {
		return "none"
	}
	counts := map[string]int{}
	for _, s := range servers {
		counts[s.Status]++
	}
	var keys []string
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return statusRank(keys[i]) < statusRank(keys[j]) })
	var parts []string
	for _, k := range keys {
		parts = append(parts, strconv.Itoa(counts[k])+" "+k)
	}
	return strings.Join(parts, ", ")
}

func statusRank(s string) int {
	switch s {
	case "connected":
		return 0
	case "pending":
		return 1
	case "needs-auth":
		return 2
	case "failed":
		return 3
	case "disabled":
		return 4
	}
	return 5
}

func shorten(p, home string) string {
	if home == "" || p == "" {
		return p
	}
	home = filepath.Clean(home)
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// Line is one label/value pair in a section.
type Line struct {
	Label, Value string
	Problem      bool // render as a warning
}

// Section is a titled group of lines.
type Section struct {
	Title string
	Lines []Line
}

// Sections lays the status out for display, in mantle's wording.
func (s Status) Sections() []Section {
	session := Section{Title: "Session", Lines: []Line{
		{Label: "mantle", Value: s.MantleVersion},
		{Label: "Claude Code", Value: s.EngineVersion},
	}}
	if s.SessionName != "" {
		session.Lines = append(session.Lines, Line{Label: "Name", Value: s.SessionName})
	}
	if s.SessionID != "" {
		session.Lines = append(session.Lines, Line{Label: "Session ID", Value: s.SessionID})
	}
	session.Lines = append(session.Lines, Line{Label: "Directory", Value: s.Cwd})
	for _, d := range s.AdditionalDirs {
		session.Lines = append(session.Lines, Line{Label: "Also allowed", Value: d})
	}

	account := Section{Title: "Account", Lines: []Line{{Label: "Provider", Value: s.Provider}}}
	if s.Account.Email != "" {
		account.Lines = append(account.Lines, Line{Label: "Email", Value: s.Account.Email})
	}
	if s.Account.Organization != "" {
		account.Lines = append(account.Lines, Line{Label: "Organization", Value: s.Account.Organization})
	}
	if s.Account.SubscriptionType != "" {
		account.Lines = append(account.Lines, Line{Label: "Plan", Value: titleCase(s.Account.SubscriptionType)})
	}
	if src := firstNonEmpty(s.Account.APIKeySource, s.Account.TokenSource); src != "" {
		account.Lines = append(account.Lines, Line{Label: "Credentials", Value: src})
	}

	model := Section{Title: "Model and modes", Lines: []Line{
		{Label: "Model", Value: s.Model},
		{Label: "Permission mode", Value: s.PermissionMode},
		{Label: "Output style", Value: s.OutputStyle},
		{Label: "Fast mode", Value: s.FastMode},
		{Label: "Sandbox", Value: s.Sandbox},
		{Label: "Tools", Value: strconv.Itoa(s.Tools)},
	}}

	mcp := Section{Title: "MCP servers", Lines: []Line{{Label: "Summary", Value: s.MCPSummary}}}
	for _, m := range s.MCP {
		v := m.Status
		if m.Error != "" {
			v += ": " + m.Error
		}
		mcp.Lines = append(mcp.Lines, Line{Label: m.Name, Value: v, Problem: m.Status == "failed" || m.Status == "needs-auth"})
	}

	settings := Section{Title: "Settings files"}
	for _, f := range s.SettingsFiles {
		v := f.Path
		switch {
		case f.Err != "":
			v += " (ignored: " + f.Err + ")"
		case !f.Exists:
			v += " (not present)"
		}
		settings.Lines = append(settings.Lines, Line{Label: f.Scope, Value: v, Problem: f.Err != ""})
	}

	memory := Section{Title: "Memory"}
	for _, m := range s.Memory {
		memory.Lines = append(memory.Lines, Line{Label: titleCase(m.Scope), Value: m.Path})
	}
	if len(memory.Lines) == 0 {
		memory.Lines = []Line{{Label: "Files", Value: "none loaded"}}
	}
	return []Section{session, account, model, mcp, settings, memory}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
