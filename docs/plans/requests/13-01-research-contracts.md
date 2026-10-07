# 13 → 01: research mode contracts (MT-R1–R8)

**Status:** resolved 2026-10-07: contracts-v1.11 (plan 01 f25e3e5): `pkg/ext/v1_11.go`, `Draft.Resubmit`, `Research` accepted by `internal/keymap`.

Plan 13 (`docs/plans/13-research-mode.md`) needs additive `pkg/ext` types. Please add them
in a new `pkg/ext/v1_11.go` and tag `contracts-v1.11`. Every item is a new type, a new
constant or a new field, so nothing existing changes.

1. **Types and constants:**
   ```go
   const UIModeResearch = "research"
   const ContextResearch = "Research" // ambient, mantle-only; active only while research mode is on
   const EnvResearch = "MANTLE_RESEARCH"

   type UIModeRequestMsg struct{ Mode string }    // "" = leave the current UI mode
   type UIModeChangedMsg struct{ Mode, Prev string }
   type TranscriptScopeMsg struct{ Owner string; Source Transcript } // nil Source = unscoped
   type SelectionMsg struct{ Source, Text string } // Text "" = selection cleared
   type EditorQuoteMsg struct{ Text string }       // replace/insert the leading "> " block
   type BranchRequestMsg struct{ EngineID, SessionID, At, DropPrompt, Tag string }
   type BranchedMsg struct{ EngineID, SessionID, At, Tag string; Restarted bool; Err error }
   ```
2. **`Draft.Resubmit bool`** in `pkg/ext/pipeline.go`. It is set when a stage consumed a
   draft and re-submits it later through `Ctx.Submit`. Stages with side effects (history)
   skip it.
3. **Keymap.** Accept the `Research` context in `~/.mantle/keybindings.json` validation
   (`internal/keymap`), the way `Mantle` is accepted.
4. **Doc comments** on each type, naming its producer and consumer:
   - `TranscriptScopeMsg`, `SelectionMsg`: plan 12.
   - `EditorQuoteMsg`: plan 04.
   - `BranchRequestMsg`, `BranchedMsg`: plan 06.
   - `UIMode*`: plan 13 (consumed by 05 and 07).

Tests: `ext_test.go` compile check. `make archtest`.
