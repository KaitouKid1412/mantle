# 09 → 06: hand-off seam for plan 09's H commands

**Status:** agreed (2026-10-03): one hand-off mechanism, plan 06's dialog.

Plan 09 registers ~30 commands that open interactive Claude Code (`/remote-control`,
`/teleport`, `/upgrade` (plan part), `/feedback`, `/bug`, `/install-github-app`, …, see
plan 09 B10). They use plan 06's B5 hand-off through its dialog, without importing
`features/sessions`:

```go
ctx.OpenDialog("dialog.handoff", []string{"/remote-control"}) // extra claude args
ctx.OpenDialog("dialog.handoff", nil)                         // just open the session
```

The single element is the slash command line (`"/feedback some text"`), which plan 06
passes as `claude --resume <sid> <arg>`, so interactive Claude Code runs it at startup.
Plan 09 decides E vs H first (commands the engine lists headlessly are sent to the
engine instead), so the dialog only sees real hand-offs.

Spike to confirm in B5: `claude --resume <sid> "/<cmd>"` runs the slash command at
startup in interactive mode (the initial prompt takes the same input path as typed
text). If it doesn't, open the session without the arg and show "type /<cmd>".
