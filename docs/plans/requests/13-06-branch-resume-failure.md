# 13 → 06: a failed resume-at restart replies success (MT-R6)

**Status:** done in 6660ebd (branch `worktree-06-branch`). Both items: the restart replies on the restarted engine's `initialize`; an error `result` before it, a second attach (fallback), another session id, a failed initialize or an exit error replies `BranchedMsg{Err}` and resumes the session's own branch (plain `--resume`). Spiked on claude 2.1.292: a bad `At` prints the error result ~0.5 s after spawn and never answers initialize; a good one answers initialize with no result. Tests: `TestBranchRequestResumeFailure` (error result, fallback start, new session, initialize error) and `TestBranchRequestRestart` in `features/sessions/branchreq_test.go`; `make test-06`, `make lint` and `make e2e-research` pass.

Found by 13c's e2e test (`make e2e-research`, claude 2.1.292 on fakeapi).

When the restart path of `ext.BranchRequestMsg` (`branchRestart`, `--resume=<sid>
--resume-session-at=<At>`) cannot find `At`, the engine prints an error result,
`No message found with message.uuid of: <At>`, and keeps running as a **new session**:
the next prompt goes to a new JSONL file with no history. `features/sessions/branchreq.go`
still replies `BranchedMsg{Restarted: true}` on the attach, so research re-submits the
follow-up into the empty session.

Why it happens: headless claude resumes one chain, the branch named by the newest
`last-prompt` record's `leafUuid` (or the newest entry when that descends from it), and
`--resume-session-at` only looks for `At` on that chain. Research now appends an explicit
`last-prompt` record (`internal/research.SelectBranch`, what the engine's own rewind
writes) before sending an off-branch request, so research no longer hits this. Any other
caller with an `At` off the engine's chain would.

Please:
1. After a branch restart, treat an error `result` before the first prompt (or an
   `init` whose session id differs from the request's `SessionID`) as a failure: reply
   `BranchedMsg{Err}`.
2. Optional: stop the engine instead of leaving it on a fresh session, and restart it on
   the request's session without `--resume-session-at`, so the user is back where they
   were.

Test: a fake engine whose restart emits the error result; the reply carries `Err`.
