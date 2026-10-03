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

Status: A1–A7 done (`make test-07`). Notes and intentional differences:
- `internal/term/terminal` (extra): shared emulator detection (`TERM_PROGRAM`,
  `KITTY_WINDOW_ID`, `GHOSTTY_RESOURCES_DIR`, `LC_TERMINAL`, …), `TmuxWrap`, `StripControls`.
- **subagentStatusLine runs once per refresh for all rows**, not once per row: Claude Code
  sends one object `{session_id, transcript_path, cwd, columns, tasks:[…]}` and reads
  JSON lines `{"id","content"}` back (`SubagentPayload`, `ParseRows`). One `Runner` serves
  it. `hook_event_name` is left out until a real run shows its value.
- Payload key set: `testdata/fixtures/07/statusline-keys.txt` is taken from the public
  docs, not from a recorded run (real `claude` runs are opt-in). Re-record it with a
  stdin-dumping statusLine command when a spike is allowed. `prompt_cache` has a type but
  the builder never fills it (mantle doesn't compute it yet); `used_percentage` is
  rounded to a whole number, as in the docs' example.
- Status line output is sanitized: SGR and OSC 8 are kept, cursor movement, clears,
  titles and other sequences are dropped (they would corrupt the inline renderer).
- `auto` notifications resolve to a desktop notification only in iTerm2, kitty and
  Ghostty, otherwise nothing (set `terminal_bell` for a bell), as in Claude Code.
  `"bell"` is accepted as `terminal_bell`. Per the docs `inputNeededNotifEnabled` is the
  *mobile push* toggle, so it is not used for terminal notifications; `notify.Prefs`
  has `SkipInputNeeded` and `Quiet` for whatever B9 maps onto them.
- Trigger policy (`notify.Decide`): never when focused; InputNeeded at once when
  blurred, after 6 s of waiting and 6 s idle when focus is unknown; TurnDone for turns
  ≥ 3 s when blurred, ≥ 30 s with ≥ 20 s idle when focus is unknown; 2 s min gap.
- OSC 9;4 is gated by `osc.ProgressSupported` (Windows Terminal, ConEmu, Ghostty ≥ 1.2,
  iTerm2 ≥ 3.6.6): older iTerm2 shows OSC 9;4 as a notification.
- PR badge: hints only for github.com and `GH_HOST` (mantle's own wording); GitLab shows
  nothing when `glab` is missing or logged out. `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`
  turns lookups off (`prbadge.Enabled`). Issue links follow the repo's host (gitlab.com
  uses `/-/issues/`; bitbucket.org, codeberg.org and gitea.com get none).

- [x] **A1 `internal/term/statusline`, the runner.**
  - `Runner{Cmd, Padding, RefreshInterval}` with `Update(payload)`: debounces at 300 ms,
    runs via `sh -c` with a timeout, `COLUMNS`/`LINES` env and JSON on stdin; output is
    trimmed and ANSI kept.
  - Periodic refresh (`refreshInterval`). Overlapping runs are cancelled, and only the
    latest result is kept.
  - Errors produce a dim one-line notice, never a crash.
  - The same runner serves `subagentStatusLine`, with one instance per subagent row.
- [x] **A2 `internal/term/statusline`, the payload.** A `Payload` struct with JSON tags
  matching the field names above, plus a builder fed by plain values. Unit test: marshal and
  compare against a golden key set.
- [x] **A3 `internal/term/notify`.**
  - `Sequence(channel, title, body string, inTmux bool) []byte` for iTerm2 (OSC 9), kitty
    (OSC 99 with `i=`/`d=`), Ghostty (OSC 777 `notify`), bell (`\a`), `iterm2_with_bell`, and
    `auto` (detect via `TERM_PROGRAM`, `KITTY_WINDOW_ID`, `GHOSTTY_RESOURCES_DIR`, …).
  - tmux passthrough wrapping (`ESC Ptmux; … ESC \`, doubling ESCs).
  - Table tests.
- [x] **A4 `internal/term/osc`.** Progress (OSC 9;4 states: none, normal, error,
  indeterminate, pause), OSC 8 hyperlink helpers (`FORCE_HYPERLINK` honoured), and a title
  sanitizer that strips control characters and caps length.
- [x] **A5 `internal/term/clipboard`.**
  - `Copy(text)`: `pbcopy` on macOS; on Linux `wl-copy`, `xclip -selection clipboard` or
    `xsel`.
  - OSC 52 when `SSH_TTY` is set or no tool exists, with tmux passthrough.
  - `ReadImage()` is **not** here; plan 04 owns image paste.
  - Tests use fake binaries on `PATH`.
- [x] **A6 `internal/term/prbadge`.**
  - Detect the forge from the git remote.
  - GitHub: `gh pr view --json number,url,state,isDraft,reviewDecision` (the result chooses
    the badge colour). GitLab: `glab mr view -F json`.
  - Cache per (repo, branch) with a TTL; refresh asynchronously; don't block on missing auth
    (returns a hint instead). Honours `prStatusFooterEnabled` and `prUrlTemplate`.
  - Footer links: `footerLinksRegexes` matching plus `owner/repo#123` issue linkification.
- [x] **A7 `internal/term/focus`.** Track terminal focus from Bubble Tea v2 focus and blur
  messages (`ReportFocus`), so notifications fire only when the terminal is unfocused or the
  user has been away.

## Part B: after `contracts-v1` and `proto-v1`

Status: B1–B13 done in `features/chrome` (stories + goldens at 60/100/160, behaviour
tests with `exttest`, vt-emulator tests in the real host in `vt_test.go`), plus the
clipboard Cmd helper `internal/term/clipcmd` (CH-28). Features and parity tags:
`chrome.footer` (CH-05/06/07/14, ED-16), `chrome.promptFrame` (CH-01–04),
`chrome.statusLine` (CH-18/19), `chrome.todos` (CH-10), `chrome.subagents`
(CH-11/12/13/14/17), `chrome.welcome` (CH-08/09/20), `chrome.releaseNotes` (CH-21/22),
`chrome.terminal` (CH-23–26, CH-29, CH-30).
Notes:
- Frame: `Wrap("input.editor")` draws a rule above and below the editor (agreed with
  plan 04: the editor draws no border); colour `bashBorder` in `!` mode, `planMode` in
  plan mode, else `promptBorder`; the top rule carries the custom session name
  (`SessionInfo.Title`). `/color <red|blue|green|yellow|purple|orange|pink|cyan|default>`
  uses the theme's named colours and also sends `set_color`; bash and plan mode win.
- Footer: indicator for every mode (default reads "manual approval"); "? for shortcuts"
  only with an empty prompt and no statusLine command (as Claude Code); background-task
  count with a `/tasks` hint; vim indicator from `ext.EditorStateMsg` (not in NORMAL).
  The host draws notices (`core.notices`); chrome forwards engine `system/notification`
  events to `Ctx.Notify`. MCP-needs-auth count is part of B10's startup notices.
- Status line: runs once a session starts, then on assistant messages, results,
  compaction, mode/vim/model/name changes, settings changes (a new command skips the
  debounce), resizes, `refreshInterval` and rate-limit resets; hidden while a dialog is
  open. `pr` and `workspace.repo` come from the PR lookup (B8). Omitted (no source yet):
  `workspace.git_worktree`, `worktree`, `thinking`, `agent`, `prompt_id`, `prompt_cache`.
  `total_cost_usd` takes the latest `result.total_cost_usd`; line counts come from
  Edit/Write `structuredPatch`. Hook gates: with `disableAllHooks` (outside managed
  settings) or managed `allowManagedHooksOnly`, only a managed `statusLine` runs.
  After integration, switch `transcript_path` to plan 06's `sessions.SessionFile` /
  `sessions.Slug` (the local slug differs for non-BMP characters and paths > 200 chars).
- Title/progress: `chrome.terminal` implements `ext.TerminalStater`. Progress is only
  sent to terminals that render OSC 9;4; `requires_action` shows the paused state; an
  error stays until the next turn or until the user types.
- Subagents (B6): rows come from `task_*` events (30 s linger, 5 rows, "+N more").
  Footer navigation: `mantle:footerSelect` focuses the rows (plan 04's editor runs it
  when ↓ has no newer history); up/down, enter opens `dialog.subagentTranscript`, x
  stops (`stop_task`) or dismisses a finished row, esc returns to the prompt. The
  transcript view draws the transcript store's items nested under the subagent's tool
  call; after integration add plan 06's subagent JSONL as a fallback for resumed
  sessions. `subagentStatusLine` runs once per change for all rows (see Part A notes).
- `/tasks` (B7) lists background work from `background_tasks_changed` plus
  backgrounded subagents; enter polls `get_task_output` every second; x stops.
- Notifications (B9): spike S4 isn't recorded yet. In `-p` the engine drops hook
  `terminalSequence` output, so an engine hook can't ring the terminal bell and
  `EngineRang` stays false; revisit when S4 lands. Key activity comes from an
  interceptor (`chrome.activity`); focus from `ReportFocus`.
- Welcome (B10): mantle's own banner; the account line shows plan and organization,
  never the email. "Engine pinned" has no ext signal: whoever pins the engine (plans
  02/11) raises that notice through `Ctx.Notify`.
- Release notes (B11): the first run only records the engine version; after an upgrade
  a short "what's new" block (≤ 3 versions, ≤ 6 items each) is printed once.
- Suspend (B12): the host returns `tea.Suspend` on ctrl+z (contracts-v1.1) and Bubble
  Tea repaints the live area on resume, so no `Reprint` is needed; `app:redraw` reprints,
  `chat:clearScreen` (cmd+k) clears the screen. ctrl+l stays `chat:clearInput` (plan 04).
- Screen reader (B13): with `Ctx.Accessibility().ScreenReader` every chrome component
  renders plain text with no box drawing, rules, glyph art or colour
  (`TestScreenReaderStories`); chrome has no animation. The layout switch is the host's.
- vt tests use `internal/testkit`; under `-race` its shutdown has a data race in the
  harness itself (reported to plan 01), not in chrome.

- [x] **B1 [M1] Prompt frame.** The box around the input (slot `input` frame, owned
  together with plan 04's editor component): border colour per mode (bash mode border,
  plan, …), session name, `/color` support (the headless `/color` command or the `set_color`
  control request, plus rendering).
- [x] **B2 [M1] Footer** (slot `belowInput`):
  - permission-mode indicator, updated from `init.permissionMode` and `status`;
  - key hints ("? for shortcuts"; contextual hints such as "esc to interrupt" are owned by
    plan 03's spinner);
  - a notice area using the core Notice API: warnings, `informational` events,
    MCP-needs-auth count;
  - the vim mode indicator (from plan 04's editor message) unless
    `statusLine.hideVimModeIndicator` is set.
- [x] **B3 [M1] Status line component** (slot `statusLine`).
  - Assembles the Payload from subscribed engine messages:
    - `init` (model, output style, fast mode, tools);
    - `result` (cost, durations, usage, modelUsage);
    - `get_context_usage` or `result.modelUsage.contextWindow`;
    - `rate_limit_event`;
    - session tracker (id, name, transcript path, cwd);
    - editor vim mode; PR badge; worktree.
  - Re-runs on change via the A1 runner. Applies `padding`.
- [x] **B4 [M1] Todo panel** (slot `aboveInput`): ctrl+t toggle; items from TodoWrite and
  Task tools with status glyphs; collapses to a summary; persists expanded state in
  mantle's state store.
- [x] **B5 [M1] Window title and progress.** Title from session name, ai-title or cwd.
  Progress indeterminate while `session_state_changed=running` and cleared on idle; error
  state on `result.is_error`. Both merged into View fields through the host.
- [x] **B6 [M2] Subagent panel** (slot `aboveInput`, below todos).
  - One row per running or recently finished task (30 s linger), showing description, last
    tool and token/duration usage from `task_progress`.
  - Optional `subagentStatusLine` per row.
  - Enter opens that subagent's transcript in an alt-screen view (reads the subagent JSONL;
    the renderers come from plan 03 via ext).
  - `x` stops it (`stop_task`).
- [x] **B7 [M2] `/tasks` (alias `/bashes`).** A dialog listing `background_tasks`; view
  output (`get_task_output`, live refresh); stop (`stop_task`). Also the footer hint when
  background work exists.
- [x] **B8 [M2] PR badge and footer links** in the footer (A6), refreshed on branch change
  and turn end.
- [x] **B9 [M2] Notifications.** Trigger on `session_state_changed=requires_action`
  (permission, dialog or question waiting) and on turn end after a long turn while the
  terminal is unfocused.
  - Honour `preferredNotifChannel`, `inputNeededNotifEnabled` and quiet settings.
  - Write via `tea.Raw`.
  - Don't double-notify if spike S4 shows the engine's own Notification hooks already ran a
    `terminal_bell`.
- [x] **B10 [M2] Welcome banner and notices.**
  - mantle's own design (don't copy the Clawd art): version (mantle and engine), model,
    cwd, account (from `initialize.account`).
  - Startup notices: `companyAnnouncements`, MCP servers needing auth, invalid settings,
    engine pinned.
  - Shown once per session start; printed into scrollback via the committer.
- [x] **B11 [M2] `/release-notes`** (native; Claude Code's is local-jsx). Show Claude Code's
  changelog from `~/.claude/cache/changelog.md` (runtime read) for versions newer than the
  last seen one (stored in mantle state), plus mantle's own changelog.
- [x] **B12 [M2] ctrl+z suspend and `app:redraw`.** Suspend via `tea.Suspend`; on resume,
  redraw with the core `Reprint` when needed. ctrl+l redraw and cmd+k clear screen follow
  Claude Code's `Chat` bindings (`chat:clearInput` is ctrl+l in 2.1.288; check the keymap
  table from plan 01).
- [x] **B13 [M2] Screen-reader flat mode.** When enabled, the host switches the
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
