# 12 → 03: transcript rendering differences (parity audit)

**Status:** open (2026-10-04). Found by the plan 12 side-by-side suite against claude
2.1.289 (inline renderer, fakeapi). Regenerate a frame with
`make parity-side-by-side PARITY_ARGS="-run <scenario>"` and read
`test/parity/out/<scenario>/<checkpoint>.txt`. Match layout and behaviour; keep mantle's
own wording.

## 1. Turn-duration line after every turn (TR-50)

Every scenario. claude ends each finished turn with "✻ <verb> for <dur> · done <time>",
even sub-second turns, and always with the clock time. mantle prints it only when
`duration_ms >= 1000`, and adds "· done <time>" only when timestamps or `timeFormat` are
configured (`render_system.go`).

```
claude                                   mantle
  Done.                                    Done.

✻ <verb> for <dur> · done <time>         ────────── (prompt frame)
```

The 1 s threshold also made the `plan-mode` frame depend on timing (the scenario now
waits 1.5 s before approving).

## 2. Bash output collapse shows 3 lines (TR-09)

`bash-output` / `collapsed`: claude shows the first 3 output lines, then
"… +37 lines (ctrl+o to expand)"; mantle shows 5, then "… +35 lines". A command with no
output: claude "⎿  Done", mantle "⎿  (no output)" (`tool-approval` / `allowed`).

## 3. Task tools are not transcript rows (TR-16)

`todos` / `todos`. claude shows no row for TaskCreate / TaskUpdate (the task list above
the prompt is the only place they show). mantle prints "⏺ Update Task(#1) ⎿ ☒ #1" rows.
Hide TaskCreate, TaskUpdate, TaskList and TaskGet rows as claude does (and TodoWrite,
which 2.1.289 rejects with "No such tool available" on current models).

## 4. Edit result and diff gutter (TR-12)

`edit-diff` / `edited`. Same content; claude's gutter is one column wider (line numbers
right-aligned in 6 columns, then two spaces); mantle's is 5 then three. The summary line
wording differs on purpose (allowlisted).

## 5. Code blocks expand tabs to 4 columns (TR-01)

`plain-qa` / `answered`: a tab-indented line in a fenced block renders with 4 columns of
indent in claude (`        return a + b` after the 2-column margin) and 2 in mantle.

## 6. Background agent rows (TR-15)

`subagent` / `done`. claude: "⎿  Backgrounded agent (↓ to manage · ctrl+o to expand)"
under the Agent row; while waiting, "✻ Waiting for 1 background agent to finish"; then
"⏺ Agent "<desc>" finished · <dur>" with no body. mantle: "⎿  Running in the
background", the finished row with the agent's answer as its body, and a
"✓ general-purpose: … · <n> tokens · <dur>" row above the prompt. Also the order: claude
shows the main reply before the finished row; mantle the reverse.

## 7. Compact boundary (TR-31, with plan 06)

`compact` / `compacted`. claude clears the screen, prints its header, then
"✻ Conversation compacted (ctrl+o for history)" *before* the "❯ /compact" echo and its
"⎿  Compacted (ctrl+o to see full summary)" line. mantle keeps the old transcript on
screen, echoes "> /compact", then a "══════ Conversation compacted (18.2k → <n> tokens) ·
ctrl+o for history ══════" rule and "⎿  Compacted".

## 8. Permission-denial summary row

See request 12-05 §1: the "⏺ Denied: Bash(…)" row from `result.permission_denials`
repeats a denial the tool row already shows.
