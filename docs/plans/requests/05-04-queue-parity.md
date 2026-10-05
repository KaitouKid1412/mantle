# 05 → 04: mid-turn pickup and sending queued messages (PARITY TC-06, TC-10)

**Status:** done by plan 05 at the coordinator's request (2026-10-05), with small edits
in `features/input/submit.go`. TC-06: a plain message typed during a turn goes out as
`next`, slash commands stay `later`. TC-10: on 2.1.289 the trigger is chat:sendNow
(ctrl+x ctrl+s) on the empty prompt, not Enter: the queue is taken back and resent as
one `now` prompt. Tests: `features/input` TestBusyPromptJoinsTheTurn and
TestSendNowSendsQueue; pty scenarios `midturn` and `queue-send`.

Both sit in `features/input` (`submit.go`), so they are yours to change; plan 05 renders
the queue and owns the rows.

1. **TC-06 mid-turn pickup.** `stagePriority` sends every prompt typed during a turn
   as `priority:"later"`, so it waits for the turn to end. Claude Code folds a plain
   message into the running turn at the next tool boundary; slash commands still run
   after the turn. S2 (plan 02 notes) found that a prompt with no priority behaves like
   `next` on 2.1.288. Suggested: while busy, leave plain prompts at `""` (or `next`) and
   keep `later` for slash commands and `chat:queueSubmit`. The queue display already
   follows `command_lifecycle` (a folded message leaves the queue when it starts), and
   take-back (`cancel_async_message`) works while it is still queued. Adjust
   TestQueueWhileBusyAndTakeBack, which asserts `later` for the second prompt.
2. **TC-10 send queued messages now.** Enter on an empty prompt while messages are
   queued does nothing (`trySubmit` returns early). Claude Code sends them at once
   ("Enter to send them immediately"): take the queued messages back
   (`cancel_async_message`) and resend them as one `priority:"now"` prompt, the way
   `chat:sendNow` does.
