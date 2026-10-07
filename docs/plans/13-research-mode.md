# Plan 13: research mode (tree-structured conversations)

| Session | Primary paths | Part B needs | Produces |
|---|---|---|---|
| mantle-13a (core) | `internal/research`, `testdata/fixtures/13`, `scripts/batonwatch` | none | the tree model, send planning, sidecar |
| mantle-13b (UI) | `features/research/{feature,state,scope,bars,actions,picker,stories}.go`, `features/all/all_research.go` | 13a's types | bars, scoped view, navigation |
| mantle-13c (integration) | `features/research/{stage,branch,mode}.go`, e2e tests | `contracts-v1.11` and requests 13-12, 13-04, 13-05, 13-06 done | working research mode |

mantle-only feature: Claude Code has no counterpart, so there is no side-by-side parity
scenario. Parity IDs are mantle-only rows `MT-R1`…`MT-R8` (listed at the end).

## Goal

Research mode turns a conversation into a tree.

- **Node.** A node is one user question plus Claude's full answer turn (text and tools).
  The first prompt of the session is the root.
- **Children.** A follow-up becomes a child of the node being viewed. The user can just
  type, or select part of the answer first, which quotes the selection (`> …`) above the
  question. A node can have many children.
- **One node per screen.**
  - Above the node: one clickable bar per ancestor, root first.
  - Below the node: one clickable bar per child.
  - Clicks and keys move between nodes.
- **Engine.** Exactly manual mode: permission mode `default`. Only the UI differs.

Decisions taken with the user (2026-10-07):
1. **Entry.** shift+tab cycle `default → research → acceptEdits → plan → …`, plus the
   `/research` command and the `--research` flag.
2. **Layout.** Research always uses the fullscreen (alt-screen) layout. Leaving restores
   the previous layout. Research is refused when the alt screen is disabled or
   screen-reader mode is on.
3. **Selection.** A selection is quoted into the prompt (`> …`) above the question.
   - `research.quoteOnSelect` (default true) quotes automatically when a selection is
     finished.
   - `mantle:research.quote` (`>`) quotes on demand.
4. **Persistence.** The tree is rebuilt from Claude Code's own session JSONL, so it
   survives `--resume` and `/resume`. The sidecar `~/.mantle/research/<sid>.json` keeps
   UI state only.
5. **External mode changes.** If the permission mode changes from outside (plan
   approval, `/permissions`), research exits with a notice.

## Start prompts

> **13a:** You are session 13a of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/13-research-mode.md`, then do the 13a tasks. Commit the types first
> (`internal/research/tree.go`, `plan.go`) so 13b can build on them. Commit with prefix `[13]`.

> **13b:** You are session 13b of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/13-research-mode.md`, then do the 13b tasks. Wait until
> `internal/research/tree.go` is committed (`git log --oneline -- internal/research`). Use
> local stand-ins for the `contracts-v1.11` types until that tag exists, then switch.
> Commit with prefix `[13]`.

> **13c:** You are session 13c of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/13-research-mode.md`, then do the 13c tasks. Start once `git tag -l` shows
> `contracts-v1.11` and requests 13-12, 13-04, 13-05 and 13-06 have **Status:** done.
> Commit with prefix `[13]`.

Owning sessions do the request notes (`docs/plans/requests/13-*.md`). Prompt for each:

> You are session NN of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md`, your plan
> and `docs/plans/requests/13-NN-*.md`, then do that request and set its **Status:** line.
> Commit with prefix `[NN]`.

Order:
1. 13a, 13b and 01 (`13-01-research-contracts.md`) start together.
2. After the `contracts-v1.11` tag: 12, 04, 05, 06, 07 and 11.
3. 13c last.

## Facts you can rely on

- **Branches share one file.** `--resume=<sid> --resume-session-at=<uuid>` keeps the
  session id. JSONL entries up to and including `<uuid>` are kept, and new turns append
  to the same file with `parentUuid=<uuid>`, so they become siblings (spike S7 in
  `02-proto-engine.md`). `internal/sessions/tree.go` already builds the UUID tree with
  several branches per file.
