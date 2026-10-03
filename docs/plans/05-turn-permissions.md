# Plan 05: Turn control, permissions, dialogs, gates

| Session | Primary paths | Part B needs | Produces |
|---|---|---|---|
| mantle-05 | `features/turn` (plus a pure package `features/turn/gates`, `features/turn/dialogs`) | tags `contracts-v1`, `proto-v1` | none |

## Goal

Everything that decides *whether* and *how* Claude proceeds:
- permission prompts for every tool type;
- AskUserQuestion, plan approval and MCP elicitation;
- permission-mode cycling;
- interrupt, queueing and send-now;
- the double-press exit rules;
- the **startup gates** (workspace trust, `.mcp.json` approval, bypass warning). These must
  run **before** any engine is spawned, because headless `claude -p` skips them.

## Start prompt

> You are session 05 of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/05-turn-permissions.md`, then execute that plan. Do Part A now. Start Part B
> once `git tag -l` shows the tags it needs. Commit with prefix `[05]`.

## Read first

- `docs/research/protocol-2.1.288.md` §4.1 (priority), §5 (`interrupt`,
  `set_permission_mode`, `stop_task`, `background_tasks`, `cancel_async_message`), §6 (all
  CLI→client requests).
- `docs/research/inventory-docs.md` §2 (turn control), §4 (permissions, modes, dialogs),
  and the NOT-IN-HEADLESS list.
- `docs/research/inventory-binary-2.1.288.md` §3 (Confirmation and Select bindings), §6
  (modes and cycle), §4 (permission settings keys).

## Facts you can rely on

- **`can_use_tool` request** (only sent because mantle spawns with
  `--permission-prompt-tool stdio`):
  - Identity: `tool_name`, `input`, `tool_use_id`, `agent_id` (background subagent asking),
    `mcp_server{name, source}`.
  - Suggestions and decision context: `permission_suggestions[PermissionUpdate]`,
    `blocked_path`, `decision_reason`, `decision_reason_type` (rule | mode |
    subcommandResults | hook | asyncAgent | sandboxOverride | workingDir | safetyCheck |
    classifier | other), `classifier_approvable`, `suppress_always_allow_rule`,
    `default_to_no`, `matched_ask_rule`.
  - Display text: `title` ("Claude wants to read foo.txt"), `display_name` ("Read file"),
    `description`, `requires_user_interaction`.
- **Response:**
  - `{"behavior":"allow","updatedInput":{…optional…},"updatedPermissions":[…],"toolUseID":"…","decisionClassification":"user_temporary|user_permanent"}`
  - `{"behavior":"deny","message":"…","interrupt":true?,"toolUseID":"…","decisionClassification":"user_reject"}`
  - "Always allow" sends one of `permission_suggestions` back in `updatedPermissions` (the
    engine persists it). `interrupt:true` on deny stops the turn.
  - There is no deadline; the session state is `requires_action` while pending.
- **PermissionUpdate:** `addRules|replaceRules|removeRules {rules[{toolName, ruleContent?}],
  behavior allow|deny|ask, destination}`, `setMode {mode, destination}`, or
  `addDirectories|removeDirectories {directories, destination}`. `destination` ∈
  userSettings | projectSettings | localSettings | session | cliArg.
- **AskUserQuestion** arrives as `can_use_tool` with `tool_name:"AskUserQuestion"` and
  `input.questions[]`, each `{question, header, options[{label, description, preview?}],
  multiSelect}` (1–4 questions, 2–4 options, plus an implicit "Other" free text).
  - Answer with allow and `updatedInput: {questions, answers: {"<question text>": "<label>"
    | "a, b" | "<free text>"}}`.
  - Not available inside subagents.
- **ExitPlanMode** arrives via `can_use_tool` (input has `plan` and possibly
  `planFilePath`). The approval options map to allow plus `updatedPermissions` `setMode`
  (acceptEdits / default / auto / bypass if available), or deny with feedback (keep
  planning). Ctrl+g edits the plan in `$EDITOR`. `showClearContextOnPlanAccept` adds a
  "clear context" option (spike: no documented wire; omit until verified).
- **`elicitation`** request: `{mcp_server_name, message, mode?, url?, elicitation_id?,
  requested_schema?, title?}`. Reply `{action: accept|decline|cancel, content?}`.
- **`request_user_dialog`** is sent only for kinds listed in
  `initialize.supportedDialogKinds`. Declare only kinds you implement. A declared kind left
  unanswered stays pending; answering `cancelled` means the user dismissed it.
- **`control_cancel_request {request_id}`** from the CLI: close that dialog, don't reply.
- **Permission modes.**
  - Values: `default` (shown as "Manual", ⏸), `acceptEdits` (⏵⏵ accept edits on), `plan`
    (⏸ plan mode on), `auto` (⏵⏵), `bypassPermissions` (⏵⏵), `dontAsk`. `manual` is an
    alias for `default`.
  - Switch with `set_permission_mode {mode}`, reply `{mode}`.
  - **Shift+tab cycle:** default → acceptEdits → plan → (bypassPermissions if enabled, else
    auto if available, else default). From bypassPermissions: auto if available, else
    default. dontAsk → default.
  - Bypass is available only if the engine was started with
    `--allow-dangerously-skip-permissions` or `--dangerously-skip-permissions`.
  - Auto mode is available per `ModelInfo.supportsAutoMode` and settings (`disableAutoMode`).
  - Headless starts in `default`. Interactive Claude Code starts in `auto` when available:
    mantle should mirror interactive behaviour by sending `set_permission_mode` after
    initialize, honouring `permissions.defaultMode` and `--permission-mode`.
- **Turn control.**
  - `interrupt {cancel_queued?}` replies `{still_queued[], cancelled[]}`; the interrupted
    turn's `result` follows.
  - `priority`: `now` ends the current turn and delivers immediately; `next` folds in
    between tool rounds; `later` queues as its own turn.
  - `cancel_async_message {message_uuid}` takes back a queued message (spike S2 confirms).
  - `stop_task {task_id}` stops a background task; `background_tasks {tool_use_id?}` is the
    ctrl+b equivalent.
- **Claude Code UX to match.**
  - Esc interrupts (work so far is kept).
  - Ctrl+c interrupts a running turn; otherwise it clears input; otherwise "Press Ctrl-C
    again to exit". Ctrl+d double-press within 800 ms exits.
  - Queued messages show gray above the prompt; "Press up to edit queued messages".
  - Ctrl+x ctrl+k stops all background agents.
  - **Permission dialog:** "Yes" / "Yes, and don't ask again for <rule>" / "No, and tell
    Claude what to do differently (esc)". Tab amends the input; shift+tab changes mode from
    inside the dialog. Background subagent prompts name the subagent.
- **Gates.**
  - `-p` never shows the trust dialog and treats the folder as trusted. Project hooks, env
    and `.mcp.json` servers run immediately.
  - Trust state lives in `~/.claude.json` `projects[<path>].hasTrustDialogAccepted`. **Read
    only, never write it;** mantle stores its own acceptances in
    `~/.mantle/state/trust.json`.
  - `.mcp.json` approval: settings `enableAllProjectMcpServers`, `enabledMcpjsonServers`,
    `disabledMcpjsonServers`. Pass the user's denials via
    `--settings '{"disabledMcpjsonServers":[…]}'` (spike S9 confirms it's honoured).
  - Bypass warning: `skipDangerousModePermissionPrompt` suppresses it.
  - Spike S10 (plan 02) settles whether trusted parent directories count.

## Part A: start immediately (pure packages, no `pkg/ext` until `contracts-v1`)

- [ ] **A1 Gates (`features/turn/gates`).**
  - `TrustState(cwd)`: read `~/.claude.json` (read-only, tolerant), mantle's trust store,
    parent-directory rule (per S10; default: exact path or a trusted ancestor if Claude
    Code does that).
  - `NeedsTrust`; `RecordTrust(cwd)` writes mantle's store atomically.
  - `McpApproval(cwd)`: parse `.mcp.json`, merge with the settings keys, return pending
    servers and the `disabledMcpjsonServers` list to pass to the engine.
  - `BypassWarningNeeded(flags, settings)`.
  - What the trust dialog lists: project allow rules, hooks, env, helpers, additional
    directories, MCP servers. Collect these for the dialog view-model.
- [ ] **A2 Dialog view-models (`features/turn/dialogs`), pure state plus key handling:**
  - **Permission prompt.** Options computed from `permission_suggestions` with readable
    labels (rule text such as `Bash(npm test:*)`, directories, mode). Deny with a feedback
    text field. Amend mode (edit tool input JSON or command). A `default_to_no` highlight.
    Header variants per tool:
    - Bash: command, description, cwd, sandbox flag;
    - Edit/MultiEdit/Write: path plus a diff preview (uses plan 03's `pkg/ui/diffview`
      once available; fall back to a plain hunk list);
    - Read: path and range; WebFetch: domain and URL; MCP: server, tool, args;
    - Generic: `title` / `display_name` / `description`.
  - **AskUserQuestion.** Tabs per question; single or multi-select; "Other" free text;
    previews (markdown); answers map built exactly as the engine expects.
  - **Plan approval.** Rendered plan (markdown); options auto-accept / manual approve /
    bypass (if available) / keep planning with feedback; ctrl+g edit.
  - **Elicitation forms.** A JSON-schema subset (string, number, boolean, enum, required);
    URL mode (open the browser, confirm).
  - **Trust, `.mcp.json` approval and bypass-warning** view-models.
- [ ] **A3 Mode-cycle function** `Next(mode, availability) Mode` and indicator text, with
  table tests.
- [ ] **A4 Tests:** key-flow tests for each view-model (keys in, response JSON out),
  including Esc → deny, tab amend, "don't ask again" mapping, multi-question answers;
  gate tests with a temp HOME (trusted, untrusted, parent-trusted, malformed
  `~/.claude.json`).

## Part B: after `contracts-v1` and `proto-v1`

- [ ] **B1 [M1] Permission flow.** Subscribe to `ext.PermissionMsg`; open
  `dialog.permission` (PlaceInline, replacing the input like Claude Code); reply via
  `Reply`.
  - Label with the engine (main / "Builder <id>" / subagent name from `agent_id`).
  - Queue several pending requests in order.
  - Close on `control_cancel_request`.
  - On (re)initialize, recover `pending_permission_requests`.
- [ ] **B2 [M1] AskUserQuestion and plan approval dialogs** wired the same way (dialog IDs
  `dialog.askUserQuestion`, `dialog.planApproval`).
- [ ] **B3 [M1] Mode cycling.** `chat:cycleMode` (shift+tab) and `confirm:cycleMode` in
  dialogs → `set_permission_mode`; update the footer indicator (plan 07 renders it from
  `SessionInfo.PermissionMode`); the startup mode follows the interactive defaults.
- [ ] **B4 [M1] Interrupt and exit.** `chat:cancel` (esc) → `Interrupt(false)` while
  running; `app:interrupt` (ctrl+c) with the clear-input / double-press-exit ladder;
  ctrl+d double press (800 ms) exits; graceful engine shutdown (`end_session`).
- [ ] **B5 [M1] Queue.**
  - Show queued prompts (gray, `SlotAboveInput`) from plan 04's `QueuedPromptsMsg` and
    engine acks (`--replay-user-messages` / `command_lifecycle`, per S2).
  - Up arrow on an empty prompt pulls queued messages back into the editor
    (`cancel_async_message`).
  - `chat:sendNow` → priority `now`; `chat:queueSubmit` → `later`.
- [ ] **B6 [M1] Startup gates wired before spawn.**
  - The host calls gates before creating the main engine. The trust dialog (centered or
    full-screen); accept records trust; decline exits.
  - `.mcp.json` approvals produce the `disabledMcpjsonServers` passed in `SpawnOpts`.
  - The bypass warning shows when starting in bypass mode.
  - **A test asserts no engine process is started until the gates pass.**
- [ ] **B7 [M2] Elicitation dialog** and `request_user_dialog` for the implemented kinds
  only; `control_cancel_request` handling for all dialogs.
- [ ] **B8 [M2] Background control.** ctrl+b / ctrl+x ctrl+b (`task:background`) →
  `background_tasks`; ctrl+x ctrl+k (`chat:killAgents`) → `stop_task` for every running
  task (from `background_tasks_changed`), with a confirmation notice.
- [ ] **B9 [M2] Usage-limit handling.**
  - On `rate_limit_event` with a reset time, show the limit notice with options (wait and
    auto-continue at `resetsAt`, `autoContinueAtUsageLimit`; switch model; stop).
  - mantle implements the wait itself, because headless has no menu.
  - Show `permission_denied` events as notices.
- [ ] **B10 [M2] API-key approval prompt.** When `ANTHROPIC_API_KEY` is present and not yet
  approved (`~/.claude.json` customApiKeyResponses; read-only), show the one-time prompt.
  mantle remembers the choice in its own store and passes it via the environment
  accordingly.
- [ ] **B11 [M2] Invalid-settings notice.** `-p` silently ignores invalid settings files, so
  validate JSON syntax of each scope at startup (plan 01's config reader reports errors)
  and show a notice.

## Design notes

- **Dialogs never block the engine reader.** Replies are Cmds that write one
  `control_response`.
- **Inline placement replaces the prompt area** (Claude Code look). Several requests stack
  with a counter "(1 of 3)".
- **"Don't ask again" rules** come only from `permission_suggestions`. Never invent rules
  client-side; the engine persists `updatedPermissions`.
- **Esc in a permission dialog** = deny without interrupt; "No, and tell Claude…" = deny
  with message. A deny with `interrupt:true` is offered as an explicit option.
- **Gate ordering:** version check (plan 02 probe) → trust → bypass warning → `.mcp.json` →
  spawn. Gates also apply to `/mantle` builder engines (their cwd is mantle's own worktree;
  auto-trusted by mantle).
- **Security.** Never auto-approve on timeout. Strip control sequences from tool input
  shown in dialogs.

## Interfaces you provide / consume

- **Provide:** dialog IDs `dialog.permission`, `dialog.askUserQuestion`,
  `dialog.planApproval`, `dialog.elicitation`, `dialog.trust`, `dialog.mcpApproval`,
  `dialog.bypassWarning`; the gates API (consumed by plan 01's host startup and plan 10's
  builder); queue state messages.
- **Consume:** `ext.PermissionMsg`, `Engine.Control("set_permission_mode" | "interrupt" |
  "stop_task" | "background_tasks" | "cancel_async_message")` (plan 02), diffview (plan 03),
  editor widget for feedback (plan 04), notices and dialogs (plan 01).
- **Requests:** plan 01 needs a "pre-spawn gate" hook in host startup; file it if it isn't
  in the API.

## Tests and done criteria

- Fixture-driven keyboard flows for every dialog, using plan 02's fixtures:
  - allow, deny, always, amend, Esc;
  - AskUserQuestion single and multi, Other;
  - plan approve and keep-planning;
  - elicitation accept and decline;
  - cancel request closes the dialog.
- Mode-cycle tables match the binary's order.
- Interrupt mid-text and mid-tool produce the right UI state (S1 facts).
- Gate tests with a temp HOME, plus the "no spawn before gates" test.
- Goldens of each dialog at 60/100 columns.

## Parity coverage

- `PM-*`: permissions and dialogs (prompt variants, suggestions, amend, attribution, plan
  approval, AskUserQuestion, elicitation, `request_user_dialog`, API-key approval,
  invalid-settings notice).
- `TC-*`: turn control (esc, ctrl+c/ctrl+d, queue, take-back, send-now, queueSubmit,
  ctrl+b, ctrl+x ctrl+k, usage-limit wait).
- `GT-*`: startup gates (trust, `.mcp.json`, bypass warning).
- `MD-*`: permission-mode cycling and indicator text.

## Out of scope

- `/permissions` rules editor panel: 08. Footer rendering of the mode: 07.
- Queue input capture: 04. Rewind: 06. Engine plumbing: 02.

## Parallel rules (reminder)

- No repo-wide rewriters (use `make fmt-05`).
- `pkg/ext` and `pkg/proto` are additive-only; requests go in `docs/plans/requests/`.
- Commit only your own files with prefix `[05]`; `make test-05`; no imports of other
  `features/*` packages (consume diffview from `pkg/ui/diffview`, the editor from
  `pkg/ui/editor`).
