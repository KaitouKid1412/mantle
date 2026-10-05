# 12 → 01: after a Reprint the live area keeps a closed dialog's height (inline)

**Status:** resolved on integration-8: plan 03's printer fix through plan 01 (dd409a8); `resume-picker` passes (2026-10-05). Found by the plan 12 side-by-side suite on integration-7
(`resume-picker`, mantle; it passed on integration-6).

`resume-picker`: start fresh, `/resume`, pick the earlier session. claude shows its header
and the conversation on screen. mantle prints the banner, the conversation and the turn
line, but they all end up in scrollback; the screen shows ~25 blank rows, the turn line
and the prompt. The scenario's `wait_for "Remember the word kumquat."` (screen only) times
out.

Raw output after the switch (`go run ./test/parity/cmd/sidebyside -targets mantle -run
'^resume-picker$' -raw`, `out/raw/resume-picker.mantle.1.raw`):

```
ESC[2J ESC[3J ESC[H ESC[?25l \r ESC[26A ESC[J  \n × 25  ────…  ❯  ────…  (footer)
… then the banner, "❯ Remember a word for me", "⏺ Remember the word kumquat.",
"✻ … for 2s · done 3:…"
```

Right after the clear, the live frame is drawn with 25 blank rows above the prompt: the
height of the `/resume` picker that just closed (the "tallest frame of the last 250 ms"
the printer and renderer keep). Everything printed next goes above that padded frame and
scrolls off the 30-row screen.

Ask: after a Reprint's clear, start the live area (and the printer's `liveHeight`) from
the current frame's real height, not the tallest recent frame; the screen is empty, so
there is nothing a shorter frame could leave behind. Regression candidates in
integration-7: 1baa21a (Reprint through the printer queue) together with plan 06's
`showAfterClear` printing the history as soon as `ScreenClearedMsg` arrives.
