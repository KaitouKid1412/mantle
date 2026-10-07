# 13 → 07: research indicator in the footer (MT-R1)

**Status:** done in 20db434. `sessionState.UIMode` follows `ext.UIModeChangedMsg`; while it
is `research` the footer shows `⌕ research · <cycle key> to change` in `suggestion`
(`ResearchIndicator`, `mode.go`). Tests: `TestFooterResearchMode`, golden
`chrome.footer_research`; `make test-07` and `make lint` pass.

Subscribe to `ext.UIModeChangedMsg` in `features/chrome`. While `Mode == ext.UIModeResearch`:
- Show a research indicator in place of the quiet manual one, using mantle's own wording,
  for example `⌕ research` plus `· <key> to change`.
- Get the key from `firstKey(ctx, ext.ContextChat, ext.ActChatCycleMode, "shift+tab")`.
- Use an existing theme token. `Suggestion` is a good fit.

Files: `features/chrome/mode.go`, `footer.go`, `session.go`.
Tests: footer golden with research on.
