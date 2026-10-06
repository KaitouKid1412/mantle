# Request 04 → 07: footer while the fullscreen / menu is open

**From:** plan 04 (input). **To:** plan 07 (chrome). **Status:** resolved by plan 04
(2026-10-06): no chrome code change needed; pinned by a chrome test.

Since worktree-04-input 5707c6e the `/` and `@` menus in fullscreen are an overlay over
the rows above the prompt (as in Claude Code), and `EditorStateMsg.Panel` counts only
what shows *below* the prompt. So in fullscreen Panel stays false while the overlay is
open: the footer and the effort line keep their rows, the prompt doesn't move, and the
overlay covers the effort line, as Claude's does.

## Footer: checked side by side (claude 2.1.290, fullscreen and inline)

Per mode (default, plan, acceptEdits), Claude's footer with the overlay open is exactly
its footer for any non-empty prompt: the manual mode drops "? for shortcuts" (and "←
for agents"), other modes keep their cycle hint. chrome's footer already does the same
(it keys off `EditorStateMsg.Empty`), so nothing changes in chrome. The remaining
differences are chrome's own wording ("manual approval" / "shift+tab to change" for
Claude's "manual mode on" / "(shift+tab to cycle)") and Claude's "← for agents" hint,
which mantle does not show at all; neither is about the overlay.

Pinned by `features/chrome` TestFooterWithFullscreenSuggestionOverlay (plan 04 added it
in plan 07's test file; no chrome code touched).
