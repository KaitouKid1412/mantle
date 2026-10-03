package ext

import tea "charm.land/bubbletea/v2"

// Additions in contracts-v1.3.

// ClaudeSettingsWriter writes Claude Code settings files. The host's Settings
// implements it; get it with ctx.Settings().(ext.ClaudeSettingsWriter). Prefer engine
// control requests (set_model, apply_flag_settings, update_settings) when one exists.
type ClaudeSettingsWriter interface {
	// SetClaude writes one top-level key of a scope file (ScopeUser, ScopeProject or
	// ScopeLocal) under a file lock, re-reading and merging just before the atomic
	// write; nil deletes the key. A SettingsMsg follows once the change is loaded.
	// mantle never writes ~/.claude.json.
	SetClaude(scope, key string, value any) tea.Cmd
}

// MCPStatusMsg summarises MCP server states for an engine, sent after each
// system/init (and mcp_status refresh) by the ecosystem feature; the footer shows it.
type MCPStatusMsg struct {
	EngineID  string
	Total     int
	Connected int
	NeedsAuth int
	Failed    int
}
