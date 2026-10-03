# Plan 07: chrome & terminal

| Session | Primary paths | Part B needs | Produces |
|---|---|---|---|
| mantle-07 | `internal/term`, `features/chrome` | tags `contracts-v1`, `proto-v1` | none |

## Goal

Everything around the conversation that isn't the transcript, the editor or a dialog:
- the prompt frame, the footer (mode indicator, hints, notices) and the user's custom status
  line;
- the todo and subagent panels and `/tasks`;
- the window title, the OSC 9;4 progress bar, desktop notifications and the clipboard;
- the welcome banner and release notes, ctrl+z suspend, and the screen-reader flat mode.

All of this must behave like Claude Code 2.1.288 and read the same settings. Part A builds
the terminal services as plain Go packages; Part B wires them into the UI through `pkg/ext`.

## Start prompt
> You are session 07 of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/07-chrome-terminal.md`, then execute that plan. Do Part A now. Start Part B
> once `git tag -l` shows the tags it needs. Commit with prefix `[07]`.

## Read first
- `docs/plans/00-overview.md`: architecture, slots, the parallel rules.
- `docs/research/inventory-docs.md`: sections 3 (chrome), 18 (status line), 8 (UI-only
  settings keys).
- `docs/research/inventory-binary-2.1.288.md`: sections 4 (settings: `statusLine`,
  `preferredNotifChannel`, `terminalProgressBarEnabled`, …), 6 (permission mode labels),
  7 (`~/.claude` layout, incl. `cache/changelog.md`), 8 (interactive features).
- `docs/research/protocol-2.1.288.md`: `system/init`, `session_state_changed`, `task_*`,
  `background_tasks_changed`, `rate_limit_event`, `result` usage fields; control requests
  `background_tasks`, `get_task_output`, `stop_task`, `get_context_usage`.
- The user's own status line script `~/.claude/statusline.sh` (read it, don't modify it). It
  uses `context_window.remaining_percentage` and `context_window.used_percentage`.

## Facts you can rely on
- **Status line settings:** `statusLine: {type:"command", command, padding, refreshInterval,
  hideVimModeIndicator}` and `subagentStatusLine` (same shape, rendered per subagent row).
- **How the command runs:** with a JSON object on stdin and `COLUMNS`/`LINES` set. Updates
  are debounced at about 300 ms. Output may be multi-line ANSI and may contain OSC 8 links.
- **Payload fields** (match Claude Code's names exactly):
  - Session: `session_id`, `session_name`, `transcript_path`, `cwd`, `version`.
  - Model: `model {id, display_name}`, `effort`, `thinking`, `fast_mode`, `output_style`.
  - Workspace: `workspace {current_dir, project_dir, …, repo/worktree}`, `worktree`, `pr`,
    `agent`.
  - Cost: `cost {total_cost_usd, total_duration_ms, total_api_duration_ms,
    total_lines_added, total_lines_removed}`.
  - Context: `context_window {…, used_percentage, remaining_percentage, …}`,
    `exceeds_200k_tokens`.
  - Other: `rate_limits`, `vim {mode}`, `prompt_cache`.

  Fields mantle can't compute yet are omitted, never faked.
- **`preferredNotifChannel`:** `auto | iterm2 | terminal_bell | iterm2_with_bell | kitty |
  ghostty | notifications_disabled`. Related settings: `inputNeededNotifEnabled`,
  `agentPushNotifEnabled`.
  - In `-p` mode Claude Code ignores hook `terminalSequence` output, so mantle must render
    notifications itself.
  - Whether the engine's `Notification` hook still fires in headless mode is spike S4
    (plan 02). Read its result in `02-proto-engine.md` before deciding on double-notify
    handling.
- **Progress bar:** OSC 9;4. Bubble Tea v2 has a native `View.ProgressBar` field.
  Controlled by setting `terminalProgressBarEnabled`.
- **Window title:** Bubble Tea v2 `View.WindowTitle`. Disabled by
  `CLAUDE_CODE_DISABLE_TERMINAL_TITLE`. `terminalTitleFromRename` controls whether
  `/rename` / `-n` names win.
- **Permission mode indicator** (footer): `default` is shown as manual (⏸), `acceptEdits`
  (⏵⏵), `plan` (⏸), `auto` (⏵⏵), `bypassPermissions` (⏵⏵). Convey the same meaning in
  mantle's own wording.
- **Todo panel:** ctrl+t is `app:toggleTodos` in the `Global` context. It is built from
  `TodoWrite` and `TaskCreate`/`TaskUpdate` tool calls, shows up to 5 items, and remembers
  whether it's expanded. Setting `todoFeatureEnabled`.
- **Subagent and background data:**
  - `system/task_started|task_progress|task_updated|task_notification`, plus
    `background_tasks_changed` (replace semantics) and `parent_tool_use_id` nesting.
  - Subagent transcripts are at `~/.claude/projects/<slug>/<session>/subagents/agent-<id>.jsonl`.
  - Control requests: `background_tasks`, `get_task_output {task_id}` and
    `stop_task {task_id}`.
- **Release notes:** `~/.claude/cache/changelog.md` exists on disk. Read it at runtime and
  never commit its contents.
- **ctrl+z:** Bubble Tea v2 suspends with `kill(0, SIGTSTP)`, which reaches the whole
  process group. The launcher (plan 10) shares the group; the engine is in its own group
  (`Setpgid`), so it keeps running. Spike S16 (plan 01) verifies this.
- **Screen-reader mode:** `--ax-screen-reader`, env `CLAUDE_AX_SCREEN_READER`, setting
  `axScreenReader`. Flat linear output: no animation, no box drawing.

## Part A: start immediately (no other session needed)
- [ ] **A1 `internal/term/statusline`, the runner.**
  - `Runner{Cmd, Padding, RefreshInterval}` with `Update(payload)`: debounces at 300 ms,
    runs via `sh -c` with a timeout, `COLUMNS`/`LINES` env and JSON on stdin; output is
    trimmed and ANSI kept.
  - Periodic refresh (`refreshInterval`). Overlapping runs are cancelled, and only the
    latest result is kept.
  - Errors produce a dim one-line notice, never a crash.
  - The same runner serves `subagentStatusLine`, with one instance per subagent row.
- [ ] **A2 `internal/term/statusline`, the payload.** A `Payload` struct with JSON tags
  matching the field names above, plus a builder fed by plain values. Unit test: marshal and
  compare against a golden key set.
- [ ] **A3 `internal/term/notify`.**
  - `Sequence(channel, title, body string, inTmux bool) []byte` for iTerm2 (OSC 9), kitty
    (OSC 99 with `i=`/`d=`), Ghostty (OSC 777 `notify`), bell (`\a`), `iterm2_with_bell`, and
    `auto` (detect via `TERM_PROGRAM`, `KITTY_WINDOW_ID`, `GHOSTTY_RESOURCES_DIR`, …).
  - tmux passthrough wrapping (`ESC Ptmux; … ESC \`, doubling ESCs).
  - Table tests.
- [ ] **A4 `internal/term/osc`.** Progress (OSC 9;4 states: none, normal, error,
  indeterminate, pause), OSC 8 hyperlink helpers (`FORCE_HYPERLINK` honoured), and a title
  sanitizer that strips control characters and caps length.
- [ ] **A5 `internal/term/clipboard`.**
  - `Copy(text)`: `pbcopy` on macOS; on Linux `wl-copy`, `xclip -selection clipboard` or
    `xsel`.
  - OSC 52 when `SSH_TTY` is set or no tool exists, with tmux passthrough.
  - `ReadImage()` is **not** here; plan 04 owns image paste.
  - Tests use fake binaries on `PATH`.
- [ ] **A6 `internal/term/prbadge`.**
  - Detect the forge from the git remote.
  - GitHub: `gh pr view --json number,url,state,isDraft,reviewDecision` (the result chooses
    the badge colour). GitLab: `glab mr view -F json`.
  - Cache per (repo, branch) with a TTL; refresh asynchronously; don't block on missing auth
    (returns a hint instead). Honours `prStatusFooterEnabled` and `prUrlTemplate`.
  - Footer links: `footerLinksRegexes` matching plus `owner/repo#123` issue linkification.
