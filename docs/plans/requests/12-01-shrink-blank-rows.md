# 12 → 01: stale blank rows above the prompt after every turn (inline)

**Status:** resolved on integration-8: plan 01's dd409a8 leaves no stale rows, plan 07's 6f1d09c adds the one blank row; after turns and resumes mantle shows claude's 1 row (2026-10-05). Found while checking the blank row above the prompt frame
(plan 03's note after 12-03 round 2) on integration-7. Plan 07 holds a matching change.

Claude Code always leaves exactly one blank row between the transcript and its prompt
box. mantle's count depends on what the live area did last. The raw screen rows directly
above the prompt frame's top rule (the parity frames fold blank runs, which hid this):

| Moment | mantle | claude |
|---|---|---|
| startup (`plain-qa` / `start`) | 0 blank rows | 1 |
| after a live turn (`plain-qa` / `answered`, `resume` / `continued`) | 3 blank rows | 1 |
| after a resume (`resume` / `resumed`) | 0 blank rows | 1 |

The 3 rows after a live turn come from the shrink workaround in `internal/app/layout.go`
(`holdHeight`): when the spinner lines go, the frame keeps its old height with blank rows
at the top, then shrinks, and "what stays behind is blank rows". Those rows stay on screen
between the last transcript line and the prompt.

Ask: when the hold releases, don't leave the freed rows visible above the live area (for
example move up over the old frame and clear it, `ESC[<n>A ESC[J`, before drawing the
shorter frame, now that the hold has done its job against the stale-content problem).
Once nothing stale is left, plan 07's prompt frame adds the one deliberate blank row
claude has (prepared on worktree-mantle-07, not committed: with today's host it would add
to the stale rows).
