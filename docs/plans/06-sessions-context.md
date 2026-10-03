# Plan 06: Sessions & context

| Session | Primary paths | Part B needs | Produces |
|---|---|---|---|
| mantle-06 | `internal/sessions`, `features/sessions` | tags `contracts-v1`, `proto-v1` | none |

## Goal

Everything about conversations as objects:
- **Lifecycle:** new, resume and continue sessions, `/clear`, `/compact`, the `/resume`
  picker.
- **History:** rendering on resume, rewind, branch and fork, export and copy.
- **Side features:** `/btw`, `/diff`, context and usage panels.
- **Generic handoff:** for any Claude Code panel mantle hasn't rebuilt yet, mantle briefly
  runs the real `claude` on the same session and then resumes.

## Start prompt

> You are session 06 of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/06-sessions-context.md`, then execute that plan. Do Part A now. Start Part B
> once `git tag -l` shows the tags it needs. Commit with prefix `[06]`.

## Read first

- `docs/research/protocol-2.1.288.md` §9 (`/clear`, `/compact` headless), §10 (session
  persistence, resume, fork), §5 (`rewind_files`, `get_context_usage`, `get_usage`,
  `get_session_cost`, `rename_session`, `generate_session_title`) and the unstable list
  (`rewind_conversation`, `fork_conversation`, `export_conversation`, `side_question`,
  `get_workspace_diff`).
- `docs/research/inventory-binary-2.1.288.md` §7 (`~/.claude` layout and **session JSONL
  records**).
- `docs/research/inventory-docs.md` §14 (checkpointing / rewind), §15 (sessions), §19
  (costs), §20 (context), §3 (`/btw`, `/diff`, recap).

## Facts you can rely on

- **Storage.** `${CLAUDE_CONFIG_DIR:-~/.claude}/projects/<slug>/<sessionId>.jsonl`.
  - **Slug rule:** the cwd with every non-alphanumeric character replaced by `-`
    (`/Users/x/proj` → `-Users-x-proj`). If longer than 200 chars, cut to 200 and append
    `-<base36 hash>` (verify the hash function against real dirs).
  - Per-session folder `<sessionId>/` with `subagents/agent-<id>.jsonl` plus `.meta.json`
    `{agentType, description, toolUseId, spawnDepth}` and `tool-results/*.txt`.
  - `file-history/<sessionId>/<hash>@vN` holds pre-edit backups used by rewind.
- **JSONL message records:** `user`, `assistant`, `attachment`, `system`.
  - Shared envelope: `parentUuid` (a tree; the resume leaf comes from
    `last-prompt.leafUuid`), `uuid`, `isSidechain`, `timestamp`, `sessionId`, `cwd`,
    `gitBranch`, `version`, `entrypoint` (`cli`, `sdk-cli`, …), `userType`, `slug`.
  - `assistant.message` is the raw API message (content blocks text / thinking / tool_use,
    usage, model); also `requestId`, `isApiErrorMessage`.
  - `user.message.content`: string or blocks (text, tool_result); `toolUseResult`
    (structured), `permissionMode`, `isMeta`, `promptId`.
  - `attachment.type`: environment, plan_mode, todo_reminder, edited_text_file,
    queued_command, nested_memory, diagnostics, … (mostly hidden in the UI; each has a
    `rendered` system-reminder).
  - `system.subtype`: turn_duration, api_error, away_summary, local_command,
    informational, compact_boundary, model_refusal_*.
- **JSONL metadata records:** `last-prompt {lastPrompt, leafUuid}`, `ai-title {aiTitle}`,
  `cost-state` (totalCostUSD, durations, lines added/removed, modelUsage), `mode`,
  `permission-mode`, `queue-operation`, `file-history-snapshot`, `file-history-delta`.
- **Resume behaviour.** Resume does **not** replay history on stdout; mantle renders it from
  JSONL. Switching sessions means restarting the engine.
  - `--resume=<id>` keeps the id; `--continue` takes the latest for the cwd.
  - `--fork-session` (with resume) gives a new id; add `--session-id=<uuid>` to choose it.
  - `--resume-session-at=<uuid>` truncates history at that entry, guarded by
    `--resume-drops-turn=<uuid>`.
  - `total_cost_usd` continues from saved totals.
- **Visibility.** Headless sessions get entrypoint `sdk-cli` and are hidden from Claude
  Code's own picker (`isHiddenFromSessionPicker`). mantle's picker shows all sessions
  (spike S5 may change the entrypoint).
- **`/clear` and `/compact`.**
  - `/clear` (aliases `reset`, `new`) is headless-capable. It emits `conversation_reset
    {new_conversation_id, trigger}`; the old transcript stays on disk.
  - `/compact [instructions]` emits `system/status: compacting`, then `compact_boundary`
    (only if compaction ran), then `result`. "Not enough messages to compact." arrives as a
    success.
- **Rewind.**
  - `rewind_files {user_message_id, dry_run?}` returns `{canRewind, error?,
    filesChanged?, insertions?, deletions?}`. It needs
    `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true` (set by plan 02) and user message
    UUIDs (`--replay-user-messages`).
  - Conversation rewind: restart with `--resume=<id> --resume-session-at=<uuid>` (fork
    first if the user wants to keep the original). The unstable `rewind_conversation`
    (fields `target_message_uuid`, `interrupt_if_running`) is used only if `Supports()`.
  - Claude Code's menu: "Restore code and conversation", "Restore conversation", "Restore
    code", "Summarize from here", "Summarize up to here" (summarize has no wire; emulate
    with a `/compact`-style prompt or mark it H).
- **Context and usage.**
  - `get_context_usage {detail?}` returns categories, totalTokens, maxTokens, percentage
    (grid rows and colours included).
  - `get_usage` returns session cost and rate limits; `get_session_cost` returns `{text}`.
  - `/context`, `/usage`, `/cost` and `/stats` also run headlessly as text, as a fallback.
- **Other session commands.**
  - `rename_session {title}`, `generate_session_title {description, persist}`.
  - Unstable: `side_question` (`/btw`), `get_workspace_diff` (`/diff`),
    `export_conversation`, `fork_conversation`.
- **Generic handoff (H).** Two processes must never write the same session JSONL at once.
  Procedure:
  1. Wait for idle (warn if background tasks exist; restarting kills them).
  2. Stop the engine (`end_session`).
  3. `tea.ExecProcess(claude --resume <sid> [/command])`. The user interacts with real
     Claude Code.
  4. On exit, restart the engine with `--resume=<sid>` and reprint new history from JSONL.

## Part A: start immediately (`internal/sessions`, pure)

- [x] **A1 Paths.** Slug function with the long-path hash (golden tests against real slugs
  observed under `~/.claude/projects`); honour `CLAUDE_CONFIG_DIR`; locate session, subagent
  and tool-result files.
- [x] **A2 Tolerant streaming JSONL reader (`reader.go`).** A line-by-line decoder with big
  buffers that keeps raw JSON. Unknown record types are kept as `Unknown`; malformed lines
  are skipped with a counter. Typed records for the message and metadata records above.
- [x] **A3 Conversation tree.** Build the `parentUuid` tree; pick the active branch (leaf from
  the last `last-prompt.leafUuid`, else the newest leaf); linearize; drop sidechains from
  the main view (they belong to subagents).
- [x] **A4 Session index (`index.go`).**
  - Metadata per session: id, cwd, project, git branch, created/modified time, first and
    last prompt, ai-title, custom name, message count, cost, entrypoint.
  - Read from **file tails and heads** (no full parse).
  - Cache at `~/.mantle/cache/sessions.idx`, keyed by (path, size, mtime).
  - Index all projects lazily.
- [x] **A5 Subagent files.** Load `.meta.json` and `agent-*.jsonl` for nesting (tool_use_id →
  child transcript).
- [x] **A6 Stats helpers.** Totals per session and per project from `cost-state` and
  `modelUsage`, for `/stats` and `/usage` fallbacks.
- [x] **A7 Tests.** Fixture JSONL files (sanitized copies trimmed to the essentials: plain
  turns, tool use, subagent, compact boundary, branches, malformed lines, unknown types).
  An opt-in test (`MANTLE_TEST_REAL_SESSIONS=1`) parses every local session read-only and
  reports unknown record types. Benchmark: index 1,000 sessions in < 200 ms warm.

**Part A notes (verified against the 2.1.288 binary and local sessions):**
- Long-slug hash: `h = h*31 + unit` in int32 over the cwd's UTF-16 units, then
  `base36(|h|)`; non-BMP characters count as two units. Goldens in `paths_test.go` come
  from node. Older engines hashed differently, so `Layout.ProjectDirs` also scans sibling
  dirs with the same 200-char prefix and checks their recorded cwd (as the engine does).
- `CLAUDE_CODE_PROJECT_DIR_NAME` replaces the slug, but only when `CLAUDE_CONFIG_DIR` is set.
- The engine reads 64 KiB heads and tails for its own listing and re-stamps title,
  last-prompt, tag, mode, pr-link, worktree-state and similar metadata at the end of
  the file, so the index reads those windows only. Lines can start with NUL bytes.
- `SessionMeta.Hidden` marks `sdk-cli`/`sdk-ts`/`sdk-py` entrypoints and daemon sessions
  (what Claude Code's picker hides); mantle's picker shows them.
- `cost-state.modelUsage[model]` = `{inputTokens, outputTokens, thinkingTokens?,
  cacheReadInputTokens, cacheCreationInputTokens, webSearchRequests, costUSD}`. Sessions
  without a cost-state fall back to summing assistant `usage` once per message id.
- Compact boundaries start a new chain (`parentUuid: null`) and point back with
  `logicalParentUuid`; `BranchOptions.AcrossCompaction` follows it.
- Opt-in run over local sessions: 649 files and 162k records parsed, no malformed lines,
  no unknown types, 490/490 slugs match. Index: 1,000 synthetic sessions in 4.5 ms warm
  (18 ms cold).

## Part B: after `contracts-v1` and `proto-v1`

- [x] **B1 [M1] Normalizer (`features/sessions/normalize.go`).** JSONL records → `ext.Item`s
  with the same `ContentKey`s plan 03 uses (user.prompt, assistant.text, assistant.thinking,
  tool.<Name> with Result, system.*). Hide meta attachments; nest subagent items.
- [x] **B2 [M1] Resume flow.**
  - `mantle -r <id>` / `--continue` / picker choice: normalize history, hand it to the
    transcript store, commit it to scrollback in chunks (`Ctx.Print`), then spawn the
    engine with `--resume=<id>` (`Engine.Restart`).
  - `--fork-session` support.
  - The session name shows in the prompt bar and title (plan 07 renders it).
- [x] **B3 [M1] `/clear` and `/compact`.**
  - `/clear` (aliases reset, new): send to the engine; on `conversation_reset`, clear the
    store and `Reprint()` with a fresh header; the session id updates.
  - `/compact [instructions]`: send to the engine; spinner text "Compacting conversation";
    show the boundary item.
- [x] **B4 [M1] `/resume` picker (alias `/continue`; `dialog.resume`, alt-screen or
  centered).**
  - Lists current-project sessions by default, including sdk-cli ones.
  - Fuzzy search over title, first/last prompt and branch.
  - Space previews (last messages); ctrl+r renames (`rename_session` when that session is
    live, otherwise the custom-title metadata written via the engine on resume).
  - ctrl+a toggles all projects; ctrl+w worktrees; ctrl+b branch filter; search by PR URL
    if a session is PR-linked.
  - Enter resumes (an engine restart).
- [x] **B5 [M1] Generic H handoff command** (`ext.Command` helper used by plans 08 and 09
  for any not-yet-native `local-jsx` command). Procedure as above, with a notice "Opening in
  Claude Code; exit to return to mantle".
- [ ] **B6 [M2] Rewind (`mantle:rewind`, `/rewind`, aliases `checkpoint`, `undo`; double-esc
  on an empty prompt).**
  - A message selector (context `MessageSelector`) over user turns, with a diff summary from
    `rewind_files` dry-run.
  - Actions: restore code + conversation, conversation only, or code only. Summarize from
    here / up to here: emulate via compact instructions or H.
  - Restore the pre-`/clear` session.
- [ ] **B7 [M2] `/branch [name]` and `/fork [prompt]`.** Branch = fork at the current point
  (`--fork-session`, restart, keep the old one resumable). `/fork` = spawn a background
  session via `claude --bg` or H (Claude Code sends forks to the background).
- [ ] **B8 [M2] `/export [filename]` and `/copy [N]`.**
  - `/export`: render the normalized transcript as markdown or plain text to a file or the
    clipboard; use the unstable `export_conversation` if supported.
  - `/copy`: copy the last or Nth-latest response; a code-block picker when several blocks
    exist; `w` writes to a file. Use plan 07's clipboard service.
- [ ] **B9 [M2] Rename and recap.** `/rename [name]` → `rename_session`. `/recap` is
  headless-capable (engine). Away summary: after N minutes away (`awaySummaryEnabled`),
  request a one-line recap and show it (headless has no auto recap).
- [ ] **B10 [M2] `/btw [question]`.**
  - A side-question overlay that doesn't interrupt.
  - Uses `side_question` if `Supports()`; otherwise a forked one-shot engine
    (`--resume=<sid> --fork-session --no-session-persistence --tools ""`).
  - Keeps about 20 exchanges of history; shift+left/right history; `c` copy, `f` fork,
    `x` clear.
- [ ] **B11 [M2] `/diff`.** Uncommitted changes and per-turn diffs: `get_workspace_diff` if
  supported, else `git diff` plus mantle's own tracking of Edit/Write results per turn.
  Alt-screen viewer with a file list (context `DiffDialog`) using `pkg/ui/diffview`.
- [ ] **B12 [M2] Passthrough commands with native polish.** `/goal [condition|clear]`
  (engine; show the active goal from `active_goal`), `/plan [open|desc]` (enable plan mode
  or open the plan file in `$EDITOR`), `/add-dir <path>` (engine command plus
  `register_repo_root`), `/cd <path>` (unstable `set_cwd` or H).
- [ ] **B13 [M2] `/context`.** A coloured grid from `get_context_usage` (categories: system
  prompt, tools, MCP tools, memory files, messages, free space), with percentages and token
  counts; `/context all` for detail.
- [ ] **B14 [M2] `/usage` (aliases cost, stats).** Session cost and tokens per model
  (`result.modelUsage`), plan usage bars from `get_usage` (rate limits with
  utilization/resetsAt; `d`/`w` toggles day/week), local stats from the index. Auto-compact
  warning when context is high ("Context left until auto-compact: N%") from
  `get_context_usage` or result usage.
- [ ] **B15 [M2] Resume-from-summary dialog** (idle > 1 h and > 100k tokens). Offer resume
  as is, or compact first. Mark as H if the engine offers no wire.

## Design notes

- **One source of truth:** the transcript store (plan 03) holds both history and live items.
  You only produce `Item`s and call `Reprint`/`Print`.
- **Never hold the JSONL open for writing.** Only the engine writes sessions. mantle reads
  tails while the engine runs (tolerate partial last lines).
- **Engine restarts** (resume, fork, rewind, handoff) go through `Engine.Restart` from plan
  02. Check `background_tasks` first and warn, because restarting kills background shells
  and subagents.
- **Picker performance:** an index cache, lazy previews, and no full parse until a session
  is selected.
- **Licensing:** write our own picker strings; don't copy Claude Code's.

## Interfaces you provide / consume

- **Provide:** `internal/sessions` (reader, tree, index, slug); history `Item`s; the H
  handoff helper (`sessions.Handoff(ctx, command string) tea.Cmd`, exported via a
  `pkg/ext`-registered command factory, ID `cmd.handoff.<name>`); command IDs `cmd.resume`,
  `cmd.clear`, `cmd.compact`, `cmd.rewind`, `cmd.branch`, `cmd.fork`, `cmd.export`,
  `cmd.copy`, `cmd.rename`, `cmd.recap`, `cmd.btw`, `cmd.diff`, `cmd.goal`, `cmd.plan`,
  `cmd.add-dir`, `cmd.cd`, `cmd.context`, `cmd.usage`.
- **Consume:** `Engine.Restart/Control/Send` and the session tracker (plan 02); transcript
  store and diffview (plan 03); clipboard service (plan 07); dialogs and `Print`/`Reprint`
  (plan 01).
- **Requests:**
  - To plan 11: `-r` with no value, `-c`, `--fork-session` and `--session-id` parsed and
    passed to you as startup options.
  - To plan 04: double-esc on an empty prompt dispatches `mantle:rewind`.

## Tests and done criteria

- Reader and tree tests on fixtures (branches, sidechains, malformed lines); slug goldens.
- The opt-in parse of all local sessions reports zero crashes.
- Picker < 200 ms warm with 1,000 sessions; key-flow tests for search, preview, rename and
  all-projects.
- Resume via fakeapi: history is printed and matches the scrollback golden, then a new turn
  appends correctly.
- Rewind fixture test: dry run, restore code, restore conversation (resume-at).
- Handoff test with a stub `claude` binary: the engine is stopped before exec and restarted
  after.

## Parity coverage

- `SS-*`: sessions (picker, `-c`/`-r`/fork, `/clear`, `/compact`, `/branch`, `/fork`,
  `/rewind`, `/export`, `/copy`, `/rename`, `/recap`, away summary, `/btw`, `/diff`,
  `/goal`, `/plan`, `/add-dir`, `/cd`, resume-from-summary, handoff).
- `CU-*`: context and usage (`/context` grid, `/usage`, `/cost`, `/stats`, rate-limit
  display, auto-compact warnings).

## Out of scope

- Rendering of items: 03. Usage-limit wait and auto-continue: 05. Clipboard and notification
  internals: 07.
- `claude agents` agent view and background sessions: H via 09/11.

## Parallel rules (reminder)

- No repo-wide rewriters (use `make fmt-06`).
- `pkg/ext` and `pkg/proto` are additive-only; requests go in `docs/plans/requests/`.
- Commit only your own files with prefix `[06]`; `make test-06`; no imports of other
  `features/*` packages.
