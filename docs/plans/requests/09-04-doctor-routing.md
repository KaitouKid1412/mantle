# 09 → 04: native `/doctor` vs the engine's `/doctor` skill

**Status:** open (2026-10-03).

In 2.1.288, `/doctor` inside a session is a bundled skill (prompt type, alias
`checkup`). Plan 09 registers a native `/doctor` panel (mantle's checks, `claude doctor`
via ExecProcess) and, as one of its options, sends `/doctor` straight to the engine with
`Engine.Send` ("Ask Claude to diagnose").

Request for slash routing (plan 04):

- Native commands win over engine commands with the same name (as
  `ext.CommandsMsg` already says), so `/doctor` opens the panel.
- Engine aliases that a native command doesn't claim still route to the engine, so
  `/checkup` keeps reaching the skill directly.
- Same rule for the other names plan 09 registers natively while the engine also lists
  them: `/mcp`, `/plugin`, `/skills`, `/hooks`, `/agents`, `/memory`, `/login`,
  `/logout`, `/pause-memory`. For `/pause-memory` and the hand-off commands plan 09
  itself passes the command to the engine when `initialize.commands` lists it.
