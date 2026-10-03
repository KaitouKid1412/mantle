# Claude Code headless protocol (stream-json over stdio)

> Internal research notes for mantle planning (Claude Code 2.1.288, collected 2026-10-03). Not shipped.
> Derived from the installed binary, the official docs and the Agent SDK; do not copy strings/schemas from here into shipped code.

Spec for a Go client. Checked against:
- Claude Code **2.1.288** (local binary, Bun-compiled, JS embedded as plaintext);
- the SDK releases built for that exact CLI: Python SDK `main` (`_cli_version.py` = `"2.1.288"`) and `@anthropic-ai/claude-agent-sdk@0.3.288` (`sdk.d.ts` matches this binary);
- the binary's own embedded copy of the protocol definitions with descriptions (frames the CLI marks `@internal`).

`claude -p` was **not** run while collecting this, so the example JSON lines below are assembled from schemas and code, not captured from real output. Plan 02's spikes verify them.

---

## 1. Transport and framing

- **Format:** newline-delimited JSON (NDJSON) both ways, one JSON object per line ending in `\n`. Conversation messages and control-protocol frames share the same stdin/stdout streams.
- **Non-JSON lines:** the Python SDK skips stdout lines that don't start with `{` (some builds print e.g. `[SandboxDebug] ...`). Blank lines are ignored. A line that starts with `{` but fails to parse is a hard error.
- **Line length:** lines can be very large (tool results, base64 images). The Python SDK caps a line at 1 MB by default (configurable). **In Go, do not use `bufio.Scanner` with its default 64 KB buffer.** Use `bufio.Reader.ReadBytes('\n')` / a `ReadSlice` loop, or a Scanner with a 32–64 MB+ buffer.
- **stderr:** diagnostics only. Pipe and drain it; if it inherits the terminal it corrupts the Bubble Tea screen. `--debug-file <path>` is also available.
- **Writes:** one writer guarded by a mutex (or a writer goroutine). Requests and responses are matched by `request_id`, never by order.
- **Unknown values:** ignore any `type`/`subtype` you don't recognise; the set keeps growing.

`StdoutMessage` in `sdk.d.ts`:

```ts
type StdoutMessage = SDKMessage | SDKActiveGoalMessage | SDKControlResponse
                   | SDKControlRequest | SDKControlCancelRequest | SDKKeepAliveMessage;
```

---

## 2. Process launch

### 2.1 What the SDKs actually run

TypeScript SDK (`ProcessTransport.initialize`, `sdk.mjs`):

```js
B=["--output-format","stream-json","--verbose","--input-format","stream-json"]
// then, conditionally (order as in code):
--thinking adaptive|disabled | --max-thinking-tokens N ; --thinking-display X
--effort L ; --max-turns N ; --max-budget-usd X ; --task-budget N ; --model M ; --agent A
--betas a,b ; --json-schema '<json>' ; --debug-file P | --debug
--permission-prompt-tool stdio          // iff canUseTool callback given (else --permission-prompt-tool <mcpTool> if named)
--permission-prompts host|none
--continue ; --resume=<id>              // equals-form on purpose (stops a "-..." value being read as a flag)
--channels X ; --allowedTools a,b ; --disallowedTools a,b ; --tools a,b | "" | default
--mcp-config '{"mcpServers":{...}}' ; --setting-sources=user,project,local ; --strict-mcp-config
--permission-mode M ; --allow-dangerously-skip-permissions ; --fallback-model M
--include-hook-events ; --include-partial-messages ; --session-mirror ; --project-config-root=D
--add-dir D (repeat) ; --await-initialize | --plugin-dir P / --plugin-dir-no-mcp P (repeat)
--fork-session ; --resume-session-at=<uuid> ; --resume-drops-turn=<uuid> ; --session-id=<uuid>
--no-session-persistence ; --managed-settings '<json>' ; --settings '<json|path>' ; extraArgs...
```

Python SDK (`_build_command`) differences:
- First flags are `--output-format stream-json --verbose`; `--input-format stream-json` is added last.
- With no system prompt given it passes **`--system-prompt ""`** (empty). `{"type":"preset","append":X}` → `--append-system-prompt X`; `{"type":"file"}` → `--system-prompt-file`.
- `--permission-prompt-tool stdio` is added when `can_use_tool` is set (via `_configure_can_use_tool`).
- `--setting-sources=...` only when set; using the `skills` option defaults it to `user,project`.
- Also passes `--include-hook-events`, `--session-mirror`, `--plugin-dir`, `--thinking` / `--max-thinking-tokens`, `--effort`, `--json-schema`.

The TS SDK sends `systemPrompt` and `appendSystemPrompt` in the **initialize request** instead of as flags. If unset, it sends `systemPrompt: [""]` (empty prompt).

**For mantle:** to keep the normal Claude Code system prompt, pass **no** `--system-prompt` and **no** `systemPrompt` in initialize.

Neither SDK passes `-p`/`--print`. The CLI is non-interactive whenever stdout is not a TTY (`Ce()` returns `!launchOptions.isInteractive()`). CLI validation messages:
- `--input-format=stream-json requires --print` (pipes satisfy this)
- `--input-format=stream-json requires output-format=stream-json`
- `When using --print, --output-format=stream-json requires --verbose`