- [ ] **A7 `internal/term/focus`.** Track terminal focus from Bubble Tea v2 focus and blur
  messages (`ReportFocus`), so notifications fire only when the terminal is unfocused or the
  user has been away.

## Part B: after `contracts-v1` and `proto-v1`
- [ ] **B1 [M1] Prompt frame.** The box around the input (slot `input` frame, owned
  together with plan 04's editor component): border colour per mode (bash mode border,
  plan, …), session name, `/color` support (the headless `/color` command or the `set_color`
  control request, plus rendering).
- [ ] **B2 [M1] Footer** (slot `belowInput`):
  - permission-mode indicator, updated from `init.permissionMode` and `status`;
  - key hints ("? for shortcuts"; contextual hints such as "esc to interrupt" are owned by
    plan 03's spinner);
  - a notice area using the core Notice API: warnings, `informational` events,
    MCP-needs-auth count;
  - the vim mode indicator (from plan 04's editor message) unless
    `statusLine.hideVimModeIndicator` is set.
- [ ] **B3 [M1] Status line component** (slot `statusLine`).
  - Assembles the Payload from subscribed engine messages:
    - `init` (model, output style, fast mode, tools);
    - `result` (cost, durations, usage, modelUsage);
    - `get_context_usage` or `result.modelUsage.contextWindow`;
    - `rate_limit_event`;
    - session tracker (id, name, transcript path, cwd);
    - editor vim mode; PR badge; worktree.
  - Re-runs on change via the A1 runner. Applies `padding`.
- [ ] **B4 [M1] Todo panel** (slot `aboveInput`): ctrl+t toggle; items from TodoWrite and
  Task tools with status glyphs; collapses to a summary; persists expanded state in
  mantle's state store.
- [ ] **B5 [M1] Window title and progress.** Title from session name, ai-title or cwd.
  Progress indeterminate while `session_state_changed=running` and cleared on idle; error
  state on `result.is_error`. Both merged into View fields through the host.
- [ ] **B6 [M2] Subagent panel** (slot `aboveInput`, below todos).
  - One row per running or recently finished task (30 s linger), showing description, last
    tool and token/duration usage from `task_progress`.
  - Optional `subagentStatusLine` per row.
  - Enter opens that subagent's transcript in an alt-screen view (reads the subagent JSONL;
    the renderers come from plan 03 via ext).
  - `x` stops it (`stop_task`).
- [ ] **B7 [M2] `/tasks` (alias `/bashes`).** A dialog listing `background_tasks`; view
  output (`get_task_output`, live refresh); stop (`stop_task`). Also the footer hint when
  background work exists.
- [ ] **B8 [M2] PR badge and footer links** in the footer (A6), refreshed on branch change
  and turn end.
- [ ] **B9 [M2] Notifications.** Trigger on `session_state_changed=requires_action`
  (permission, dialog or question waiting) and on turn end after a long turn while the
  terminal is unfocused.
  - Honour `preferredNotifChannel`, `inputNeededNotifEnabled` and quiet settings.
  - Write via `tea.Raw`.
  - Don't double-notify if spike S4 shows the engine's own Notification hooks already ran a
    `terminal_bell`.
- [ ] **B10 [M2] Welcome banner and notices.**
  - mantle's own design (don't copy the Clawd art): version (mantle and engine), model,
    cwd, account (from `initialize.account`).
  - Startup notices: `companyAnnouncements`, MCP servers needing auth, invalid settings,
    engine pinned.
  - Shown once per session start; printed into scrollback via the committer.
