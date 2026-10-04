# PARITY: Claude Code 2.1.288 feature inventory for mantle

This is the complete list of what the Claude Code 2.1.288 terminal UI does, and how mantle
covers each item. It was built from three research passes:

- the installed binary (`~/.local/share/claude/versions/2.1.288`, build `17fe1eb7`);
- the official docs at code.claude.com;
- the headless protocol, matched to SDK 0.3.288.

Every feature, slash command, keybinding context, UI settings key, dialog, view, CLI flag
and subcommand maps to exactly one row below, or to a row through one of the indexes at the
end.

This table is written once. Per-feature progress lives in each plan's checklist and in
`Parity:` tags on registrations; `make parity` builds the status report. Only the
coordinator edits this file, and only to fix ownership or add newly discovered features.

## Legend

| Column | Values |
|---|---|
| **Code** | **E** engine passthrough: the `claude` engine does it, mantle forwards or sends it · **R** rendered by mantle from engine data · **N** native mantle implementation (settings files, control requests, `claude` subcommands) · **H** hand off to the real `claude` (stop engine, run `claude --resume <sid>` or the command, restart engine) · **X** known gap, reason in Notes |
| **M** | **M1** daily driver · **M2** full parity · **M3** polish |
| **Plan** | Owner plan, `01`–`12` (see `docs/plans/`) |
| **Part** | **A** needs nothing from other sessions · **B** needs `contracts-v1` (01) and/or `proto-v1` (02) |

**User decision:** local terminal features are rebuilt natively. Cloud and product features
are **H**. Infeasible features are **X**.

## Summary

| Area | Prefix | Owner | Rows | E | R | N | H | X |
|---|---|---|---:|---:|---:|---:|---:|---:|
| Engine plumbing + engine-provided features | ENG | 02 | 59 | 25 | 3 | 31 | 0 | 0 |
| Prompt editor | ED | 04 | 40 | 0 | 1 | 39 | 0 | 0 |
| Autocomplete | AC | 04 | 22 | 2 | 3 | 17 | 0 | 0 |
| History | HI | 04 | 10 | 0 | 0 | 10 | 0 | 0 |
| Turn control | TC | 05 | 21 | 2 | 4 | 15 | 0 | 0 |
| Transcript rendering | TR | 03 | 60 | 0 | 54 | 6 | 0 | 0 |
| Views | VW | 03 / 07 / 12 | 24 | 0 | 7 | 17 | 0 | 0 |
| Chrome & terminal | CH | 07 | 30 | 0 | 7 | 23 | 0 | 0 |
| Permissions, dialogs & startup gates | PD | 05 | 45 | 0 | 0 | 45 | 0 | 0 |
| Sessions | SE | 06 | 43 | 5 | 1 | 35 | 2 | 0 |
| Context & usage | CU | 06 | 11 | 1 | 5 | 5 | 0 | 0 |
| Settings & model panels | ST | 08 | 36 | 4 | 0 | 32 | 0 | 0 |
| Ecosystem panels + engine-passthrough commands | EC | 09 | 47 | 17 | 1 | 23 | 6 | 0 |
| Config compatibility | CF | 01 | 20 | 0 | 1 | 19 | 0 | 0 |
| CLI flags, subcommands, drift | CLI | 11 | 28 | 5 | 0 | 20 | 3 | 0 |
| Cloud & product hand-offs | CL | 09 / 11 | 27 | 2 | 0 | 0 | 25 | 0 |
| Known gaps | GAP | various | 13 | 0 | 0 | 0 | 0 | 13 |
| mantle-only | MT | 10 / 01 | 36 | 0 | 0 | 36 | 0 | 0 |
| **Total** | | | **572** | **63** | **87** | **373** | **36** | **13** |

---

## ENG: engine plumbing (owner 02)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| ENG-01 | Headless spawn with SDK-equivalent flags | mantle starts `claude` in the background | N | M1 | 02 | A | `--output-format stream-json --input-format stream-json --verbose --include-partial-messages --permission-prompt-tool stdio`; never `--system-prompt` |
| ENG-02 | Child environment cleanup/set | Engine behaves like normal Claude Code | N | M1 | 02 | A | Unset `CLAUDECODE`, `NODE_OPTIONS`, `DEBUG`, `CLAUDE_CODE_SIMPLE`; set `CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS=1` and `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true` |
| ENG-03 | NDJSON transport, multi-MB lines | Large tool outputs and images never break the UI | N | M1 | 02 | A | `bufio.Reader`, not the default Scanner; skip non-`{` lines; one locked writer |
| ENG-04 | Typed decoding with raw retention | Unknown events don't crash mantle | N | M1 | 02 | A | Envelope `{type, subtype}` first, then typed; keep `Raw` |
| ENG-05 | `initialize` handshake | Commands, models, account and output styles known at start | N | M1 | 02 | B | Response also carries `pending_permission_requests` |
| ENG-06 | `system/init` per-turn tracking | Footer and menus reflect tools, MCP, model, mode, skills, plugins | R | M1 | 02 | B | Newest wins; `capabilities[]` gates features |
| ENG-07 | Control request correlator | Requests like `/context` return reliably | N | M1 | 02 | B | `request_id` map, timeouts (60 s for initialize), error replies |
| ENG-08 | Capability cache `Supports(subtype)` | UI hides features the engine can't do | N | M1 | 02 | B | An "unsupported subtype" reply disables that capability |
| ENG-09 | `unstable.go` for undocumented subtypes | Advanced features degrade gracefully | N | M2 | 02 | B | e.g. `rewind_conversation`, `side_question`, `get_workspace_diff`, each with a fallback |
| ENG-10 | CLI→client requests as messages | Dialogs appear when Claude asks | N | M1 | 02 | B | `can_use_tool`, `hook_callback`, `elicitation`, `request_user_dialog`, `mcp_message`; `control_cancel_request` |
| ENG-11 | Stream delta coalescing | Smooth streaming without CPU spikes | N | M1 | 02 | B | Flush at most every ~16 ms, or immediately on a non-delta event (S17) |
| ENG-12 | `session_state_changed` | Accurate busy/idle/waiting indicators | R | M1 | 02 | B | `idle`, `running`, `requires_action` |
| ENG-13 | Session tracker | Correct session id, model and mode everywhere | N | M1 | 02 | B | Handles the id change on `conversation_reset`; cwd |
| ENG-14 | Supervisor restart variants | Resume, fork and rewind work | N | M1 | 02 | B | `--resume=`, `--fork-session`, `--resume-session-at=`, `--resume-drops-turn=` |
| ENG-15 | Engine crash recovery | Notice with the error and "restart with --resume" | N | M1 | 02 | B | Shows the stderr tail |
| ENG-16 | stderr ring buffer and debug log | Diagnostics without screen corruption | N | M1 | 02 | B | `--debug-file` passthrough |
| ENG-17 | Multiple engines with EngineID | Main, builder, /btw and background sessions coexist | N | M1 | 02 | B | Every message tagged with its engine |
| ENG-18 | Process group and shutdown sequence | Ctrl+C in `$EDITOR` doesn't kill Claude; clean exit | N | M1 | 02 | B | `Setpgid`; `end_session`, then close stdin, SIGTERM, SIGKILL |
| ENG-19 | Engine binary discovery and minimum version | Clear error if `claude` is missing or too old | N | M1 | 02 | A | Minimum 2.1.288; `MANTLE_CLAUDE_BIN` override |
| ENG-20 | Conformance probe (zero tokens) | Warning when a new Claude Code breaks mantle | N | M2 | 02 | B | Checks commands, skills, plugins, hooks and memory are visible (S6) |
| ENG-21 | Engine version pinning | Offer to stay on the last good engine | N | M2 | 02 | B | Uses `~/.local/share/claude/versions/` |
| ENG-22 | `--bare` default guard | Hooks, skills, MCP and CLAUDE.md never silently vanish | N | M1 | 02 | B | Unset `CLAUDE_CODE_SIMPLE`; the probe detects it |
| ENG-23 | User-message fields | Queueing, pastes and human-only features work | N | M1 | 02 | A | `uuid`, `priority`, `shouldQuery`, `client_composed`, `origin:{kind:"human"}`, `pasted_content`, `inline_pastes` |
| ENG-24 | `--replay-user-messages` acks | Queued messages turn from gray to sent | N | M1 | 02 | B | `isReplay` echoes with the uuid (S2) |
| ENG-25 | `--include-hook-events` | Hook progress lines visible | N | M1 | 02 | B | S4 |
| ENG-26 | `--forward-subagent-text` | Subagent text streams nested | N | M1 | 02 | B | S12 |
| ENG-27 | Prompt suggestions channel | Gray predicted next prompt | N | M2 | 02 | B | `initialize.promptSuggestions` / `prompt_suggestion` frames |
| ENG-28 | `CLAUDE_CODE_ENTRYPOINT` choice | Sessions visible and features ungated | N | M1 | 02 | A | S5; `sdk-cli` hides sessions from Claude Code's own picker |
| ENG-29 | `keep_alive` / `update_environment_variables` | None (robustness) | N | M2 | 02 | B | |
| ENG-30 | `auth_status` frames | Login progress shown | R | M2 | 02 | B | Hidden `--enable-auth-status` |
| ENG-31 | fakeclaude scripted engine | None (deterministic tests) | N | M1 | 02 | A | emit / expect / respond / request / delay |
| ENG-32 | fakeapi Messages-API mock | None (free offline end-to-end tests of the real engine) | N | M1 | 02 | B | `ANTHROPIC_BASE_URL` plus isolated `CLAUDE_CONFIG_DIR` (S14) |
| ENG-33 | Sanitizing fixture recorder | None (test fixtures) | N | M1 | 02 | A | `scripts/record-fixture.sh` |
| ENG-34 | `scripts/sdk-diff` | None (drift alert) | N | M2 | 02 | A | Diffs SDK `0.3.<patch>` subtypes against `pkg/proto` |
| ENG-35 | Built-in tools execution | Bash, Read, Write, Edit, Glob, Grep, NotebookEdit, LSP, PowerShell, WebFetch, WebSearch… run | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-36 | Subagents | Built-in and custom agents, background, nesting | E | M1 | 02 | B | Verify via conformance probe/fixture; fork mode is off in `-p` unless `CLAUDE_CODE_FORK_SUBAGENT=1` |
| ENG-37 | MCP servers | All scopes, stdio/sse/http, tool search, resources, prompts, channels | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-38 | Hooks execution | All 33 events; command/http/mcp_tool/prompt/agent types | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-39 | Skills engine | SKILL.md discovery, frontmatter, substitutions, `!cmd`, chaining up to 6 | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-40 | Plugin loading | `enabledPlugins`, `--plugin-dir`, marketplaces | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-41 | CLAUDE.md and memory | CLAUDE.md, CLAUDE.local.md, rules, AGENTS.md, `@imports`, auto memory | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-42 | Compaction | Auto-compact and `/compact` | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-43 | Sandbox | Filesystem and network sandboxing | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-44 | Auto mode classifier | `auto` mode decisions, `autoMode` config | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-45 | Output styles | Default, Explanatory, Learning, Concise, Proactive, custom | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-46 | Permission rule evaluation | allow/deny/ask rules, protected paths, rm circuit breaker | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-47 | File checkpointing | Pre-edit backups for rewind | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-48 | Background tasks, Monitor, Cron, ScheduleWakeup | Long-running work continues | E | M1 | 02 | B | Verify via fixture; `-p` kills background Bash ~5 s after the result (S1/S2) |
| ENG-49 | Worktrees | `EnterWorktree`/`ExitWorktree`, `-w` | E | M2 | 02 | B | Verify via conformance probe/fixture |
| ENG-50 | Workflows | Workflow tool, `ultracode` keyword | E | M2 | 02 | B | Needs `origin:{kind:"human"}`; launch approval through `can_use_tool` |
| ENG-51 | Cross-session messaging | SendMessage, ListAgents, inbox socket | E | M2 | 02 | B | Verify via fixture; `crossSessionInbound` |
| ENG-52 | Model resolution, fast mode, advisor, fallback chains | Aliases, `[1m]`, opusplan | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-53 | Prompt caching and system-prompt snapshot | None (cost and latency) | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-54 | Settings precedence inside the engine | Engine honours managed > flag > local > project > user | E | M1 | 02 | B | Verify via conformance probe/fixture; ConfigChange hook |
| ENG-55 | Providers and auth | claude.ai, API key, apiKeyHelper, Bedrock, Vertex, Foundry, gateway | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-56 | Telemetry and OTEL env | Enterprise telemetry | E | M2 | 02 | B | Verify via conformance probe/fixture |
| ENG-57 | Shell snapshot / session-env / `CLAUDE_ENV_FILE` | Bash tool sees the user's shell environment | E | M1 | 02 | B | Verify via conformance probe/fixture |
| ENG-58 | Structured tool results (`tool_use_result`) | Rich data for renderers | E | M1 | 02 | B | Verify via fixture |
| ENG-59 | Engine-side expansion of `@path`, `@server:res`, `/cmd` | Mentions and commands expand | E | M1 | 02 | B | Verify via fixture; `client_composed:true` disables it |

