# 12 → 01: turning the fullscreen layout on, live switching, mouse switch, sidebar widths

**Status:** open (2026-10-04). Plan 12's fullscreen feature (B7–B9) renders through the
B14 hooks in `contracts-v1.4` (`fullscreenView`, OnMouse hit-testing, `ext.MouseEvent`).
What's missing is on the host side.

## 1. Choose the layout at startup (`cmd/mantle-ui`)

`app.Options.Layout` is never set, so mantle is always inline. Claude Code's rules:
- fullscreen when the merged setting `tui` is `"fullscreen"` or `CLAUDE_CODE_NO_FLICKER`
  is set (truthy);
- never when `CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN` is set, or in screen-reader mode
  (`Ctx.Accessibility().ScreenReader`), which always stays inline.

## 2. Switch at runtime

`/tui fullscreen` (plan 08) writes the setting but the layout can't change until
restart. Proposal (additive):

```go
// LayoutRequestMsg asks the host to switch between Inline and Fullscreen. The host
// applies the startup rules above (it may refuse, e.g. in screen-reader mode), then
// broadcasts LayoutChangedMsg.
type LayoutRequestMsg struct{ Mode LayoutMode }

// LayoutChangedMsg reports the new layout after a switch.
type LayoutChangedMsg struct{ Mode LayoutMode }
```

The host could also react to `SettingsMsg{Changed: ["tui"]}` itself. Leaving fullscreen
should resume the printer and `Reprint` if the inline watermark is stale (00-overview,
"Switching modes at runtime").

## 3. `CLAUDE_CODE_DISABLE_MOUSE`

In fullscreen `View.MouseMode` is always `MouseModeCellMotion`. With
`CLAUDE_CODE_DISABLE_MOUSE` set it should be `MouseModeNone` (keyboard scrolling still
works through the `Scroll` context).

## 4. Resizable sidebars

`SidebarWidth(w)` is fixed at `min(40, max(20, w/4))`. Claude Code resizes panes with
`ctrl+x` arrows (`Pane` context, `pane:grow` / `pane:shrink`). Proposal: the host keeps a
width per sidebar slot (default as today, clamped to 15…w/2) and adds

```go
// SidebarResizeMsg changes a sidebar slot's width by Delta columns (negative shrinks).
type SidebarResizeMsg struct {
	Slot  Slot // SlotSidebarL or SlotSidebarR
	Delta int
}
```

Plan 12 binds `pane:grow` / `pane:shrink` to send it for the sidebar that has focus.

## 5. Mouse events can arrive out of order

`View.OnMouse` returns a Cmd, and Bubble Tea runs it with `go p.Send(cmd())`, so a
click, the drag motions and the release are each delivered from their own goroutine and
can be reordered (a release before the last motion ends a drag early). Proposal: keep
the last Compositor in the Root, and in `Update` hit-test raw `tea.MouseMsg`s against it
and deliver `AddressedMsg{MouseEvent}` synchronously, in order; drop `View.OnMouse`.
That also makes the wheel arrive once (next note).

## Note: wheel events arrive twice in fullscreen

Bubble Tea delivers a wheel event both to `View.OnMouse` (→ `AddressedMsg{MouseEvent}`)
and to `Update` (→ `routeWheel` → `scroll:*` actions). Plan 12 scrolls only through the
`scroll:*` actions and ignores wheel `MouseEvent`s; other features should do the same.
