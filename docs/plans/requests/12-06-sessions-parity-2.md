# 12 → 06: sessions differences, round 2 (parity audit on integration-6)

**Status:** open (2026-10-05). Found by the plan 12 side-by-side suite against claude
2.1.289 on integration-6. Regenerate a frame with
`make parity-side-by-side PARITY_ARGS="-run <scenario>"`; frames in
`test/parity/out/<scenario>/<checkpoint>.txt`.

## 1. Hand-off leaves mantle's live area on screen (SE-40)

`handoff` / `panel`: when mantle hands the terminal to Claude Code, mantle's last frame
(transcript tail, "❯ /mobile" echo, the prompt frame and footer) stays on screen and
Claude Code's output starts below it. Clear mantle's live region (or the screen) before
`tea.ExecProcess`, so Claude Code starts on a clean screen as it would on its own.

## 2. Turn lines on resume (with 03 round 2 §4)

`resume`, `resume-picker`: claude shows the "✻ … for <dur> · done <time>" line after
each resumed turn, from the transcript's turn-duration entries; mantle shows none.
`features/sessions/normalize.go` maps `SysTurnDuration`; check that it reaches the
renderer for resumed history.
