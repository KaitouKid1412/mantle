# 12 → 03: streaming-text cache in the fullscreen layout

**Status:** resolved 2026-10-04 in plan 03 commit 18ce53f: finished text items drop their
streaming state in any layout, and the store has `Lines(c, w) ([]string, []ext.Block)`,
which the fullscreen viewport uses when present.

In the fullscreen layout the transcript store is drawn by plan 12's viewport
(`features/fullscreen`), which calls the resolved renderers through `Ctx.Renderer` and
caches the resulting lines itself, keyed by item id, revision, children, width, view
mode, theme and expansion.

Two things on plan 03's side:

1. **The text renderer's streaming cache never shrinks in fullscreen.** The
   streaming-markdown state (`f.texts`, `textEntry`, `forget(id)` in render_text.go) is
   pruned only by the inline commit policy, which does nothing while `Layout() !=
   Inline`. In a long fullscreen session it grows with every assistant block. Suggestion:
   also forget an entry once its item is `Done` and has been rendered once, whatever the
   layout (or on `ext.LayoutChangedMsg{Mode: Fullscreen}`).
2. **View modes and the focus-view summary.** Fullscreen reads `verbose` / `viewMode`
   from settings itself, but it can't see the transcript's brief toggle (ctrl+shift+b),
   the engine's view mode, or the "Used N tools" focus summary. For identical content in
   both layouts, an additive way to get "the transcript's lines at width w" would help,
   for example an optional interface on the Transcript the store already implements:

   ```go
   // Lines returns the transcript rendered at width w in the current view mode, one
   // block per visible item (focus/brief summaries included), with block IDs.
   Lines(c ext.Ctx, w int) []ext.Block // plus IDs, or a []struct{ID string; Block ext.Block}
   ```

   The ctrl+o viewer's `transcriptLines` already computes this.

Nothing breaks without these; fullscreen works with the renderers today.