- [ ] **B11 [M2] `/release-notes`** (native; Claude Code's is local-jsx). Show Claude Code's
  changelog from `~/.claude/cache/changelog.md` (runtime read) for versions newer than the
  last seen one (stored in mantle state), plus mantle's own changelog.
- [ ] **B12 [M2] ctrl+z suspend and `app:redraw`.** Suspend via `tea.Suspend`; on resume,
  redraw with the core `Reprint` when needed. ctrl+l redraw and cmd+k clear screen follow
  Claude Code's `Chat` bindings (`chat:clearInput` is ctrl+l in 2.1.288; check the keymap
  table from plan 01).
- [ ] **B13 [M2] Screen-reader flat mode.** When enabled, the host switches the
  layout-mode flag: no spinner animation (static "Working…" lines), no borders, linear
  appends, and the footer and status line printed as plain lines. Coordinate the flag
  through ext (plan 01 owns the host switch; you own the chrome behaviour).

## Design notes
- **Keep services and components apart.** Part A packages have no Bubble Tea dependency
  except returning byte slices or plain values. Part B components wrap them in Cmds. That
  keeps them testable without a terminal and reusable by plan 12's fullscreen mode.
- **The status line runner never blocks Update.** Runs happen in Cmds; results come back as
  `statusLineMsg{rev, lines}`, and stale revisions are dropped.
