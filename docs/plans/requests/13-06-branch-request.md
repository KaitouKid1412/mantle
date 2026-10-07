# 13 → 06: branch requests and shared Normalize (MT-R6, MT-R8)

**Status:** open (needs `contracts-v1.11`)

Research mode lets the user ask a follow-up from any earlier node, which needs the engine
moved to that point in the same session.

1. **Move `features/sessions/normalize.go`** (JSONL entries → `[]*ext.Item`) into
   `internal/sessions/normalize`, so `features/research` can render off-branch nodes.
   Keep a thin forwarding function in `features/sessions`.
2. **Handle `ext.BranchRequestMsg{EngineID, SessionID, At, DropPrompt, Tag}`:**
   - **Fast path.** When `DropPrompt != ""` and the engine supports
     `rewind_conversation`, call `Engine.Rewind` (`internal/engine/unstable.go`) with the
     target `DropPrompt`. Do not restore files.
   - **Otherwise,** use the existing modeSwitch path: `loadCmd(loadReq{mode: modeSwitch,
     leaf: At, sw: switchOpts{at: At}})`.
     - Same session, so no `ForkSession`.
     - **Never** `ResumeDropsTurn`.
     - Run `restartGuard` as rewind does.
   - **Reply** `ext.BranchedMsg{Tag, At, SessionID, Restarted, Err}` once the engine is
     attached and idle (after `EngineAttachMsg` and the history replay), or with `Err` on
     failure. Do not refill the prompt; research re-submits it itself.
3. **Refuse while a turn is running.** Reply with `Err` (research blocks this too).

Tests: the restart path asserts the SpawnOpts (`Resume`, `ResumeSessionAt`, no fork, no
drops-turn); the fast path uses a fake engine; the error path is covered.