Adding `--print` explicitly does no harm.

### 2.2 Usable flags (from `claude --help` plus hidden options in the binary)

| Flag | Notes |
|---|---|
| `--output-format stream-json --input-format stream-json --verbose` | Required trio |
| `--include-partial-messages` | Adds `stream_event` frames (raw Anthropic SSE events) |
| `--replay-user-messages` | Echoes stdin user messages back as `user` with `isReplay: true` and your `uuid` |
| `--include-hook-events` | Without it only `SessionStart`/`Setup` hooks produce `hook_started`/`hook_progress`/`hook_response`; with it every hook event does |
| `--permission-prompt-tool stdio` | Makes the CLI send `can_use_tool` control requests. **Without it any "ask" decision becomes a denial** (reported as `system/permission_denied`) |
| `--permission-prompts host\|none` | Default `host`; `none` denies anything that would prompt |
| `--permission-mode` | `default`, `acceptEdits`, `plan`, `bypassPermissions`, `dontAsk`, `auto`; `manual` aliases `default` (`kg(e){return e==="manual"?"default":e}`) |
| `--allow-dangerously-skip-permissions` | Needed before `bypassPermissions` can be enabled mid-session |
| `--model`, `--fallback-model`, `--effort`, `--thinking adaptive\|disabled`, `--max-thinking-tokens`, `--thinking-display` | Model and thinking |
| `--allowedTools`, `--disallowedTools`, `--tools` | Comma-separated |
| `--mcp-config <json\|file>`, `--strict-mcp-config` | MCP config |
| `--setting-sources=user,project,local` | Omit to load all three (CLI default) |
| `--settings <json\|file>` | Flag-tier settings |
| `--append-system-prompt[-file]`, `--system-prompt[-file]` | System prompt overrides |
| `--max-turns`, `--max-budget-usd`, `--task-budget` | Limits |
| `--resume=<id\|title>`, `--continue`, `--fork-session`, `--session-id=<uuid>`, `--resume-session-at=<uuid>`, `--resume-drops-turn=<uuid>`, `--no-session-persistence` | See section 10 |
| `--add-dir`, `--plugin-dir`, `--agents <json>`, `--agent`, `-n/--name` | Extra context and agents |
| `--prompt-suggestions` | `prompt_suggestion` frame after each turn |
| `--forward-subagent-text` | Forwards subagent text and thinking (default: only their tool_use/tool_result) |
| `--enable-auth-status` (hidden) | Emits `auth_status` frames |
| `--await-initialize` (hidden) | CLI reads `initialize` as the first stdin line during startup (needed for `plugins` in initialize) |
| `--debug-file <path>` | Debug log destination |

### 2.3 Environment variables

Python SDK child environment (verbatim):

```python
inherited_env = {k: v for k, v in os.environ.items() if k != "CLAUDECODE"}
process_env = {**inherited_env, "CLAUDE_CODE_ENTRYPOINT": "sdk-py", **self._options.env,
               "CLAUDE_AGENT_SDK_VERSION": __version__}
# + CLAUDE_CODE_SDK_READS_SESSION_STATE=1 (unless set), CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true (opt-in),
#   PWD=cwd, OTEL TRACEPARENT/TRACESTATE
```

TS SDK: sets `CLAUDE_CODE_ENTRYPOINT=sdk-ts` if unset, `CLAUDE_AGENT_SDK_VERSION=0.3.288`, `CLAUDE_CODE_SDK_READS_SESSION_STATE=1`, `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING` if requested; deletes `NODE_OPTIONS` and `DEBUG`.

What matters for mantle:
- **Remove `CLAUDECODE`** so the child doesn't think it is nested inside Claude Code.
- **`CLAUDE_CODE_ENTRYPOINT`:** if unset in non-interactive mode the CLI sets `sdk-cli` (and rewrites `cli` to `sdk-cli`). Transcripts with entrypoint `sdk-cli`/`sdk-ts`/`sdk-py` are **hidden from the interactive `/resume` picker** (code exported as `Wgs as isHiddenFromSessionPicker`). `claude --resume <id>` still works on them.
- **`CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS=1`:** enables `system/session_state_changed` (`idle`/`running`/`requires_action`), the most reliable busy/idle signal. `CLAUDE_CODE_SDK_READS_SESSION_STATE=1` sends the same frames marked `sdk_host_only: true`.
- **`CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true`:** required for `rewind_files`.
- **`CLAUDE_CONFIG_DIR`:** relocates `~/.claude`, including session storage.

### 2.4 Suggested mantle invocation

```
claude --output-format stream-json --input-format stream-json --verbose \
  --include-partial-messages --permission-prompt-tool stdio [--replay-user-messages] \
  [--model M] [--permission-mode M] [--resume=<id> [--fork-session]] [--add-dir ...]
env: CLAUDECODE removed, CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS=1
cwd: project directory, stdin/stdout/stderr: pipes
```

**Shutdown:** close stdin, wait ~5 s, SIGTERM, wait ~5 s, SIGKILL (Python). TS uses ~2 s grace. Optionally send the `end_session` control request first.

---

