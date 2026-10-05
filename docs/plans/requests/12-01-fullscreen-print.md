# 12 → 01: printed output is lost in the fullscreen layout

**Status:** resolved 2026-10-05: contracts-v1.10 (ext.PrintedMsg, plan 01 80c6b0c); plan 12's viewport shows printed blocks in place (features/fullscreen/printed.go). Found by the plan 12 fullscreen scenarios (fs-*,
default-renderer) against claude 2.1.289.

In the fullscreen layout `Ctx.Print` drops its blocks (`internal/app/commit.go`,
`enqueuePrint`: "the fullscreen renderer draws the transcript itself; no scrollback").
The transcript is drawn by plan 12's viewport, but other features print things that are
not transcript items, and in fullscreen they never appear:

- chrome's banner (claude shows its header at the top of the fullscreen transcript:
  `fs-slash-menu` / `start`, `default-renderer` / `answered`);
- the panel closing lines plans 08 and 09 just added ("⎿  Help closed", "⎿  MCP servers
  closed", "⎿  Kept model as …");
- anything a mod prints.

Ask (additive): in the fullscreen layout, deliver printed blocks to features instead of
dropping them, for example

```go
// PrintedMsg carries blocks a feature printed while the fullscreen layout is active
// (inline they go to scrollback). The fullscreen transcript shows them in place.
type PrintedMsg struct {
	Blocks []string
}
```

broadcast when `Print` is called in fullscreen (and nothing in inline, where scrollback
already has them). Plan 12's viewport will keep them in its document, anchored after the
transcript item that was last when they arrived, so the banner sits at the top and closing
lines follow their command.

Switching layouts (`/tui`) reprints anyway, so no history needs to cross the switch.
