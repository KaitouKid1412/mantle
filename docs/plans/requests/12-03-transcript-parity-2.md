# 12 → 03: transcript differences, round 2 (parity audit on integration-6)

**Status:** open (2026-10-05). Found by the plan 12 side-by-side suite against claude
2.1.289 on integration-6. Regenerate a frame with
`make parity-side-by-side PARITY_ARGS="-run <scenario>"`; frames in
`test/parity/out/<scenario>/<checkpoint>.txt`. Keep mantle's own wording.

## 1. Single newlines in assistant text are joined (TR-01) — both layouts

`fs-scroll` (any layout): an answer whose lines are separated by single newlines
("Line 01 of the long answer.\nLine 02 …") shows one line per source line in claude.
mantle joins them into one wrapped paragraph ("Line 01 of the long answer. Line 02 of
the long answer. Line 03 …"). Claude renders a single newline in assistant markdown as a
line break.

## 2. "Waiting…" only under Bash prompts

`edit-diff` / `edit-prompt`, `ask-user` / `question`, `plan-mode` / `plan-dialog`:
claude shows "⎿  Waiting…" under a pending Bash call only. While an Edit prompt is open
it shows just "⏺ Update(main.go)"; while AskUserQuestion or the plan approval is open it
shows no row for the tool at all. mantle adds "⎿  Waiting…" under Edit, and shows
"⏺ Asked questions ⎿ Waiting…" and "⏺ Plan ready for review ⎿ Waiting…" rows.

## 3. Engine notices rendered as answers

`plain-qa` / `answered`, `plan-mode`: mantle shows the engine's
"We're changing auto mode to no longer charge for classifier requests … ask your gateway
to implement …" message as an assistant row. claude 2.1.289 doesn't show it in the
transcript (it appears with fakeapi because the requests go to a local gateway). Show
engine notices of this kind as a notice (or not at all), not as a reply.

## 4. Turn line on resumed sessions (with 06)

`resume` / `resumed`, `resume-picker` / `resumed`: claude shows "✻ <verb> for <dur> ·
done <time>" after each resumed turn (from the transcript's turn-duration entries);
mantle shows none for resumed turns.

## 5. Fullscreen tool rows (VW-15)

`fs-tool-approval`: in the fullscreen layout claude draws a pending Bash call as
"⏺ <description>" with "⎿  $ <command>" under it, and a finished one inside a collapsed
"Ran 1 shell command" summary. mantle's fullscreen viewport uses the inline rows
("⏺ Bash(touch …)", "⎿  Done"). The viewport takes plan 03's `Store.Lines`, so this is a
layout-dependent choice in the renderers (`ctx.Layout()`).