## 3. Session sequence

1. Spawn the process.
2. Client sends `control_request` `initialize`. CLI replies with `control_response` (commands, models, account, …). With `--enable-auth-status` an `auth_status` frame follows.
3. Client sends a `user` message. For that turn the CLI emits:
   - `system/init` (sent at the start of **every** turn);
   - `stream_event`s if partial messages are enabled;
   - one `assistant` message per finished content block (several share one `message.id`);
   - `user` messages carrying `tool_result`s;
   - possibly CLI→client `control_request`s (`can_use_tool`, `hook_callback`, …);
   - **exactly one `result` per turn**;
   - possibly `prompt_suggestion`, and `session_state_changed: idle`.
4. More user messages may be written any time; mid-turn ones are queued (see `priority`, 4.1). The CLI runs until stdin EOF and exits non-zero if the last result was an error.

The Python SDK waits for the initialize response before writing the first prompt (60 s timeout, or `CLAUDE_CODE_STREAM_CLOSE_TIMEOUT`).

---

## 4. Messages the client writes to stdin

### 4.1 User message

From `SDKUserMessage` and the CLI's input parser:

```json
{"type":"user","session_id":"","parent_tool_use_id":null,
 "uuid":"<client uuid, optional but recommended>",
 "message":{"role":"user","content":"text"  |  [ {"type":"text","text":"..."},
   {"type":"image","source":{"type":"base64","media_type":"image/png","data":"<b64>"}},
   {"type":"document","source":{...}} ]},
 "priority":"now|next|later", "shouldQuery":true, "client_composed":true,
 "origin":{"kind":"human"}, "pasted_content":[...], "inline_pastes":["..."], "timestamp":"ISO"}
```

- `message.content` follows Anthropic `MessageParam`. `session_id` is ignored (Python sends `""` or `"default"`); flags decide the session.
- `uuid` is the correlation key. It comes back in `result.user_message_uuid(s)`, on the first assistant/stream frame of the turn, in replay echoes, in `cancel_async_message`, and in interrupt receipts.
- `shouldQuery: false` adds the message to the transcript without starting a turn; it merges into the next message that does.
- `client_composed: true` sends text exactly as written: no `@path` expansion, no slash-command dispatch, no turn-start attachments. Needs CLI ≥ 2.1.248.
- `origin: {kind:"human"}` must be set or human-only behaviours (e.g. the `ultracode` workflow keyword) are refused.
- `priority` for messages sent while a turn runs:
  - `now`: delivered immediately by ending the current turn;
  - `next`: folded into the running turn between tool rounds;
  - `later`: queued as its own turn;
  - default: `later` (or `next` behind a feature flag), per code `Xnt()`.

### 4.2 Control frames from the client

- `{"type":"control_request","request_id":"<unique>","request":{"subtype":...}}` (section 5)
- `{"type":"control_response","response":{"subtype":"success","request_id":"<id>","response":{...}}}` or `{"subtype":"error","request_id":"<id>","error":"msg"}` — answers to CLI-originated requests (section 6)
- `{"type":"control_cancel_request","request_id":"<id of one of YOUR requests>"}` (no reply)
- `{"type":"keep_alive"}`
- `{"type":"update_environment_variables","variables":{"K":"V"},"request_id":"opt"}`

SDK request id format: `req_{counter}_{8hex}`. Any unique string works.

---

## 5. Control requests the client sends

Documented (`SDKControlRequestInner` in `sdk.d.ts`, plus the SDK methods that send them):