- **Never set `ResumeDropsTurn` for research.** `--resume-drops-turn` only passes when
  exactly one turn follows the target.
- **Fast path.** `rewind_conversation` (unstable, `internal/engine/unstable.go`,
  `Engine.Rewind`) removes a user message and everything after it without a restart. It
  only applies when the target node is an ancestor of the engine's current leaf.
- **Node identity.** Prompts carry a client UUID (`ext.Prompt.UUID`, item `user:<uuid>`),
  and the engine writes the same UUID into the JSONL. Node ID = that UUID. Verify against
  `features/sessions/normalize.go` once.
- **Restart path.** `features/sessions/resume.go` `loadCmd(modeSwitch)` → `startEngine` →
  `ext.EngineStartMsg` → `internal/engine` `Restart`. It resets the store and reprints
  (`TranscriptHistoryMsg{Reset}`, `ScreenClearedMsg`).
- **One viewport.** Only one `SlotLive` component is visible in fullscreen. Research does
  not add one; it scopes the existing `fullscreen.viewport` with `TranscriptScopeMsg`.
- **Stage order.** Prompt stages run history 100, slash 200, bash 300, attachments 400,
  priority 500, send 1000. Research's stage at 600 sees only real prompts.

## Design

### Tree model (`internal/research`, pure)
```go
type NodeState int // Done | Running | Interrupted | Failed
type Node struct {
    ID        string // prompt uuid
    Parent    *Node
    Children  []*Node // file order, live ones appended
    Prompt    string  // question, quote stripped (bar label)
    Quote     string  // leading "> " block, if any
    LeafUUID  string  // last JSONL entry of this turn on its path; "" while running
    State     NodeState
    Compacted bool    // a compact boundary sits between parent and this node
}
type Tree struct { Roots []*Node; EngineLeaf string /* node ID */ ; byID map[string]*Node }
```
- **`Build(*sessions.Transcript) *Tree`:**
  - A node starts at every real user prompt. Skip tool results, meta, local commands,
    compact summaries and sidechains.
  - A node owns its entries up to the next real prompt.
  - `LeafUUID` is the newest main-path entry in that span. Parallel tool-result side
    branches neither make nodes nor move the leaf.
  - Follow `LogicalParentUUID` across compact boundaries and set `Compacted`.
  - Orphans get a synthetic session root.
  - `EngineLeaf` is the node that holds `ActiveLeaf()`.
- **`live.go`:**
  - `PromptSent(uuid, parent, text)` adds a Running node.
  - `TurnEnded(lastUUID, state)` sets its leaf.
  - `Merge` re-reads the JSONL after each result. The JSONL is authoritative.
- **`plan.go`, `Plan(t, viewing, busy) SendPlan{Kind, At, DropPrompt, Reason}`:**
  - **Plain** when viewing the engine leaf.
  - **Rewind** when viewing an ancestor of the leaf. `DropPrompt` is the child on the
    current path.
  - **Resume** at `viewing.LeafUUID` otherwise.
  - **Blocked** when busy and not on the leaf, or when the target is still running.
  - Set `NeedsConfirm` when `At` crosses a compact boundary (full context reload).
- **`sidecar.go`:** `~/.mantle/research/<sid>.json` (take `MantleDir` from
  `internal/config`). Writes are atomic (temp file + rename). Unknown or corrupt files
  are ignored.
  ```json
  {"v":1,"active":true,"viewing":"<uuid>","lastChild":{"<uuid>":"<uuid>"}}
  ```
- **`quote.go`:** `FormatQuote(sel)` prefixes each line with `> ` and adds a blank line.
  `SplitQuote(prompt) (quote, question)`.

### UI (`features/research`)
- **`scope.go`:** a scoped `ext.Transcript` for the viewed node.
  - On the engine's current branch, it slices the live store from `user:<id>` up to the
    next top-level prompt item, so streaming shows.
  - Off branch, it uses `Normalize(leaf=node.LeafUUID)` sliced the same way. This is
    computed lazily, off the UI goroutine, and cached per node.
  - Research sends `TranscriptScopeMsg` on every navigation, and again after
    `TranscriptHistoryMsg{Reset}` and `ScreenClearedMsg`.
