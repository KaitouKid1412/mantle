# Claude Code feature inventory: official docs

> Internal research notes for mantle planning (Claude Code 2.1.288, collected 2026-10-03). Not shipped.
> Derived from the installed binary, the official docs and the Agent SDK; do not copy strings/schemas from here into shipped code.

Read as raw markdown from code.claude.com (v2.1.28x era). Categorized inventory, followed by the interactive-only features mantle must rebuild, features documented as not supported or different in headless mode, items whose wire format is undocumented, and the URLs read.

**Legend**
- **[A] ENGINE**: the `claude` binary provides it when driven by `claude -p --input-format stream-json --output-format stream-json --verbose` (flags, stream messages, control requests / SDK `Query` methods, slash commands sent as prompt text).
- **[B] UI**: purely an interactive-TUI feature. mantle must reimplement it, possibly using engine data.
- **[C] UNCLEAR**: docs are silent, or the wire format is undocumented.
- **NOT-IN-HEADLESS**: documented as unavailable or different under `-p` / Agent SDK.

---

## 0. Headless transport baseline (what mantle talks to)

**Process flags (all [A])**
- **Core:** `-p`, `--input-format stream-json`, `--output-format stream-json`, `--verbose` (`--verbose` is required by `--prompt-suggestions` and appears in every stream-json example).
- **Optional stream flags:** `--include-partial-messages` (token deltas), `--replay-user-messages` (echoes user messages with a UUID; needed for checkpoint IDs), `--include-hook-events`, `--forward-subagent-text`, `--prompt-suggestions`.
- **Print-only flags:** `--max-turns`, `--max-budget-usd`, `--json-schema`, `--no-session-persistence`, `--init`, `--maintenance`, `--append-subagent-system-prompt[-file]`, `--permission-prompts host|none`, `--permission-prompt-tool`.
- **`--bare`:** docs say it "will become the default for `-p` in a future release". It skips hooks, skills, plugins, MCP, CLAUDE.md, auto memory, system reminders and background tasks. mantle must make sure it does not run bare or it loses parity. No documented opt-out flag yet.
- **Signals:** SIGTERM exits 143 and leaves the turn unfinished. SIGINT or the `interrupt` control request ends the turn cleanly.
- **Limits:** piped stdin capped at 10 MB.

**Input (stdin) user message fields [A]**
- `message` content can include text and base64 image blocks.
- `priority`: `now` / `next` / `later`.
- `origin: {kind:"human"}`: must be set, or human-only behaviours (e.g. the `ultracode` workflow keyword) are refused.
- `shouldQuery:false`: inject context without starting a turn.
- `client_composed`: disables `@path`, `@server:resource` and `/command` expansion.
- `pasted_content` and `inline_pastes`: mark pasted text.
- `uuid`: echoed back as `user_message_uuid` / `user_message_uuids`.

**Output message types [A]**
- `system/init`: model, tools, mcp_servers, permissionMode, slash_commands, terminal_slash_commands, output_style, skills, plugins, plugin_errors, mcp_server_errors, fast_mode_state, capabilities.
- `assistant`: carries `aborted`, an `error` category, and `context_usage` (on `/context` replies).
- `user`: carries `tool_use_result` (structured tool output, Agent output, resourceLinks, structuredContent) and `isReplay`.
- `stream_event` (deltas, ping keepalives).
- `result`: success/error subtypes, `total_cost_usd`, `modelUsage`, `permission_denials`, `terminal_reason`, `local_command`, `deferred_tool_use`, `fast_mode_state`, `origin`, `queued_turn_count`, `startup_failure_reason`.
- System subtypes: `status` (compacting, permissionMode), `compact_boundary`, `api_retry`, `informational` (warnings, hook systemMessage), `permission_denied`, `notification`, `plugin_install`, `worker_shutting_down`, `files_persisted`, `task_started` / `task_progress` / `task_updated` / `task_notification` / `background_tasks_changed`, `thinking_tokens`, `commands_changed`, `hook_started` / `hook_progress` / `hook_response`.
- Top-level types: `tool_progress` (30 s heartbeats, subagent retry), `tool_use_summary`, `auth_status`, `rate_limit_event`, `prompt_suggestion`, `conversation_reset` (on `/clear`).

**Control requests [A]**
- Envelope: `{type:"control_request", request_id, request}`.
- Documented wire names: `initialize` (returns commands, agents, models, account, output styles, fast mode, `pending_permission_requests`), `interrupt` (with `cancel_queued`), `can_use_tool` (permission prompt to host), `register_repo_root`.
- Capabilities exposed via SDK methods (wire names [C] in docs; see `protocol-2.1.288.md` for the names found in the SDK/binary): setPermissionMode, setModel, setMaxThinkingTokens, applyFlagSettings, updateSettings, reinitialize, supportedCommands / Models / Agents, mcpServerStatus, getContextUsage, readFile, reloadPlugins, reloadSkills, reloadOutputStyles, accountInfo, reconnectMcpServer, toggleMcpServer, setMcpServers, readMcpResource, rewindFiles, stopTask, hook callbacks, onElicitation.

---

## 1. Prompt input and editing

