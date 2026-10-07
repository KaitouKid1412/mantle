# 13 → 07: research indicator in the footer (MT-R1)

**Status:** open (needs `contracts-v1.11`)

Subscribe to `ext.UIModeChangedMsg` in `features/chrome`. While `Mode == ext.UIModeResearch`:
- Show a research indicator in place of the quiet manual one, using mantle's own wording,
  for example `⌕ research` plus `· <key> to change`.
- Get the key from `firstKey(ctx, ext.ContextChat, ext.ActChatCycleMode, "shift+tab")`.
- Use an existing theme token. `Suggestion` is a good fit.

Files: `features/chrome/mode.go`, `footer.go`, `session.go`.
Tests: footer golden with research on.