- **`bars.go`:** two components, both `SlotOpts{Modes: Fullscreen}`.
  - **`research.ancestors`** (`SlotHeader`): one row per ancestor from root to parent.
    Capped at `max(3, H/5)` rows: the root, then `… N more` (opens the picker), then the
    nearest ancestors. Each row shows `▸ <question…> · N branches`.
  - **`research.children`** (`SlotAboveInput`): one row per child, `▾ <question…>` or
    `▾ re "<quote…>" <question…>`, with the same cap.
  - With no children, `research.children` shows a dim hint `↳ type to ask a follow-up`.
  - A click maps `MouseEvent.Y` to a row and navigates. The hovered or keyboard-selected
    row uses the `Suggestion` token.
  - A `●` marks the engine's current tip.
- **`picker.go`:** dialog `research.tree` (PlaceCentered), an indented tree with a filter.
  Enter navigates.
- **`actions.go`:** in the ambient context `Research`, active only in research mode:

  | Action | Key | Effect |
  |---|---|---|
  | `mantle:research.parent` | alt+up | go to parent |
  | `mantle:research.child` | alt+down | go to the last-visited (or first) child |
  | `mantle:research.prevSibling` / `.nextSibling` | alt+left / alt+right | move among siblings |
  | `mantle:research.root` / `.tip` | alt+home / alt+end | go to root / engine's current tip |
  | `mantle:research.tree` | ctrl+t | open the tree picker |
  | `mantle:research.quote` | `>` (Scroll context) | quote the selection |
  | `mantle:research.toggle` | none | enter or leave research mode |

  Navigation only changes the view; it never touches the engine.
- **`stage.go`:** stage `research.route` at priority 600.
  - Off → Continue.
  - **Plain** → Continue, after recording the parent.
  - **Blocked** → Reject with a notice; the text stays in the editor.
  - **Rewind or Resume:**
    1. Consume and stash the draft.
    2. Send `BranchRequestMsg`.
    3. On `BranchedMsg` success, `ctx.Submit(Draft{…, Resubmit: true})`.
    4. On error, `EditorSetTextMsg` puts the text back, plus a notice.
- **`mode.go`:**
  - **Enter:**
    1. Remember `ctx.Layout()`.
    2. Send `LayoutRequestMsg{Fullscreen}`.
    3. Activate `Research`.
    4. Load the tree and sidecar.
    5. Scope to the sidecar's `viewing` node, or to the tip.
    6. Broadcast `UIModeChangedMsg`.
  - **Leave:** clear the scope, restore the layout, deactivate `Research`, save the
    sidecar.
  - **Entry points:** handles `UIModeRequestMsg`, the `/research [on|off]` command and
    `EnvResearch`.
  - **Session changes:** on a new session id it reloads, and re-enters if the sidecar says
    `active`. On a fork (`/branch`) it copies the sidecar.
  - **External mode change:** a permission mode other than `default` exits research with a
    notice.

### `pkg/ext` additions (request 13-01, `pkg/ext/v1_11.go`, tag `contracts-v1.11`)
```go
const UIModeResearch = "research"
const ContextResearch = "Research" // ambient, mantle-only
const EnvResearch = "MANTLE_RESEARCH"
type UIModeRequestMsg struct{ Mode string } // "" = leave the current UI mode
type UIModeChangedMsg struct{ Mode, Prev string }
type TranscriptScopeMsg struct{ Owner string; Source Transcript } // nil Source = unscoped
type SelectionMsg struct{ Source, Text string }                   // Text "" = cleared
type EditorQuoteMsg struct{ Text string }
type BranchRequestMsg struct{ EngineID, SessionID, At, DropPrompt, Tag string }
type BranchedMsg struct{ EngineID, SessionID, At, Tag string; Restarted bool; Err error }
// Draft gains: Resubmit bool (re-run after a stage consumed it; history stage skips it)
```