| Feature | What the user does | Tag |
|---|---|---|
| Multiline: `\`+Enter, Option+Enter, Shift+Enter, Ctrl+J, paste | Newlines in prompt | [B] |
| Readline editing: Ctrl+A/E/K/U/W/Y, Alt+Y (paste ring), Alt+B/F/D, Ctrl+_ (undo) | Word/line edits, kill ring; Ctrl+W deletes back to whitespace | [B] |
| Vim editor mode (`/config` > Editor mode, `editorMode`) | NORMAL/INSERT/VISUAL/V-LINE; motions, operators, text objects, `.` repeat, `vimInsertModeRemaps` (e.g. `jj` for Esc); `/` in NORMAL opens history search | [B] |
| Command history: Up/Down, per working directory | Recall prompts incl. earlier sessions; consecutive duplicates collapse | [B] (file `~/.claude/history.jsonl`; [C] whether `-p` writes it) |
| Ctrl+R reverse search | Inline (classic) or dialog (fullscreen) with session/project/all scope | [B] |
| Ctrl+S stash/restore prompt | Keeps text, cursor, pastes and mode | [B] |
| Ctrl+G / Ctrl+X Ctrl+E external editor | Edit prompt in `$EDITOR`; optional last-response context (`externalEditorContext`) | [B] |
| Ctrl+V / Cmd+V / Alt+V image paste | Inserts an `[Image #N]` chip; drag-and-drop images; Cmd-click opens image | [B] capture; [A] engine accepts image blocks |
| Large paste collapse | `[Pasted text #N +X lines]` above 800 chars or 3 lines; restore with Ctrl+Y; `~/.claude/paste-cache` | [B]; [A] marking via `pasted_content` / `inline_pastes` |
| Invisible-character stripping | Removes zero-width/bidi/tag chars on Enter, then asks for a second Enter | [B] ([C] for stream-json input) |
| `@` file mention + autocomplete | Path dropdown; `respectGitignore`, `fileSuggestion` command; also suggests live sessions | [B] dropdown; [A] engine expands `@path` (and adds nested CLAUDE.md) |
| `@server:proto://res` MCP resource mention | Resources listed with files in autocomplete | [B] dropdown; [A] expansion |
| `/` command menu + filtering, highlight rules, hidden commands, mid-prompt `/` completion, ghost text | Menu of built-ins, skills, plugin commands, MCP prompts | [B]; [A] list via `slash_commands`, `supportedCommands()`, `commands_changed` |
| `!` shell mode | Runs command directly, output added to context, Claude replies (`respondToBashCommands`); Tab history completion; live path completion; Ctrl+B to background | [B] (no documented stream-json equivalent; emulate by running the command and injecting output, e.g. `shouldQuery`) |
| `:` emoji shortcodes | `:heart:` replacement and popup (`emojiCompletionEnabled`) | [B] |
| `?` on empty prompt | Shortcut help panel | [B] |
| Spellcheck (`spellcheck`, aspell/hunspell/ispell) | Underlines misspellings | [B] |
| Prompt suggestions | Gray predicted next prompt; Tab/Right to accept | [A] via `--prompt-suggestions` (off by default in `-p`) + [B] rendering; initial git-history example [B]/[C] |
| `ultrathink` keyword rainbow rendering | Visual only | [B] |
| Voice dictation (Space hold/tap, `/voice`) | Live speech-to-text into prompt; claude.ai only, local mic | [B]/[C] (speech service not exposed to SDK) |

## 2. Turn control, queueing and process controls

| Feature | Tag |
|---|---|
| Esc interrupts the turn (keeps work so far); queued messages send next | [A] `interrupt` |
| Ctrl+C: interrupt, else clear input, twice to exit; Ctrl+D double-press exit (800 ms); Ctrl+Z suspend; Ctrl+L redraw | [B] |
| Queue messages while Claude works (gray until picked up; mid-turn pickup at tool boundaries; commands run after turn) | [A] stdin queue + `priority`; [B] display |
| Ctrl+Enter / Ctrl+X Ctrl+S "send now" (moves backgroundable work aside, else interrupts) | [A] `priority:"now"` + `origin:{kind:"human"}` |
| Ctrl+X Enter `chat:queueSubmit` (queue, never interrupt) | [A] `priority:"later"` |
| Up from first line takes back queued messages | [C] (only `interrupt` + `cancel_queued` documented) |
| Ctrl+B backgrounds the running Bash command or agent (tmux: twice) | [C] (no explicit control documented; `priority:"now"` backgrounds work as a side effect) |
| Ctrl+X Ctrl+K stops all background subagents (+ artifact auto-replies) | [A] `stopTask` per task + [B] |
| `/model`, `/effort`, `/fast` applied mid-turn | [A] |

## 3. Transcript rendering, views and chrome (all need TUI work)

| Feature | Tag |
|---|---|
| Message rendering: markdown, syntax highlighting (`syntaxHighlightingDisabled`), `maxProseWidth`, diffs for Edit/Write (word-level), dimmed rejected diffs, Bash output collapse, "Called slack 3 times" MCP grouping, `⧉ Selected N lines` IDE context line | [B] from `assistant`/`user`/`tool_use_result` [A] |
| Thinking display (collapsed; gray italic in verbose; `showThinkingSummaries`) | [B]; [A] thinking blocks; set `thinking.display:"summarized"` |
| Ctrl+O transcript viewer (timestamps, model per message, expands collapsed items; classic Ctrl+E show-all; fullscreen `/` search, n/N, j/k, g/G, `{`/`}`, `[` to scrollback, `v` to editor, `?`) | [B] |
| `verbose`, `viewMode` (default/verbose/focus), `/focus` view | [B] |
| Turn duration line ("Cooked for 1m 6s · done 6:05 PM"; `showTurnDuration`, `timeFormat`, `timeZone`) | [B] from `result.duration_ms` |
| Spinner with verbs/tips/shimmer (`spinnerVerbs`, `spinnerTipsEnabled`, `spinnerTipsOverride`, `prefersReducedMotion`), thinking token counter | [B]; [A] `thinking_tokens`, `tool_progress` |
| Task checklist (Ctrl+T, up to 5 items, persists expanded state) | [B] from TaskCreate/TaskUpdate/TodoWrite tool calls [A]; task tools only on older models unless `CLAUDE_CODE_ENABLE_TODO_TOOLS=1` |
| Subagent/fork panel below prompt (rows; Enter opens transcript and sends follow-ups; `x` stops; 30 s linger; footer hint "/tasks to see subagents") | [B]; data [A] task_* + `parent_tool_use_id`; follow-ups to a specific subagent from the host [C] |
| `/tasks` background work list | [B]; [A] task events, `stopTask`, `background_tasks_changed` |
| `/diff`: diff panel (fullscreen, 110+ cols, auto-open at 144 cols, per-turn T1/T2 views, `ask` button, Ctrl+X B base cycling) or diff dialog (classic) | [B] (built-in mod `cc-plugin-diff`); mantle uses git + its own tracking of Edit/Write calls |
| `/btw` side-question overlay (no tools, 20-exchange memory, Shift+Left/Right history, `c` copy, `f` fork, `x` clear) | [B] (emulate with a forked `--resume --fork-session --no-session-persistence --tools ""` call) |
| Session recap after 3+ min away | [B]. NOT-IN-HEADLESS: "automatic recap never appears in non-interactive mode". `/recap` as text [C] |
| PR/MR footer badge (colored underline; gh/glab auth hints), issue `owner/repo#123` links, `footerLinksRegexes`, `FORCE_HYPERLINK` | [B] |
| Startup header/notices (model source file, login-expiry, MCP needs-auth count, auto-mode notice, `companyAnnouncements`), release notes | [B]; some arrive as `informational` [A] |
| Fullscreen renderer (`/tui fullscreen`, `tui`): fixed input, virtual scroll, mouse click/hover/select/drag, double/triple-click, copy-on-select, Ctrl+Shift+C, Shift+arrows extend selection, PgUp/PgDn, Ctrl+Home/End, Jump-to-bottom with new count, sticky prompt header, `/scroll-speed`, wheel acceleration, `autoScrollEnabled` | [B] |
| Classic renderer, `/tui`, `CLAUDE_CODE_DISABLE_MOUSE` | [B] |
| Screen reader mode (`--ax-screen-reader`, `CLAUDE_AX_SCREEN_READER`, `axScreenReader`) | [B] |
| Themes (`/theme`: auto, light/dark, daltonized, ANSI, custom in `~/.claude/themes/*.json` with token overrides; live reload; Ctrl+T toggles syntax highlighting in picker) | [B] |
| Prompt bar color (`/color`), session name on prompt bar | [A] command works in `-p`; [B] rendering |
| Terminal title (generated or renamed; `terminalTitleFromRename`), terminal progress bar OSC 9;4 (`terminalProgressBarEnabled`) | [B]. AI-generated session titles are not produced for shell-started `-p` runs |
| Desktop notifications (Ghostty/Kitty/iTerm2 OSC; `preferredNotifChannel`, `terminal_bell`) | [B] |
| Session quality survey ("How is Claude doing this session?") | [B] |

## 4. Permissions, modes and safety dialogs

| Feature | Tag |
|---|---|
| Modes: `default` (labeled "Manual", alias `manual`), `acceptEdits`, `plan`, `auto`, `dontAsk`, `bypassPermissions` | [A] |
| Shift+Tab cycle (auto > default > acceptEdits > plan > [bypass] > auto); status labels `⏸ manual mode on`, `⏵⏵ accept edits on`, etc. | [B]; [A] `setPermissionMode`; bypass needs `--allow-dangerously-skip-permissions` |
| Starting mode rules | [A]. NOT-IN-HEADLESS (different): `-p`/SDK starts in `default` when feature flags are fetched; interactive starts in `auto`. Resume rules also differ |
| Permission prompt dialog: Yes / "Yes, and don't ask again for X" / "Yes, and switch to auto mode" / No; Esc declines; Tab adds a comment; Shift+Tab allows for the session; one-time-only prompts; `.claude` folder session grants; "Bash command (unsandboxed)" | [B]; [A] `can_use_tool` with `suggestions`, `blockedPath`, `decisionReason`, `defaultToNo`, `suppressAlwaysAllowRule`, `agentID`, `mcpServer`; reply allow (`updatedInput`, `updatedPermissions`) or deny (`message`, `interrupt`). "Comment after Yes" must be emulated with a follow-up message [C] |
| Background-subagent prompts surfaced in the main session, naming the subagent | [B] (uses `agentID`) |
| Plan approval dialog (Yes + auto mode / auto-accept edits / bypass; manual approve; keep planning; `showClearContextOnPlanAccept`; Ctrl+G edits plan; plan generates session title) | [B]; [A] ExitPlanMode via `can_use_tool` (input has `plan`, `planFilePath`) + `setMode` updates; clear-context variant [C] |
| AskUserQuestion dialog (1–4 questions, 2–4 options, multiSelect, "Other" free text, `askUserQuestionTimeout`) | [B]; [A] via `can_use_tool` returning `updatedInput.answers` / `response`; option previews (markdown/html) |
| MCP elicitation dialogs (form / URL mode) | [B]; [A] SDK `onElicitation` (wire [C]); Elicitation hooks |
| `/permissions` dialog (rules by scope, add/remove, working dirs, auto-mode denials, Auto mode rules tab) | [B]; [A] settings files + `applyFlagSettings({permissions})` |
| Rule syntax (`Tool`, `Tool(spec)`, `Tool(param:value)`, wildcards, `mcp__srv__*`, `Read`/`Edit` path anchors, `Cd(...)`, `Agent(...)`, `Skill(...)`, `WebFetch(domain:)`) | [A] |
| Protected paths, critical-path `rm` circuit breaker, symlink checks | [A] |
| Auto mode classifier, `autoMode` config, `/auto-mode-setup`, `claude auto-mode defaults|config|reset` | [A] |
| Workspace trust dialog (lists allow rules, dirs, hooks, helpers) | [B]. NOT-IN-HEADLESS: `-p` never shows it and treats the folder as trusted for hooks/env/`.mcp.json`; project allow rules are ignored with a warning |
| `.mcp.json` per-server approval dialog | [B]. NOT-IN-HEADLESS: `-p` connects without asking |
| External CLAUDE.md import approval dialog | [B]/[C] |
| Bypass-permissions first-run warning dialog (`skipDangerousModePermissionPrompt`) | [B]. NOT-IN-HEADLESS: no dialog; `--bg` refused until accepted interactively |
| `ANTHROPIC_API_KEY` one-time approve prompt | [B] |
| Sandbox (`/sandbox` toggle, auto-allow vs regular mode, network host prompt, unsandboxed retry prompt) | [A] behaviour via settings; [B] `/sandbox` UI; network-host prompt routing in headless [C] |
| Additional dirs (`--add-dir`, `/add-dir`, `additionalDirectories`), `/cd` | [A] `--add-dir`, `register_repo_root`; `/cd` [C] |
| `--permission-prompts none`, `dontAsk` for unattended runs | [A] |

## 5. Built-in slash commands

Notes: skills, workflows and other prompt-based commands are generally [A] when typed as prompt text in `-p`. "Terminal-only" built-ins such as `/login` are NOT-IN-HEADLESS. `system/init` exposes `terminal_slash_commands` (e.g. `exit`).

**Documented as working in `-p` [A]**
- `/model <m>` (session only, not saved).
- `/effort <lvl|ultracode on/off|status>`.
- `/fast` (only if `fastMode` was set in `--settings`).
- `/color`, `/rename`, `/config key=value`.
- `/mcp` (text summary; `reconnect|enable|disable`).
- `/output-style`, `/advisor <m|off>`, `/autocompact <n>`.
- `/reload-plugins` (no MCP changes applied), `/reload-skills`.
- `/compact [instr]` (`local_command`), `/clear` (`conversation_reset`).
- `/context` (returns `context_usage`), `/usage` / `/cost` / `/stats` (text).
- `/goal`, `/import` (lists only; confirm via the command it prints), `/init`, `/agents` (prints a reminder).

**Skills / workflows [A] (prompt-driven)**
- `/batch`, `/claude-api`, `/code-review` (`/review`; `ultra` goes to the cloud), `/simplify`, `/security-review`.
- `/fewer-permission-prompts`, `/update-config`, `/run`, `/verify`, `/run-skill-generator`.
- `/dataviz`, `/design`, `/design-sync`, `/slides`, `/artifact-capabilities`, `/artifact-diagramming`.
- `/workflow-authoring`, `/plugin-authoring`, `/claude-in-chrome`, `/debug`, `/doctor` (may ask questions [C]), `/loop` (`/proactive`; needs an idle open session [C]).
- `/deep-research` (workflow), `/schedule` (`/routines`, via RemoteTrigger).

**Interactive UI commands mantle must rebuild [B]**
- `/help`, `/config` panel (no-arg; tabs Config/Status/Usage/Stats), `/status`, `/theme`, `/tui`, `/focus`, `/scroll-speed`, `/keybindings`, `/terminal-setup`, `/statusline` (agent-assisted [C]).
- `/permissions` (`/allowed-tools`), `/hooks`, `/memory`, `/skills`, `/skill-doctor`, `/plugin` (no-arg menu), `/mcp` (no-arg panel + OAuth).
- `/model` picker (with `s` = session only; effort arrows), `/effort` slider (Tab toggles ultracode).
- `/rewind` (`/checkpoint`, `/undo`), `/resume` (`/continue`) picker, `/branch`, `/export` (dialog; with filename [C]), `/copy [N]` (picker; `w` writes to file).
- `/diff`, `/btw`, `/tasks` (`/bashes`), `/workflows`, `/list-agents` (`/peers`), `/recap` [C], `/release-notes`, `/powerup`, `/sandbox`, `/voice`, `/ide`, `/chrome`, `/vim` (removed).
- `/feedback`, `/bug` (`/share`; consent screen), `/rate-limit-options`, `/usage-credits`, `/upgrade`, `/privacy-settings`, `/passes`, `/stickers`, `/radio`, `/mobile` (`/ios`, `/android`; QR code), `/heapdump` (hidden).
- `/install-github-app`, `/install-slack-app`, `/web-setup`, `/remote-env`, `/setup-bedrock`, `/setup-vertex` (hidden), `/team-onboarding`, `/insights` [C], `/design-login`.

**Terminal-only session or flow commands (NOT-IN-HEADLESS, or [C]); use CLI equivalents**
- `/login`, `/logout`: use `claude auth login|logout|status`.
- `/exit` (`/quit`).
- `/background` (`/bg`), `/fork`, `/subtask`, `/stop`: use `claude --bg`, `claude attach|stop|logs|rm|respawn`, `claude agents --json`.
- `/teleport` (`/tp`), `/desktop` (`/app`), `/remote-control` (`/rc`), `/autofix-pr`, `/ultrareview` (`claude ultrareview` CLI is [A]), `/add-dir` ([C] via `register_repo_root`), `/cd` [C].

**Removed:** `/pr-comments`, `/ultraplan`, `/vim`.

## 6. Custom commands, skills and MCP prompts

- **Skills [A]:** `SKILL.md` in `~/.claude/skills`, `.claude/skills` (nested and parent discovery, live reload), `.claude/commands/*.md`, plugin skills (`plugin:name`), synced claude.ai skills (`anthropic-skills:`).
  - Frontmatter: name, description, when_to_use, argument-hint, arguments, disable-model-invocation, user-invocable, allowed-tools, disallowed-tools, model, effort, context:fork, agent, background, hooks, paths, shell.
  - Substitutions: `$ARGUMENTS`, `$N`, `$name`, `${CLAUDE_SESSION_ID}`, `${CLAUDE_EFFORT}`, `${CLAUDE_SKILL_DIR}`, `${CLAUDE_PROJECT_DIR}`, plugin vars. Dynamic `` !`cmd` `` injection.
  - Chaining: up to 6 skills (`/a /b args`).
  - In `-p`, include `/skill-name` in the prompt and the engine expands it.
- **Skill visibility overrides [A]:** `skillOverrides`, changeable via `applyFlagSettings`. The `/skills` list UI that cycles visibility is [B].
- **MCP prompts as commands [A]:** `/server:prompt (MCP)` or `/mcp__server__prompt args`; `commands_changed` fires when they appear.
- **Menu entry metadata [A]:** argument hints, aliases and `builtin` flag via `SlashCommand`; the menu UI itself is [B].

## 7. CLI subcommands (all [A]; mantle can shell out to them)

- **Auth:** `claude auth login|logout|status`, `claude setup-token`.
- **Agents and background sessions:** `claude agents [--json --all --cwd]`, `claude attach|logs|stop|kill|respawn|rm <id>`, `claude daemon status|stop`.
- **MCP:** `claude mcp add|list|get|remove|login|logout`.
- **Plugins:** `claude plugin install|uninstall|enable|disable|update|list|details|configure|prune|init|validate|eval|test|tag` and `claude plugin marketplace add|list|remove|update`.
- **Diagnostics and maintenance:** `claude doctor`, `claude purge`, `claude update`, `claude install`, `claude auto-mode defaults|config|reset`, `claude import`.
- **Cloud, remote, review:** `claude ultrareview`, `claude remote-control`, `claude self-hosted-runner`, `claude gateway`.

**Session/launch flags used with `-p` [A]**
- Session: `--model`, `--effort`, `--fallback-model`, `--permission-mode`, `--dangerously-skip-permissions`, `--allow-dangerously-skip-permissions`, `--resume`, `--continue`, `--fork-session`, `--session-id`, `--name`.
- Directories, tools, config sources: `--add-dir`, `--agent`, `--agents`, `--tools`, `--allowedTools`, `--disallowedTools`, `--mcp-config`, `--strict-mcp-config`, `--settings`, `--setting-sources`, `--plugin-dir`, `--plugin-url`.
- System prompt: `--system-prompt*`, `--append-system-prompt*`, `--exclude-dynamic-system-prompt-sections`, `--system-prompt-snapshot`.
- Other: `--autocompact`, `--advisor`, `--betas`, `--debug`, `--debug-file`, `--worktree` (skips the trust check under `-p`), `--chrome`, `--channels`, `--restricted`, `--safe-mode`, `--bare`, `--disable-slash-commands`, `--ide` [C in `-p`].
- **Undocumented in `--help` but documented elsewhere:** `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true claude -p --resume <id> --rewind-files <uuid>`.

**Not combinable with `-p`:** `--bg`, and `--cloud <task>`. `--cloud <session-id> -p` instead queues a message into that cloud session.

**Interactive-only flags:** `--remote-control`, `--tmux`, `--teammate-mode`, `--teleport`, `--from-pr` (opens picker), `--ax-screen-reader`, `--desktop`.

## 8. Settings

- **Scopes [A]:** precedence managed > `--settings` > `.claude/settings.local.json` (repo root) > `.claude/settings.json` > `~/.claude/settings.json`. Permission rules and hooks merge across scopes. Global config lives in `~/.claude.json`; `/config` writes it. Settings files are live-reloaded, firing the `ConfigChange` hook.
- **Engine-affecting keys [A]:** model and effort keys, permissions, sandbox, hooks, env, autoCompact, memory, MCP, plugins, worktree, attribution, auth/providers, telemetry.
- **UI-only keys mantle must read and honour [B]:**
  - Editing and input: `editorMode`, `vimInsertModeRemaps`, `emojiCompletionEnabled`, `spellcheck`, `fileSuggestion`, `respectGitignore`.
  - Display: `theme`, `tui`, `viewMode`, `verbose`, `syntaxHighlightingDisabled`, `maxProseWidth`, `prefersReducedMotion`, `autoScrollEnabled`, `wheelScrollAccelerationEnabled`, `axScreenReader`, `showTurnDuration`, `timeFormat`, `timeZone`.
  - Spinner and footer: `spinnerTipsEnabled`, `spinnerTipsOverride`, `spinnerVerbs`, `footerLinksRegexes`, `prStatusFooterEnabled`, `companyAnnouncements`.
  - Status lines: `statusLine`, `subagentStatusLine`.
  - Terminal: `terminalProgressBarEnabled`, `terminalTitleFromRename`, `preferredNotifChannel`.
  - Behaviour toggles: `promptSuggestionEnabled`, `respondToBashCommands`, `defaultShell`, `awaySummaryEnabled`, `autoContinueAtUsageLimit`, `askUserQuestionTimeout`, `dialogExpiry`, `showClearContextOnPlanAccept`, `showThinkingSummaries`.
  - Global config: `copyOnSelect`, `copyFullResponse`, `externalEditorContext`, `leftArrowOpensAgents`, `defaultToAgentsView`, `diffTool`, `autoConnectIde`, `claudeInChromeDefaultEnabled`.

## 9. Keybindings [B]

- **File:** `~/.claude/keybindings.json`, hot-reloaded. Bindings use `context` plus a `bindings` map; chords (3 s window); `null` unbinds.
- **Contexts:** Global, Chat, Autocomplete, Settings, Confirmation, Tabs, Help, Transcript, HistorySearch, Task, ThemePicker, Attachments, Footer, MessageSelector, DiffDialog, DiffPanel, ModelPicker, EffortSlider, Select, Plugin, Pane, PaneField, Agents, Scroll.
- **Actions:** `app:*`, `history:*`, `chat:*` (submit, queueSubmit, sendNow, cycleMode, modelPicker, fastMode, thinkingToggle, stash, imagePaste, externalEditor, killAgents, undo, newline), `autocomplete:*`, `confirm:*`, `transcript:*`, `historySearch:*`, `task:background`, `footer:*`, `diff:*`, `scroll:*`, `selection:*`, `modelPicker:*`, `effortSlider:*`, `select:*`, `plugin:*`, `settings:*`, `agents:*`, `voice:pushToTalk`.
- **Reserved:** Ctrl+C, Ctrl+D, Ctrl+M, Ctrl+[, Ctrl+I, Ctrl+H.
- **Undocumented chords:** Ctrl+X Ctrl+A and Ctrl+X Tab are default Chat chords whose purpose isn't documented [C] (binary: `abovePrompt:toggle` / `abovePrompt:focus`).

## 10. Hooks

- **Events [A]:** SessionStart, Setup, InstructionsLoaded, UserPromptSubmit, UserPromptExpansion, MessageDisplay, PreToolUse (allow/deny/ask/defer), PermissionRequest, PermissionDenied, PostToolUse, PostToolUseFailure, PostToolBatch, Notification, SubagentStart/Stop, TaskCreated/Completed, Stop, StopFailure, TeammateIdle, ConfigChange, CwdChanged, DirectoryAdded, FileChanged, WorktreeCreate/Remove, PreCompact/PostCompact, PreModelSwitch/PostModelSwitch, Elicitation/ElicitationResult, SessionEnd.
- **Handler types:** command, http, mcp_tool, prompt, agent; support `async`, `if`, `once`, `statusMessage`, timeouts.
- **Headless behaviour:**
  - `systemMessage` arrives as `informational` [A].
  - `terminalSequence` is ignored in `-p`/SDK (NOT-IN-HEADLESS); mantle must render notifications itself.
  - `MessageDisplay` runs once per message in `-p`; whether its `displayContent` reaches the stream is [C].
  - Notification `permission_prompt` fires about 6 s after a `canUseTool` request in host mode; other notification types [C].
  - Hook lifecycle events visible with `--include-hook-events`.
  - SDK programmatic hooks available via the initialize request [A].
- **`/hooks` browser:** [B].

## 11. MCP

- **Engine [A]:** scopes local/project/user/managed/plugin/claude.ai connectors; `${VAR}` expansion; tool search; auto-reconnect; discovery cache; long-call auto-backgrounding (`task_notification`); `requiresUserInteraction`; channels; resources/prompts; Claude Code as an MCP server (`claude mcp serve`).
- **OAuth:** NOT-IN-HEADLESS (no `/mcp` panel). Claude is told the server needs auth. mantle should run `claude mcp login <name>` (supports `--no-browser`).
- **`/mcp` panel [B]:** status, cached state, Reconnect / Disable / Clear auth / Re-authenticate, tool counts, warnings. Engine data from `mcpServerStatus()` [A].
- **Elicitation:** dialog [B], wire [C] (see protocol notes: `elicitation` control request).

## 12. Agents, parallelism and long-running work

- **Subagents [A]:** built-in and custom (`.claude/agents`, `--agents`), foreground/background, nesting, `parent_tool_use_id` streaming (`--forward-subagent-text`).
  - Fork mode NOT-IN-HEADLESS by default (off in `-p`/SDK; enable with `CLAUDE_CODE_FORK_SUBAGENT=1`).
  - `AskUserQuestion` is not available in subagents (SDK).
- **Agent teams:** NOT-IN-HEADLESS ("Claude doesn't spawn teammates in `-p`, including Agent SDK sessions"). Teammate display modes (tmux/iTerm2 split panes) are interactive.
- **Background sessions / agent view:** `claude agents` table, peek/reply, attach, pin, group, Ctrl+X stop/delete, `←` on empty prompt backgrounds the session. All [B]; scriptable via `claude agents --json` and `claude attach|logs|stop|rm|respawn`. Attaching from a headless driver [C].
- **Dynamic workflows [A]:** run in `-p`, but launch approval goes through `canUseTool`. The `ultracode` keyword needs `origin:human`. Runs don't pause at the usage limit in `-p`. The `/workflows` progress view (pause, restart agent, save) is [B]/[C].
- **Cross-session messaging [A]:** `-p` binds an inbox socket. Held messages expire after `dialogExpiry`; set `crossSessionInbound: accept` to receive unattended. `/list-agents` is [B].
- **`/goal` [A]:** works in `-p` (loops to completion).
- **`/loop` and Cron tools [A]:** session-scoped; fire only while the session is idle and open [C].
- **Channels [A]:** in `-p`, AskUserQuestion and plan approval are disabled.
- **Worktrees [A]:** `-w`, EnterWorktree/ExitWorktree. Exit cleanup prompt [B]; `-p` never cleans up.
- **Background Bash in `-p`:** killed about 5 s after the result. Background subagents waited on up to 10 minutes (`CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS`). Unattended time limits apply. (In a long-lived stream-json session the process stays up between turns, so this mostly matters at exit.)

## 13. Memory / CLAUDE.md

- **Engine [A]:** CLAUDE.md, `CLAUDE.local.md`, `.claude/rules`, AGENTS.md (built-in mod), `@imports` (4 hops), auto memory, `InstructionsLoaded`.
- **UI [B]:** the `/memory` picker (open in editor, toggle auto memory).
- **[C]:** the external-import approval dialog, and the legacy `#` quick-memory entry (a theme token still references "`#` memory entries").

## 14. Checkpointing and rewind

- **Rewind menu [B]:** Esc Esc or `/rewind`. Actions: restore code + conversation / conversation only / code only; summarize from here / up to here (optional instruction); restore the pre-`/clear` session.
- **Engine [A]:**
  - `rewindFiles(uuid)` needs `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true` and `--replay-user-messages` to get UUIDs.
  - Conversation rewind uses `resumeSessionAt` / `resumeDropsTurn` + fork (CLI: `--resume-session-at`, `--resume-drops-turn`; see protocol notes).
  - "Summarize from/up to here" is [C] (not exposed).

## 15. Sessions

- **Engine [A]:** `--resume <id|name|path>`, `--continue`, `--fork-session`, `/rename`, `--name`, transcripts at `~/.claude/projects/<proj>/<session>.jsonl` (subagents, tool-results).
- **NOT-IN-HEADLESS / different:**
  - `-p` and SDK sessions are excluded from the interactive picker and from `claude --continue` (but `claude -p --continue` includes them).
  - No AI-generated titles for shell-started `-p` runs.
  - Permission mode is not restored on resume, except plan mode under specific conditions.
- **UI [B]:**
  - Session picker: grouping, preview with Space, Ctrl+R rename, Ctrl+A all projects, Ctrl+W worktrees, Ctrl+B branch filter, search by PR URL.
  - "Resume from summary" dialog (Pro/Max; idle > 1 h and > 100k tokens).
  - `/branch`.
  - The TS SDK's `listSessions` / `getSessionMessages` read local files; mantle can replicate that in Go.

## 16. Model configuration

- **Engine [A]:**
  - Aliases: `default`, `best`, `fable`, `sonnet`, `opus`, `haiku`, `[1m]` variants, `opusplan`.
  - `setModel` / `applyFlagSettings({model})`; mid-turn switch applies on the next request.
  - Effort levels low–max plus ultracode; persist with `updateSettings('userSettings', {effortLevel})`.
  - Thinking toggle via `applyFlagSettings` / the `thinking` option; `alwaysThinkingEnabled`. Can't be turned off on Opus 5.5, Sonnet 5.5 or Fable.
  - Fast mode (`fastMode`; in `-p` needs opt-in via `--settings`; `fast_mode_state` and `fast_mode_disabled_reason` reported).
  - Advisor, fallback chains, auto-compact window, PreModelSwitch hooks.
  - Model metadata via `supportedModels()` (effort levels, fast/auto support).
- **UI [B]:** `/model` picker (prices, Default row, `s` for session only); cache-warm confirmation dialog on `/model` and `/effort` switches; Alt+P / Alt+T / Alt+O shortcuts; Fable usage-credits consent prompt (not forwarded) [C]; model-choice dialog after a safety refusal [C].

## 17. Output styles [A]

- Built-ins: Default, Proactive, Concise, Explanatory, Learning. Custom styles in `output-styles/`.
- `/output-style` works in `-p`.
- `reloadOutputStyles()` and `outputStyle` via settings / `updateSettings('localSettings')`.
- The `/config` menu entry is [B].

## 18. Status line [B]

- **mantle must:** run the user's `statusLine.command` with a JSON object on stdin, set `COLUMNS`/`LINES`, debounce at 300 ms, support `refreshInterval`, render multi-line ANSI and OSC 8 links. Also `subagentStatusLine`.
- **JSON fields:** model, cwd/workspace (repo, worktree), cost, context_window, effort, thinking, fast_mode, rate_limits, prompt_cache, session_id/name, vim.mode, agent, pr, worktree, output_style, version, transcript_path.
- **Gap:** most fields can be rebuilt from stream data, but `prompt_cache`, `pr`, rate-limit percentages and the session title need mantle-side computation [C].

## 19. Costs, usage, limits

- **Engine [A]:** `/usage` text, `result.total_cost_usd` / `modelUsage` (client estimates), `rate_limit_event` (status, `resetsAt`, utilization, credits_required), `api_retry`.
- **UI [B]:** plan usage bars and breakdown (`d`/`w`, `r` retry).
- **Auto-continue at usage limit:** NOT-IN-HEADLESS ("background sessions and `-p` runs: menu row isn't available"). mantle must implement the wait itself from `resetsAt`.
- **Other UI:** `/rate-limit-options` and `/usage-credits` are [B].

## 20. Context window and compaction [A]

- Auto-compact, `/compact`, `compact_boundary`, `status: "compacting"`.
- The `/context` colored grid is [B], from `getContextUsage()` data (grid rows and colors included).

## 21. Plugins, marketplaces, mods

- **Engine [A]:** plugin loading (`enabledPlugins`, `--plugin-dir`, `--plugin-url`), `plugin_errors` in init, `claude plugin` CLI, `reloadPlugins({holdOnCacheImpact})`.
- **UI [B]:** the `/plugin` manager (Discover/Installed tabs, Space/I/F keys).
- **Claude Code mods:** hooks and commands run in `-p` [A]. Drawing does not (NOT-IN-HEADLESS): panes, bands, toasts, replaced rows and the built-in `cc-plugin-diff` UI render only in the terminal and Desktop.

## 22. Integrations

- **IDE:** the `ide` MCP server via `~/.claude/ide/<port>.lock` (selection context, diff in IDE, `getDiagnostics`); `/ide`; `autoConnectIde`; `--ide`. Works in `-p`? [C].
- **Chrome** (`--chrome`, `/chrome`): [A]/[C].
- **Computer use:** NOT-IN-HEADLESS ("requires an interactive session").
- **Remote Control:** an interactive process feature; its local-only command list is a useful proxy for what remote/SDK clients can do. `-p` support [C].
- **Others:** `/desktop`, `/teleport`, cloud sessions (`--cloud`), artifacts (off by default in SDK contexts), deep links (`claude-cli://` registration after the first interactive prompt) are [B]/[C].

## 23. Auth, diagnostics, data

- **Auth:** `/login` and `/logout` NOT-IN-HEADLESS; use `claude auth ...` / `setup-token` [A].
- **Diagnostics:** `claude doctor` [A]; `/doctor` [C]; `--debug` / `--debug-file` [A].
- **Startup:** `startup_failure_reason` [A]; invalid-settings dialog and corrupted `~/.claude.json` reset prompt are [B].
- **Feedback and telemetry:** `/feedback` consent flow [B]; telemetry env vars [A]; feedback survey [B].

---

## Interactive-only features a replacement UI must rebuild

1. **Prompt editor:** multiline, readline keys with kill ring and undo, vim mode with remaps, Ctrl+S stash, Ctrl+G external editor, emoji shortcodes, spellcheck, invisible-character stripping, paste collapsing (`[Pasted text #N]` + paste-cache), image paste and drag-drop chips.
2. **Autocomplete:** `/` command menu (hidden commands, alias highlighting, mid-prompt completion, ghost text), `@` file/resource/session picker honouring `fileSuggestion` and `respectGitignore`, `!` shell mode with history and path completion.
3. **History:** Up/Down recall per directory, Ctrl+R search (inline and dialog with scope cycling); read and write `~/.claude/history.jsonl`.
4. **Turn controls:** Esc, Ctrl+C/Ctrl+D double-press semantics, queue display (gray pending), take-back of queued messages, Ctrl+B background, Ctrl+X Ctrl+K kill agents, Ctrl+Z, Ctrl+L.
5. **Transcript renderer:** markdown and syntax highlighting, tool-call collapsing and grouping, Edit/Write diffs, thinking display, Ctrl+O transcript viewer with search/navigation, verbose/focus/viewMode, turn-duration line, spinner with verbs/tips and thinking-token counter, `maxProseWidth`.
6. **Fullscreen/alt-screen renderer:** mouse click/hover/select/copy, scroll and auto-follow, jump-to-bottom, sticky prompt header, `/scroll-speed`. Also a screen-reader (flat) mode.
7. **Footer/status area:** permission-mode indicator, task checklist (Ctrl+T), subagent/fork panel with transcript drill-in, background tasks (`/tasks`), PR/MR badge (gh/glab), `footerLinksRegexes`, issue links, notices.
8. **Custom status line** and subagent status line runner with the full JSON payload.
9. **Dialogs:**
   - Permission prompt (options, comments, session/always grants, switch-to-auto, background-subagent attribution), AskUserQuestion, plan approval (edit plan, clear-context), MCP elicitation (form/URL).
   - Workspace trust, `.mcp.json` server approval, external-import approval, bypass-mode warning, API-key approval.
   - Model-switch cache warning, resume-from-summary, Fable consent, invalid settings, usage-limit options.
10. **Pickers and panels:** `/model` (with effort, `s` for session only), `/effort` slider, `/config` (Config/Status/Usage/Stats tabs), `/status`, `/permissions` (incl. Auto mode tab), `/hooks`, `/mcp` (plus OAuth via `claude mcp login`), `/plugin`, `/skills`, `/memory`, `/theme` (plus custom themes), `/resume` session picker (reading `~/.claude/projects`), `/rewind` menu, `/copy` picker, `/export` dialog, `/release-notes`, `/help`, `/workflows` progress view, `/btw` overlay, `/diff` panel/dialog, `/context` grid, `/usage` plan bars.
11. **Session recap**, auto-continue-at-usage-limit wait (from `rate_limit_event`), prompt-suggestion rendering, initial example prompt.
12. **Terminal integration:** title, OSC 9;4 progress, desktop notifications/bell (`preferredNotifChannel`; replaces the hook `terminalSequence` lost in `-p`), keybindings.json parser, themes, `/tui` choice.
13. **Agent view** (`claude agents`) equivalent, if parity extends there: `claude agents --json` plus attach/stop/logs/respawn/rm.
14. **Voice dictation** (likely infeasible: claude.ai STT not exposed) and the session-quality survey.
15. **Mod-drawn UI** (panes, bands, toasts): the protocol is internal, so parity is likely not achievable.

## Documented as NOT supported or different in headless (`-p` / SDK)

- **Unavailable:**
  - Terminal-only built-ins (e.g. `/login`); MCP OAuth panel; computer use; spawning agent-team teammates.
  - Automatic session recap; usage-limit auto-continue and its menu; AI session titles for shell-started runs.
  - Workspace trust dialog and per-server `.mcp.json` approval (both skipped as if trusted); bypass warning dialog.
  - Hook `terminalSequence` (ignored); mod drawing; plugin MCP changes on `/reload-plugins`; worktree cleanup prompt; retirement warnings on stderr (stream-json).
- **Different defaults or behaviour:**
  - Fork mode off; starting permission mode `default`; prompt suggestions off.
  - `/model` and `/effort` changes not saved as defaults; `/fast` requires `fastMode` in `--settings`.
  - Plan mode keeps its blocks even when bypass is available.
  - MessageDisplay hook runs once per message; workflow keyword needs `origin:human`; workflows don't pause at the usage limit.
  - Background Bash killed about 5 s after the result; unattended background-command time limits apply.
  - Channel runs disable AskUserQuestion and plan approval; held cross-session messages expire.
  - `-p` sessions hidden from the interactive picker and `--continue`.
  - `--bare` will become the `-p` default in a future release.

## Unclear or undocumented (verify against the binary / in spikes)

- Exact wire names for most control requests (only `initialize`, `interrupt`, `can_use_tool` and `register_repo_root` are named in the docs; the SDK/binary names are in `protocol-2.1.288.md`).
- How a raw CLI host registers as the permission host (the SDK's internal wiring is undocumented; it is `--permission-prompt-tool stdio`).
- Wire format for elicitation, and for dialogs forwarded to an SDK host (`dialogExpiry` mentions them).
- Undoing a queued message, Ctrl+B equivalent, `/btw`, `/export`, `/recap`, `/cd`, `/doctor` behaviour in `-p`.
- Sandbox network-host prompt routing in headless; `--ide` in `-p`; Remote Control from `-p`.
- Whether `-p` writes `history.jsonl`; whether MessageDisplay `displayContent` surfaces in the stream.
- "Summarize from here" and conversation rewind via CLI flags.

## URLs read

All under `https://code.claude.com/docs`:
- Index: `/llms.txt`.
- Core and reference (`/en/<page>.md`): interactive-mode, commands, cli-reference, headless, keybindings, permission-modes, permissions, settings, settings-reference, hooks, mcp, sub-agents, skills, output-styles, statusline, memory, checkpointing, model-config, terminal-config, fullscreen, costs, sessions, agent-view, sandboxing, voice-dictation, accessibility, cross-session-messaging, agent-teams, workflows, goal, scheduled-tasks, remote-control, channels, fast-mode, advisor, chrome, computer-use, artifacts, deep-links, worktrees, tools-reference, env-vars, vs-code, jetbrains, prompt-caching, common-workflows, data-usage, authentication, claude-directory, glossary. Headings only for troubleshooting, debug-your-config, context-window.
- Plugins (`/en/plugins/<page>.md`): overview, install, cli-reference, mods/overview, mods/interface, mods/events, mods/api.
- Agent SDK (`/en/agent-sdk/<page>.md`): overview, streaming-vs-single-mode, typescript (Options, Query, control responses, all message types, permission types), python (ClaudeSDKClient methods), user-input, claude-code-features, permissions, hooks, file-checkpointing, todo-tracking, streaming-output.
