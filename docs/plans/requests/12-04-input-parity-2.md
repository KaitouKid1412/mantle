# 12 → 04: input differences, round 2 (parity audit on integration-6)

**Status:** open (2026-10-05). Found by the plan 12 side-by-side suite against claude
2.1.289 on integration-6. Regenerate a frame with
`make parity-side-by-side PARITY_ARGS="-run <scenario>"`; frames in
`test/parity/out/<scenario>/<checkpoint>.txt`. Keep mantle's own wording.

## 1. History numbering

`history` / `recalled`: after two prompts, one ↑ shows the newer prompt titled
"History 2/2" in claude (entries counted oldest first) and "History 1/2" in mantle.

## 2. Fullscreen ctrl+r is a dialog (VW-22, moved from 12)

`fs-history-search` / `search`: in the fullscreen layout claude opens a "Search prompts"
dialog (scope "everywhere", rows "<age> ago  <prompt>", a preview box, a search box at
the bottom, "↑/↓ to nav · Enter to use · Esc to cancel · ctrl+s to scope"). Inline it is
the one-line search mantle has. The history data and search are plan 04's, so the
fullscreen presentation is too; plan 12 hands VW-22 over (its viewport has nothing to add).

## 3. Esc in history search restores the prompt

`fs-history-search` / `closed`: esc in claude's search returns to the prompt as it was
(empty here); mantle keeps the match ("alpha prompt") in the prompt.

## 4. `/clear` echo

`clear` / `cleared`: claude echoes "❯ /clear" at the top of the cleared screen (after its
header); mantle shows no echo.

## 5. Menu entries claude doesn't list

`slash-menu` / `menu`, `fs-slash-menu` / `menu`, `slash-menu` / `filtered`: with an API key
and no claude.ai login, claude's first page is /add-dir, /autocompact, /background,
/branch, /btw, /bug, /cd, /clear, /color, /compact, /config, /context. mantle also lists
/agent-view, /agents, /auto-mode-setup and /batch there, and /auto-mode-setup and
/claude-api when filtering "/model". Find out which of these the engine marks hidden or
unavailable (or which registering plan should set `Command.Hidden` /
`CommandVisibilityMsg`), and match claude's list.