- **Notification triggers are pure functions** of (state change, focus, settings, last
  notification time), so they can be table-tested.
- **Parity check for the payload:** run Claude Code interactively with a `statusLine`
  command that saves its stdin to a file, do the same under mantle, and diff the key sets.
  Document any intentional differences in this plan's checklist.
- **Footer height** counts toward the live-area budget (plan 01). Keep the footer at 1 line
  by default and the status line at its natural height (it can be multi-line).
- **Write your own text.** Hints, labels and the welcome copy must be mantle's own words
  that convey the same meaning (licensing hygiene).

## Interfaces you provide / consume
- **Provide:**
  - `internal/term/{statusline,notify,osc,clipboard,prbadge,focus}`.
  - ext components `chrome.footer`, `chrome.promptFrame`, `chrome.statusLine`,
    `chrome.todos`, `chrome.subagents`, `chrome.welcome`.
  - Commands `/tasks` (`/bashes`) and `/release-notes`; the title and progress View
    contributions.
  - **A clipboard service** for the other plans: `/copy` (06) and transcript copy (03, 12).
    Expose it as an ext action or Cmd helper; don't make them import `features/chrome`.
- **Consume:** `pkg/ext` (slots, Subscribe, Notice, Settings, Store); `pkg/proto` event
  types; the session tracker data from plan 02's engine messages; the editor's vim-mode
  message (plan 04, by message type in `pkg/ext`, not by import); renderers from plan 03 for
  the subagent transcript view.

## Tests and done criteria
- Unit tests for every Part A package: notification sequences per channel with and without
  tmux, OSC encodings, debounce and cancel behaviour (using `testing/synctest` and the
  injected Clock), PR badge parsing with fake `gh`/`glab`, clipboard fallbacks.
- **Status line parity test:** the key set mantle sends equals Claude Code's for the same
  session (fixture-driven), with intentional omissions listed.
- vt-emulator goldens (from plan 01's testkit) for the footer in each permission mode, the
  todo panel (collapsed and expanded), the subagent panel, the `/tasks` dialog and the
  welcome banner, at 60/100/160 columns. Every component has a Story.
- vt test: OSC 9;4 is written on running and cleared on idle; the title updates on rename.
- Screen-reader mode golden shows no box-drawing characters and no animation frames.
- `make test-07` passes; `go build -tags no_chrome ./cmd/mantle-ui` still builds.

## Parity coverage
- `CH-*`: prompt frame, mode indicator, hints, notices, todo panel, subagent panel and
  `subagentStatusLine`, `/tasks`, PR badge, footer links, statusLine, welcome banner,
  release notes, title, OSC 9;4, notifications, clipboard, suspend, redraw.
- `VW-*`: screen-reader flat mode.
- `CF-*` rows for the UI keys this plan honours: `statusLine`, `subagentStatusLine`,
  `preferredNotifChannel`, `inputNeededNotifEnabled`, `terminalProgressBarEnabled`,
  `terminalTitleFromRename`, `prStatusFooterEnabled`, `prUrlTemplate`, `footerLinksRegexes`,
  `companyAnnouncements`, `todoFeatureEnabled`, `axScreenReader`.

Tag each registration with its PARITY IDs (`Feature.Parity`).

## Out of scope
- **Spinner, turn duration, thinking display:** plan 03.
- **Editor and its vim mode:** plan 04 (you only display the indicator).
- **Permission and plan dialogs:** plan 05.
- **`/context`, `/usage`:** plan 06.
- **`/theme`:** plan 08 (the theme engine is plan 01).
- **Fullscreen mouse selection:** plan 12.
- **Push notifications to mobile** (`agentPushNotifEnabled` relies on Claude's cloud): H/X,
  noted in PARITY.md.

**Reminder of the parallel rules:** edit mainly your primary paths; contracts are
additive-only; no repo-wide rewriters; commit only your files with `[07]`; per-area build
tag `no_chrome`.
