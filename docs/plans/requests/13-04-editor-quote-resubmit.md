# 13 → 04: quote into editor, resubmit drafts (MT-R6, MT-R7)

**Status:** done in b07ad9f (`Editor.SetQuote` in `pkg/ui/editor/quote.go`, handled in
`features/input`; `stageHistory` skips `Resubmit`). Tests: `TestSetQuote*`
(`pkg/ui/editor`), `TestEditorQuote`, `TestResubmitSkipsHistory` (`features/input`).
Quoting also leaves bash mode; text lines without `>` get `> ` (blank lines become `>`).

1. **`ext.EditorQuoteMsg{Text}`.**
   - If the buffer starts with a quote block (lines starting with `> `, then one blank
     line), replace that block. Otherwise insert `> `-prefixed lines plus a blank line at
     the start.
   - Keep the rest of the buffer (the typed question) and put the cursor at the end.
   - It must be one undo step.
   - Empty `Text` removes an existing quote block.
2. **`Draft.Resubmit`.** `stageHistory` must not record a draft whose `Resubmit` is true;
   it was recorded the first time. Every other stage runs as usual.
   - Research's stage (priority 600) consumes some drafts while it switches branches, and
     re-submits them with `ctx.Submit`.
   - The echo and send path needs no change.

Tests: quote insert, replace and remove, plus undo. A resubmitted draft is not added to
history twice.
