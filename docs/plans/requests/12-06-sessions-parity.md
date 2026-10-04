# 12 → 06: /clear, resume, compact, rewind and /context (parity audit)

**Status:** open (2026-10-04). Found by the plan 12 side-by-side suite against claude
2.1.289 (inline renderer, fakeapi). Regenerate a frame with
`make parity-side-by-side PARITY_ARGS="-run <scenario>"` and read
`test/parity/out/<scenario>/<checkpoint>.txt`. Match layout and behaviour; keep mantle's
own wording.

## 1. /clear publishes a session id the engine never uses (SE-15) — bug

`clear` / `cleared`. After `/clear`, mantle shows its banner twice and nothing else. A
debug run (banner tagged with `ctx.Session()` ids) showed three ids: the original
session, then `conversation_reset.new_conversation_id` (`ffd1…`), then the id the engine
actually continues with (`bd32…`). Only the original and `bd32…` have transcript files in
`projects/`; `ffd1…` appears nowhere on disk. `onConversationReset` (clear.go) publishes
`SessionChangedMsg` with `NewConversationID`, so chrome's welcome prints a banner for
it, and again for the real id. The same phantom id goes into `f.cleared`, so restoring
"the pre-/clear session" and anything else keyed by session id can point at nothing.

Ask: don't treat `new_conversation_id` as the session id; publish the session change when
the engine reports its real id (or verify the two match first). The raw output also shows
two screen clears (two `Reprint`s) for one `/clear`; one is enough.

What claude shows after `/clear`: its header, then the "❯ /clear" echo, then the empty
prompt. mantle should end up with one banner and (request 12-04 §7) the echo.

## 2. Resuming from the picker prints the conversation twice (SE-11, SE-07) — bug

`resume-picker` / `resumed`: after picking the earlier session in `/resume`, mantle
shows the conversation, then its banner, then the conversation again. claude shows its
header and the conversation once (with the turn line, see 12-03 §1).

```
mantle
> Remember a word for me
⏺ Remember the word kumquat.
   <work>
   /help for commands · /mantle to change mantle itself
> Remember a word for me
⏺ Remember the word kumquat.
────────────────────────────────── Remember a word for me ──
```

## 3. Session title in the prompt frame (SE-18, with 07)

`resume` / `resumed`, `resume-picker` / `resumed`: after a resume mantle titles the
prompt's top rule with the first prompt ("── Remember a word for me ──"); claude shows no
title (it labels the prompt bar only for a named session, /rename). The resumed info's
`Title` seems to carry the first prompt or summary; only a user-set name should reach the
frame.

## 4. Picker details (SE-11, SE-12)

`resume-picker` / `picker`: claude groups by project ("work") and shows "now · HEAD ·
133.1KB" (file size) under each session, with a search box drawn as a box; mantle groups
by day ("Today") and shows "just now · HEAD · headless". The "headless" label is accurate
for SDK sessions, but claude shows the size there.

## 5. /compact screen (SE-16, with 03 §7)

`compact` / `compacted`: claude clears the screen and starts over with its header, the
compact boundary line, then the "/compact" echo and result. mantle keeps the old
transcript on screen and adds its boundary after the echo.

## 6. Rewind selector (SE-22)

`rewind` / `selector` (now opened with two esc presses 200 ms apart, see 12-04 §1):
claude lists each prompt with "No code changes" under it and ends with "❯ (current)",
selected by default; mantle lists the prompts with the last one selected and no
"(current)" row or code-change line.

## 7. /context output (CU-01, CU-02)

`context` / `shown`: claude prints the grid and legend into the transcript, then
"Auto-compact window", and a "Skills · /skills" breakdown listing each skill with its
token cost. mantle shows the grid as a panel, with no skills breakdown.