## Tasks

### 13a (core) [Part A]
- [x] `Makefile`: `PKGS_13 = ./internal/research/... ./features/research/...` (done by the coordinator).
- [x] Types: `tree.go` and `plan.go` signatures. Commit these first.
- [x] Spike, using fakeapi with an isolated `CLAUDE_CONFIG_DIR` (no real API). Confirm:
  - resume-at keeps the sid;
  - siblings are appended to the same file;
  - `rewind_conversation` followed by a send makes a sibling.

  Save the sanitized result as `testdata/fixtures/13/branching.jsonl` and record the
  findings under "Spike notes" below.
- [x] `Build`, `Merge`, `Plan`, sidecar, quote, all with table tests.
- [ ] Side quest: `scripts/batonwatch` (done by the coordinator), and later
  `batonwatch report` once the run ends.

### 13b (UI) [Part A]
- [x] `feature.go` (Register `research`, `Parity: MT-R*`), `state.go` (view state, navigation).
- [x] `scope.go`, `bars.go`, `actions.go`, `picker.go`, `stories.go`.
- [x] `features/all/all_research.go` with `//go:build !no_research`.
- [x] exttest goldens at widths 60/100/160:
  - bar caps;
  - mouse row → navigation;
  - keyboard navigation;
  - scoped content for nodes on and off the current branch.

### 13c (integration) [Part B]
- [x] `stage.go`, `branch.go`, `mode.go`, the subscriptions and the sidecar wiring.
- [x] e2e vt test (mantle-ui + fakeapi), `test/e2e/research`, `make e2e-research`:
  1. Ask Q1, then Q2. Go back to Q1 and ask Q3.
  2. Assert Q1 has two child bars and only one node is on screen.
  3. Click the parent bar.
  4. Quit, run `--resume`, and assert the same tree and the same viewed node.
  5. (added) From the resumed session, ask a follow-up to Q2, off the engine's branch:
     the engine restarts at Q2 (resume-at) in the same session file.
  `TestResearchEntryPoints` covers shift+tab, ctrl+t and `/research off`.
- [x] Checklist run, then tick MT-R1…R8. The run was the automated vt runs (no human
  pass), plus unit tests:

  | ID | Evidence |
  |---|---|
  | MT-R1 | `TestResearchEntryPoints` (shift+tab), `TestResearchTree` (`--research`), `TestEnterAndLeave`, `TestEnterRefused`, `TestEnvEntersOnStart`, `TestResearchEnteredElsewhereIsManual` (05) |
  | MT-R2 | `TestResearchTree` (one node on screen), 13b scope tests |
  | MT-R3 | `TestResearchTree` (click on `▸ alpha question`), 13b bar tests |
  | MT-R4 | `TestResearchTree` (click on `▾ beta question`), 13b bar tests |
  | MT-R5 | `TestResearchTree` (alt+up), `TestResearchEntryPoints` (ctrl+t), 13b key tests |
  | MT-R6 | `TestResearchTree` (rewind and resume-at follow-ups), `TestRouteRewindThenResubmit`, `TestRouteResumeAt`, `TestRouteBlocked`, `TestSelectBranch` |
  | MT-R7 | `TestQuoteOnSelect`, 12's `TestVTResearchQuoteKey`, 04's `TestEditorQuote` |
  | MT-R8 | `TestResearchTree` (`--resume`), `TestSidecarRoundTrip`, `TestForkCopiesSidecar` |

