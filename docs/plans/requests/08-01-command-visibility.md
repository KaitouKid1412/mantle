# Request 08 → 01: hide registered commands at runtime (availability)

From plan 08, for plan 01 (API steward). Plan 09 needs the same. Additive.

**Status:** open (2026-10-05).

## Why

The coordinator decided that mantle's `/` menu hides the commands that Claude Code hides
for the current account or provider, re-evaluated after `/login`. Whether a command is
available is only known at runtime, from `initialize` (account, models, engine command
list). For example, `/fast` is hidden when no model offers fast mode, and `/advisor` is
hidden when the advisor feature is off (its headless twin is then missing from
`initialize.commands`).

`ext.Command.Hidden` is fixed at registration. Hiding a built-in also has a side effect
today: `uiCtx.Commands()` skips a hidden built-in without reserving its name, so the
engine's runtime command of the same name (e.g. the headless `/fast`) appears in its
place.

## Proposal

```go
// CommandVisibilityMsg hides or shows registered commands at runtime (availability for
// the current account, provider or model). Source names the feature deciding (its
// entries replace that source's earlier ones). A hidden command keeps its name
// reserved: runtime commands with the same name (the engine's list) don't show either,
// and typing it still runs the registered command.
type CommandVisibilityMsg struct {
	Source string
	Hidden map[string]bool // command name → hidden (false re-shows)
}
```

Host: keep an overlay per Source; `Commands()` (menus, /help) leaves out names the
overlay hides and marks them as seen; `Command(name)` keeps resolving them, so a hidden
command still runs when typed (Claude Code's hidden commands do too).

Plan 08 sends it after `initialize`, `list_models` and `commands_changed`, so a
`/login` that restarts the engine re-evaluates visibility.
