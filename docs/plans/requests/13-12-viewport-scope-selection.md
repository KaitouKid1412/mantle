# 13 → 12: scoped viewport and selection broadcast (MT-R2, MT-R7)

**Status:** open (needs `contracts-v1.11`)

Research mode shows one conversation node at a time in the existing fullscreen viewport
instead of adding its own `SlotLive` component (only one is visible).

1. **Scope.** Handle `ext.TranscriptScopeMsg` in `features/fullscreen/transcriptview.go`
   and `render.go`.
   - While `Source != nil`, take items from `Source` instead of `ctx.Transcript()`, and
     skip the `printed` merge.
   - On every scope change, reset the selection, search and expansion state, and scroll to
     the top.
   - `Source == nil` restores today's behaviour exactly.
   - Cache keys must include the scope (`Owner` plus the first item ID) so switching nodes
     does not reuse lines.
   - Keep following the tail while the scoped node is streaming and the view is at the
     bottom.
2. **Selection broadcast.** When a selection is finalized (mouse release, or the end of a
   keyboard extension), broadcast `ext.SelectionMsg{Source: <viewport ID>, Text}`.
   - Send `Text: ""` when the selection is cleared.
   - `Text` must be source-like: join soft-wrapped lines and drop gutters and glyphs, the
     same way copy does.
   - Copy-on-select is unchanged.
3. **The `>` key.** Let an ambient-context binding (`Research`: `>` → `mantle:research.quote`)
   win over the viewport's default key handling while a selection exists.

Tests:
- Scoped render shows only the scoped items.
- A nil scope restores the full transcript.
- `SelectionMsg` is emitted on mouse release and cleared on click.
