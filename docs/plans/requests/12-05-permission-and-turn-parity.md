# 12 → 05: permission dialogs, plan approval, esc and auto mode (parity audit)

**Status:** open (2026-10-04). Found by the plan 12 side-by-side suite against claude
2.1.289 (inline renderer, fakeapi). Regenerate any frame below with
`make parity-side-by-side PARITY_ARGS="-run <scenario>"`; the report is in
`test/parity/out/<scenario>/<checkpoint>.txt`. Match layout and behaviour; write mantle's
own wording (licensing).

## 1. Esc on a permission prompt ends the turn (PD-13, TC-01)

`tool-approval` / `denied`. claude treats esc on a permission prompt as an interrupt: the
tool row reads "Interrupted · What should Claude do instead?" and the turn ends; the next
prompt is the user's. mantle denies the call and lets the model continue (it consumed the
next scripted turn), then adds a "Denied: Bash(…)" row from the result's
`permission_denials`.

```
claude                                              mantle
⏺ Bash(rm -rf scratch-dir)                          ⏺ Bash(rm -rf scratch-dir)
  ⎿  Interrupted · What should Claude do instead?     ⎿  The user denied this tool call.
                                                    ⏺ Noted: keep the scratch directory.
✻ <verb> for <dur> · done <time>                    ⏺ Denied: Bash(command: "rm -rf scratch-dir", description: "Remove the scratch directory")
```

Ask: on esc, deny and interrupt the turn (as "No, and stop" does), and don't print the
`permission_denials` summary for a call the transcript already shows as denied (that row
is plan 03's renderer; drop it when 05 has shown the denial).

## 2. Dialog shape and options (PD-01, PD-02, PD-08, PD-09)

`tool-approval` / `permission-prompt`, `edit-diff` / `edit-prompt`, `tool-always`.

- claude draws the dialog between a full-width top rule and the footer, no box: title
  ("Bash command"), a one-line tip, the description, the command between dashed rules,
  the question, numbered options, then "Esc to cancel · Tab to amend". mantle draws a
  rounded box with the command first and the description second.
- claude offers 4 options for Bash (Yes / Yes and always allow access to `<dir>` from this
  project / Yes and switch to auto mode / No) and 3 for Edit (Yes / Yes and accept edits
  for this session (shift+tab) / No). mantle offers 6 for Bash and 5 for Edit, including
  "No, and tell Claude what to do differently" and "No, and stop" as separate options, and
  "allow all edits for this session" on a Bash prompt.
- claude's option 2 for this Bash call is only the directory-access suggestion; mantle's
  option 2 also adds a `Bash(touch parity-marker.txt)` rule, so in `tool-always` the second
  identical call prompts again in claude and runs silently in mantle. Offer exactly the
  suggestions claude shows for the request (`permission_suggestions` filtered the same
  way), and phrase the directory option as claude does in meaning.
- mantle adds "Outside the allowed directories: <path>" under the description; claude
  doesn't show it.
- claude shows "⎿  Waiting…" under the tool row while its prompt is open; mantle shows
  nothing there.
- Edit prompt: claude shows the diff with full line numbers and three lines of context
  between dashed rules; mantle shows only the changed lines numbered from 1
  (`1 - return …` / `1 + return …`) inside the box.

## 3. Plan approval (PD-21, PD-22, TR-18)

`plan-mode` / `plan-dialog`, `approved`. With 2.1.289's ExitPlanMode, claude shows a short
dialog: title "Exit plan mode?", one line, and two options (Yes, and switch to default
(ask each time) for this session / No). mantle shows its "Ready to code?" dialog with four
options and an empty "Here is Claude's plan:" body. After approval claude adds an
"⏺ Exited plan mode" row; mantle doesn't. (The plan text in 2.1.289 comes from the plan
file, not the tool input; mantle's transcript shows the input's plan, claude shows none.)

## 4. Auto mode as the default (PD-19, PD-34)

`plain-qa` / `start`, `footer-modes` / `mode-3`.

- Without `--permission-mode`, claude 2.1.289 starts in auto mode and prints a one-time
  notice under the header ("Auto mode is now … default permission mode …"). mantle starts
  in manual mode. Ask: start the engine in auto mode when no mode is configured and the
  engine reports auto available, and show the one-time notice in mantle's words.
- shift+tab from plan mode goes straight to auto in claude (no prompt, since auto is the
  default). mantle opens "Turn on auto mode?" with "No" preselected and stays in plan mode.
  Skip the first-use prompt when auto mode is the default or was already accepted
  (`skipAutoPermissionPrompt`).

## 5. AskUserQuestion dialog (PD-24)

`ask-user` / `question`. claude: a "☐ <header>" tab line, the question, options with
descriptions, "3. Type something.", then a rule and "4. Chat about this", footer
"Enter to select · ↑/↓ to navigate · Esc to cancel". mantle: a box titled with the header,
"3. Other", and no "Chat about this" option.

## 6. Interrupt line (TC-01, TC-14)

`interrupt` / `interrupted`. claude puts "⎿  Interrupted · What should Claude do
instead?" directly under the partial answer; mantle leaves a blank line, then
"⎿  Interrupted by user". Keep the wording yours but drop the blank line and add the
"what next" hint.
