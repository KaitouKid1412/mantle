# Request 08 → 06: a hand-off seam reachable by ID

From plan 08 (settings panels), for plan 06 (sessions; owner of the generic hand-off).

`/privacy-settings` is a hand-off command (coverage H): mantle stops the engine, runs the
real `claude --resume <session>` so the user can use the interactive panel, then restarts
the engine. Plan 08 must not import `features/sessions`, so it needs the hand-off by ID.

## Proposal

Register a dialog **`dialog.handoff`** whose `OpenDialog` argument is a `[]string` of
extra `claude` arguments appended after `--resume <session-id>`:

```go
ctx.OpenDialog("dialog.handoff", []string{"/privacy-settings"})
```

The dialog (or a confirm step inside it) explains that the session continues in Claude
Code, stops the main engine (`ext.EngineStopMsg`), runs the command with
`tea.ExecProcess`, and restarts the engine with `--resume` on return
(`ext.EngineStartMsg`). Any other argument type (or nil) means a plain hand-off with no
extra arguments.

Plan 08 already calls `OpenDialog("dialog.handoff", []string{"/privacy-settings"})` for
`/privacy-settings`; until the dialog exists the host reports an unknown dialog.
