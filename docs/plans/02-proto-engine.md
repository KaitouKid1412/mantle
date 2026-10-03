# Plan 02: Proto & engine

| Session | Primary paths | Part B needs | Produces |
|---|---|---|---|
| mantle-02 | `pkg/proto`, `internal/engine`, `internal/testkit/enginefake`, `cmd/fakeclaude`, `cmd/fakeapi`, `scripts/record-fixture.sh`, `scripts/sdk-diff` | tag `contracts-v1` | tag `proto-v1`; afterwards you are the **protocol steward** |

## Goal

A reliable, typed, testable connection to the real `claude` binary, plus fakes, so every
other plan can develop and test without spending money. You also answer the open protocol
questions (spikes) before others build on guesses.

## Start prompt

> You are session 02 of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/02-proto-engine.md`, then execute that plan. Do Part A now. Start Part B once
> `git tag -l` shows the tags it needs. Commit with prefix `[02]`.

## Read first

- `docs/research/protocol-2.1.288.md`: the whole file; it is your spec.
- `docs/research/design-review.md` §1.4 (engine client), §6 (risks), §7 (spikes).
- `docs/research/inventory-docs.md` §0 (headless transport baseline) and the "not supported
  in headless" list.

## Facts you can rely on (Claude Code 2.1.288, SDK 0.3.288)

- **Framing.** NDJSON both ways over stdin/stdout. Lines can be many MB (tool results,
  base64 images).
  - Use `bufio.Reader.ReadSlice`/`ReadBytes('\n')`, **not** a default `bufio.Scanner`.
  - Skip stdout lines not starting with `{`. A line that starts with `{` but doesn't parse
    is an error.
  - Pipe and drain stderr; never inherit it, or it corrupts the TUI.
- **SDK spawn arguments** (copy them; Anthropic keeps this path working).
  - Always: `--output-format stream-json --verbose --input-format stream-json`.
  - Conditionally:
    - Model and thinking: `--thinking adaptive|disabled`, `--thinking-display`,
      `--max-thinking-tokens`, `--effort`, `--model`, `--fallback-model`.
    - Permissions: `--permission-prompt-tool stdio`, `--permission-prompts host|none`,
      `--permission-mode`.
    - Sessions: `--resume=<id>` (equals form), `--continue`, `--fork-session`,
      `--resume-session-at=<uuid>`, `--resume-drops-turn=<uuid>`, `--session-id=<uuid>`,
      `--no-session-persistence`.
    - Tools and config: `--allowedTools`, `--disallowedTools`, `--tools`,
      `--mcp-config`, `--strict-mcp-config`, `--setting-sources=`, `--settings`,
      `--add-dir`, `--plugin-dir`, `--agents`, `--agent`.
    - Streaming extras: `--include-hook-events`, `--include-partial-messages`,
      `--replay-user-messages`, `--prompt-suggestions`, `--forward-subagent-text`.
    - Other: `-n/--name`, `--debug-file`.
  - Neither SDK passes `-p`; non-TTY stdout makes the CLI non-interactive. Passing `-p`
    explicitly does no harm.
- **mantle's spawn.**
  - Base: `claude --output-format stream-json --input-format stream-json --verbose
    --include-partial-messages --permission-prompt-tool stdio --include-hook-events
    --forward-subagent-text --replay-user-messages`.
  - Plus as needed: `-n`, `--model`, `--permission-mode`,
    `--resume=<id> [--fork-session] [--resume-session-at=<uuid>]`, `--add-dir…`,
    `--settings '<json>'` (startup gates, for example `disabledMcpjsonServers`).
  - Never pass `--system-prompt` (the default Claude Code prompt must stay).
  - Turn on prompt suggestions via `initialize.promptSuggestions: true`.
- **Environment.**
  - Delete `CLAUDECODE`, `NODE_OPTIONS`, `DEBUG`, `CLAUDE_CODE_SIMPLE`, and
    `CLAUDE_CODE_SAFE_MODE` unless the user asked for safe mode.
  - Set `CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS=1` and
    `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true`.
  - `CLAUDE_CODE_ENTRYPOINT`: decided by spike S5. If unset, the CLI uses `sdk-cli`, which
    hides sessions from Claude Code's own `/resume` picker.
- **Process.** `SysProcAttr{Setpgid: true}`, so terminal signals don't reach the engine and
  it survives suspend. Record the pgid in `~/.mantle/run/<pid>.json` for the launcher.
  - Shutdown: `end_session`, close stdin, wait about 5 s, SIGTERM the group, wait about 5 s,
    SIGKILL.
- **Turn shape.**
  - `system/init` arrives at the start of **every** turn.
  - Then `stream_event` deltas, then one `assistant` message per finished content block
    (grouped by `message.id`), then `user` tool_results, possibly control requests, and
    **exactly one `result` per turn**.
  - `session_state_changed: idle` means truly done, including background work.
- **API failures** arrive as `result` with `subtype: success` and `is_error: true`.
- **Interrupted turns** (from code reading; spike S1 confirms) end with
  `error_during_execution` and `terminal_reason` `aborted_streaming` or `aborted_tools`.
- **Resume does not replay history on stdout.** Plan 06 reads the JSONL.

## `pkg/proto` type inventory (write all of these; each struct keeps `Raw json.RawMessage`)

**Envelope.** `{type, subtype?}`. Decode the envelope first, then the typed struct. Unknown
types or subtypes produce `Unknown{Type, Subtype, Raw}` and are never dropped.

**stdout messages**
- `system`, by subtype:
  - `init`: cwd, session_id, tools, mcp_servers{name,status,source}, model,
    permissionMode, slash_commands, terminal_slash_commands, apiKeySource, betas,
    claude_code_version, output_style, agents, skills, plugins, plugin_errors,
    mcp_server_errors, capabilities[], memory_paths, scratchpad_path, fast_mode_state,
    fast_mode_disabled_reason, effort, view_mode.
  - `compact_boundary`: compact_metadata{trigger, pre_tokens, post_tokens, duration_ms,
    preserved_*}.
  - `status`: status = compacting | requesting | null; permissionMode, compact_result,
    compact_error.
  - `api_retry`: attempt, max_retries, retry_delay_ms, error_status, error.
  - `informational`: content, level, tool_use_id, prevent_continuation, tag.
  - `local_command_output`.
  - `hook_started`, `hook_progress`, `hook_response`: hook_id, hook_name, hook_event,
    stdout, stderr, output, exit_code, outcome.
  - `task_started`, `task_progress`, `task_updated`, `task_notification`,
    `background_tasks_changed`.
  - `session_state_changed`, `commands_changed`, `notification`, `thinking_tokens`,
    `permission_denied`, `model_refusal_fallback`, `model_refusal_no_fallback`,
    `memory_recall`, `files_persisted`, `elicitation_complete`, `plugin_install`,
    `control_request_progress`, `worker_shutting_down`.
  - Internal or optional: `session_title_changed`, `turn_duration`, `api_error`,
    `model_fallback`, `away_summary`.
- `assistant`: message (BetaMessage: id, model, content blocks text / thinking /
  redacted_thinking / tool_use / server_tool_use, stop_reason, usage), parent_tool_use_id,
  error category, request_id, user_message_uuid(s), aborted, supersedes[], subagent_type,
  context_usage, usage_report, local_command_* fields.
- `user`: message.content (tool_result blocks), parent_tool_use_id, tool_use_result
  (structured; keep it raw plus typed helpers for Bash, Edit, Write, Read and Agent),
  isSynthetic, isReplay, file_attachments.
- `stream_event`: event (message_start, content_block_start, content_block_delta with
  text_delta / thinking_delta / input_json_delta / signature_delta, content_block_stop,
  message_delta, message_stop), parent_tool_use_id, ttft_ms.
- `result`, shared fields:
  - Timing and status: duration_ms, duration_api_ms, is_error, num_turns, stop_reason,
    terminal_reason, result_index, queued_turn_count.
  - Cost and usage: total_cost_usd, usage, modelUsage{model: inputTokens, outputTokens,
    cache*, costUSD, contextWindow, maxOutputTokens}.
  - Other: permission_denials[], user_message_uuid(s), fast_mode_state.
  - `subtype` `success`: result, api_error_status, structured_output, deferred_tool_use,
    local_command.
  - `subtype` one of the `error_*` values: errors[], startup_failure_reason.
- Other top-level types: `tool_progress`, `tool_use_summary`, `auth_status`,
  `rate_limit_event{rate_limit_info{status, resetsAt, rateLimitType, utilization, …}}`,
  `prompt_suggestion`, `conversation_reset{new_conversation_id, trigger}`, `active_goal`,
  `keep_alive`, `command_lifecycle`.

**stdin messages**
- `user`: message{role, content: string | blocks text / image (base64) / document}, uuid,
  parent_tool_use_id, priority (`now` | `next` | `later`), shouldQuery,
  client_composed, origin{kind:"human"}, pasted_content, inline_pastes, timestamp.
- Also: `control_request`, `control_response`, `control_cancel_request`, `keep_alive`,
  `update_environment_variables`.

**Control requests, client → CLI.** Typed request and response structs for every
documented subtype:
- `initialize`: request with hooks, agents, promptSuggestions, supportedDialogKinds,
  forwardSubagentText, title, …; response with commands[{name, description, argumentHint,
  aliases, builtin}], agents, output_style, available_output_styles, models[ModelInfo],
  account, pid, current_permission_mode, session_state, capabilities, fast_mode_state,
  pending_permission_requests, pending_user_dialog_requests.
- Turn and settings control: `interrupt`, `set_permission_mode`, `set_model`,
  `set_max_thinking_tokens`, `apply_flag_settings`, `update_settings`, `get_settings`,
  `get_hooks_listing`, `list_permission_rules`.
- MCP: `mcp_status`, `mcp_reconnect`, `mcp_toggle`, `mcp_set_servers`, `mcp_call`,
  `mcp_read_resource`, `set_mcp_permission_mode_override`.
- Usage and info: `get_context_usage`, `get_usage`, `get_session_cost`, `list_models`,
  `get_binary_version`.
- Tasks and messages: `rewind_files`, `cancel_async_message`, `stop_task`,
  `background_tasks`, `get_task_output`.
- Session: `rename_session`, `generate_session_title`, `reload_plugins`, `reload_skills`,
  `reload_output_styles`.
- Files and misc: `read_file`, `file_suggestions`, `seed_read_state`, `register_repo_root`,
  `set_color`, `end_session`.

**Control requests, CLI → client.**
- `can_use_tool`:
  - Fields: tool_name, input, tool_use_id, permission_suggestions[PermissionUpdate],
    blocked_path, decision_reason, decision_reason_type, classifier_approvable,
    suppress_always_allow_rule, default_to_no, matched_ask_rule, title, display_name,
    description, agent_id, mcp_server{name, source}, requires_user_interaction.
  - Response `PermissionResult`: allow{updatedInput?, updatedPermissions?, toolUseID,
    decisionClassification} or deny{message, interrupt?, toolUseID, decisionClassification}.
- `hook_callback`: callback_id, input (HookInput), tool_use_id. Response: hook JSON output.
- `mcp_message`, `elicitation` (accept / decline / cancel + content),
  `request_user_dialog`, `oauth_token_refresh`.

**PermissionUpdate.** One of:
- `addRules` / `replaceRules` / `removeRules` with rules[{toolName, ruleContent?}],
  behavior allow / deny / ask, and destination;
- `setMode` with mode and destination;
- `addDirectories` / `removeDirectories` with directories and destination.

`destination` ∈ userSettings | projectSettings | localSettings | session | cliArg.

**Undocumented subtypes** (in the binary, unstable) go in `internal/engine/unstable.go`
only, never in `pkg/proto`'s stable set: `rewind_conversation`, `fork_conversation`,
`export_conversation`, `get_status`, `side_question`, `set_cwd`, `add_directory`,
`mcp_authenticate`, `mcp_clear_auth`, `claude_authenticate`, `get_memory_dialog`,
`get_skills_dialog`, `get_sandbox_dialog`, `get_workspace_diff`, `list_directory`.

## Part A: start immediately

- [ ] **A1 Spikes S1–S14, S17.** These use real `claude` and cost a few cents. Script each
  in `scripts/spikes/` and record the facts in a "Facts verified on 2.1.288" section at the
  bottom of this file.
  - **S1** What an interrupt produces mid-text vs mid-tool; the `interrupt(cancel_queued)`
    response.
  - **S2** `priority` now/next/later behaviour while a turn runs; does
    `--replay-user-messages` give acks; can `cancel_async_message` take back a queued
    message.
  - **S3** Does headless write `~/.claude/history.jsonl` and `~/.claude/paste-cache`?
  - **S4** Does the Notification hook fire in headless mode? What does
    `--include-hook-events` emit?
  - **S5** Which `CLAUDE_CODE_ENTRYPOINT` values (`cli`, `sdk-cli`, `mantle`) change
    `/resume` visibility in Claude Code, or gate any features?
  - **S6** A zero-API conformance probe: initialize, then check commands, skills, plugins,
    hooks (`get_hooks_listing`) and memory files (`get_context_usage`) are visible, then
    `end_session`.
  - **S7** Exact semantics of `--resume-session-at`, `--fork-session`,
    `--resume-drops-turn`; `rewind_files` with `dry_run`.
  - **S8** How local slash-command output arrives (synthetic assistant + `result.local_command`
    vs `local_command_output`).
  - **S9** Is `--settings '{"disabledMcpjsonServers":[…]}'` honoured headlessly?
  - **S10** Trust: do trusted parent directories count? What does headless load in an
    untrusted repo (hooks, `.mcp.json`, env)?
  - **S11** `file_suggestions` latency and shape; how to list MCP resources for `@`.
  - **S12** `--forward-subagent-text` events and `parent_tool_use_id`.
  - **S13** Thinking deltas and `--thinking-display` / `thinking.display: summarized`.
  - **S14** Real claude (headless and interactive) against fakeapi with an isolated
    `CLAUDE_CONFIG_DIR` (pre-seed API-key approval). Do this together with A6.
  - **S17** CPU at peak delta rates; choose the coalescing interval (default 16 ms).
- [ ] **A2 `pkg/proto`.** All types above; envelope decode; `Unknown`; marshal helpers for
  stdin messages; JSON round-trip tests on hand-written and recorded samples.
  - If plan 01 created placeholders (`pkg/proto/placeholder.go`), replace them with the
    real types under the same names.
    - **Delete `placeholder.go` in the same commit** that adds the real types; otherwise the
      package has duplicate declarations and everyone's build breaks.
    - Keep every placeholder field (name and JSON tag), because `pkg/ext` may already use it.
    - Run `go build ./...` before committing.
  - Commit, run `git tag proto-v1`, and add a line under "Milestones" in
    `docs/plans/00-overview.md`.
- [ ] **A3 Transport (`internal/engine/transport.go`).**
  - A reader goroutine that accumulates multi-MB lines and decodes them.
  - A writer goroutine with a queue and a mutex-free single writer; never block the UI on a
    pipe.
  - A stderr ring buffer (last 64 KB).
- [ ] **A4 fakeclaude script format and runner (`internal/testkit/enginefake`,
  `cmd/fakeclaude`).**
  - A JSONL script of steps: `emit <json>`, `expect <matcher>` (stdin message or control
    response), `respond <json>` (reply to a client control request), `request <json>` (CLI →
    client control request), `delay <ms>`, `exit <code>`.
  - Runs in-process (`io.Pipe`) or as a binary on the PATH as `claude`.
- [ ] **A5 Fixture recorder (`scripts/record-fixture.sh` plus a Go helper).**
  - Tees both directions with timestamps into `testdata/fixtures/02/<name>.jsonl`.
  - Sanitises home paths (`$HOME` → `/home/user`), usernames, emails, tokens and
    session-specific UUIDs (stable remap).
- [ ] **A6 fakeapi (`cmd/fakeapi`).** A Messages API SSE mock: scripted text, thinking and
  tool_use turns; generic replies for title and Haiku side calls; `/v1/messages` and count
  tokens. Used with `ANTHROPIC_BASE_URL` and an isolated `CLAUDE_CONFIG_DIR`, for free,
  deterministic end-to-end tests of the real engine.

## Part B: after `contracts-v1`

- [ ] **B1 [M1] Process supervisor (`internal/engine/process.go`).**
  - Flag builder (the table above) and environment cleanup; `Setpgid`.
  - Restart with `resume`, `fork-session` or `resume-session-at`.
  - On unexpected exit: an `EngineExitedMsg` carrying the stderr tail; the UI offers
    "restart with --resume".
  - `Stop()` for handoff (plan 06), so the engine never runs while interactive `claude`
    writes the same JSONL.
- [ ] **B2 [M1] Correlator.** `request_id` → pending map, with timeouts (60 s for
  initialize, 30 s by default); `control_cancel_request` for our own requests; error
  responses become typed errors.
- [ ] **B3 [M1] Capability cache.** From the initialize response and init
  `capabilities[]`. An "unsupported subtype" error turns the capability off.
  `Supports(subtype)` feeds the UI.
- [ ] **B4 [M1] CLI → client requests.** Each becomes a `tea.Msg` carrying a `Reply` func
  that writes exactly one `control_response`. `control_cancel_request` closes the matching
  dialog (send a `CancelMsg`).
  - Declare only the dialog kinds that plans 05/09 actually implement in
    `supportedDialogKinds`.
  - Answer `hook_callback` with `{}` unless a feature registered a callback.
- [ ] **B5 [M1] Coalescer.** Merge `stream_event` deltas per (message id, content index);
  flush at most every 16 ms (S17 value), or immediately on any non-delta event; preserve
  order.
- [ ] **B6 [M1] `ext.Engine` implementation and tea bridge (`internal/engine/bridge.go`).**
  - Several engines keyed by `EngineID` (main, builder, btw, bg).
  - `Send` (stamps `uuid` and `origin:{kind:"human"}` for user input), `Interrupt`,
    `Control`, `Supports`, `Restart`.
- [ ] **B7 [M1] Session tracker.** Session id (updated on `conversation_reset`), model,
  permission mode, state (idle / running / requires_action), cwd, and init data (tools,
  commands, skills, plugins, MCP servers, output style, fast mode). Exposed through
  `ext.SessionInfo`.
- [ ] **B8 [M2] `unstable.go`.** Wrappers for the undocumented subtypes, each with
  `Supports` and a documented fallback:
  - `rewind_conversation` → `--resume-session-at`;
  - `side_question` → forked engine;
  - `get_workspace_diff` → `git diff`.
- [ ] **B9 [M2] Conformance probe and pinning.**
  - Run S6's probe when `claude --version` changes; store passing versions in
    `~/.mantle/state/engines.json`.
  - On failure, notify and offer to pin the last passing binary
    (`~/.local/share/claude/versions/<v>`).
  - Guards against `--bare` becoming the headless default.
- [ ] **B10 [M2] `scripts/sdk-diff`.** Fetch `@anthropic-ai/claude-agent-sdk@0.3.<patch>`
  for CLI `2.1.<patch>` into a cache (never commit the .d.ts) and diff the message unions
  and control subtypes against a table in `pkg/proto`.
- [ ] **B11 [M1] Fixtures for every flow,** recorded through fakeapi where possible:
  - plain answer; tool use allow / deny / always; AskUserQuestion; ExitPlanMode;
  - interrupt during text and during a tool; queued message with priority;
  - `/compact`; `/clear`; resume; subagent; background task; elicitation;
  - local slash command; rate_limit_event; api_retry.

## Design notes

- **Decode for resilience.** Never fail the session on an unknown field or type. Log
  unknowns at debug level, and keep the raw JSON so renderers can show a generic block.
- **Ordering.** The bridge emits messages in stdout order. The coalescer must not reorder a
  delta past a non-delta.
- **Engine identity.** Engines are cheap. `/btw` and the `/mantle` builder are separate
  processes with their own `EngineID`.
- **Shutdown.** Kill the engine's group if the UI dies (the launcher also does this,
  plan 10).
- **License.** Write the protocol types by hand. Don't vendor the SDK's `.d.ts`.

## Interfaces you provide / consume

- **Provide:** `pkg/proto` (all types), the `ext.Engine` implementation, Engine*Msg
  messages, `enginefake` (in-process and binary), `fakeapi`, fixtures, the probe, `sdk-diff`.
- **Consume:** `pkg/ext` (plan 01).
- **Requests:** startup gate results from plan 05 arrive as `SpawnOpts.Settings` JSON; plan
  06 calls `Stop`/`Restart` for handoff and resume.

## Tests and done criteria

- Every fixture decodes and round-trips; unknown types are preserved.
- `-race` tests cover the correlator, writer queue, coalescer ordering and cancel.
- enginefake drives scripted flows in unit tests for other plans.
- With fakeapi, real headless claude answers a scripted prompt and runs a Bash tool in a
  temp dir, offline and at no cost.
- The spike facts section is filled in.

## Parity coverage

- `EN-*`: engine plumbing (spawn, flags and env, init data, capability detection, probe,
  crash recovery, several engines, drift guard).

## Out of scope

- Rendering: 03. Dialogs that answer `can_use_tool`: 05. JSONL history: 06. CLI flag
  parsing: 11 (you provide the flag *builder*; plan 11 decides which user flags are
  forwarded). Launcher: 10.

## Parallel rules (reminder)

- No repo-wide rewriters; use `make fmt-02`.
- After `proto-v1`, `pkg/proto` is **additive-only**.
- Commit only your own files with prefix `[02]`; scoped tests (`make test-02`); no
  cross-feature imports.

## Facts verified on 2.1.288

_(fill in from the spikes)_
