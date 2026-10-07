# 13 → 05: research step in the shift+tab cycle (MT-R1)

**Status:** open (needs `contracts-v1.11`)

Research mode is a UI mode on top of permission mode `default`. The cycle becomes
`default → research → acceptEdits → plan → (bypass|auto) → default`.

1. **`features/turn/mode/mode.go`.** Add the pure function
   `NextUI(m Mode, research, researchAvail bool, a Availability) (Mode, bool)`:
   - `(Default, false)` → `(Default, true)` if `researchAvail`.
   - `(Default, true)` → `(AcceptEdits, false)`.
   - Otherwise the result of `Next`.
   - Table tests.
2. **`features/turn/modes.go` `cycleMode`:**
   - Use `NextUI`.
   - Entering research sends `ext.UIModeRequestMsg{Mode: ext.UIModeResearch}` and no
     control request.
   - Leaving research sends `UIModeRequestMsg{""}` plus `setMode(AcceptEdits)`.
   - Track the research flag from `ext.UIModeChangedMsg`.
   - `researchAvail` = `ctx.Command("research")` resolves (a mod that removes the feature
     drops the step).
   - While research is on, `userMode` stays `default`, so `maybeStartupMode` keeps manual
     after the engine restarts that research triggers.
3. **Dialogs.** shift+tab inside dialogs (`dialogs.CycleMode`) keeps today's behaviour.

Tests: the cycle with and without research, and no `set_permission_mode` when entering
research.
