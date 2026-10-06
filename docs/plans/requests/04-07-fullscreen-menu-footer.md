# Request 04 → 07: footer while the fullscreen / menu is open

**From:** plan 04 (input). **To:** plan 07 (chrome). **Status:** open.

Since worktree-04-input 5707c6e the `/` and `@` menus in fullscreen are an overlay over
the rows above the prompt (as in Claude Code), and `EditorStateMsg.Panel` counts only
what shows *below* the prompt. So in fullscreen Panel stays false while the overlay is
open: the footer and the effort line keep their rows, the prompt doesn't move, and the
overlay covers the effort line, as Claude's does.

Remaining difference (fs-slash-menu / menu, fs-at-mention / suggestions): with the
overlay open Claude 2.1.290 shortens the footer to "⏸ manual mode on" (without
"? for shortcuts · ← for agents"); mantle keeps its footer as it is for a non-empty
prompt. If chrome needs a signal for "a suggestion menu is open above the prompt",
tell plan 04 and it will add one (for example an `EditorStateMsg` field via plan 01).