## ED: prompt editor (owner 04)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| ED-01 | Editing buffer | Type, move, wrap; grapheme-aware cursor | N | M1 | 04 | A | Custom `pkg/ui/editor` (bubbles textarea lacks the needed features) |
| ED-02 | Newline: `\`+Enter | Backslash then Enter inserts a newline | N | M1 | 04 | A | |
| ED-03 | Newline: Shift+Enter | Works on kitty/Ghostty/WezTerm/iTerm2 (CSI u) | N | M1 | 04 | A | S15 terminal matrix |
| ED-04 | Newline: Option/Alt+Enter | ESC CR | N | M1 | 04 | A | |
| ED-05 | Newline: Ctrl+J | `chat:newline` | N | M1 | 04 | A | |
| ED-06 | Bracketed paste with newlines | Paste multi-line text without submitting | N | M1 | 04 | A | `PasteStartMsg`/`PasteMsg`/`PasteEndMsg` |
| ED-07 | Enter submits | `chat:submit` | N | M1 | 04 | B | |
| ED-08 | Readline movement | Ctrl+A/E, Alt+B/F, arrows, Home/End | N | M1 | 04 | A | |
| ED-09 | Readline kill | Ctrl+K/U, Ctrl+W (to whitespace), Alt+D | N | M1 | 04 | A | |
| ED-10 | Kill ring | Ctrl+Y yank, Alt+Y yank-pop | N | M1 | 04 | A | |
| ED-11 | Undo | Ctrl+_ / Ctrl+- / Ctrl+Shift+- (`chat:undo`) | N | M1 | 04 | A | |
| ED-12 | Double-Esc clears input | Esc Esc on non-empty input | N | M1 | 04 | B | On empty input during idle, Esc Esc opens rewind (SE-22) |
| ED-13 | Ctrl+L clears input | `chat:clearInput` | N | M1 | 04 | B | |
| ED-14 | Vim mode | NORMAL/INSERT/VISUAL/V-LINE/REPLACE, motions, operators, text objects, `.` | N | M2 | 04 | A | `editorMode:"vim"`; pure state machine first |
| ED-15 | Vim insert-mode remaps | e.g. `jj` for Esc | N | M2 | 04 | A | `vimInsertModeRemaps` |
| ED-16 | Vim mode indicator | `-- INSERT --` below the prompt | N | M2 | 04 | B | `statusLine.hideVimModeIndicator` |
| ED-17 | Vim `/` history search | `/` in NORMAL opens history search | N | M2 | 04 | B | |
| ED-18 | Stash prompt | Ctrl+S stashes and restores text, cursor, pastes and mode | N | M2 | 04 | B | `chat:stash` |
| ED-19 | External editor | Ctrl+G or Ctrl+X Ctrl+E opens `$EDITOR`/`$VISUAL` | N | M2 | 04 | B | `externalEditorContext`; S16 |
| ED-20 | Large paste collapse | Above 800 chars or 3 lines → `[Pasted text #N +L lines]` | N | M1 | 04 | B | |
| ED-21 | paste-cache storage | Pastes saved to `~/.claude/paste-cache/<hash>.txt` | N | M1 | 04 | A | S3: check whether headless writes it |
| ED-22 | Expand a collapsed paste | Restore the full text of a chip | N | M2 | 04 | B | |
| ED-23 | Paste metadata to the engine | Engine knows what was pasted | N | M1 | 04 | B | `pasted_content`, `inline_pastes` |
| ED-24 | Image paste from clipboard | Ctrl+V (Alt+V) → `[Image #N]` chip | N | M1 | 04 | A | `osascript … «class PNGf»`; `chat:imagePaste` |
| ED-25 | Drag-and-drop image | Dropping a file path makes an image chip | N | M1 | 04 | B | Detect image paths in `PasteMsg` |
| ED-26 | Attachments navigation | Left/Right select, Backspace/Delete remove, Down/Esc exit | N | M2 | 04 | B | Attachments context |
| ED-27 | Atomic chips | Pastes and images edit as single units | N | M1 | 04 | A | |
| ED-28 | Images sent as base64 blocks | Claude sees the image | N | M1 | 04 | B | |
| ED-29 | Emoji shortcodes | `:heart:` replacement and popup | N | M2 | 04 | B | `emojiCompletionEnabled`; goldmark-emoji set |
| ED-30 | Spellcheck | Misspellings underlined | N | M3 | 04 | B | `spellcheck{enabled, checker, color}`; aspell/hunspell |
| ED-31 | Invisible-character stripping | Zero-width/bidi/tag chars removed on Enter, then a second Enter confirms | N | M2 | 04 | B | |
| ED-32 | `ultrathink` highlight | Rainbow keyword | N | M2 | 04 | B | Visual only |
| ED-33 | `ultracode` highlight and toggle | Keyword highlight; Meta+W toggles the trigger | N | M2 | 04 | B | `chat:workflowKeywordToggle` |
| ED-34 | Prompt-suggestion ghost text | Gray next-prompt prediction; Tab/Right accepts | R | M2 | 04 | B | Data from ENG-27; `promptSuggestionEnabled` |
| ED-35 | Initial example prompt | Suggested first prompt from git history | N | M3 | 04 | B | |
| ED-36 | `?` shortcut help | `?` on an empty prompt shows the shortcuts panel | N | M2 | 04 | B | |
| ED-37 | Placeholder and inline hints | Prompt placeholder text | N | M1 | 04 | B | |
| ED-38 | Real cursor placement for IME | IME candidates appear at the cursor | N | M1 | 04 | A | `Rendered.Cursor` |
| ED-39 | Large-input performance | A 1 MB paste stays responsive | N | M1 | 04 | A | |
| ED-40 | Keyboard fallback for Terminal.app | Newline still possible without CSI u | N | M1 | 04 | A | `/terminal-setup` (ST-25) installs mappings |

## AC: autocomplete (owner 04)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| AC-01 | `/` command menu | Leading `/` opens the command list | N | M1 | 04 | B | |
| AC-02 | Fuzzy filtering | Menu filters as you type | N | M1 | 04 | B | sahilm/fuzzy |
| AC-03 | Merged command sources | Native + engine headless commands, skills, plugin commands, MCP prompts, custom commands | R | M1 | 04 | B | From `initialize.commands` / `init.slash_commands` + the native registry |
| AC-04 | Live command refresh | New skills or MCP prompts appear immediately | R | M1 | 04 | B | `commands_changed` |
| AC-05 | Alias matching and highlighting | `/bashes` finds `/tasks` | N | M1 | 04 | B | |
| AC-06 | Hidden commands | Not listed but still runnable | N | M1 | 04 | B | |
| AC-07 | Argument hints | e.g. `/model [model]` | R | M1 | 04 | B | `argumentHint` |
| AC-08 | Mid-prompt `/` completion | Completion after text | N | M2 | 04 | B | |
| AC-09 | Ghost-text completion | Inline gray completion of a command | N | M2 | 04 | B | |
| AC-10 | Argument completions | e.g. model names, session ids | N | M2 | 04 | B | |
| AC-11 | Autocomplete keys | Tab accept, Esc dismiss, Up/Down | N | M1 | 04 | B | Autocomplete context |
| AC-12 | `@` file mentions | Dropdown of matching paths | N | M1 | 04 | B | `file_suggestions` control request, debounced (S11) |
| AC-13 | `respectGitignore` / `fileSuggestion` | Suggestions follow the user's config | E | M1 | 04 | B | Engine-side via `file_suggestions` |
| AC-14 | `@server:resource` mentions | MCP resources listed with files | N | M2 | 04 | B | S11 |
| AC-15 | `@` live-session suggestions | Other sessions offered | N | M3 | 04 | B | |
| AC-16 | `!` shell mode entry | `!` switches to bash mode, with border colour | N | M1 | 04 | B | |
| AC-17 | `!` command execution | Output added to context; Claude replies if `respondToBashCommands` | N | M1 | 04 | B | No stream-json equivalent; emulate (run + inject via `shouldQuery`). Needs a spike |
| AC-18 | `!` history completion | Tab completes earlier commands | N | M2 | 04 | B | |
| AC-19 | `!` path completion | Live path completion | N | M2 | 04 | B | |
| AC-20 | `!` backgrounding | Ctrl+B backgrounds a running `!` command | N | M2 | 04 | B | |
| AC-21 | Slash dispatch to engine | Prompt-type and headless-local commands run in the engine | E | M1 | 04 | B | Sent as user text |
| AC-22 | Native vs engine vs H routing | Interactive commands open native panels, otherwise hand off | N | M1 | 04 | B | Native registry first, then engine, then H fallback (SE-40) |

## HI: history (owner 04)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| HI-01 | Up/Down recall | Previous prompts for this project | N | M1 | 04 | B | |
| HI-02 | Read `history.jsonl` | Shares history with Claude Code | N | M1 | 04 | A | `{display, pastedContents, timestamp, project, sessionId}` |
| HI-03 | Write `history.jsonl` | mantle prompts appear in Claude Code history | N | M1 | 04 | A | S3; append single lines with `O_APPEND` |
| HI-04 | Consecutive duplicates collapse | | N | M1 | 04 | A | |
| HI-05 | Multiline-aware Up/Down | Up moves within text before recalling | N | M1 | 04 | A | |
| HI-06 | Ctrl+R reverse search | Inline search | N | M2 | 04 | B | |
| HI-07 | Search scope cycling | Ctrl+S cycles session/project/all | N | M2 | 04 | B | |
| HI-08 | HistorySearch keys | Ctrl+R next, Esc/Tab accept, Enter execute, Ctrl+C cancel | N | M2 | 04 | B | |
| HI-09 | History restores pastes | Recalled prompts keep their paste chips | N | M2 | 04 | B | |
| HI-10 | History across sessions | Includes earlier sessions in this directory | N | M1 | 04 | A | |

## TC: turn control (owner 05)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| TC-01 | Esc interrupts the turn | Partial work kept | N | M1 | 05 | B | `interrupt`; S1 |
| TC-02 | Interrupt and cancel queue | Also drops queued messages | N | M2 | 05 | B | `cancel_queued`; S1 |
| TC-03 | Ctrl+C semantics | Interrupt, else clear input, twice to exit ("Press Ctrl-C again to exit") | N | M1 | 05 | B | |
| TC-04 | Ctrl+D double press exits | Within 800 ms | N | M1 | 05 | B | |
| TC-05 | Queue while busy | Enter during a turn queues a gray pending message | N | M1 | 05 | B | |
| TC-06 | Mid-turn pickup | Queued message folded in between tool rounds | E | M1 | 05 | B | `priority:"next"`; S2 |
| TC-07 | Take back queued messages | Up edits queued messages | N | M2 | 05 | B | `cancel_async_message`; S2 |
| TC-08 | Send now | Ctrl+Enter or Ctrl+X Ctrl+S | N | M1 | 05 | B | `priority:"now"` + `origin:{kind:"human"}` |
| TC-09 | Queue submit | Ctrl+X Enter queues without interrupting | N | M2 | 05 | B | `priority:"later"` |
| TC-10 | Send queued immediately | Enter on queued messages | N | M2 | 05 | B | |
| TC-11 | Background the running task | Ctrl+B or Ctrl+X Ctrl+B | N | M2 | 05 | B | `background_tasks` control request; Task context |
| TC-12 | Kill all background agents | Ctrl+X Ctrl+K | N | M2 | 05 | B | `stop_task` per task |
| TC-13 | Mid-turn model, effort or fast switch | Applies on the next request | E | M1 | 05 | B | |
| TC-14 | Interrupted-turn markers | "Interrupted" shown | R | M1 | 05 | B | `terminal_reason` `aborted_streaming`/`aborted_tools`; rendered by 03 |
| TC-15 | Waiting-for-you state | Indicator while a dialog is pending | R | M1 | 05 | B | `requires_action` |
| TC-16 | Usage-limit wait and auto-continue | Waits until reset, then continues | N | M2 | 05 | B | `rate_limit_event.resetsAt`; `autoContinueAtUsageLimit`; not in `-p` |
| TC-17 | Turn and result tracking | Spinner stops exactly at turn end | R | M1 | 05 | B | One `result` per turn; `queued_turn_count` |
| TC-18 | Exit commands | `/exit`, `/quit`, `:q`, `:q!`, `:wq`, `:wq!` | N | M1 | 05 | B | |
| TC-19 | Exit cleanup and resume hint | Terminal restored; "resume with …" printed | N | M1 | 05 | B | |
| TC-20 | Esc precedence | Esc closes menus/dialogs before interrupting | N | M1 | 05 | B | `chat:cancel` |
| TC-21 | Command lifecycle states | queued/started/completed/cancelled | R | M3 | 05 | B | `command_lifecycle` (internal) |

## TR: transcript rendering (owner 03)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| TR-01 | Markdown | Headings, lists, quotes, tables, task lists, inline code | R | M1 | 03 | A | goldmark + custom ANSI renderer in Claude Code's look |
| TR-02 | Code block highlighting | Fenced code coloured | R | M1 | 03 | A | chroma v2, mapped to theme tokens |
| TR-03 | `syntaxHighlightingDisabled` | Plain code when disabled | R | M1 | 03 | B | |
| TR-04 | `maxProseWidth` | Prose wrapped narrower than the terminal | R | M2 | 03 | A | |
| TR-05 | Streaming markdown | Text appears smoothly; closed blocks committed while streaming | R | M1 | 03 | A | Block cache; progressive commit |
| TR-06 | User prompt | Prompt with paste and image chips | R | M1 | 03 | B | |
| TR-07 | Assistant text | | R | M1 | 03 | B | |
| TR-08 | Thinking | Collapsed "Thought for Ns"; gray italic in verbose; summaries | R | M1 | 03 | B | `showThinkingSummaries`, `--thinking-display`; S13 |
| TR-09 | Bash tool | Command, collapsed output, stderr, exit code, interrupted, background | R | M1 | 03 | B | |
| TR-10 | Read tool | File, line range, images | R | M1 | 03 | B | |
| TR-11 | Write tool | New file preview or diff | R | M1 | 03 | B | |
| TR-12 | Edit/MultiEdit tool | Word-level diff | R | M1 | 03 | B | From `structuredPatch` |
| TR-13 | Glob/Grep tools | Pattern, counts, paths | R | M1 | 03 | B | |
| TR-14 | WebFetch/WebSearch tools | URL or query, result summary | R | M1 | 03 | B | |
| TR-15 | Agent/Task tool | Subagent with nested items | R | M1 | 03 | B | `parent_tool_use_id`; S12 |
| TR-16 | TodoWrite / TaskCreate / TaskUpdate / TaskList / TaskGet | Checklist updates | R | M1 | 03 | B | Feeds the CH-10 panel |
| TR-17 | NotebookEdit tool | Cell edits | R | M2 | 03 | B | |
| TR-18 | ExitPlanMode / EnterPlanMode | Plan rendered as markdown, with plan file path | R | M1 | 03 | B | |
| TR-19 | AskUserQuestion result | Questions and chosen answers | R | M1 | 03 | B | |
| TR-20 | MCP tool calls | Generic renderer; "Called slack 3 times" grouping | R | M1 | 03 | B | `mcp__server__tool` |
| TR-21 | Skill tool | Skill name and arguments | R | M1 | 03 | B | |
| TR-22 | Background shell tools | TaskStop, TaskOutput, BashOutput, Monitor | R | M2 | 03 | B | |
| TR-23 | Other tools | LSP, PowerShell, ToolSearch, Enter/ExitWorktree, Cron*, ScheduleWakeup, RemoteTrigger, Workflow, SendMessage, ListAgents, PushNotification, SendUserMessage, SendUserFile, StructuredOutput, ReportFindings, ProposeGoal, EndConversation | R | M2 | 03 | B | One renderer per ContentKey |
| TR-24 | MCP resource tools | ListMcpResources, ReadMcpResource | R | M2 | 03 | B | |
| TR-25 | Unknown-tool fallback | Name, input JSON, result | R | M1 | 03 | B | |
| TR-26 | Tool errors and structured results | Red error state; rich output | R | M1 | 03 | B | `is_error`, `tool_use_result` |
| TR-27 | Rejected/denied calls | Dimmed rejected diffs; denial lines | R | M1 | 03 | B | `permission_denials` |
| TR-28 | Aborted markers | Partial response marked aborted | R | M1 | 03 | B | `assistant.aborted` |
| TR-29 | `supersedes` | Replaced content marked "response replaced" | R | M2 | 03 | B | Content may already be in scrollback |
| TR-30 | Assistant error categories | Auth, rate limit, overloaded, billing, max_output_tokens… | R | M1 | 03 | B | |
| TR-31 | Compact boundary | "Conversation compacted" separator | R | M1 | 03 | B | `compact_boundary` |
| TR-32 | Compacting status | "Compacting conversation…" | R | M1 | 03 | B | `status: compacting` |
| TR-33 | API retry | "Retrying in Ns (attempt x/y)" | R | M1 | 03 | B | `api_retry` |
| TR-34 | Informational messages | Warnings, hook systemMessage, by level | R | M1 | 03 | B | `informational` |
| TR-35 | Hook lines | Hook running/output with statusMessage | R | M2 | 03 | B | `hook_*`; needs `--include-hook-events` |
| TR-36 | Permission-denied notices | Auto-mode classifier denials | R | M1 | 03 | B | `system/permission_denied` |
| TR-37 | Model refusal fallback | Refusal and fallback-model notice | R | M2 | 03 | B | `model_refusal_*` |
| TR-38 | Local command output | Output of `/context`, `/usage` and similar | R | M1 | 03 | B | Synthetic assistant message plus `local_command`; S8 |
| TR-39 | Task lifecycle lines | Background task started/progress/done | R | M2 | 03 | B | `task_*` |
| TR-40 | Rate-limit warnings inline | Approaching or over the limit | R | M1 | 03 | B | `rate_limit_event` |
| TR-41 | Tool-use summaries | Condensed summary lines | R | M2 | 03 | B | `tool_use_summary` |
| TR-42 | Tool progress | Elapsed time on running tools | R | M1 | 03 | B | `tool_progress` |
| TR-43 | Memory recall display | | R | M3 | 03 | B | `memory_recall` |
| TR-44 | Conversation reset view | Fresh transcript after `/clear` | R | M1 | 03 | B | `conversation_reset`, reprint |
| TR-45 | Spinner verbs | ~189 verbs, customizable | R | M1 | 03 | B | `spinnerVerbs{mode, verbs}`; don't copy the list from the binary (licensing) |
| TR-46 | Spinner tips | Rotating tips | R | M1 | 03 | B | `spinnerTipsEnabled`, `spinnerTipsOverride{tips, tipsFile}` |
| TR-47 | Spinner shimmer / reduced motion | Animation honours preference | R | M2 | 03 | B | `prefersReducedMotion` |
| TR-48 | Spinner details | Elapsed time, ↑↓ token counts, "esc to interrupt" | R | M1 | 03 | B | |
| TR-49 | Thinking token counter | | R | M1 | 03 | B | `thinking_tokens` |
| TR-50 | Turn duration line | "Cooked for 1m 6s · done 6:05 PM" | R | M1 | 03 | B | `showTurnDuration`, `timeFormat`, `timeZone` |
| TR-51 | Message timestamps | | R | M2 | 03 | B | `showMessageTimestamps` |
| TR-52 | Transcript store | None (single source of truth) | N | M1 | 03 | B | Stable IDs, revisions, nesting |
| TR-53 | Commit policy | Finished items go to native scrollback with no ghost lines | N | M1 | 03 | B | Uses 01's `Print`/`Reprint`; S15 |
| TR-54 | Live-area overflow | "+N more running"; streaming tail | N | M1 | 03 | B | Budget H − 1 rows |
| TR-55 | Collapsible items | Long output collapsed; expandable in ctrl+o | R | M1 | 03 | B | |
| TR-56 | File hyperlinks | Paths clickable (OSC 8) | R | M2 | 03 | A | |
| TR-57 | Wide-character widths | CJK and emoji align correctly | N | M1 | 03 | A | x/ansi |
| TR-58 | Image placeholders | `[Image]` in the transcript | R | M1 | 03 | B | |
| TR-59 | Diffview widget | Gutters, word-level highlights | N | M1 | 03 | A | `pkg/ui/diffview`; also used by PD-03 |
| TR-60 | Highlight caches and size caps | Huge files stay fast | N | M1 | 03 | A | Skip highlighting above ~2k lines |

## VW: views (owner 03; screen reader 07; fullscreen 12)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| VW-01 | Ctrl+O transcript viewer | "Showing detailed transcript" (alt screen) | R | M2 | 03 | B | `app:toggleTranscript` |
| VW-02 | Transcript navigation keys | less-style: j/k, g/G, Space, b, Ctrl+U/D/B/F/N/P, arrows, Home/End | N | M2 | 03 | B | Transcript context |
| VW-03 | Show all | Ctrl+E expands every collapsed item | R | M2 | 03 | B | `transcript:toggleShowAll` |
| VW-04 | Transcript search | `/`, then n/N | N | M2 | 03 | B | |
| VW-05 | Timestamps and model per message | | R | M2 | 03 | B | |
| VW-06 | Open in editor / back to scrollback | `v` opens in editor; `[` returns to scrollback | N | M3 | 03 | B | |
| VW-07 | Exit the viewer | Ctrl+C, Esc, q | N | M2 | 03 | B | |
| VW-08 | Verbose | `verbose` setting / `--verbose` | R | M1 | 03 | B | |
| VW-09 | `viewMode` | default/verbose/focus | R | M2 | 03 | B | |
| VW-10 | Focus view | `/focus`: prompt, summary and response only | R | M2 | 03 | B | Headless twin exists |
| VW-11 | Brief mode | Ctrl+Shift+B, `/brief` | R | M2 | 03 | B | `app:toggleBrief`; SendUserMessage |
| VW-12 | `defaultView` | Start in chat or transcript | N | M2 | 03 | B | |
| VW-13 | Inline (classic) renderer default | Native scrollback, select and copy work | N | M1 | 03 | B | Layout primitives by 01 |
| VW-14 | Screen-reader flat mode | Linear output | N | M2 | 07 | B | `--ax-screen-reader`, `axScreenReader`, `CLAUDE_AX_SCREEN_READER` |
| VW-15 | Fullscreen renderer | `/tui fullscreen`, `tui` setting | N | M3 | 12 | B | |
| VW-16 | Fullscreen scrolling | Virtual scroll, PgUp/PgDn, Ctrl+Home/End | N | M3 | 12 | B | Scroll context |
| VW-17 | Mouse wheel | Wheel with acceleration | N | M3 | 12 | B | `wheelScrollAccelerationEnabled` |
| VW-18 | Selection and copy | Drag, double/triple click, Shift+arrows extend, copy-on-select, Ctrl+Shift+C / Cmd+C | N | M3 | 12 | B | `copyOnSelect` |
| VW-19 | Jump to bottom | Unread count; auto-follow | N | M3 | 12 | B | `autoScrollEnabled` |
| VW-20 | Sticky prompt header | | N | M3 | 12 | B | |
| VW-21 | `/scroll-speed` | Adjust scroll speed | N | M3 | 12 | B | `CLAUDE_CODE_SCROLL_SPEED` |
| VW-22 | Fullscreen history-search dialog | | N | M3 | 12 | B | |
| VW-23 | Renderer env vars | `CLAUDE_CODE_DISABLE_MOUSE`, `DISABLE_ALTERNATE_SCREEN`, `NO_FLICKER`, `DISABLE_VIRTUAL_SCROLL` | N | M3 | 12 | B | |
| VW-24 | Sidebar slots | Panes for mods (mantle) | N | M3 | 12 | B | lipgloss Compositor |

## CH: chrome & terminal (owner 07)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| CH-01 | Prompt box frame | | N | M1 | 07 | B | |
| CH-02 | `/color` | Prompt bar colour | N | M2 | 07 | B | `set_color`; headless twin exists |
| CH-03 | Session name on prompt bar | From `-n` / `/rename` | R | M2 | 07 | B | |
| CH-04 | Mode-specific border | Bash mode and plan mode colours | N | M1 | 07 | B | |
| CH-05 | Permission-mode indicator | "⏸ manual mode on", "⏵⏵ accept edits on", plan/auto/bypass | R | M1 | 07 | B | |
| CH-06 | Key hints | "? for shortcuts", "esc to interrupt" | N | M1 | 07 | B | |
| CH-07 | Notices area | Informational and notification frames with timeouts | R | M1 | 07 | B | `system/notification` |
| CH-08 | `companyAnnouncements` | | N | M2 | 07 | B | |
| CH-09 | Startup notices | Model source, login expiry, MCP needs-auth count, auto-mode notice | R | M2 | 07 | B | |
| CH-10 | Todo panel | Ctrl+T, up to 5 items, remembers expanded state | R | M1 | 07 | B | `app:toggleTodos`, `todoFeatureEnabled` |
| CH-11 | Subagent panel | Rows below the prompt; Enter opens transcript; x stops; 30 s linger | R | M2 | 07 | B | |
| CH-12 | `subagentStatusLine` | Custom status line per subagent | N | M2 | 07 | A | |
| CH-13 | `/tasks` (`/bashes`) | Background work list with output and stop | N | M2 | 07 | B | `get_task_output`, `stop_task` |
| CH-14 | Background tasks indicator | | R | M2 | 07 | B | `background_tasks_changed` |
| CH-15 | PR/MR badge | Coloured PR status in the footer | N | M2 | 07 | A | gh/glab, cached; `prStatusFooterEnabled`, `prUrlTemplate` |
| CH-16 | Issue and custom footer links | `owner/repo#123` | N | M2 | 07 | B | `footerLinksRegexes`, `FORCE_HYPERLINK` |
| CH-17 | Footer navigation | Up/Down, Left/Right, Enter open, Esc, x close | N | M2 | 07 | B | Footer context |
| CH-18 | statusLine runner | User script output under the prompt | N | M1 | 07 | A | JSON on stdin, `COLUMNS`/`LINES`, `padding`, `refreshInterval`, 300 ms debounce, timeout, ANSI and OSC 8 |
| CH-19 | statusLine payload | All documented fields | N | M1 | 07 | A | model, cwd, workspace, cost, context_window, effort, thinking, fast_mode, rate_limits, prompt_cache, session_id/name, vim.mode, agent, pr, worktree, output_style, version, transcript_path, exceeds_200k_tokens |
| CH-20 | Welcome banner | Startup header | N | M2 | 07 | B | Our own art, not the Claude mascot (licensing) |
| CH-21 | Release notes on update | "What's new" after a version change | N | M2 | 07 | B | |
| CH-22 | `/release-notes` | | N | M2 | 07 | B | |
| CH-23 | Terminal title | Session title or name | N | M1 | 07 | A | `terminalTitleFromRename`, `CLAUDE_CODE_DISABLE_TERMINAL_TITLE` |
| CH-24 | Terminal progress bar | OSC 9;4 | N | M1 | 07 | A | `terminalProgressBarEnabled` |
| CH-25 | Desktop notifications | iTerm2, Kitty, Ghostty, bell | N | M2 | 07 | A | `preferredNotifChannel` |
| CH-26 | Notification triggers | Input needed, turn done while idle, permission prompt | N | M2 | 07 | B | `inputNeededNotifEnabled`; S4 |
| CH-27 | Hook `terminalSequence` | Hook-requested terminal sequences honoured | N | M3 | 07 | B | Ignored by the engine in `-p`; recover from hook events (see GAP-06) |
| CH-28 | Clipboard service | Copy works locally and over SSH/tmux | N | M1 | 07 | A | `pbcopy`; OSC 52 when `SSH_TTY` is set |
| CH-29 | Ctrl+Z suspend | `fg` resumes | N | M2 | 07 | B | S16 |
| CH-30 | Redraw / clear screen | Ctrl+L `app:redraw`; Cmd+K `chat:clearScreen` | N | M1 | 07 | B | Uses `Reprint` |

## PD: permissions, dialogs & startup gates (owner 05)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| PD-01 | Generic permission dialog | Title, display name, description | N | M1 | 05 | B | `can_use_tool` |
| PD-02 | Bash variant | Command, description, "unsandboxed" label | N | M1 | 05 | B | |
| PD-03 | Edit variant | Diff preview | N | M1 | 05 | B | Uses `pkg/ui/diffview` |
| PD-04 | Write variant | File preview | N | M1 | 05 | B | |
| PD-05 | Read / blocked path / working dir variant | Offer to add a directory | N | M1 | 05 | B | `blocked_path` |
| PD-06 | WebFetch variant | Domain | N | M1 | 05 | B | |
| PD-07 | MCP tool variant | Server name and source | N | M1 | 05 | B | `mcp_server` |
| PD-08 | Yes / Yes don't ask again / No | "Don't ask again for X" | N | M1 | 05 | B | `permission_suggestions` → `updatedPermissions` |
| PD-09 | "Yes, and switch to auto mode" | | N | M2 | 05 | B | `setMode` update |
| PD-10 | Deny with feedback | "No, and tell Claude what to do differently" | N | M1 | 05 | B | `deny{message}` |
| PD-11 | Tab to amend | Add a comment or amend the input | N | M2 | 05 | B | |
| PD-12 | Shift+Tab inside the dialog | Change mode / allow for this session | N | M2 | 05 | B | |
| PD-13 | Esc declines | | N | M1 | 05 | B | |
| PD-14 | One-time-only prompts | No "always" option; default to No | N | M1 | 05 | B | `suppress_always_allow_rule`, `default_to_no` |
| PD-15 | `.claude` folder session grants | | N | M2 | 05 | B | |
| PD-16 | Attribution | Which subagent or engine asks ("Builder <id>") | N | M1 | 05 | B | `agent_id`, EngineID |
| PD-17 | Cancelled requests close dialogs | | N | M1 | 05 | B | `control_cancel_request` |
| PD-18 | Recover pending prompts | Prompts survive a UI hiccup | N | M2 | 05 | B | Re-send `initialize` → `pending_permission_requests` |
| PD-19 | Shift+Tab mode cycle | default → acceptEdits → plan → bypass/auto | N | M1 | 05 | B | `set_permission_mode`; `chat:cycleMode` |
| PD-20 | Bypass availability | Only with `--allow-dangerously-skip-permissions` | N | M1 | 05 | B | |
| PD-21 | Plan approval dialog | Approve + auto / accept edits / bypass; manual; keep planning with feedback | N | M1 | 05 | B | ExitPlanMode via `can_use_tool` |
| PD-22 | Edit the plan | Ctrl+G opens the plan in the editor | N | M2 | 05 | B | |
| PD-23 | Clear-context option on approval | | N | M2 | 05 | B | `showClearContextOnPlanAccept` |
| PD-24 | AskUserQuestion dialog | 1–4 questions, 2–4 options, multiSelect, Other | N | M1 | 05 | B | Answer via `updatedInput.answers` |
| PD-25 | AskUserQuestion previews | Markdown preview per option | N | M2 | 05 | B | |
| PD-26 | `askUserQuestionTimeout` | | N | M2 | 05 | B | |
| PD-27 | MCP elicitation, form mode | JSON schema → form | N | M2 | 05 | B | `elicitation` → accept/decline/cancel |
| PD-28 | MCP elicitation, URL mode | Open a URL to continue | N | M2 | 05 | B | |
| PD-29 | `request_user_dialog` kinds | Forwarded dialogs | N | M2 | 05 | B | Declare only implemented `supportedDialogKinds` |
| PD-30 | Workspace trust gate | Trust prompt before Claude runs in a folder | N | M1 | 05 | A | `-p` skips trust; read `hasTrustDialogAccepted`; mantle trust store; S10 |
| PD-31 | `.mcp.json` server approval gate | Approve project MCP servers | N | M1 | 05 | A | Pass via `--settings disabledMcpjsonServers`; S9 |
| PD-32 | `enableAllProjectMcpServers` | Honoured by the gate | N | M1 | 05 | A | |
| PD-33 | Bypass-mode warning | First-use warning | N | M1 | 05 | A | `skipDangerousModePermissionPrompt` |
| PD-34 | Auto-mode first-use prompt | | N | M2 | 05 | A | `skipAutoPermissionPrompt` |
| PD-35 | `ANTHROPIC_API_KEY` approval | One-time approve | N | M2 | 05 | A | |
| PD-36 | External CLAUDE.md import approval | | N | M2 | 05 | B | Needs a spike |
| PD-37 | Invalid settings notice | Bad settings file reported | N | M2 | 05 | A | `-p` silently ignores invalid files |
| PD-38 | Corrupted `~/.claude.json` notice | Report only; never write the file | N | M3 | 05 | A | |
| PD-39 | Sandbox network-host prompt | | N | M2 | 05 | B | Routing in headless unclear |
| PD-40 | Sandbox unsandboxed-retry prompt | | N | M2 | 05 | B | |
| PD-41 | Confirmation keys | Enter yes, Esc no, Up/Down, Tab next field, Space toggle | N | M1 | 05 | B | Confirmation context |
| PD-42 | `dialogExpiry` | Held dialogs expire | N | M3 | 05 | B | |
| PD-43 | No spawn before gates | None (security guarantee) | N | M1 | 05 | B | Test asserts it |
| PD-44 | Workflow launch approval | | N | M2 | 05 | B | `can_use_tool` for Workflow |
| PD-45 | Dialog view-models | None (pure, story-tested) | N | M1 | 05 | A | |

## SE: sessions (owner 06)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| SE-01 | Tolerant JSONL reader | None (reads past sessions) | N | M1 | 06 | A | user/assistant/attachment/system + metadata records |
| SE-02 | cwd → slug | | N | M1 | 06 | A | Non-alphanumerics → `-`; above 200 chars, append a hash |
| SE-03 | Subagent transcripts | Nested history on resume | N | M2 | 06 | A | `<sid>/subagents/agent-*.jsonl` + `.meta.json` |
| SE-04 | Large tool results | Full output on resume | N | M2 | 06 | A | `<sid>/tool-results/*.txt` |
| SE-05 | Session index cache | Fast picker | N | M1 | 06 | A | Keyed by (path, size, mtime); reads last-prompt, ai-title, cost-state from file tails |
| SE-06 | Normalizer to transcript items | None | N | M1 | 06 | B | |
| SE-07 | Resume | Prior history printed, then the session continues | N | M1 | 06 | B | History is not replayed by the engine; spawn `--resume=` |
| SE-08 | `-c` / `--continue` | Most recent session here | N | M1 | 06 | B | Includes `sdk-cli` sessions |
| SE-09 | `-r <id\|search>` | | N | M1 | 06 | B | |
| SE-10 | `--fork-session` / `--session-id` | | N | M2 | 06 | B | |
| SE-11 | `/resume` (`/continue`) picker | List, search, Space preview | N | M1 | 06 | B | |
| SE-12 | Picker extras | Ctrl+R rename, Ctrl+A all projects, Ctrl+W worktrees, Ctrl+B branch filter, PR URL search | N | M2 | 06 | B | |
| SE-13 | Shows headless sessions | mantle sessions are listed (Claude Code's own picker hides them) | N | M1 | 06 | A | S5 |
| SE-14 | Grouping | By project and date | N | M2 | 06 | B | |
| SE-15 | `/clear` (`/reset`, `/new`) | New conversation; the old one stays resumable | R | M1 | 06 | B | `conversation_reset` + reprint |
| SE-16 | `/compact [instructions]` | Summarize to free context | E | M1 | 06 | B | |
| SE-17 | `/rename [name]` | | N | M2 | 06 | B | `rename_session` |
| SE-18 | Session titles | AI title for mantle sessions | N | M2 | 06 | B | `generate_session_title` (headless runs get none by default) |
| SE-19 | `/branch [name]` | Branch the conversation here | N | M2 | 06 | B | `--fork-session` + `--resume-session-at` |
| SE-20 | `/fork [prompt]` | Copy the conversation into a new session | N | M2 | 06 | B | `fork_conversation` (unstable) or a forked engine; background variant is H |
| SE-21 | `/subtask <task>` | Subagent with full context | H | M2 | 06 | B | local-jsx; no headless twin |
| SE-22 | `/rewind` (`/checkpoint`, `/undo`), Esc Esc | Message selector | N | M2 | 06 | B | MessageSelector context; S7 |
| SE-23 | Rewind: code + conversation | | N | M2 | 06 | B | `rewind_files` + `--resume-session-at` |
| SE-24 | Rewind: conversation only | | N | M2 | 06 | B | `--resume-session-at` / `--resume-drops-turn` |
| SE-25 | Rewind: code only | | N | M2 | 06 | B | `rewind_files` (dry run first) |
| SE-26 | Summarize from here / up to here | | N | M3 | 06 | B | Not exposed; may need `rewind_conversation` (unstable) |
| SE-27 | Restore the pre-`/clear` session | | N | M2 | 06 | B | |
| SE-28 | `/export [filename]` | To a file or the clipboard | N | M2 | 06 | B | `export_conversation` (unstable) or render from JSONL |
| SE-29 | `/copy [N]` | Copy the last or Nth response; code-block picker; `w` writes a file | N | M2 | 06 | B | |
| SE-30 | `/recap` | One-line recap | E | M2 | 06 | B | Headless local command |
| SE-31 | Away summary | Recap after 3+ minutes away | N | M2 | 06 | B | `awaySummaryEnabled`; not automatic in `-p` |
| SE-32 | `/btw` side question | Overlay; no tools; history, c copy, f fork, x clear | N | M2 | 06 | B | `side_question` (unstable) or a forked one-shot engine |
| SE-33 | `/diff` | Uncommitted changes and per-turn diffs | N | M2 | 06 | B | `get_workspace_diff` (unstable) or `git diff` |
| SE-34 | Diff dialog/panel keys | Source/file navigation; Ctrl+X B base cycle; noise filter; pre-session toggle | N | M2 | 06 | B | DiffDialog/DiffPanel contexts |
| SE-35 | `/goal [condition\|clear]` | | E | M2 | 06 | B | Headless local |
| SE-36 | `/plan [open\|desc]` | Enter plan mode or view the plan | N | M2 | 06 | B | |
| SE-37 | `/add-dir <path>` | | E | M2 | 06 | B | Headless local; also `register_repo_root` |
| SE-38 | `/cd <path>` | Move the session to another directory | N | M2 | 06 | B | `set_cwd` (unstable) or engine restart in the new cwd |
| SE-39 | Resume-from-summary dialog | Offered when idle >1 h and >100k tokens | N | M3 | 06 | B | |
| SE-40 | Generic H handoff | Any panel not yet native opens in real Claude Code, then returns | H | M1 | 06 | B | Stop engine → `tea.ExecProcess("claude --resume <sid>")` → restart engine |
| SE-41 | Session switch = engine restart | | N | M1 | 06 | B | |
| SE-42 | Permission mode on resume | Follows engine rules | E | M2 | 06 | B | Not restored except plan mode |
| SE-43 | Conversation tree resolution | Correct branch shown on resume | N | M1 | 06 | A | `parentUuid` chain from `last-prompt.leafUuid` |

## CU: context & usage (owner 06)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| CU-01 | `/context` grid | Coloured context-usage grid | N | M2 | 06 | B | `get_context_usage` |
| CU-02 | `/context all` | Full breakdown | N | M2 | 06 | B | `detail:"full"` |
| CU-03 | `/usage` (`/cost`, `/stats`) | Cost, plan-usage bars, activity stats | N | M2 | 06 | B | `get_usage`, `get_session_cost` |
| CU-04 | Usage panel keys | d/w period, r retry, t sort by tokens | N | M2 | 06 | B | Settings context |
| CU-05 | Cost display | Session cost | R | M1 | 06 | B | `result.total_cost_usd`, `modelUsage` |
| CU-06 | Rate-limit state | Utilization and reset time | R | M1 | 06 | B | `rate_limit_event` |
| CU-07 | Auto-compact warning | "Context low" percentage | R | M1 | 06 | B | |
| CU-08 | `/autocompact [auto\|tokens]` | | E | M2 | 08 | B | Headless local |
| CU-09 | Context % for the statusLine | `context_window.used/remaining_percentage` | R | M1 | 06 | B | The user's statusline uses these |
| CU-10 | Cost warnings | | R | M3 | 06 | B | `DISABLE_COST_WARNINGS` |
| CU-11 | Local JSONL activity stats | | N | M3 | 06 | A | |

## ST: settings & model panels (owner 08)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| ST-01 | `/model` picker | Models with descriptions | N | M1 | 08 | B | `list_models` / `initialize.models` |
| ST-02 | Effort and session-only in the picker | ←/→ effort; `s` this session only | N | M1 | 08 | B | ModelPicker context |
| ST-03 | `/model <name>` | Direct switch | E | M1 | 08 | B | |
| ST-04 | Persist model like Claude Code | | N | M1 | 08 | B | `set_model`; per-model `modelSettings` |
| ST-05 | Default row / reset | | N | M1 | 08 | B | `set_model(null)` |
| ST-06 | Meta+P model picker | `chat:modelPicker` | N | M1 | 08 | B | |
| ST-07 | `/effort` slider | low…max; Tab toggles ultracode; `s` | N | M2 | 08 | B | EffortSlider context |
| ST-08 | `/effort <lvl\|ultracode on/off\|status>` | | E | M2 | 08 | B | Headless local |
| ST-09 | Increase/decrease effort keys | `chat:increaseEffort`/`decreaseEffort` | N | M2 | 08 | B | |
| ST-10 | `/fast [on\|off]`, Meta+O | Fast mode | N | M2 | 08 | B | `fast_mode_state`; in `-p` needs `fastMode` in `--settings` |
| ST-11 | Thinking toggle Meta+T | | N | M2 | 08 | B | `set_max_thinking_tokens` / `apply_flag_settings`; can't be turned off on some models |
| ST-12 | Default to newer model, Ctrl+Y | `chat:defaultToNewerModel` | N | M3 | 08 | B | |
| ST-13 | Model-switch cache warning | Confirm a switch that drops the warm cache | N | M2 | 08 | B | |
| ST-14 | `/config` panel | Tabs Config / Status / Usage / Stats | N | M2 | 08 | B | |
| ST-15 | `/config key=value` | | E | M1 | 08 | B | Headless local |
| ST-16 | mantle section in `/config` | Generated from SettingSpecs | N | M2 | 08 | B | |
| ST-17 | Settings keys | `/` search, j/k, paging, Space/Enter | N | M2 | 08 | B | Settings context |
| ST-18 | `/status` | Version, model, account, connectivity, tools, MCP | N | M2 | 08 | B | |
| ST-19 | `/theme` picker | Live preview of 6 built-ins + custom | N | M2 | 08 | B | ThemePicker context |
| ST-20 | Theme picker extras | Ctrl+T toggles syntax highlighting; Ctrl+E edits a custom theme | N | M2 | 08 | B | |
| ST-21 | `/output-style [style]` | Picker | N | M2 | 08 | B | `update_settings` / `reload_output_styles`; headless twin with an argument |
| ST-22 | `/permissions` (`/allowed-tools`) | Rules by scope; add/remove allow/deny/ask; working dirs | N | M2 | 08 | B | `list_permission_rules`; atomic settings writer |
| ST-23 | `/permissions` auto-mode tab | Auto-mode denials and rules | N | M2 | 08 | B | |
| ST-24 | `/keybindings` | Opens the keybindings file | N | M2 | 08 | B | |
| ST-25 | `/terminal-setup` | Installs Shift+Enter / Option+Enter mappings per terminal | N | M2 | 08 | A | iTerm2, VS Code, Ghostty, etc. |
| ST-26 | `/vim` | Toggle `editorMode` (moved to `/config`) | N | M2 | 08 | B | |
| ST-27 | `/advisor <model\|off>` | | E | M2 | 08 | B | Headless local |
| ST-28 | `/help` | Commands and shortcuts | N | M1 | 08 | B | |
| ST-29 | Help keys | Esc closes | N | M1 | 08 | B | Help context |
| ST-30 | `/version` | mantle + engine versions | N | M1 | 08 | B | |
| ST-31 | `/sandbox` | Sandbox status and toggle | N | M2 | 08 | B | Settings `sandbox.enabled`; `get_sandbox_dialog` (unstable) |
| ST-32 | `/restart [update]` | Restart mantle and the engine | N | M2 | 08 | B | Exit 75 + `--resume` |
| ST-33 | Write-back compatibility | Values mantle writes are read correctly by Claude Code | N | M2 | 08 | B | Temp-HOME test |
| ST-34 | Model and effort data layer | None | N | M1 | 08 | A | `ModelInfo` → picker items |
| ST-35 | `/config` option table | None | N | M2 | 08 | A | Keys, types, defaults |
| ST-36 | `/status` data assembly | None | N | M2 | 08 | A | |

## EC: ecosystem panels + engine-passthrough commands (owner 09)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| EC-01 | `/mcp` panel | Servers, status, tool counts, warnings | N | M2 | 09 | B | `mcp_status`; headless text form exists |
| EC-02 | MCP reconnect / enable / disable | | N | M2 | 09 | B | `mcp_reconnect`, `mcp_toggle` |
| EC-03 | MCP OAuth | Authenticate a server | N | M2 | 09 | B | `tea.ExecProcess("claude mcp login [--no-browser] <name>")`, then reconnect |
| EC-04 | MCP clear auth | | N | M2 | 09 | B | `claude mcp logout` |
| EC-05 | MCP add/remove/get | Includes add-json, add-from-claude-desktop, reset-project-choices | N | M2 | 09 | A | `claude mcp …` wrappers |
| EC-06 | `/plugin` manager | Discover / Installed tabs, marketplaces | N | M2 | 09 | B | `claude plugin … --json` |
| EC-07 | Plugin keys | Space toggle, i install, f favorite, Ctrl+S cycle marketplace | N | M2 | 09 | B | Plugin context |
| EC-08 | Plugin actions | install/uninstall/enable/disable/update/configure, then reload | N | M2 | 09 | B | `reload_plugins` |
| EC-09 | `/reload-plugins [--force]`, `/reload-skills` | | E | M1 | 09 | B | Headless local |
| EC-10 | `/skills` | List with visibility cycling | N | M2 | 09 | B | `skillOverrides` via `apply_flag_settings` |
| EC-11 | `/skill-doctor` | Unused skills costing context | E | M2 | 09 | B | Headless local |
| EC-12 | `/hooks` | Hook configuration browser | N | M2 | 09 | B | `get_hooks_listing` |
| EC-13 | `/agents` | Agent definitions panel | N | M2 | 09 | B | Removed in 2.1.288 (prints a reminder); mantle reads agent files |
| EC-14 | `/memory` | CLAUDE.md files; open in editor; toggle auto memory | N | M2 | 09 | B | `get_memory_dialog` (unstable) or file discovery |
| EC-15 | `/pause-memory` (`/memory-pause`, `/toggle-memory`) | | N | M2 | 09 | B | Not headless |
| EC-16 | `/doctor` (`/checkup`) | Health check | N | M2 | 09 | B | Bundled skill (E) + `claude doctor` + mantle checks |
| EC-17 | `/login` | | N | M2 | 09 | B | Exec `claude auth login`, then restart the engine |
| EC-18 | `/logout` | | N | M2 | 09 | B | Exec `claude auth logout` |
| EC-19 | `/upgrade` | Update Claude Code | N | M2 | 09 | B | Exec `claude update`; plan upgrade is H |
| EC-20 | `/feedback`, `/bug` (`/share`, `/report`) | Consent screen, report | H | M2 | 09 | B | |
| EC-21 | `/import [codex\|gemini\|cursor]` | | E | M2 | 09 | B | Headless lists; confirm with `claude import` |
| EC-22 | `/init` | Create CLAUDE.md | E | M1 | 09 | B | Prompt type |
| EC-23 | `/insights` | Session analysis report | E | M2 | 09 | B | Prompt type |
| EC-24 | `/statusline` | Status line setup agent | H | M2 | 09 | B | Prompt type with `disableNonInteractive` |
| EC-25 | `/team-onboarding` | | E | M2 | 09 | B | Prompt type |
| EC-26 | `/commit-push-pr`, `/security-review` | | E | M1 | 09 | B | Prompt type |
| EC-27 | Bundled skills | code-review (review), simplify, commit, pr, verify, debug, batch, loop (proactive), schedule (routines), run, run-skill-generator, fewer-permission-prompts, claude-code-docs, update-config, keybindings-help, explain-usage, memory-types, claude-api, claude-in-chrome, design, design-sync, setup-claude, artifact-*, dataviz, prototype, whiteboard, workshop, workflow-authoring, plugin-authoring, cowork-plugin, deep-research | E | M1 | 09 | B | Typed as `/name args` |
| EC-28 | Custom commands and skills | `.claude/commands/*.md`, user/project skills | E | M1 | 09 | B | |
| EC-29 | Plugin commands | `/plugin:name` | E | M1 | 09 | B | |
| EC-30 | MCP prompts as commands | `/server:prompt (MCP)`, `/mcp__srv__prompt` | E | M1 | 09 | B | |
| EC-31 | Synced claude.ai skills | `anthropic-skills:*` | E | M1 | 09 | B | |
| EC-32 | `/list-agents` (`/peers`) | | E | M2 | 09 | B | Headless local |
| EC-33 | `/auto-mode-setup` | | E | M2 | 09 | B | Headless local |
| EC-34 | `/ultrareview` | Cloud multi-agent review | E | M2 | 09 | B | Headless local; billed |
| EC-35 | `/heapdump` (hidden) | | E | M3 | 09 | B | |
| EC-36 | `/design-consent`, `/design-revoke` (hidden) | | E | M3 | 09 | B | |
| EC-37 | `/powerup` | Interactive lessons | H | M3 | 09 | B | Novelty |
| EC-38 | `/stickers`, `/radio` | | H | M3 | 09 | B | Novelty |
| EC-39 | `/wellbeing` (`/breaks`, `/break-reminder`, `/downtime`) | | H | M3 | 09 | B | `breakReminder`, `quietHours` |
| EC-40 | `/workflows` | Workflow progress view (pause, restart agent, save) | H | M2 | 09 | B | Undocumented UI |
| EC-41 | Plugin and MCP errors surfaced | | R | M1 | 09 | B | `init.plugin_errors`, `mcp_server_errors` |
| EC-42 | Agent discovery | None | N | M2 | 09 | A | `~/.claude/agents`, `.claude/agents`, plugin agents |
| EC-43 | Skill discovery | None | N | M2 | 09 | A | |
| EC-44 | Memory-file discovery | None | N | M2 | 09 | A | CLAUDE.md, CLAUDE.local.md, rules, imports |
| EC-45 | Hooks listing parsing | None | N | M2 | 09 | A | |
| EC-46 | `claude auth status --json` wrapper | None | N | M2 | 09 | A | |
| EC-47 | `claude doctor` wrapper | None | N | M2 | 09 | A | |

## CF: config compatibility (owner 01)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| CF-01 | Read all settings scopes | Same behaviour as Claude Code | N | M1 | 01 | B | managed > flag > local > project > user |
| CF-02 | Read `~/.claude.json` (read-only) | Trust, account display, per-project data | N | M1 | 01 | B | Never write it (no lockfile in 2.1.288) |
| CF-03 | mantle settings | `~/.mantle/settings.json` | N | M1 | 01 | B | Never put mantle keys in `~/.claude/settings.json` (strict schema) |
| CF-04 | Settings writer | | N | M1 | 01 | B | Atomic; flock; re-read and merge |
| CF-05 | Hot reload | Settings changes apply live | N | M1 | 01 | B | fsnotify → `SettingsMsg` |
| CF-06 | Honour UI settings keys | See "UI settings keys index" | R | M1 | 01 | B | Each key is implemented by the plan listed in the index |
| CF-07 | `keybindings.json` parsing | Same file works in mantle | N | M1 | 01 | B | Contexts, bindings map, modifiers, special keys, `null` unbinds |
| CF-08 | Reserved-key validation | | N | M1 | 01 | B | ctrl+c/d/m/i/h/[, `ctrl+\`, cmd+c/v/x/q/w… errors; ctrl+z warning |
| CF-09 | Default bindings table | Claude Code defaults out of the box | N | M1 | 01 | A | Action IDs verbatim |
| CF-10 | mantle keybindings | `~/.mantle/keybindings.json` with `mantle:*` actions | N | M1 | 01 | B | |
| CF-11 | Keybinding hot reload and conflict report | | N | M2 | 01 | B | |
| CF-12 | Built-in themes | auto, dark, light, light-/dark-daltonized, light-/dark-ansi | N | M1 | 01 | B | Our own palettes (licensing) |
| CF-13 | Custom themes | `~/.claude/themes/*.json` (`custom:*`), live reload | N | M2 | 01 | B | |
| CF-14 | Light/dark auto detection | | N | M1 | 01 | B | `BackgroundColorMsg.IsDark()` |
| CF-15 | Colour profile downgrade | `NO_COLOR`, `FORCE_COLOR`, 256/16 colours | N | M1 | 01 | B | |
| CF-16 | `CLAUDE_CONFIG_DIR` honoured | | N | M1 | 01 | B | |
| CF-17 | UI env vars | `DISABLE_TERMINAL_TITLE`, `SYNTAX_HIGHLIGHT`, `ACCESSIBILITY`, `SCROLL_SPEED`… | N | M2 | 01 | B | |
| CF-18 | Settings validation warnings | | N | M2 | 01 | B | |
| CF-19 | Managed settings sources | managed-settings.json, `.d/` drop-ins, MDM plist | N | M2 | 01 | B | |
| CF-20 | Chord engine | Space-separated chords with a 3 s timeout | N | M1 | 01 | B | |

## CLI: flags, subcommands, drift (owner 11)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| CLI-01 | Flag table | Every 2.1.288 flag classified | N | M1 | 11 | A | See "CLI flags index" |
| CLI-02 | Unknown flags forwarded verbatim | New claude flags keep working | N | M1 | 11 | A | |
| CLI-03 | Positional prompt | `mantle "fix the bug"` starts with that prompt | N | M1 | 11 | B | |
| CLI-04 | `-p` / `--print` | Execs `claude -p …` directly | N | M1 | 11 | A | Launcher-level passthrough |
| CLI-05 | Session flags | `-c`, `-r`, `--fork-session`, `--session-id`, `-n` | N | M1 | 11 | B | |
| CLI-06 | `-r` with no value | Opens the picker | N | M1 | 11 | B | |
| CLI-07 | Model and permission flags | `--model`, `--effort`, `--fallback-model`, `--permission-mode`, `--dangerously-skip-permissions`, `--allow-dangerously-skip-permissions` | E | M1 | 11 | A | Forwarded; mantle tracks the values |
| CLI-08 | Config, tool and system-prompt flags | `--add-dir`, `--agent(s)`, `--tools`, `--allowedTools`, `--disallowedTools`, `--mcp-config`, `--strict-mcp-config`, `--settings`, `--setting-sources`, `--plugin-dir/url`, `--system-prompt*`, `--append-system-prompt*`, `--betas`, `--autocompact`, … | E | M1 | 11 | A | Forwarded |
| CLI-09 | `--verbose`, `--debug`, `--debug-file` | | N | M1 | 11 | A | `--verbose` also sets the view |
| CLI-10 | `--ax-screen-reader` | | N | M2 | 11 | B | Consumed (VW-14) |
| CLI-11 | `--ide`, `--chrome`/`--no-chrome` | | E | M2 | 11 | A | Forwarded; IDE behaviour in `-p` unclear |
| CLI-12 | `--safe-mode`, `--bare`, `--restricted` | | E | M2 | 11 | A | Forwarded; `--bare` shows a warning |
| CLI-13 | `-v` / `--version` | mantle + claude versions | N | M1 | 11 | A | |
| CLI-14 | `-h` / `--help` | mantle help + claude flags | N | M1 | 11 | A | |
| CLI-15 | `-w` / `--worktree` | | E | M2 | 11 | A | Forwarded |
| CLI-16 | `--tmux` | | H | M2 | 11 | A | Interactive-only |
| CLI-17 | `--bg` / `--background` | | H | M2 | 11 | A | Not combinable with `-p` |
| CLI-18 | `--remote-control`, `--teleport`, `--cloud`, `--environment`, `--desktop`, `--from-pr`, `--file`, `--brief` | | H | M2 | 11 | A | Decide per flag; `--brief` may be forwarded |
| CLI-19 | Subcommand passthrough | `mantle mcp …` etc. exec claude | N | M1 | 11 | A | agents, attach, auth, auto-mode, doctor, gateway, import, install, logs, mcp, plugin(s), purge, respawn, rm, setup-token, stop/kill, ultrareview, update/upgrade |
| CLI-20 | Drift: `claude --help` flags | | N | M2 | 11 | A | |
| CLI-21 | Drift: `initialize.commands` vs slash index | | N | M2 | 11 | B | |
| CLI-22 | Drift: keybinding action IDs from the binary | | N | M2 | 11 | A | |
| CLI-23 | Drift: SDK control/message subtypes | | N | M2 | 11 | A | Uses 02's `sdk-diff` |
| CLI-24 | Drift: settings schema | | N | M2 | 11 | A | schemastore claude-code-settings.json |
| CLI-25 | Auto-run drift on engine version change | | N | M2 | 11 | B | |
| CLI-26 | Argument validation mirrors claude | e.g. `--input-format` requires `--print` | N | M3 | 11 | A | |
| CLI-27 | `cli.Parse` wiring | | N | M1 | 11 | B | Call site in `cmd/mantle-ui` |
| CLI-28 | `/install` and `claude install` | Install a native build | N | M2 | 11 | A | Passthrough |

## CL: cloud & product hand-offs (owner 09 or 11)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| CL-01 | `/remote-control` (`/rc`) | Remote Control | H | M2 | 09 | B | |
| CL-02 | `/teleport` (`/tp`) | | H | M2 | 09 | B | |
| CL-03 | `/desktop` (`/app`) | Continue in Claude Desktop | H | M2 | 09 | B | |
| CL-04 | `/mobile` (`/ios`, `/android`) | QR code | H | M3 | 09 | B | |
| CL-05 | `--cloud`, `/session` (`/remote`) | Cloud session URL and QR | H | M2 | 11 | B | |
| CL-06 | `/web-setup` | | H | M3 | 09 | B | |
| CL-07 | `/remote-env` | | H | M3 | 09 | B | |
| CL-08 | `/ultraplan` | | H | M2 | 09 | B | |
| CL-09 | `/autofix-pr` | | H | M2 | 09 | B | claude.ai only |
| CL-10 | Agent view `claude agents` | Background sessions table | H | M2 | 11 | B | `claude agents --json` for a future native view |
| CL-11 | attach / logs / stop / rm / respawn | | H | M2 | 11 | B | Passthrough |
| CL-12 | `/background` (`/bg`) | Send the session to the background | H | M2 | 09 | B | |
| CL-13 | `/stop` | Stop a background session | E | M2 | 09 | B | Headless local |
| CL-14 | `/chrome` | Claude in Chrome settings | H | M2 | 09 | B | `--chrome` forwarded |
| CL-15 | `/ide` | IDE integrations | H | M2 | 09 | B | `--ide` in `-p` unclear |
| CL-16 | `/artifacts`, Ctrl+] | Browse/open artifacts | H | M2 | 09 | B | `app:openArtifact` |
| CL-17 | `/design-login` | | H | M3 | 09 | B | |
| CL-18 | `/passes` | | H | M3 | 09 | B | |
| CL-19 | `/usage-credits` (`/extra-usage`) | | E | M2 | 09 | B | Headless local |
| CL-20 | `/rate-limit-options` | | H | M2 | 09 | B | |
| CL-21 | `/setup-bedrock`, `/setup-vertex` | | H | M3 | 09 | B | |
| CL-22 | `/install-github-app` | | H | M3 | 09 | B | |
| CL-23 | `/install-slack-app` | | H | M3 | 09 | B | local, not headless |
| CL-24 | `/privacy-settings` | | H | M2 | 09 | B | |
| CL-25 | `/cloud-plugins` | | H | M3 | 09 | B | |
| CL-26 | `/daemon` | Background services and routines | H | M3 | 09 | B | |
| CL-27 | `/pro-trial-expired` (hidden) | | H | M3 | 09 | B | |

## GAP: known gaps (X)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| GAP-01 | Voice dictation | `/voice`, hold Space to talk | X | – | 04 | – | claude.ai speech-to-text isn't exposed to headless clients |
| GAP-02 | Claude Code's own TypeScript mods drawing UI | Panes, bands, toasts, AbovePrompt/Pane contexts, the cc-plugin-diff UI, dev-mods, `claude plugin test` | X | – | 10 | – | Internal protocol; doesn't render in `-p`. mantle's `/mantle` mods replace this |
| GAP-03 | Agent-team split panes / `--teammate-mode` | | X | – | 05 | – | Teammates aren't spawned in `-p` |
| GAP-04 | Computer use | | X | – | 02 | – | Interactive sessions only |
| GAP-05 | Session-quality survey | "How is Claude doing?" | X | – | 07 | – | Deliberately omitted; `feedbackSurveyRate` ignored |
| GAP-06 | Hook `terminalSequence` | | X | – | 07 | – | Ignored in `-p`; partly recovered by CH-27 |
| GAP-07 | Fable usage-credits consent prompt | | X | – | 05 | – | Not forwarded to headless clients (unverified) |
| GAP-08 | Model-choice dialog after a safety refusal | | X | – | 03 | – | Partly shown via TR-37 |
| GAP-09 | Gated features | `app:toggleTerminal` (Meta+J), `/loops`, `chat:cycleProactivity`, `chat:attentionUp/Down` | X | – | 11 | – | Behind flags in 2.1.288; drift detection notices when they ship |
| GAP-10 | Claude Code's own `/resume` hides mantle sessions | | X | – | 02 | – | `sdk-cli` entrypoint (S5); mitigated by SE-13 |
| GAP-11 | Deep links (`claude-cli://`) | | X | – | 11 | – | Registered by interactive Claude Code only |
| GAP-12 | Push notifications to mobile | | X | – | 07 | – | `agentPushNotifEnabled` needs claude.ai remote |
| GAP-13 | Internal commands | `/__remote-workflow`, `/workflow-launch-exec` | X | – | – | – | Not user-facing |

## MT: mantle-only (owner 10; extension API 01)

| ID | Feature | What the user sees/does | Code | M | Plan | Part | Notes |
|---|---|---|---|---|---|---|---|
| MT-01 | `pkg/ext` registration | Features and mods register the same way | N | M1 | 01 | A | `Feature{ID, Order, After, Parity, Setup}` |
| MT-02 | Overrides | Mods Replace/Wrap/Remove/Alias any built-in | N | M1 | 01 | B | Resolved after all Setup calls; conflicts reported |
| MT-03 | Subscriptions, interceptors, prompt stages | | N | M1 | 01 | A | API in contracts-v1 |
| MT-04 | Slots and dialog placement | | N | M1 | 01 | B | |
| MT-05 | Panic isolation | A crashing feature is disabled with a notice | N | M1 | 01 | B | `~/.mantle/state/disabled.json` |
| MT-06 | Arch test | | N | M1 | 01 | A | No cross-feature imports; `mods/` imports only `pkg/` |
| MT-07 | Stories / `mantle-ui story <id> --width N` | | N | M1 | 01 | B | |
| MT-08 | `mantle-ui catalog --json` | | N | M1 | 01 | B | |
| MT-09 | `mantle-ui selftest` | | N | M1 | 01 | B | Registry resolves; stories render at 60/100/160 columns |
| MT-10 | Launcher supervisor | Crash-safe terminal | N | M1 | 10 | A | Same process group; termios restore |
| MT-11 | Exit 75 restart protocol | | N | M1 | 10 | A | `~/.mantle/run/<pid>.json` |
| MT-12 | Versions store | | N | M1 | 10 | A | Immutable `versions/<id>/`; `current`/`last-good` symlinks; GC |
| MT-13 | Probation and auto-rollback | A bad build rolls back by itself | N | M1 | 10 | A | |
| MT-14 | `mantle versions\|rollback\|doctor\|--safe` | | N | M1 | 10 | A | Handled by the launcher |
| MT-15 | Install | `make install` / `scripts/install.sh` | N | M1 | 10 | A | Clone `~/.mantle/src`; link `~/.local/bin/mantle` |
| MT-16 | `/mantle <request>` | Builder modifies mantle | N | M1 | 10 | B | Worktree on `mod/<id>`; builder engine |
| MT-17 | Builder rules and permissions | | N | M1 | 10 | B | acceptEdits; allowed Bash; no `go get` |
| MT-18 | Pipeline steps 1–9 | | N | M1 | 10 | A | Protected paths, gofmt, vet, archtest, apidiff, build, test, selftest, pty smoke boot |
| MT-19 | Fix loop | Up to 3 rounds | N | M1 | 10 | B | |
| MT-20 | Promote | Active on next launch | N | M1 | 10 | B | Trailers; rebase onto `user`; flock |
| MT-21 | Restart now | | N | M1 | 10 | B | Exit 75 + `--resume` when idle and no background tasks |
| MT-22 | `/mantle list\|show\|status` | | N | M1 | 10 | B | |
| MT-23 | `/mantle undo <id>\|rollback\|retry` | | N | M1 | 10 | B | |
| MT-24 | Config-first triage | Settings change instead of rebuild | N | M2 | 10 | B | |
| MT-25 | Visual preview | Before/after story diff | N | M2 | 10 | B | `selfmod.confirm` |
| MT-26 | `/mantle update` | Mods re-applied on new core | N | M2 | 10 | B | Builder resolves conflicts |
| MT-27 | `/mantle edit <id>` | | N | M2 | 10 | B | |
| MT-28 | `/mantle upstream <id>` | Export a mod to `~/mantle` | N | M2 | 10 | B | |
| MT-29 | Dev mode | `selfmod.source=~/mantle` | N | M2 | 10 | B | Never auto-merges into `main` |
| MT-30 | Builder cost and budget | | N | M1 | 10 | B | `--max-budget-usd` |
| MT-31 | `docs/EXTENDING.md` | | N | M2 | 10 | B | |
| MT-32 | fd-handoff instant restart | | N | M3 | 10 | B | |
| MT-33 | Go toolchain check | | N | M1 | 10 | A | |
| MT-34 | `mods.json` derived from git | | N | M1 | 10 | A | `git log --grep Mantle-Mod` |
| MT-35 | Builder progress block | Collapsible background task | N | M1 | 10 | B | |
| MT-36 | Several instances on different versions | | N | M2 | 10 | A | GC keeps referenced versions |

---

## Slash command index

All built-in commands found in the 2.1.288 binary. "Headless" means the engine accepts it in
stream-json mode: prompt-type commands, plus local commands with `supportsNonInteractive`.

| Command | Aliases | Type | Headless | Code | Row |
|---|---|---|---|---|---|
| `/add-dir` | | local-jsx (+ headless twin) | yes | E | SE-37 |
| `/advisor` | | local-jsx | yes | E | ST-27 |
| `/agents` | | local (hidden, "removed") | yes | N | EC-13 |
| `/artifacts` | | local-jsx | no | H | CL-16 |
| `/auto-mode-setup` | | local-jsx | yes | E | EC-33 |
| `/autocompact` | | local-jsx | yes | E | CU-08 |
| `/autofix-pr` | | local-jsx | no | H | CL-09 |
| `/background` | bg | local-jsx | no | H | CL-12 |
| `/branch` | | local-jsx | no | N | SE-19 |
| `/brief` | | local-jsx | no | R | VW-11 |
| `/btw` | | local-jsx | no | N | SE-32 |
| `/bug` | share, report | local-jsx | no | H | EC-20 |
| `/cd` | | local-jsx | no | N | SE-38 |
| `/chrome` | | local-jsx | no | H | CL-14 |
| `/clear` | reset, new | local | yes | R | SE-15 |
| `/cloud-plugins` | | local-jsx | no | H | CL-25 |
| `/color` | | local-jsx | yes | N | CH-02 |
| `/compact` | | local | yes | E | SE-16 |
| `/config` | settings | local-jsx | yes (`key=value`) | N | ST-14 (args: ST-15) |
| `/context` | | local-jsx | yes | N | CU-01 |
| `/copy` | | local-jsx | no | N | SE-29 |
| `/daemon` | | local-jsx | no | H | CL-26 |
| `/design-login` | | local-jsx | no | H | CL-17 |
| `/desktop` | app | local-jsx | no | H | CL-03 |
| `/diff` | | local-jsx | no | N | SE-33 |
| `/effort` | | local-jsx | yes | N | ST-07 (args: ST-08) |
| `/exit` | quit (`:q`, `:q!`, `:wq`, `:wq!`) | local-jsx | yes | N | TC-18 |
| `/export` | | local-jsx | no | N | SE-28 |
| `/fast` | | local-jsx | yes | N | ST-10 |
| `/feedback` | report | local-jsx | no | H | EC-20 |
| `/focus` | | local-jsx | yes | R | VW-10 |
| `/fork` | | local-jsx | no | N | SE-20 |
| `/goal` | | local-jsx | yes | E | SE-35 |
| `/help` | | local-jsx | no | N | ST-28 |
| `/hooks` | | local-jsx | no | N | EC-12 |
| `/ide` | | local-jsx | no | H | CL-15 |
| `/import` | | local-jsx | yes | E | EC-21 |
| `/install` | | local-jsx | no | N | CLI-28 |
| `/install-github-app` | | local-jsx | no | H | CL-22 |
| `/install-slack-app` | | local | no | H | CL-23 |
| `/keybindings` | | local | no | N | ST-24 |
| `/list-agents` | peers | local | yes | E | EC-32 |
| `/login` | | local-jsx | no | N | EC-17 |
| `/logout` | | local-jsx | no | N | EC-18 |
| `/loops` | | local-jsx (disabled) | no | X | GAP-09 |
| `/mcp` | | local-jsx | yes (text) | N | EC-01 |
| `/memory` | | local-jsx | no | N | EC-14 |
| `/mobile` | ios, android | local-jsx | no | H | CL-04 |
| `/model` | | local-jsx | yes | N | ST-01 (args: ST-03) |
| `/output-style` | | local (+ hidden local-jsx stub) | yes | N | ST-21 |
| `/passes` | | local-jsx | no | H | CL-18 |
| `/pause-memory` | memory-pause, toggle-memory | local | no | N | EC-15 |
| `/permissions` | allowed-tools | local-jsx | no | N | ST-22 |
| `/plan` | | local-jsx | no | N | SE-36 |
| `/plugin` | plugins, marketplace | local-jsx | no | N | EC-06 |
| `/powerup` | | local-jsx | no | H | EC-37 |
| `/privacy-settings` | | local-jsx | no | H | CL-24 |
| `/pro-trial-expired` | | hidden | no | H | CL-27 |
| `/radio` | | local | no | H | EC-38 |
| `/rate-limit-options` | | local-jsx | no | H | CL-20 |
| `/recap` | | local | yes | E | SE-30 |
| `/release-notes` | | local-jsx | no | N | CH-22 |
| `/reload-plugins` | | local | yes | E | EC-09 |
| `/reload-skills` | | local | yes | E | EC-09 |
| `/remote-control` | rc | local-jsx | no | H | CL-01 |
| `/remote-env` | | local-jsx | no | H | CL-07 |
| `/rename` | | local-jsx | yes | N | SE-17 |
| `/restart` | update | local | no | N | ST-32 |
| `/resume` | continue | local-jsx | no | N | SE-11 |
| `/rewind` | checkpoint, undo | local | no | N | SE-22 |
| `/sandbox` | | local-jsx | no | N | ST-31 |
| `/scroll-speed` | | local-jsx | no | N | VW-21 |
| `/session` | remote | local-jsx | no | H | CL-05 |
| `/setup-bedrock` | | local-jsx | no | H | CL-21 |
| `/setup-vertex` | | local-jsx (hidden) | no | H | CL-21 |
| `/skill-doctor` | | local-jsx | yes | E | EC-11 |
| `/skills` | | local-jsx | no | N | EC-10 |
| `/status` | | local-jsx | no | N | ST-18 |
| `/stickers` | | local | no | H | EC-38 |
| `/stop` | | local-jsx | yes | E | CL-13 |
| `/subtask` | | local-jsx | no | H | SE-21 |
| `/tasks` | bashes | local-jsx | no | N | CH-13 |
| `/teleport` | tp | local-jsx | no | H | CL-02 |
| `/terminal-setup` | | local-jsx | no | N | ST-25 |
| `/theme` | | local-jsx | no | N | ST-19 |
| `/tui` | | local-jsx | no | N | VW-15 |
| `/ultraplan` | | local-jsx | no | H | CL-08 |
| `/ultrareview` | | local-jsx | yes | E | EC-34 |
| `/upgrade` | | local-jsx | no | N | EC-19 |
| `/usage` | cost, stats | local-jsx | yes (text) | N | CU-03 |
| `/usage-credits` | extra-usage (hidden) | local-jsx | yes | E | CL-19 |
| `/version` | | local-jsx | no | N | ST-30 |
| `/vim` | | hidden stub | no | N | ST-26 |
| `/voice` | | local | no | X | GAP-01 |
| `/web-setup` | | local-jsx | no | H | CL-06 |
| `/wellbeing` | breaks, break-reminder, downtime | local-jsx | no | H | EC-39 |
| `/workflows` | | local-jsx | no | H | EC-40 |
| `/heapdump` | | local (hidden) | yes | E | EC-35 |
| `/design-consent`, `/design-revoke` | | local (hidden) | yes | E | EC-36 |
| `/__remote-workflow`, `/workflow-launch-exec` | | internal | – | X | GAP-13 |
| `/init` | | prompt | yes | E | EC-22 |
| `/insights` | | prompt | yes | E | EC-23 |
| `/statusline` | | prompt (`disableNonInteractive`) | no | H | EC-24 |
| `/team-onboarding` | | prompt | yes | E | EC-25 |
| `/commit-push-pr` | | prompt | yes | E | EC-26 |
| `/security-review` | | prompt | yes | E | EC-26 |
| `/doctor` | checkup | bundled skill | yes | N | EC-16 |
| `/code-review` | review (`ultra` → ultrareview) | bundled skill | yes | E | EC-27 |
| `/simplify`, `/commit`, `/pr`, `/verify`, `/debug`, `/batch` | | bundled skill | yes | E | EC-27 |
| `/loop` | proactive | bundled skill | yes | E | EC-27 |
| `/schedule` | routines | bundled skill | yes | E | EC-27 |
| `/run`, `/run-skill-generator`, `/fewer-permission-prompts` | | bundled skill | yes | E | EC-27 |
| `/claude-code-docs`, `/update-config`, `/keybindings-help` (model-only), `/explain-usage`, `/memory-types` | | bundled skill | yes | E | EC-27 |
| `/claude-api`, `/claude-in-chrome`, `/design`, `/design-sync` | | bundled skill | yes | E | EC-27 |
| `/setup-claude` | setup-cowork | bundled skill | yes | E | EC-27 |
| `/artifact-design`, `/artifact-diagramming`, `/artifact-capabilities`, `/artifact-components`, `/artifact-pr-review`, `artifact-<kind>` | | bundled skill | yes | E | EC-27 |
| `/dataviz`, `/prototype`, `/whiteboard`, `/workshop` | | bundled skill | yes | E | EC-27 |
| `/workflow-authoring`, `/plugin-authoring`, `cowork-plugin`, `/deep-research` | | bundled skill | yes | E | EC-27 |
| Custom / plugin / MCP-prompt / synced skills | | prompt | yes | E | EC-28 … EC-31 |

## Keybinding contexts index

Action IDs are copied verbatim from Claude Code, so `~/.claude/keybindings.json` works
unchanged. The keymap engine itself is owned by 01 (CF-07, CF-09, CF-20).

| Context | Main actions (defaults) | Owner | Rows |
|---|---|---|---|
| Global | ctrl+c interrupt, ctrl+d exit, ctrl+t toggleTodos, ctrl+o toggleTranscript, ctrl+shift+b toggleBrief, ctrl+r history:search, ctrl/meta+up/down diff file list, ctrl+] openArtifact; app:help, app:redraw, app:toggleReplTab, app:toggleDiffNoiseFilter, app:toggleDiffPreSession, app:toggleTerminal (gated) | 01 (dispatch); actions by owners | TC-03/04, CH-10, VW-01, VW-11, HI-06, SE-34, CL-16, ST-28, CH-30, GAP-09 |
| Chat | escape cancel, enter submit, ctrl+j newline, ctrl+l clearInput, cmd+k clearScreen, ctrl+x ctrl+k killAgents, shift+tab cycleMode, meta+p modelPicker, meta+o fastMode, meta+t thinkingToggle, meta+w workflowKeywordToggle, ctrl+y defaultToNewerModel, ctrl+x enter queueSubmit, ctrl+enter / ctrl+x ctrl+s sendNow, up/down history, ctrl+_ undo, ctrl+g / ctrl+x ctrl+e externalEditor, ctrl+s stash, ctrl+v imagePaste, increase/decreaseEffort, cycleProactivity, attentionUp/Down | 04 (editing) / 05 (turn) / 08 (model) | ED-*, TC-08/09/12, PD-19, ST-06/09/10/11/12, GAP-09 |
| Autocomplete | tab accept, esc dismiss, up/down | 04 | AC-11 |
| Confirmation | enter yes, esc no, up/down, tab nextField, space toggle, shift+tab cycleMode | 05 | PD-41, PD-12 |
| Settings | esc, up/down, j/k, ctrl+p/n, home/end, pageup/down, space/enter, `/` search, r retry, d/w period, t sortByTokens, ctrl+u/d | 08 | ST-17, CU-04 |
| Tabs | tab/shift+tab, left/right | 01 (`pkg/ui`) | ST-14 |
| Transcript | ctrl+e toggleShowAll, ctrl+c/esc/q exit, less-style scrolling | 03 | VW-02/03/07 |
| HistorySearch | ctrl+r next, esc/tab accept, ctrl+c cancel, enter execute, ctrl+s cycleScope | 04 | HI-06/07/08 |
| Task | ctrl+b / ctrl+x ctrl+b background | 05 | TC-11 |
| ThemePicker | ctrl+t toggle syntax highlighting, ctrl+e edit custom theme | 08 | ST-20 |
| Scroll | pageup/down, wheel, ctrl+home/end, copy, shift+arrows/home/end extend selection, selection:clear | 12 | VW-16/17/18 |
| Help | esc | 08 | ST-29 |
| Attachments | left/right, backspace/delete remove, down/esc exit | 04 | ED-26 |
| Footer | up/down (ctrl+p/n), left/right, enter open, esc clear, x close | 07 | CH-17 |
| AbovePrompt, AbovePromptInput, AbovePromptSelect, Pane, PaneField | plugin panes: tab, enter/space, esc, scrolling, ctrl+x arrows resize, ctrl+x x close; chords ctrl+x ctrl+a / ctrl+x tab | – | GAP-02 |
| DiffDialog | esc, left/right source, up/down j/k file, paging | 06 | SE-34 |
| DiffPanel | ctrl+x b cycleDiffBase | 06 | SE-34 |
| ModelPicker | left/right effort, s this session only | 08 | ST-02 |
| EffortSlider | left/right, tab toggleUltracode, s | 08 | ST-07 |
| Select | arrows, j/k, ctrl+n/p, paging, home/end, enter, esc | 01 (`pkg/ui`) | – (shared widget) |
| Plugin | space toggle, i install, f favorite, ctrl+s cycle marketplace | 09 | EC-07 |
| Agents | ctrl+s switchView, ctrl+t togglePin, ctrl+f find, ctrl+r rename, ctrl/meta+up/down group | 11 (H) | CL-10 |
| MessageSelector | up/down, top/bottom, select | 06 | SE-22 |
| voice | space pushToTalk | – | GAP-01 |

## UI settings keys index

Settings keys mantle must read and honour (from `settings.json` scopes or global config).
Engine-only keys are handled by the engine (ENG-54) and aren't listed here.

| Key | Purpose | Owner | Row |
|---|---|---|---|
| `editorMode` | normal / vim | 04 | ED-14 |
| `vimInsertModeRemaps` | e.g. `{"jj":"<Esc>"}` | 04 | ED-15 |
| `keybindingFlavor` | deprecated; ignore with warning | 01 | CF-18 |
| `emojiCompletionEnabled` | `:emoji:` | 04 | ED-29 |
| `spellcheck` | `{enabled, checker, color}` | 04 | ED-30 |
| `promptSuggestionEnabled` | ghost text | 04 | ED-34 |
| `respondToBashCommands` | `!` mode replies | 04 | AC-17 |
| `fileSuggestion`, `respectGitignore` | `@` suggestions (engine-side) | 04 | AC-13 |
| `externalEditorContext` (global) | ctrl+g context | 04 | ED-19 |
| `theme` | built-in or `custom:*` | 01 / 08 | CF-12, ST-19 |
| `syntaxHighlightingDisabled` | | 03 | TR-03 |
| `maxProseWidth` | | 03 | TR-04 |
| `prefersReducedMotion` | | 03 | TR-47 |
| `verbose` | | 03 | VW-08 |
| `viewMode` | default / verbose / focus | 03 | VW-09 |
| `defaultView` | chat / transcript | 03 | VW-12 |
| `showThinkingSummaries` | | 03 | TR-08 |
| `showTurnDuration`, `timeFormat`, `timeZone` | | 03 | TR-50 |
| `showMessageTimestamps` | | 03 | TR-51 |
| `spinnerVerbs`, `spinnerTipsEnabled`, `spinnerTipsOverride` | | 03 | TR-45/46 |
| `tui` | default / fullscreen | 12 | VW-15 |
| `autoScrollEnabled`, `wheelScrollAccelerationEnabled` | | 12 | VW-19, VW-17 |
| `copyOnSelect` (global) | | 12 | VW-18 |
| `axScreenReader` | | 07 | VW-14 |
| `statusLine` (`command`, `padding`, `refreshInterval`, `hideVimModeIndicator`) | | 07 | CH-18, ED-16 |
| `subagentStatusLine` | | 07 | CH-12 |
| `todoFeatureEnabled` | | 07 | CH-10 |
| `prStatusFooterEnabled`, `prUrlTemplate` | | 07 | CH-15 |
| `footerLinksRegexes` | | 07 | CH-16 |
| `companyAnnouncements` | | 07 | CH-08 |
| `terminalProgressBarEnabled` | | 07 | CH-24 |
| `terminalTitleFromRename` | | 07 | CH-23 |
| `preferredNotifChannel`, `inputNeededNotifEnabled` | | 07 | CH-25/26 |
| `askUserQuestionTimeout` | | 05 | PD-26 |
| `dialogExpiry` | | 05 | PD-42 |
| `showClearContextOnPlanAccept` | | 05 | PD-23 |
| `skipDangerousModePermissionPrompt`, `skipAutoPermissionPrompt` | | 05 | PD-33/34 |
| `enableAllProjectMcpServers`, `enabledMcpjsonServers`, `disabledMcpjsonServers` | | 05 | PD-31/32 |
| `autoContinueAtUsageLimit` | | 05 | TC-16 |
| `permissions.defaultMode`, `permissions.disableBypassPermissionsMode`, `permissions.disableAutoMode` | mode cycle limits | 05 | PD-19/20 |
| `awaySummaryEnabled` | | 06 | SE-31 |
| `autoCompactEnabled`, `autoCompactWindow` | warnings and `/autocompact` | 06 | CU-07/08 |
| `copyFullResponse` (global), `diffTool` (global) | `/copy`, `/diff` | 06 | SE-29, SE-33 |
| `fileCheckpointingEnabled` | rewind availability | 06 | SE-22 |
| `model`, `effortLevel`, `modelSettings`, `fastMode`, `fastModePerSessionOptIn`, `alwaysThinkingEnabled`, `advisorModel` | model panel | 08 | ST-01…ST-11, ST-27 |
| `outputStyle` | | 08 | ST-21 |
| `sandbox.enabled` | `/sandbox` | 08 | ST-31 |
| `breakReminder`, `quietHours` | `/wellbeing` | 09 | EC-39 |
| `skillOverrides` | `/skills` | 09 | EC-10 |
| `leftArrowOpensAgents`, `defaultToAgentsView`, `autoConnectIde`, `claudeInChromeDefaultEnabled` (global) | | 11 / 09 | CL-10, CL-15, CL-14 |
| `voiceEnabled`, `voice` | | – | GAP-01 |
| `feedbackSurveyRate` | | – | GAP-05 |
| `agentPushNotifEnabled` | | – | GAP-12 |

## CLI flags index (owner 11)

**Consumed**: mantle interprets it (it may also forward it). **Forwarded**: passed to the
engine unchanged. **Interactive-only**: no headless equivalent; H or warn. **Internal**: set
by mantle itself. **-p only**: only meaningful with `-p`, which execs `claude` directly.

| Flag | Treatment | Notes |
|---|---|---|
| `-p, --print` | consumed | Exec `claude -p …` (CLI-04) |
| `--output-format`, `--input-format` | -p only / internal | mantle always uses stream-json internally |
| `--json-schema`, `--max-turns`, `--max-budget-usd`, `--no-session-persistence`, `--init`, `--maintenance` | -p only | `--max-budget-usd` also used by the builder |
| `--include-partial-messages`, `--include-hook-events`, `--replay-user-messages`, `--forward-subagent-text` | internal | Always set by mantle |
| `--permission-prompt-tool` | internal | Always `stdio` |
| `--permission-prompts host\|none` | consumed | mantle is always the host; forwarded in `-p` |
| `--prompt-suggestions` | consumed | Via `initialize.promptSuggestions` |
| `-c, --continue` | consumed | Renders history, then forwards (SE-08) |
| `-r, --resume [id\|search]` | consumed | No value → picker (CLI-06) |
| `--fork-session`, `--session-id` | forwarded | |
| `-n, --name` | consumed + forwarded | Prompt bar and title |
| `--from-pr [n\|url]` | interactive-only | Picker/H; uncertain |
| `--bg, --background` | interactive-only | H (CLI-17) |
| `--add-dir` | forwarded | |
| `-w, --worktree [name]` | forwarded | Decide in M2 (CLI-15) |
| `--tmux` | interactive-only | H |
| `--agent`, `--agents` | forwarded | |
| `--model`, `--fallback-model`, `--effort` | forwarded | mantle tracks the values |
| `--autocompact`, `--betas` | forwarded | |
| `--permission-mode` | forwarded | mantle tracks the mode |
| `--dangerously-skip-permissions` | forwarded | Triggers the bypass gate (PD-33) |
| `--allow-dangerously-skip-permissions` | forwarded | Enables bypass in the cycle |
| `--allowedTools`, `--disallowedTools`, `--tools` | forwarded | |
| `--restricted` | forwarded | |
| `--system-prompt[-file]`, `--append-system-prompt[-file]` | forwarded | mantle never sets them for the main engine |
| `--exclude-dynamic-system-prompt-sections`, `--system-prompt-snapshot` | forwarded | |
| `--settings`, `--setting-sources` | forwarded | mantle also reads them for UI keys |
| `--mcp-config`, `--strict-mcp-config` | forwarded | |
| `--plugin-dir`, `--plugin-url` | forwarded | |
| `--disable-slash-commands` | forwarded + consumed | mantle hides skills in the menu |
| `--bare` | forwarded | Shows a warning (drops hooks/skills/MCP/CLAUDE.md) |
| `--safe-mode` | forwarded | Also offer `mantle --safe` |
| `--ide` | forwarded | Behaviour in `-p` uncertain |
| `--chrome`, `--no-chrome` | forwarded | |
| `--desktop` | interactive-only | H |
| `--cloud [desc\|id\|url]`, `--environment` | interactive-only | H; `--cloud <id> -p` queues a message |
| `--teleport [session]` | interactive-only | H |
| `--remote-control [name]`, `--remote-control-session-name-prefix` | interactive-only | H |
| `--file <file_id:path>` | forwarded | uncertain |
| `--brief` | forwarded + consumed | VW-11 |
| `--teammate-mode` | interactive-only | GAP-03 |
| `--channels` | forwarded | |
| `-d, --debug [filter]`, `--debug-file` | forwarded | |
| `--verbose` | consumed | View mode; always set internally anyway |
| `--ax-screen-reader` | consumed | VW-14 |
| `-v, --version` | consumed | CLI-13 |
| `-h, --help` | consumed | CLI-14 |
| Hidden: `--thinking`, `--max-thinking-tokens`, `--thinking-display`, `--task-budget` | forwarded | |
| Hidden: `--await-initialize`, `--enable-auth-status`, `--session-mirror` | internal | ENG-30 |
| Hidden: `--resume-session-at`, `--resume-drops-turn`, `--rewind-files` | internal | Rewind (SE-22…SE-25) |

## Unclear items to verify

Spikes come from the design review; each is run by the plan shown and recorded as "Facts
verified on 2.1.288" in that plan's file.

| Spike | Plan | Question | Rows affected |
|---|---|---|---|
| S1 | 02 | What does an interrupted turn produce, during text vs during a tool? What does `interrupt(cancel_queued)` do? | TC-01, TC-02, TC-14, TR-28 |
| S2 | 02 | How do `priority` now/next/later behave? Do replay echoes ack queued messages? Can `cancel_async_message` take a message back? | TC-05…TC-10, ENG-24 |
| S3 | 02 | Does headless write `history.jsonl` and `paste-cache`? | HI-03, ED-21 |
| S4 | 02 | Does the Notification hook fire in `-p`? What does `--include-hook-events` emit? | CH-26, TR-35, ENG-25 |
| S5 | 02 | Which `CLAUDE_CODE_ENTRYPOINT` values change `/resume` visibility or gate features? | ENG-28, SE-13, GAP-10 |
| S6 | 02 | Can a zero-token probe see skills, plugins, hooks and CLAUDE.md? | ENG-20, ENG-22 |
| S7 | 02 | Exact semantics of `--resume-session-at`, `--fork-session`, `--resume-drops-turn`; `rewind_files` dry run | SE-19, SE-22…SE-27 |
| S8 | 02 | Local slash-command output: `local_command_output` vs a synthetic assistant message | TR-38 |
| S9 | 02 | Does headless honour `disabledMcpjsonServers` passed via `--settings`? | PD-31 |
| S10 | 02 | Trust semantics (do trusted parent dirs count?) and what `-p` loads in an untrusted repo | PD-30 |
| S11 | 02 | `file_suggestions` latency and shape; how to list MCP resources for `@` | AC-12, AC-14 |
| S12 | 02 | `--forward-subagent-text` streams and `parent_tool_use_id` | TR-15, ENG-26 |
| S13 | 02 | Thinking deltas and `--thinking-display` | TR-08 |
| S14 | 02 | Real claude, headless and interactive, against fakeapi with an isolated `CLAUDE_CONFIG_DIR` | ENG-32 |
| S15 | 01 | Println chunking, flicker on commit, resize, tmux, shift+enter per terminal | TR-53, ED-03, ED-40 |
| S16 | 01 | Launcher and engine process groups: ctrl+z, `fg`, `$EDITOR`, crash restore | MT-10, CH-29, ED-19, ENG-18 |
| S17 | 02 | CPU at peak delta rates; coalescing interval | ENG-11 |

Open questions without a spike yet. The owner plan adds a spike when it reaches the row.

| Question | Owner | Rows |
|---|---|---|
| Emulating `!` bash mode (run locally and inject output vs an engine path) | 04 | AC-17 |
| `side_question` request/response shape for `/btw` | 06 | SE-32 |
| `export_conversation`, `set_cwd`, `rewind_conversation` shapes | 06 | SE-28, SE-38, SE-26 |
| `/doctor` and `/recap` behaviour in `-p` | 09 / 06 | EC-16, SE-30 |
| Whether `MessageDisplay` hook `displayContent` reaches the stream | 03 | TR-34 |
| Sandbox network-host prompt routing in headless | 05 | PD-39 |
| `--ide` behaviour in `-p` | 09 | CL-15, CLI-11 |
| Elicitation wire format details | 05 | PD-27, PD-28 |
| Wire payload of `background_tasks` (Ctrl+B) | 05 | TC-11 |
| External CLAUDE.md import approval in headless | 05 | PD-36 |
| Fable consent prompt forwarding | 05 | GAP-07 |
| When `supersedes` actually happens | 03 | TR-29 |
