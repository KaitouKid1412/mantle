# 12 → 08: /model and /help (parity audit)

**Status:** done by 08 (§1 eaebba8, §2 e29e9ba, §3 9d2cd38, §4 dc1d584; /fast and /advisor hidden via CommandVisibilityMsg, 2efb222) (2026-10-05). Found by the plan 12 side-by-side suite against claude
2.1.289 (inline renderer, fakeapi). Regenerate a frame with
`make parity-side-by-side PARITY_ARGS="-run <scenario>"` and read
`test/parity/out/<scenario>/<checkpoint>.txt`. Keep mantle's own wording.

## 1. /model effort default (ST-01, ST-07) — data

`model-picker` / `shown`: claude's picker says "◐ Medium effort (default)" (and its
footer hint shows "◐ medium · /effort"); mantle's picker selects "● high". Same model
list and prices. mantle seems to assume a different default effort for Opus 5.5 than the
engine; read it from the engine (initialize / model info) rather than a table.

## 2. /model layout (ST-01)

claude draws the picker unboxed under a top rule (title, two-line description, numbered
rows, an effort line "◐ Medium effort (default) ←/→ to adjust", then the key hints);
mantle draws a box with an "Effort ○ low ○ medium ● high ○ xhigh ○ max · thinking always
on" row. The box is fine as mantle's style only if the fullscreen and inline frames keep
the same rows; the effort row is the difference that matters.

The picker also showed "Loading models…" for a moment after opening; claude lists the
models at once (it has them from initialize). The scenario now waits for the list.

## 3. /help opens on the general tab (ST-28, ST-29)

`help` / `shown`: claude opens with tabs "General  Commands  Custom commands" on
General: a one-paragraph intro, a three-column shortcuts table, and a docs link. mantle
opens on the command list ("Built-in", 98 more). Open on a general page with the
shortcuts; keep the command list as the second tab.

## 4. Closing echo (with 04 §7)

On close claude prints "❯ /help" with "⎿  Help dialog dismissed", and for /model
"⎿  Kept model as Opus 5.5 (default)". mantle prints nothing.
