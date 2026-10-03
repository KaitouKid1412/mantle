# 06 → 01 (cc 02, 03, 11): history hand-off and startup resume

**Status:** open (2026-10-03).

Plan 06 turns JSONL history into `ext.Item`s (`features/sessions/normalize.go`), using
plan 03's conventions: `user.prompt`/`user.bash` carry `*proto.User` (ID `user:<uuid>`),
text and thinking carry `*proto.ContentBlock` (`txt:`/`thk:<uuid>:<i>`), tool calls carry
`*proto.ToolUse` with `Result` set (ID = tool_use id), subagent items nest by `ParentID`,
`system.compact_boundary` carries `*proto.CompactBoundary`.

## 1. History → transcript store (01 adds, 03 handles)

Features can't import `features/transcript`, so the items need a message. Additive:

```go
// TranscriptHistoryMsg hands finished items from an earlier session (plan 06's JSONL
// normalizer) to the transcript store of EngineID. With Reset the store is emptied
// first (session switch, rewind, return from a hand-off). Items with a ParentID nest
// under that item. The sender follows it with Ctx.Reprint() when the screen must be
// redrawn; otherwise the commit policy prints the new items as usual.
type TranscriptHistoryMsg struct {
	EngineID string
	Items    []*Item
	Reset    bool
}
```

Plan 03: on this message, `Reset()` if asked, then `AppendHistory(items)`.

## 2. Startup resume (01 host, 11 flags)

- Plan 11 resolves `-r <id|search>` and `-c` with `sessions.ResolveResume` (in
  `internal/sessions`, importable from `internal/cli`): it returns the session id and
  its JSONL path, or the candidates when a search is ambiguous.
- The host spawns the main engine with `SpawnOpts{Resume: id, ForkSession, SessionID}` as
  usual, and **sets the main engine's `SessionInfo.SessionID` (and `Cwd`) to the resolved
  id before running `OnStart` hooks**. Plan 06's OnStart hook prints the history for that
  id before the engine produces any output. (Fallback: plan 06 also loads history on the
  first `SessionChangedMsg` while the store is empty.)
- `-r` with no value: spawn a fresh engine and run the `resume` command with empty args
  from OnStart (`ctx.Command("resume")` then `Run(ctx, "")`); it opens `dialog.resume`.
  Choosing a session restarts the engine with `--resume`.

## 3. `Engine.Restart` semantics (02)

Plan 06 calls `Restart` with only the session fields set (`Resume`, `ForkSession`,
`ResumeSessionAt`, `ResumeDropsTurn`, `SessionID`, `Cwd` for `/cd`). Please treat it as
"same launch options as before, with these session fields replaced", so `ExtraArgs`,
`Settings` (gates), `Model` and `PermissionMode` carry over. `Stop: true` stops the process
for a hand-off; plan 06 keeps the `Engine` handle and calls `Restart` on it afterwards, so
the handle must stay usable after a Stop (even if the host detaches it).
