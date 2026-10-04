# 09 → 01: Claude Code settings writer, MCP status message

**Status:** resolved 2026-10-03 in `contracts-v1.3`: `ext.ClaudeSettingsWriter`
(`SetClaude(scope, key, value)`, implemented by the host's settings via `internal/config`)
and `ext.MCPStatusMsg` as proposed.

1. **Settings writer.** `ext.ClaudeSettingsWriter` (`ctx.Settings().(ext.ClaudeSettingsWriter)`)
   with `SetClaude(scope, key, value) tea.Cmd`: locked, re-read-and-merge, atomic write;
   `SettingsMsg` follows. Plan 09 uses it for `skillOverrides` (/skills, followed by
   `reload_skills`) and `autoMemoryEnabled` (/memory, /pause-memory).
2. **MCP status.** `ext.MCPStatusMsg{EngineID, Total, Connected, NeedsAuth, Failed}`. Plan 09
   sends it after every `system/init` and `mcp_status` refresh; the footer (plan 07) shows
   it. Plan 09 also raises a notice (`Key: "mcp.needs-auth"`) when the needs-auth count
   goes up.
