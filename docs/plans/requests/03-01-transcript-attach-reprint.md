# 03 → 01: transcript attach and reprint hand-off

**Status:** resolved 2026-10-03 in `contracts-v1.1` (`ext.TranscriptAttachMsg`, `ext.ScreenClearedMsg`, `Ctx.Renderer`), as proposed.

Plan 03 owns the transcript store and the commit policy; the host owns `Ctx.Transcript()`,
`Ctx.Print` and `Ctx.Reprint`. Two hand-offs are not in `contracts-v1` yet. Both are
additive.

## 1. How the host learns about the store

`Ctx.Transcript()` must return plan 03's store, but nothing in `pkg/ext` lets a feature
hand it over. Proposal (mirrors `EngineAttachMsg`):

```go
// TranscriptAttachMsg registers the transcript store with the host, so
// Ctx.Transcript() returns it. The transcript feature sends it from OnStart.
type TranscriptAttachMsg struct{ Transcript Transcript }
```

## 2. Who reprints the store

`Ctx.Reprint()` says "clear screen and scrollback, then reprint the store". Rendering
the store needs the commit policy (watermark, progressive blocks, view mode, the
renderer registry), which lives in plan 03. Proposal: the host only clears, then
delivers a message; the commit policy resets its watermark and prints everything
finished with `Ctx.Print`:

```go
// ScreenClearedMsg is delivered after Ctx.Reprint has cleared the screen and the
// scrollback (ESC[2J ESC[3J ESC[H). The transcript commit policy reprints the store
// with Ctx.Print when it sees it.
type ScreenClearedMsg struct{}
```

Ordering: the Print Cmds plan 03 returns while handling `ScreenClearedMsg` must run after
the clear, which holds if the host emits the clear first and only then delivers the
message.

## 3. Renderer lookup

Renderers are registered with `AddRenderer`, but overrides (Replace/Wrap from mods)
are resolved by the host. The commit policy needs the *resolved* renderer for a key.
Proposal: `Ctx.Renderer(key ContentKey) Renderer` (resolution order of
`ContentKey.Candidates()`, overrides applied; never nil, falls back to `default`).

Until these land, plan 03 keeps a local renderer table (its own registrations only) and
sends nothing, so `Ctx.Transcript()` stays nil.
