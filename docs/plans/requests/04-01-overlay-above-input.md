# Request 04 → 01: fullscreen overlay above the input (FYI, optional promotion)

**From:** plan 04 (input). **To:** plan 01 (host, pkg/ext steward). **Status:** done in
the host (worktree-04-input 5707c6e); promotion to pkg/ext optional.

## What changed in internal/app

`internal/app/fullscreen.go` has a new optional interface, matched structurally:

```go
type aboveInputOverlay interface {
	OverlayAboveInput() bool
}
```

In the fullscreen layout, a SlotAboveInput component that reports true takes no rows of
the bottom stack. Its lines, padded to the full width, are layered (Z 2) over the rows
directly above the input, stacked upwards. Inline it is an ordinary component. Test:
`internal/app/fullscreen_overlay_test.go`.

## Why

Claude Code draws the `/` and `@` suggestion menus this way in fullscreen (2.1.289 and
2.1.290, checked side by side): over the transcript's last rows and the effort line, so
the input bar never moves. `input.suggestions` uses it.

## Optional

If you prefer a documented pkg/ext optional interface (like `TerminalStater`), promote
it in a later contracts tag with the same method name; nothing else needs to change.