13c notes:
- **Permission mode.** Research does not refuse other modes. Plan 05 pins `userMode` to
  default on `UIModeChangedMsg{research}` and switches a running engine, so `/research`
  and `--research` work from auto (2.1.292's default). Research leaves only when the mode
  changes *away from* default.
- **Off-branch follow-ups write one record.** See the spike note on chain selection
  below. Before a Resume plan, `research.SelectBranch` appends an explicit `last-prompt`
  record for the target's nearest message. This is the only write mantle makes to a
  session file. Request `13-06-branch-resume-failure.md` asks 06 to report a failed
  resume-at instead of replying success.
- **Fork.** Only `--resume --fork-session` restarts are seen (EngineStartMsg). `/branch`
  forks into a background `claude --bg`, whose session id mantle never learns, so its
  sidecar is not copied.
- **Queued prompts.** While the tip's answer runs, a follow-up is blocked
  (`ReasonRunning`) rather than queued into the running turn.

## Risks
- **Restart cost.** A Resume restart re-runs SessionStart hooks (source `resume`) and
  reconnects MCP, which takes about 1–3 s. Prefer the Rewind fast path. The setting
  `research.prewarm` (default off) branches while the user types.
- **`rewind_conversation` is undocumented.** Plan 06's restart fallback covers it.
- **Compaction.** Branching to a node before a compact boundary reloads the full context,
  so it asks for a confirm.
- **Quoted text.** Selections are rendered text, so plan 12 unwraps soft wraps and drops
  gutters.

## Parity coverage (mantle-only)
| ID | Feature |
|---|---|
| MT-R1 | research step in the shift+tab cycle, `/research`, `--research` |
| MT-R2 | one node per screen (scoped viewport) |
| MT-R3 | ancestor bars, clickable |
| MT-R4 | child bars, clickable |
| MT-R5 | keyboard navigation and tree picker |
| MT-R6 | follow-up from any node (rewind or resume-at branching) |
| MT-R7 | selection quoted into the prompt |
| MT-R8 | tree restored on `--resume` / `/resume` |

## Side quest: file-baton lock tracking

Goal: while the sessions above run in parallel, record every lock acquire, wait, handoff
and release for each file, and check that file-baton behaved correctly.

### What file-baton exposes
- **State.** `.git/file-baton/state.json`:
  - `locks[path]{owner, status, since, last_edit, queue[], handoff{from, reason, delivered}}`;
  - `sessions`;
  - `touched`.
- **Log.** `.git/file-baton/log`, with lines of the form `<ts> <sid8> <event> <msg>`.
- **Gap.** The `stop`, `session-end` and timeout releases don't name the files, so
  releases are reconstructed by polling the state and diffing snapshots.

### Tool
`scripts/batonwatch` is dev-only and not built into `bin/`.

- **`batonwatch record`** polls the state every 500 ms and appends to
  `.git/file-baton-watch/events.jsonl`:
  - acquire, queue, dequeue, handoff and release events (release with a cause taken from
    the nearest log line);
  - the raw log lines.
- **`batonwatch report`** writes a per-file timeline and invariant checks to
  `docs/plans/13-baton-report.md` (short session ids only).
- **`batonwatch live`** shows a table that refreshes every second.

### Run
1. Start `batonwatch record` before launching the sessions.
2. Run `batonwatch report` after the last commit.
3. Write any violations up as notes for the file-baton repo. Never change the plugin from
   mantle sessions.

### Results (2026-10-07 run, `docs/plans/13-baton-report.md`)
- **The run:** 38 minutes, 10 sessions, 80 locks taken and released, 172 decisions logged.
  - There were **no invariant violations**.
  - There were also no denies, waits or handoffs, so those paths were not exercised.
- **Each session had its own git worktree.** `claude --bg` puts every session in a
  worktree under `.claude/worktrees/<name>` on its own branch.
  - file-baton keeps one shared state in the common `.git`.
  - It keys locks by **absolute path**, so the same file in two worktrees counts as two
    files and the sessions never contended.
  - Coordination happened at merge time instead: the coordinator merged each branch into
    main.
  - Its log names files **relative to the session's checkout**. batonwatch now maps both
    forms to `[worktree] rel/path`.
- **Shell edits bypass the baton.** 50 committed files were never locked. All of them
  were edited through Bash rather than the edit tools:
  - `cat > f <<EOF`
  - `python3` rewrite scripts
  - `sed -i`
  - test `-update` goldens

  Examples: `pkg/ext/v1_11.go` (01), `features/fullscreen/viewport.go` (12),
  `features/sessions/resume.go` (06), `features/turn/mode/mode.go` (05). In a shared
  checkout this would defeat file-baton; CLAUDE.md rule 4 already forbids shell edits
  across directories. Goldens written by tests are expected.
- **Possible upstream improvements to file-baton:**
  - name the released files in its `stop` and `session-end` log lines;
  - log paths the same way the state stores them;
  - optionally warn when a Bash command writes into a tracked file (`>`, `sed -i`,
    `python3 - <<`).

## Spike notes
Run on 2026-10-07 with claude 2.1.292, fakeapi and an isolated `CLAUDE_CONFIG_DIR`
(`MANTLE_SPIKES=1 go test -run Spike -v ./internal/research`; add
`MANTLE_SPIKE_FIXTURE=1` to rewrite `testdata/fixtures/13/branching.jsonl`). Sequence:
Q1, Q2; restart with `--resume=<sid> --resume-session-at=<Q1 leaf>`, then Q3; then
`rewind_conversation` (target Q3) and Q4.

- **resume-at keeps the sid.** After the restart `system/init` has the same session id
  and no new session file appears.
- **Siblings share the file.** Q3 is appended to the same JSONL with
  `parentUuid = <Q1 leaf>`, so Q2 and Q3 are siblings.
- **Rewind then send makes a sibling.** `rewind_conversation` is supported natively
  (`Rewound:true, Restarted:false`, `PrefillText` = Q3's text). Q4 is then written with
  `parentUuid = <Q1 leaf>`, so Q1 has three children: Q2, Q3, Q4.
- **Node identity.** The client prompt UUID is the JSONL `uuid` of the user entry, as
  `features/sessions/normalize.go` assumes (`user:<uuid>`).
- **Q1's leaf is not the assistant record.** Each turn ends with an `attachment` entry
  after the assistant message, and the next prompt hangs off that attachment. `Build`
  uses the newest main-path entry of the span, which matches.
  `Rewind`'s `PrecedingAssistantUUID` names the assistant record instead; do not use it
  as `At`.
- **`last-prompt` lags after resume-at.** The record written after Q3's turn named Q1's
  assistant entry. Before it was flushed, the previous record still pointed into Q2's
  branch, so `ActiveLeaf(hint)` said Q2. `Build` therefore takes `EngineLeaf` from the
  newest main-path entry (`Tree.ActiveLeaf("")`). The live tree (`PromptSent`,
  `SetEngineLeaf`) is preferred over both while mantle runs.
- **Compaction.** When a compact boundary follows a turn, that node's `LeafUUID` is the
  compact summary, after the boundary. A follow-up there keeps the compacted context, so
  `NeedsConfirm` is set only when branching above the node whose span holds the
  boundary.
- **resume-at only reaches the loaded chain** (found by 13c's e2e test, 2.1.292).
  `--resume` loads one chain: the branch of the newest `last-prompt` record's `leafUuid`,
  walked up to a user or assistant message (the newest entry wins when it descends from
  that leaf). `--resume-session-at` searches only that chain's messages. For an `At` on
  another branch, the engine prints `No message found with message.uuid of: <At>` and
  carries on as a new session. The spike only branched to an ancestor of the loaded
  chain, so it did not see this. `rewind_conversation` itself writes
  `{"type":"last-prompt","leafUuid":<message>,"explicit":true,"rewound":true}`, which is
  the "lag" above. Research selects an off-branch node the same way
  (`SelectBranch`), at the nearest message: a trailing attachment is not on the chain.
- **mantle's only session-file write (user-approved 2026-10-07).** Before a resume-at
  restart to another branch, `SelectBranch` appends one `last-prompt` record to the
  session JSONL. It is append-only, and the record has the shape the engine writes
  itself. mantle writes nothing else to session files.
- **Fixture hygiene.** The spike keeps only each attachment's `type` and drops its
  `rendered*` fields, which carry the engine's own prompt text. Paths are rewritten to
  `/work/demo`.