| subtype | request fields | success `response` |
|---|---|---|
| `initialize` | see 5.1 | `SDKControlInitializeResponse` (section 8) |
| `interrupt` | `cancel_queued?: bool` | `{still_queued: string[], cancelled?: string[]}`; the interrupted turn's `result` follows |
| `set_permission_mode` | `mode` | `{mode}` (error codes e.g. `invalid_mode`) |
| `set_model` | `model?: string\|null` (null or `"default"` resets) | `{}` |
| `set_max_thinking_tokens` | `max_thinking_tokens?: number\|null`, `thinking_display?: "summarized"\|"omitted"\|"highlights"\|null` | `{}` |
| `apply_flag_settings` | `settings: {...}` e.g. `{"effortLevel":"high"}`; null clears a key | `{}` |
| `update_settings` | `source: "localSettings"\|"userSettings"`, `settings` (allowlist: `outputStyle`, `effortLevel`) | `{}` |
| `get_settings`, `get_hooks_listing`, `list_permission_rules` | none | settings / hooks listing / `{state}` |
| `mcp_status` | none | `{mcpServers: McpServerStatus[]}` |
| `mcp_reconnect` | `serverName` | `{}` |
| `mcp_toggle` | `serverName`, `enabled` | `{}` |
| `mcp_set_servers` | `servers: Record<name, config>` | `{added, removed, errors}` |
| `mcp_call` | `tool: "mcp__srv__tool"`, `arguments` | tool result (no model turn, no permission check) |
| `mcp_read_resource` | `serverName`, `uri: "ui://..."` | `{contents:[...]}` |
| `mcp_message` | `server_name`, `message` (JSON-RPC from an SDK server) | `{}` |
| `get_context_usage` | `detail?: "summary"\|"full"` | `SDKControlGetContextUsageResponse` (categories, totalTokens, maxTokens, percentage, …) |
| `get_usage` | `skip_behaviors?` | `SDKControlGetUsageResponse` (session cost, rate limits) |
| `get_session_cost` | none | `{text}` |
| `list_models` | none | `{models: ModelInfo[]}` |
| `get_binary_version` | none | `{version, buildTime}` |
| `rewind_files` | `user_message_id`, `dry_run?` | `{canRewind, error?, filesChanged?, insertions?, deletions?, skippedLinks?}` |
| `cancel_async_message` | `message_uuid` | `{cancelled: bool}` |
| `stop_task` | `task_id` | `{}` |
| `background_tasks` | `tool_use_id?` | like Ctrl+B; SDK method returns a boolean; exact wire payload unconfirmed |
| `get_task_output` | `task_id` | `{output, total_bytes, truncated}` |
| `rename_session` | `title`, `source?: "remote"\|"host"`, `session_id?` | `{}` |
| `generate_session_title` | `description`, `persist?` | `{title}` |
| `reload_plugins` | `hold_on_cache_impact?` | `{commands, agents, plugins, mcpServers, error_count, held?, cache_impact?}` |
| `reload_skills` | none | `{skills: SlashCommand[]}` |
| `reload_output_styles` | none | `{available_output_styles}` |
| `read_file` | `path`, `max_bytes?`, `encoding?` | `{contents, absPath, truncated?, encoding?}` |
| `file_suggestions` | `query` | `{suggestions:[{path}], cwd?}` (good for `@` autocomplete) |
| `seed_read_state` | `path`, `mtime` | `{}` |
| `register_repo_root` | `directory`, `reload_claude_md?`, `reload_plugins?`, `reload_skills?` | none |
| `set_color` | `color` | none |
| `set_mcp_permission_mode_override` | `serverName`, `mode: "default"\|"auto"\|null` | `{warning?}` |
| `end_session` | `reason?` | success, then the CLI exits |

