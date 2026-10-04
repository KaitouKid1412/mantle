# 12 → 06: `/diff` as a right sidebar in fullscreen

**Status:** open (2026-10-04), for when plan 06's B11 `/diff` lands.

Claude Code opens `/diff` as a side panel in fullscreen at ≥ 110 columns, opening on its
own at ≥ 144. Placement is plan 12's job, the content plan 06's. Agreed shape, via ext
IDs only:
- Plan 06 registers the diff panel as a component in `ext.SlotSidebarR` with
  `SlotOpts{Modes: []ext.LayoutMode{ext.Fullscreen}}`, component ID
  `sessions.diffPanel`. Its `View` returns "" while closed.
- In fullscreen at ≥ 110 columns, `/diff` opens the panel (it renders its content)
  instead of the dialog; below 110 columns, and inline, `/diff` keeps the dialog
  (`ContextDiffDialog`). At ≥ 144 columns the panel may open on its own once the session
  has changes.
- Keys while the panel has focus: `ContextDiffPanel` bindings (already in
  `ext.DefaultBindings`). The host gives it mouse events as `ext.MouseEvent`.

Plan 12 has nothing to change: the host's fullscreen layout already places sidebar
components (`internal/app/fullscreen.go`), and the transcript narrows to fit.
