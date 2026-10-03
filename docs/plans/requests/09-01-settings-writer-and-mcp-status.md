# 09 → 01: Claude Code settings writer, MCP status message

**Status:** open (2026-10-03). Both additive.

## 1. Writing a Claude Code settings key

`/skills` cycles `skillOverrides.<name>` and `/memory` toggles `autoMemoryEnabled`. Both
must persist, and `update_settings` only accepts a short allowlist (outputStyle,
effortLevel), so a file write is needed. The overview asks for one atomic, flock'd
merge-writer for `settings*.json`; it isn't in the contracts yet.

Proposal (on `ScopedSettings`, which only mantle implements):

```go
// SetClaude merges key=value into one scope's settings file (userSettings,
// projectSettings or localSettings) with an atomic, locked read-modify-write that
// keeps every other key. A nil value deletes the key. SettingsMsg follows.
SetClaude(scope, key string, value any) tea.Cmd
```

Until it lands, plan 09 writes through `internal/claudecli/settingsfile` (lock file +
temp file + rename, unknown keys preserved), plus `apply_flag_settings` so the running
engine sees the change at once. Plan 09 switches to `SetClaude` when it exists.

## 2. MCP needs-auth count for the footer

Plan 07's footer shows how many MCP servers need authentication. Plan 09 computes it
from each `system/init`. Proposal:

```go
// MCPStatusMsg summarises an engine's MCP servers after each system/init.
type MCPStatusMsg struct {
	EngineID                          string
	Total, Connected, NeedsAuth, Failed int
}
```

Until then plan 09 raises a notice with `Key: "mcp.needs-auth"` when the count goes up.