**Undocumented but present in 2.1.288** (the CLI's subtype map `Pk`; shapes unpublished and **unstable**): `claim_session`, `set_cwd`, `add_directory`, `get_status`, `get_plan`, `export_conversation`, `fork_conversation`, `rewind_conversation` (fields `target_message_uuid`, `interrupt_if_running`, `last_seen_user_message_uuid`), `side_question`, `submit_feedback`, `message_rated`, `channel_enable`, `mcp_authenticate`, `mcp_oauth_callback_url`, `mcp_clear_auth`, `claude_authenticate`, `claude_oauth_callback`, `claude_oauth_wait_for_completion`, `get_memory_dialog`, `get_skills_dialog`, `get_sandbox_dialog`, `get_chrome_*`, `select_chrome_browser`, `set_prompt_suggestions_paused`, `stage_file`, `poll_event`, `list_directory`, `get_workspace_diff`, `ultrareview_launch`, `remote_control`, `turn_handoff`, and the `ui_*` family.

### 5.1 `initialize` request (all fields optional)

```ts
{ subtype:'initialize',
  hooks?: { [HookEvent]: [{matcher?: string, hookCallbackIds: string[], timeout?: number}] },
  sdkMcpServers?: string[], sdkMcpServerConfigs?: {[name]:{timeout?}}, sdkMcpServerManifests?: {...},
  jsonSchema?: object, systemPrompt?: string[], appendSystemPrompt?: string, systemPromptSnapshot?: boolean,
  planModeInstructions?: string, toolAliases?: {[k]:string}, excludeDynamicSections?: boolean,
  agents?: {[name]: AgentDefinition /* description, prompt, tools?, disallowedTools?, model?, skills?, maxTurns?, ... */},
  title?: string, skills?: string[], promptSuggestions?: boolean, agentProgressSummaries?: boolean,
  forwardSubagentText?: boolean, supportedDialogKinds?: string[], perTaskStopAffordance?: boolean,
  plugins?: [{type:'local', path, skipMcpDiscovery?}] /* only with --await-initialize */ }
```

The CLI type-checks: `hooks` entries need `hookCallbackIds` string arrays; `skills` and `sdkMcpServers` must be string arrays. Re-sending `initialize` returns `pending_permission_requests` and `pending_user_dialog_requests`, letting a client recover prompts it missed.

`HookEvent` values: `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PostToolBatch`, `Notification`, `UserPromptSubmit`, `UserPromptExpansion`, `SessionStart`, `SessionEnd`, `Stop`, `StopFailure`, `SubagentStart`, `SubagentStop`, `PreCompact`, `PostCompact`, `PreModelSwitch`, `PostModelSwitch`, `PermissionRequest`, `PermissionDenied`, `Setup`, `TeammateIdle`, `TaskCreated`, `TaskCompleted`, `Elicitation`, `ElicitationResult`, `ConfigChange`, `WorktreeCreate`, `WorktreeRemove`, `InstructionsLoaded`, `CwdChanged`, `FileChanged`, `DirectoryAdded`, `MessageDisplay`.

---

## 6. Control requests the CLI sends to the client

Each must get exactly one `control_response`, unless the CLI later sends `control_cancel_request` for that `request_id` (then abandon it, don't reply). Handle asynchronously so the stdout reader never blocks.

### 6.1 `can_use_tool` (only with `--permission-prompt-tool stdio`)

```ts
{ subtype:'can_use_tool', tool_name, input, tool_use_id,
  permission_suggestions?: PermissionUpdate[], blocked_path?, decision_reason?, decision_reason_type?:
  'rule'|'mode'|'subcommandResults'|'permissionPromptTool'|'hook'|'asyncAgent'|'sandboxOverride'|'workingDir'|'safetyCheck'|'classifier'|'other',
  classifier_approvable?, suppress_always_allow_rule?, default_to_no?, matched_ask_rule?:{source,tool_name,rule_content?},
  title?  /* "Claude wants to read foo.txt" */, display_name? /* "Read file" */, description?,
  agent_id?, mcp_server?:{name,source}, requires_user_interaction? }
```

Response (CLI schema `yae`; error text "Expected {behavior: 'allow', updatedInput?: object} or {behavior: 'deny', message: string}"):

```json
{"behavior":"allow","updatedInput":{...optional...},"updatedPermissions":[PermissionUpdate...],
 "toolUseID":"<tool_use_id>","decisionClassification":"user_temporary|user_permanent|user_reject"}
{"behavior":"deny","message":"why","interrupt":true,"toolUseID":"...","decisionClassification":"user_reject"}
```

- `updatedInput` optional since 2.1.207 (Python SDK always sends the original input anyway).
- `interrupt: true` on deny also stops the turn.
- "Always allow": send one of the `permission_suggestions` back in `updatedPermissions`.

`PermissionUpdate` is one of:
- `{type:"addRules"|"replaceRules"|"removeRules", rules:[{toolName, ruleContent?}], behavior:"allow"|"deny"|"ask", destination}`
- `{type:"setMode", mode, destination}`
- `{type:"addDirectories"|"removeDirectories", directories:[...], destination}`

`destination`: `userSettings`, `projectSettings`, `localSettings`, `session`, `cliArg`.

**AskUserQuestion** also arrives via `can_use_tool` (`tool_name: "AskUserQuestion"`, `input.questions[]` with `question`, `header`, `options[{label, description}]`, `multiSelect`). Answer allow with `updatedInput: {questions, answers: {"<question text>": "<label>" | "a, b"}}`. **ExitPlanMode** prompts use the same path (input has `plan`, `planFilePath`).

While a prompt is pending, session state is `requires_action`. There is no deadline.

### 6.2 `hook_callback`

Request: `{subtype:"hook_callback", callback_id, input: HookInput, tool_use_id?}`. `HookInput` contains `session_id`, `transcript_path`, `cwd`, `hook_event_name`, plus event-specific fields.

Response is hook JSON output: `{continue?, suppressOutput?, stopReason?, decision?: "approve"|"block", systemMessage?, reason?, hookSpecificOutput?: {hookEventName, ...}}` or `{async: true, asyncTimeout?}`. E.g. PreToolUse uses `permissionDecision: "allow"|"deny"|"ask"|"defer"`.

### 6.3 Other CLI-originated requests

- **`mcp_message`** `{server_name, message: JSONRPC}` → reply `{mcp_response: <JSON-RPC response>}`; for notifications reply `{"jsonrpc":"2.0","result":{}}`. Only for in-process ("sdk") MCP servers.
- **`elicitation`** `{mcp_server_name, message, mode?, url?, elicitation_id?, requested_schema?, title?, ...}` → reply `{action: "accept"|"decline"|"cancel", content?}`. TS SDK defaults to decline.
- **`request_user_dialog`**: only sent for kinds listed in `supportedDialogKinds`. (A declared kind that is mishandled stays pending; answering `cancelled` counts as the user dismissing it.)
- **`oauth_token_refresh`**, **`host_auth_token_refresh`**: only with env flags set; ignorable.

---

## 7. Messages the CLI writes to stdout

Every message carries `uuid` and `session_id` unless noted.

### 7.1 Core conversation messages

**`system` / `init`** (`wze()` in the binary; sent at the start of each turn, newest wins):
- `cwd`, `session_id`, `tools[]`, `mcp_servers[{name, status, source?}]`, `model`, `permissionMode`
- `slash_commands[]` (names), `terminal_slash_commands?[]`, `apiKeySource`, `betas`, `claude_code_version`
- `output_style`, `agents[]`, `skills[]`, `plugins[{name, path, source, version?}]`, `plugin_errors?`, `mcp_server_errors?`
- `capabilities?[]`: `interrupt_receipt_v1`, `msg_lifecycle_v1`, `interrupt_cancel_queued_v1`, `interrupt_send_now_v1`, `task_list_restore_v1`, `task_list_restored_v1`, `mcp_read_resource_v1`, `mcp_tool_ui_meta_v1`, `ui_surface_v1`, `queued_notifications`
- `analytics_disabled`, `product_feedback_disabled`, `memory_paths?`, `scratchpad_path?`, `fast_mode_state`, `fast_mode_disabled_reason`, `effort?`, `view_mode?`

**`assistant`:** `{message: BetaMessage, parent_tool_use_id, error?, uuid, session_id, request_id?, user_message_uuid(s)?, aborted?: true, supersedes?: uuid[], subagent_type?, timestamp?, context_usage?, usage_report?}`
- **One message per finished content block.** Consecutive messages share `message.id`; on these `stop_reason` is null and `usage` isn't final.
- `error` ∈ `authentication_failed`, `oauth_org_not_allowed`, `account_on_hold`, `verification_required`, `billing_error`, `rate_limit`, `overloaded`, `invalid_request`, `model_not_found`, `server_error`, `unknown`, `max_output_tokens`, `cloud_credential_error`.
- If `supersedes` is present, remove those earlier messages.

**`user`** (emitted by the CLI): `{message: {role: "user", content: [{type: "tool_result", tool_use_id, content, is_error?}]}, parent_tool_use_id, tool_use_result?, isSynthetic?, uuid, timestamp?, ...}`. `tool_use_result` holds the tool's structured output (shapes in `sdk-tools.d.ts`: `BashOutput`, `FileEditOutput`, …; Edit/Write carry `structuredPatch`). Replays (`SDKUserMessageReplay`) add `isReplay: true` and `file_attachments?`.

**`stream_event`:** `{event: BetaRawMessageStreamEvent (message_start | content_block_start | content_block_delta(text_delta / thinking_delta / input_json_delta / signature_delta) | content_block_stop | message_delta | message_stop), parent_tool_use_id, uuid, session_id, ttft_ms?, user_message_uuid?}`. The complete `assistant` message still follows.

**`result`:** exactly one per turn.
- Shared fields:
  - timing/turns: `duration_ms`, `duration_api_ms`, `num_turns`, `stop_reason`
  - status: `is_error`, `terminal_reason?`, `result_index?`, `queued_turn_count?`
  - cost/usage: `total_cost_usd` (running total), `usage` (main loop only: `input_tokens`, `output_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`, `server_tool_use{web_search_requests, web_fetch_requests}`, `service_tier`, `cache_creation{ephemeral_1h_input_tokens, ephemeral_5m_input_tokens}`, `output_tokens_details`, …), `modelUsage: {[model]: {inputTokens, outputTokens, cacheReadInputTokens, cacheCreationInputTokens, webSearchRequests, costUSD, contextWindow, maxOutputTokens, ...}}`
  - other: `permission_denials[{tool_name, tool_use_id, tool_input}]`, `user_message_uuid(s)?`, `fast_mode_state?`, `uuid`, `session_id`
- `subtype: "success"`: `result` (final text), `api_error_status?`, `structured_output?`, `deferred_tool_use?`, `ttft_ms?`, `local_command?`. **An API failure arrives as `success` with `is_error: true`** and the error text in `result`.
- `subtype` ∈ `error_during_execution`, `error_max_turns`, `error_max_budget_usd`, `error_max_structured_output_retries`: `errors: string[]`, `startup_failure_reason?`.
- **Interrupted turns** (from code reading): `error_during_execution` with `terminal_reason` `aborted_streaming` or `aborted_tools`.
- `terminal_reason` values: `blocking_limit`, `rapid_refill_breaker`, `prompt_too_long`, `image_error`, `model_error`, `api_error`, `malformed_tool_use_exhausted`, `aborted_streaming`, `aborted_tools`, `stop_hook_prevented`, `hook_stopped`, `tool_deferred`, `max_turns`, `background_requested`, `completed`, `budget_exhausted`, `structured_output_retry_exhausted`, `tool_deferred_unavailable`, `turn_setup_failed`.

### 7.2 Other `system` subtypes in the public union

| subtype | fields |
|---|---|
| `compact_boundary` | `compact_metadata{trigger: manual\|auto, pre_tokens, post_tokens?, duration_ms?, preserved_segment?, preserved_messages?}` |
| `status` | `status: "compacting"\|"requesting"\|null`, `permissionMode?`, `compact_result?`, `compact_error?` |
| `api_retry` | `attempt`, `max_retries`, `retry_delay_ms`, `error_status`, `error`, `no_response?` |
| `informational` | `content`, `level: info\|notice\|suggestion\|warning`, `tool_use_id?`, `prevent_continuation?`, `tag?` |
| `local_command_output` | `content` (in the schema; see section 9 for actual delivery) |
| `hook_started` / `hook_progress` / `hook_response` | `hook_id`, `hook_name`, `hook_event`, plus `stdout`, `stderr`, `output`, `exit_code?`, `outcome` |
| `task_started` | `task_id`, `tool_use_id?`, `description`, `subagent_type?`, `task_type?`, `is_backgrounded?`, `spawn_depth?`, `workflow_name?`, `prompt?`, `skip_transcript?`, `ambient?` |
| `task_progress` | `task_id`, `description`, `usage{total_tokens, tool_uses, duration_ms}`, `last_tool_name?`, `summary?` |
| `task_updated` | `task_id`, `patch{status?, description?, end_time?, error?, is_backgrounded?, ...}` |
| `task_notification` | `task_id`, `status: completed\|failed\|stopped`, `output_file`, `summary`, `usage?`, `reason?` |
| `background_tasks_changed` | `tasks[{task_id, task_type, description, ambient?}]` (replace semantics) |
| `session_state_changed` | `state: idle\|running\|requires_action` (needs the env var from 2.3) |
| `commands_changed` | `commands: SlashCommand[]` (replace cached list) |
| `notification` | `key`, `text`, `priority`, `color?`, `timeout_ms?` |
| `thinking_tokens` | `estimated_tokens`, `estimated_tokens_delta` |
| `permission_denied` | `tool_name`, `tool_use_id`, `agent_id?`, `decision_reason_type?`, `decision_reason?`, `message` |
| `model_refusal_fallback` / `model_refusal_no_fallback` | `original_model`, `fallback_model?`, `retracted_message_uuids?`, `content`, … |
| `memory_recall` | `mode`, `memories[]` |
| `files_persisted` | file upload results |
| `elicitation_complete` | elicitation id |
| `plugin_install` | `status`, `name?`, `error?` |
| `control_request_progress` | progress for a long-running request |
| `worker_shutting_down`, `mirror_error` | — |

Internal subtypes seen in the binary but not in public types (treat as optional): `session_title_changed{title}`, `turn_duration{duration_ms, ...}`, `api_error`, `model_fallback`, `post_turn_summary`, `session_metadata`, `away_summary`, `stop_hook_summary`, and others.

### 7.3 Other top-level types

- `tool_progress`: `{tool_use_id, tool_name, parent_tool_use_id, elapsed_time_seconds, task_id?, heartbeat?, subagent_retry?}`
- `tool_use_summary`: `{summary, preceding_tool_use_ids}`
- `auth_status`: `{isAuthenticating, output[], error?}`
- `rate_limit_event`: `{rate_limit_info: {status, resetsAt?, rateLimitType?, utilization?, overage...}}`
- `prompt_suggestion`: `{suggestion}`
- `conversation_reset`: `{new_conversation_id, trigger?: clear|plan_mode_exit|fresh_session|onboarding, user_message_uuid?}` — start a fresh transcript view; the session id changes.
- `active_goal`, `keep_alive` (ignore), `control_request`, `control_response`, `control_cancel_request`
- `transcript_mirror`: only with `--session-mirror`
- `command_lifecycle`: `{command_uuid, state: queued|started|completed|cancelled|discarded|refused}` (internal; tied to `msg_lifecycle_v1`)

---

## 8. Initialize response (`Lp()` in the binary)

```ts
{ commands: SlashCommand[] /* {name, description, argumentHint, aliases?, builtin?} */,
  agents: [{name, description, model?}], output_style, available_output_styles: string[], user_output_styles_dir,
  models: ModelInfo[] /* {value, resolvedModel?, displayName, description, supportsEffort?, supportedEffortLevels?,
                        supportsAdaptiveThinking?, supportsFastMode?, supportsAutoMode?} */, unavailable_models?,
  account: {email?, organization?, subscriptionType?, tokenSource?, apiKeySource?,
            apiProvider?: 'firstParty'|'bedrock'|'vertex'|'foundry'|'anthropicAws'|'anthropicGoogleCloud'|'mantle'|'gateway'},
  pid, current_permission_mode, hooks_applied?, plugins_applied?, sdk_mcp_manifests_parked?, workspace_trust_recorded?,
  fast_mode_state?, fast_mode_disabled_reason?, session_state, capabilities?, feedback_*, analytics_disabled, proactivity,
  remote_control_* … }
// envelope also carries pending_permission_requests[] and pending_user_dialog_requests[] (since 2.1.268)
```

`commands` is the same filtered list as `init.slash_commands` (headless-usable only; section 9). Later changes arrive as `system/commands_changed`. The TS SDK's `supportedCommands()`, `supportedModels()` and `accountInfo()` read from this response.

Note: `apiProvider` includes `'mantle'` — "Mantle" is an internal Anthropic provider name (also `CLAUDE_CODE_USE_MANTLE`).

---

## 9. Slash commands in stream-json mode

- **Dispatch:** send `/<name> args` as an ordinary user message (without `client_composed`).
- **Available headlessly** (`_de()` → `aDe(e) = e.type==="prompt" && !e.disableNonInteractive || e.type==="local" && e.supportsNonInteractive`):
  - All **prompt-type** commands: custom `.claude/commands/*.md`, skills, plugin and MCP prompt commands, bundled skills (`code-review`, `verify`, …). The only built-in prompt command that opts out is `statusline`.
  - **Local** commands with a headless version: `add-dir`, `advisor`, `agents`, `auto-mode-setup`, `autocompact`, `clear` (aliases `reset`, `new`), `color`, `compact`, `config`, `context`, `design-consent`, `design-revoke`, `effort`, `exit`, `extra-usage`, `fast`, `focus`, `goal`, `import`, `list-agents`, `mcp`, `model`, `output-style`, `recap`, `reload-plugins`, `reload-skills`, `rename`, `skill-doctor`, `stop`, `usage`, `usage-credits`, `ultrareview`, `heapdump` (hidden), plus internal ones.
- **Rejected:** Ink/`local-jsx` UI commands (`/theme`, `/resume`, `/login`, `/help`, `/terminal-setup`, …) produce `/<x> isn't available in this environment.` as the result, `num_turns: 0`, no model turn, `commandOutcome.kind = "unavailable_headless"`.
- **Unknown names:** since 2.1.274 a `/name` matching nothing is sent to the model as ordinary text with a note that no command ran (`cmd_unknown_model_fallback`). Older versions returned `Unknown command: /x`.
- **Local command output** (`GPt()`): `system/init`, then a **synthetic `assistant` message** (text extracted from `<local-command-stdout>`, `stop_reason: "end_turn"`, plus `local_command_source`, `local_command_run{command, args}`, `local_command_outcome?`, `context_usage?` for `/context`, `usage_report?` for `/usage`), then a `result` with `subtype: "success"`, `num_turns: 0`, `result: <text>`, `local_command: <name>`. The `/cmd` user message may be replayed with `isReplay: true`.
- **`/compact`:** `status: compacting`, then `compact_boundary` (only if compaction ran), then `result`. "Not enough messages to compact." comes back as a success result.
- **`/clear`:** produces `conversation_reset` with a new session id; the old transcript stays on disk.
- Treat `initialize.commands` as the runtime truth (build gates `isEnabled` can toggle local commands).

---

## 10. Session persistence, resume and fork

- **Storage:** `${CLAUDE_CONFIG_DIR:-~/.claude}/projects/<slug>/<sessionId>.jsonl`.
  - `slug` = cwd with every non-alphanumeric character replaced by `-` (`/Users/alice` → `-Users-alice`). If longer than 200 chars: cut to 200 and append `-<base36 hash>`.
  - Each session also has a `<sessionId>/` folder with `subagents/` and `tool-results/`.
- **JSONL contents:** messages linked through `parentUuid`, plus metadata entries (`file-history-snapshot`, `permission-mode`, `mode`, `ai-title`, `last-prompt`, `cost-state`, attachments). See `inventory-binary-2.1.288.md` §7 for record details.
- **Resume options:**
  - `--resume=<id>` keeps the same session id; `--continue` picks the most recent session for this cwd.
  - `--fork-session` (with `--resume`/`--continue`) gives a new id and leaves the original untouched; add `--session-id=<uuid>` to choose it.
  - `--resume-session-at=<uuid>` truncates history at that entry; `--resume-drops-turn=<uuid>` guards that truncation and refuses with `error_during_execution` "Resume rejected by --resume-drops-turn:".
  - `--no-session-persistence` writes nothing.
- **History is NOT replayed on stdout when resuming.** The SDK's `getSessionMessages()`/`listSessions()` read the JSONL files directly; mantle must do the same to show earlier turns.
- **Switching sessions:** `/resume` isn't available headlessly; restart the process with different flags.
- **Cost after resume:** `total_cost_usd` and `modelUsage` continue from the totals saved in the transcript.
- `-p`/SDK sessions are excluded from the interactive picker and from interactive `claude --continue` (but `claude -p --continue` includes them).

---

## 11. Go implementation checklist

1. One reader goroutine; route by `type`:
   - `control_response` → complete the pending request in a `map[request_id]chan`;
   - `control_request` → handle in its own goroutine and reply;
   - `control_cancel_request` → cancel that handler;
   - `keep_alive` → drop;
   - everything else → UI events.
2. Every outgoing `control_request` goes through a helper with a timeout (60 s for initialize). Error replies are `{subtype: "error", error}` and may include `error_code`.
3. Track turns with `result` (one per turn) and `session_state_changed` (`idle` = truly finished, including background agents).
4. Render streaming text from `stream_event` deltas, then swap in finished `assistant` blocks grouped by `message.id`. Nest subagent output by `parent_tool_use_id`.
5. Treat the `type`/`subtype` lists here as known values; tolerate anything unknown (keep raw JSON).

---

## 12. Open questions (to verify in plan 02 spikes)

- **`initialize` may not be required first.** No rule found; SDKs always send it before the first prompt, and hooks/agents/SDK MCP servers only take effect if it is.
- **`priority` behaviour** (`now`/`next`/`later`) and its default come from minified code (`Xnt`); the default depends on a feature flag.
- **Interrupted-turn result subtype** (`error_during_execution` + `aborted_*`) is from reading `owo`/`nwo`; not observed live.
- **`command_lifecycle`, `session_title_changed`, `turn_duration`, `api_error`, `model_fallback`** are `@internal`; unknown whether they are emitted on plain stdio.
- **Undocumented control subtypes** (`rewind_conversation`, `fork_conversation`, `export_conversation`, `get_status`, `side_question`, `set_cwd`, `claude_authenticate`, …) exist in 2.1.288 but shapes are unpublished and may change.
- **No history replay on resume** is inferred from finding no replay path in print mode plus the SDK reading JSONL; confirm with one live run.
- **`CLAUDE_CODE_ENTRYPOINT` choice:** unset gives `sdk-cli` (hidden from the interactive picker); `cli` is rewritten to `sdk-cli` in non-interactive mode. Other behaviours depending on an SDK entrypoint (`F$()`) are not fully mapped.
- **Headless local commands** can be toggled by build gates (`isEnabled`); `initialize.commands` is the runtime truth.
