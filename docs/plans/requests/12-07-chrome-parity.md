# 12 → 07: banner, footer, status line and task panel (parity audit)

**Status:** open (2026-10-04). Found by the plan 12 side-by-side suite against claude
2.1.289 (inline renderer, fakeapi). Plan 07's session is now plan 12's, so plan 12 can
take these if the coordinator agrees. Regenerate a frame with
`make parity-side-by-side PARITY_ARGS="-run <scenario>"` and read
`test/parity/out/<scenario>/<checkpoint>.txt`. Keep mantle's own wording and art.

## 1. Banner at startup (CH-20)

`plain-qa` / `start`, `footer-modes`, `statusline` / `start`: claude prints its header
(art, version, model and account, working directory) before the first prompt. mantle
prints its banner only on the first `SessionChangedMsg` with a session id, which comes
after the first turn starts, so an idle start shows no banner. Print it at startup from
what is known (version, cwd, model from settings) and don't print it again for that
session. (After `/clear`, see 12-06 §1: one banner, not two.)

## 2. Footer layout (CH-05, ST-07)

Every scenario. claude's footer line: mode and hints on the left ("⏸ manual mode on ·
? for shortcuts · ← for agents"), and the effort hint right-aligned on the same line
("◐ medium · /effort"). mantle: "⏸ manual approval · shift+tab to change" on the left
and "? for shortcuts" on the right, with no effort hint. The mode wording is mantle's
own; the effort hint is missing. While background agents exist claude swaps in
"/tasks to see subagents"; with tasks it shows "ctrl+t to hide tasks" / "ctrl+t to show
tasks".

## 3. Status line placement and first run (CH-18, CH-19)

`statusline` / `start`, `after-turn`: claude shows the statusLine output *above* the
mode line (footer is two lines: status line, then modes), and runs the command at
startup. mantle shows it *below* the mode line and only after the first turn.

## 4. Task panel (CH-10, TR-16)

`todos` / `todos`, `toggled`. When tasks are created claude shows the panel expanded
above the prompt: a header "2 tasks (1 done, 1 in progress, 0 open)" and one row per task
("✔ Read main.go", "◼ Rename greet"); ctrl+t hides it. mantle shows one collapsed line
("Tasks 1/2 done · Renaming greet (ctrl+t to show)") and expands to "✓ Read main.go",
"▸ Renaming greet". Fixed already in plan 12 (2052890): checklist calls now apply only
after a successful result, so a rejected TodoWrite no longer fills the panel.

## 5. Prompt frame title (with 06 §3)

After a resume mantle titles the prompt's top rule with the session's first prompt;
claude shows no title unless the session was named.
