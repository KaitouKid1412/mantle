# Plan 12: fullscreen renderer & parity audit

| Session | Primary paths | Part B needs | Produces |
|---|---|---|---|
| mantle-12 | `features/fullscreen`, `test/e2e`, `test/parity` | **M1 reached** (all `[M1]` tasks in plans 01–11 done) plus `contracts-v1`, `proto-v1` | `docs/parity-audit.md` |

## Goal

Three jobs that only make sense once mantle is a working daily driver (M1):

1. **Fullscreen renderer** (Claude Code's `/tui fullscreen`, setting `tui: fullscreen`):
   - alt screen with a fixed input and a virtualized transcript;
   - mouse scrolling, click, selection and copy;
   - jump-to-bottom and a sticky prompt header;
   - **sidebar slots** for mods (mantle's extension point for pane-style UIs).
2. **Performance:** end-to-end benchmarks (startup, streaming, long transcripts, memory)
   and the fixes they lead to, filed with the owning plans.
3. **The parity audit:**
   - a **side-by-side harness** runs interactive `claude` and `mantle` against the same
     scripted fake API in terminal emulators, and diffs their frames;
   - an opt-in end-to-end run against the real API;
   - closing out `docs/PARITY.md` row by row.

## Start prompt
> You are session 12 of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/12-fullscreen-audit.md`, then execute that plan. Do Part A now. Start Part B
> once `git tag -l` shows the tags it needs and M1 is reached. Commit with prefix `[12]`.

**How to check M1:** every `[M1]` checkbox in `docs/plans/01`–`11` is ticked, and `mantle`
can hold a real conversation (resume, tools with permission prompts, plan mode, `/model`,
`/resume`, `/mantle`).

## Read first
- `docs/plans/00-overview.md`: the rendering architecture (transcript store, committer,
  slots, inline vs fullscreen), the parallel rules.
- `docs/plans/01-contracts-shell.md`: layout modes (`Inline`, `Fullscreen`, `AltView`),
  slots including `header`/`sidebarLeft`/`sidebarRight`, the testkit (x/vt harness).
- `docs/plans/03-render-transcript.md`: the transcript store, renderers, the ctrl+o viewer
  (search, keys).
- `docs/plans/02-proto-engine.md`: fakeapi, isolated `CLAUDE_CONFIG_DIR`, spike S14 (real
  claude against fakeapi), S17 (delta rates).
- `docs/research/inventory-docs.md`: section 3 (the fullscreen renderer feature list) and
  section 9 (`Scroll` keybinding context).
- `docs/research/design-review.md`: sections 1.3 (rendering) and 6 (risks).

## Facts you can rely on
- **Bubble Tea v2** View fields: `AltScreen`, `MouseMode` (`MouseModeCellMotion` for
  click, drag and wheel), `OnMouse`, `ReportFocus`, `Cursor`. `tea.Println` is a no-op in
  alt screen.
- **lipgloss v2** has `Canvas`, `Layer` and `Compositor`, with `Hit(x, y)` for mouse
  hit-testing of overlapping layers.
- **Claude Code's fullscreen features:**
  - fixed input, virtual scroll, mouse click/hover/select/drag, double-click (word) and
    triple-click (line) selection;
  - copy-on-select (global config `copyOnSelect`), ctrl+shift+c / cmd+c copy, shift+arrows
    and shift+home/end extend the selection;
  - PgUp/PgDn, ctrl+home/end, a "jump to bottom" with a new-message count, a sticky prompt
    header;
  - `/scroll-speed`, wheel acceleration (`wheelScrollAccelerationEnabled`), auto-follow
    (`autoScrollEnabled`); `/` search with n/N in the transcript.
  - Environment switches: `CLAUDE_CODE_DISABLE_MOUSE`,
    `CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN`, `CLAUDE_CODE_DISABLE_VIRTUAL_SCROLL`.
  - Keybinding context `Scroll` (pageup/pagedown, wheel, ctrl+home/end, copy, extend
    selection) and the `selection:clear` action.
- **Mouse capture disables the terminal's own text selection**, so fullscreen must provide
  selection and copy itself. Copy goes through plan 07's clipboard service.
- **`/diff`** opens as a side panel in fullscreen at ≥110 columns (auto-opens at 144). The
  `/diff` content is plan 06's; you provide the sidebar placement.
- **Driving interactive `claude` offline:** set `ANTHROPIC_BASE_URL` to fakeapi, use an
  isolated `CLAUDE_CONFIG_DIR`, and set `ANTHROPIC_API_KEY` with its one-time approval and
  workspace trust pre-seeded **in that temp config only** (never the user's real files).
  Also set `DISABLE_AUTOUPDATER=1`. Spike S14 (plan 02) documents the exact seeding.

## Part A: start once M1 is reached (A1–A3 may start earlier; they need only plan 02's fakeapi and plan 01's vt harness)

Status (2026-10-04, taken over by session 07): A1–A4 done. The `mantle` target runs
mantle-ui built from the tree (`BuildMantleUI`) directly, with `MANTLE_HOME` in the
workspace and the installed claude as its engine on the same fakeapi and config.
- `test/parity`: `Term` (pty + x/vt, screen + scrollback + alt-screen flag),
  `.scn` scenarios (`ready`, `type`, `keys`, `paste`, `resize`, `wait_for [timeout]`,
  `wait_gone`, `settle`, `checkpoint`, `sleep`; header `name`, `size`, `script`, `args`,
  `files`), `Run` in an isolated workspace (`/tmp/parity-<scenario>-<8 hex>/{work,config,
  home}`, fixed-length so truncated paths compare equal), fakeapi via httptest,
  `fakeapi.SeedConfig` + `fakeapi.Env` (enough for interactive claude 2.1.288: no
  onboarding, key approval or trust dialog; confirmed with plan 02).
- Normalizer rules (all listed in each report): workspace paths, logo art, spinner line,
  turn-done verb, temp paths, uuids, tool ids, dates, clock times, costs, token counts,
  durations, percentages; colour stripping; trailing-space trim; scrollback tail (200).
- Report: `test/parity/out/` (gitignored) with `summary.md`, per-checkpoint side-by-side
  + unified diff, and per-target frames; allowlist `test/parity/allowlist.txt`
  (`scenario | checkpoint | pattern | reason`). `make parity-side-by-side`
  (`PARITY_ARGS="-run plain -targets claude"`).
- Self-tests: the runner against a shell target (`make test-12`), and
  `MANTLE_PARITY_CLAUDE=1` / `MANTLE_PARITY_MANTLE=1 go test ./test/parity -run
  Deterministic`, which run every scenario twice per target and require identical
  normalized frames. A `blank-lines` rule drops leading blank lines and folds blank runs
  (inline frames start wherever the cursor was).
- Observed in claude 2.1.288 (useful for B1/B2): the prompt is `❯` between two full-width
  rules; auto mode is the default permission mode, with a one-time notice under the
  header; the footer reads "⏵⏵ auto mode on (shift+tab to cycle) · ← for agents"; an
  effort hint ("◐ medium · /effort") sits right-aligned above the prompt; finished turns
  end with "✻ <verb> for <dur> · done <time>".
- [x] **A1 Scenario format and runner** (`test/parity`).
  - A scenario is a scripted list of steps: `keys`, `paste`, `resize`, `wait_for <text>`,
    `checkpoint <name>`, `sleep`, plus a fakeapi script reference.
  - The runner starts a program in a pty (`creack/pty`) attached to an x/vt emulator at a
    fixed size, feeds the steps, and captures **screen and scrollback** at each checkpoint.
- [x] **A2 Normalization:** strip or collapse volatile content (spinner glyphs and verbs,
  durations, timestamps, costs, token counts, session ids, temp paths), with optional
  colour stripping and line-trailing-space trimming. Every rule is listed in the report.
- [x] **A3 Two targets:** `claude` (interactive TUI, isolated config, fakeapi) and
  `mantle` (current build, same fakeapi script, same isolated `CLAUDE_CONFIG_DIR` for its
  engine). The same scenario runs on both.
- [x] **A4 Report generator:** per scenario and checkpoint, side-by-side frames (text) and a
  diff summary, written to `test/parity/out/` (gitignored), plus a summary table in
  markdown.

## Part B: after M1, `contracts-v1` and `proto-v1`

Status (2026-10-04): B7–B9 done in `features/fullscreen` on plan 01's B14 hooks
(`contracts-v1.4`, `-v1.6`); B1–B6 wait for M1 (`integration-2`).
- Viewport (`fullscreen.viewport`, `SlotLive`, fullscreen only): plan 03's store through
  `Ctx.Renderer`, cached per item (id, rev, children revs, width, view mode, theme,
  expanded); a blank line before each item as inline; only the visible window is
  materialized (`CLAUDE_CODE_DISABLE_VIRTUAL_SCROLL` draws everything). When the store
  offers `Lines(ctx, w)` (plan 03, request 12-03) the viewport uses it, so view modes,
  brief, focus summaries and grouped MCP runs match inline exactly.
- Scrolling: the `scroll:*` actions in the `Scroll` context (made active in fullscreen):
  PgUp/PgDn, ctrl+home/end, wheel (3 lines per notch, `/scroll-speed` →
  mantle setting `fullscreen.scrollSpeed`, `CLAUDE_CODE_SCROLL_SPEED` wins, acceleration
  with `wheelScrollAccelerationEnabled`), auto-follow (`autoScrollEnabled`), and the
  "↓ N new lines · ctrl+end" pill.
- Sticky header (`fullscreen.header`, `SlotHeader`): the prompt of the turn in view, once
  it has scrolled above the window.
- Mouse (host OnMouse → `ext.MouseEvent`): drag selection, double-click word (paths and
  URLs whole), triple-click line, shift+arrows/home/end extend, `selection:clear`,
  copy-on-select (`copyOnSelect`) and ctrl+shift+c / cmd+c through `clipcmd` (plain text,
  soft-wrapped lines rejoined); click expands a collapsible item, hover shows a hint.
- Search: clicking focuses the transcript (context `Scroll`): `/` query, enter, n/N,
  j/k/space/b/d/u/g/G, esc clears the search, esc again (or any other key) returns to the
  prompt. Smart-case. Plan 03's viewer search isn't exported through ext, so fullscreen
  has its own (`search.go`); ctrl+o still opens plan 03's viewer.
- Sidebars: the host composes `SlotSidebarL` / `SlotSidebarR`; ctrl+x arrows
  (`pane:grow` / `pane:shrink`, also bound in `Scroll`) resize the sidebar last clicked,
  else the right one, via `ext.SidebarResizeMsg`. `/diff` is plan 06's
  `sessions.diffPanel` in the right sidebar (request 12-06); the EXTENDING.md example is
  section 8 (request 12-10, tested by `TestSidebarExample`).
- Switching: the host picks the layout from `tui` / `CLAUDE_CODE_NO_FLICKER` /
  `CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN` and screen-reader mode, and switches live on
  `ext.LayoutRequestMsg` (request 12-01, resolved); plan 08's `/tui` (and the /config
  Renderer row) sends it, and /config has a fullscreen-only
  `wheelScrollAccelerationEnabled` row. `CLAUDE_CODE_DISABLE_MOUSE` is the host's
  `Options.NoMouse`.
- Tests: `make test-12`: model unit tests, story goldens at 80/120/200 columns, vt tests
  in the real host (layout, PgUp/ctrl+home/end, pill, header, wheel, drag-copy, click
  expand, search, sidebar + resize), and full-screen vt goldens at 80×24, 120×40 and
  200×60 (`TestVTFullscreenGoldens`).
- [x] **B1 [M2] Scenario suite** covering every PARITY area. At minimum:
  - plain Q&A with markdown and code; tool approval: allow once / always / deny with
    feedback;
  - Edit diff display; Bash output collapse; plan mode and plan approval; AskUserQuestion;
  - todo list (ctrl+t); subagent with nested output; interrupt with esc; queued message
    while busy;
  - large paste collapse; `@` file mention; `/` menu; history ↑ and ctrl+r;
  - `/model` picker; `/context`; `/compact`; `/clear`; resume (`-c`, `/resume`); rewind;
  - statusLine output; footer modes (shift+tab cycle); `/help`; `/mcp` (fake status); an
    H hand-off command.

  Done: 27 scenarios in `test/parity/scenarios` (each lists its PARITY rows in a `parity:`
  header), run against claude 2.1.289 and mantle, offline, deterministic on both targets
  (`MANTLE_PARITY_CLAUDE=1 MANTLE_PARITY_MANTLE=1 go test ./test/parity -run
  Deterministic`). Harness additions: `prompt:` (positional first prompt, first start
  only), `env:`, `settings:`, `restart`, `@target` steps (listed in the report as flow
  differences), quoted `args:`, `{{work}}`-style script placeholders, `-raw` dumps, a
  `<target>.final.txt` frame for failed runs. Fixture app: `testdata/fixtures/12/app`.
  - Every run pins `"tui": "default"` (inline) unless the scenario sets `tui`: claude
    2.1.289 starts fullscreen with a fresh config, and mantle followed inline until the
    user's decision (unset `tui` = the installed Claude Code's default, plan 01).
  - Emulator fix: x/vt reads bytes 0x80-0x9F inside OSC strings as C1 controls, so
    claude's title "✳ Claude Code" (0x9C = ST) leaked onto the screen; `vtfix.go`
    sanitizes string payloads before the emulator.
  - Esc twice must be ≥ ~50 ms apart for mantle (30 ms reads as alt+esc); claude accepts
    both. Scenarios space them 200 ms (request to 04).
- [x] **B2 [M2] Triage diffs.** For each meaningful difference, file a request
  (`docs/plans/requests/12-NN-<slug>.md`) to the owning plan, with the scenario, checkpoint
  and both frames. Track them in this plan's checklist. Intentional differences (mantle's
  own wording, extra mantle features) go in an allowlist with a reason.

  Triaged against claude 2.1.289 on 2026-10-04 (inline renderer). Intentional
  differences: `test/parity/allowlist.txt` (banner, mantle-only notices, own wording);
  the report compares frames with a line diff, so a checkpoint is "allowed" only when
  every added or removed line is listed. Requests (status 2026-10-05):
  - [ ] [12-05](requests/12-05-permission-and-turn-parity.md) (05; §1 done, esc ends the
    turn): dialog shape and option sets; plan approval for 2.1.289; auto mode as the
    default; AskUserQuestion. Transcript rows routed to 12-03.
  - [ ] [12-03](requests/12-03-transcript-parity.md) (03; turn line, 3-line collapse and
    hidden task rows done): diff gutter; tab width; background agent rows; compact screen
    (with 06 §5); denial row; "Waiting…"; "Exited plan mode"; interrupt line.
  - [ ] [12-04](requests/12-04-input-parity.md) (04; esc esc, ❯ and no placeholder done;
    Panel/FrameTitle sent): `/` menu layout and filtering; `@` marker; paste hint;
    one-line ctrl+r; echo of panel commands.
  - [x] [12-06](requests/12-06-sessions-parity.md) (06): phantom session id, resume
    printing twice, resume title, picker, rewind "(current)", /context — done; chrome's
    part of the phantom id fixed in plan 07 (f74843a); compact screen with 03.
  - [x] [12-07](requests/12-07-chrome-parity.md) (07, 006ad5a, f74843a, 2130fa1): banner at
    startup, footer layout and effort hint, status line above the modes and at startup,
    task panel expanded, Panel/FrameTitle.
  - [x] [12-08](requests/12-08-panels-parity.md) (08): effort default, picker, /help on
    General, closing lines.
  - [x] [12-09](requests/12-09-ecosystem-parity.md) (09): /mcp closing line; hand-off
    confirmation kept (note).
- [ ] **B3 [M2] Performance suite** (`test/e2e/perf`):
  - **startup:** cold launch to first frame for `mantle` vs `claude` (median of N);
  - **long transcripts:** resume a 10k-item session; time to interactive; steady `View()`
    cost (target < 1 ms); RSS;
  - **streaming:** a 2k-token answer at peak delta rate (S17); frame time; no ghost lines in
    scrollback;
  - **large tool output** (multi-MB) and very long lines; CJK and emoji width correctness;
  - **idle CPU** with the spinner and status line refresh.

  Regressions become requests to the owners (01 committer and slot cache, 02 coalescer, 03
  renderers and caches).
- [ ] **B4 [M2] Opt-in real end-to-end** (`MANTLE_E2E_REAL=1 make e2e-real`). A handful of
  scenarios against the real engine and API: answer, tool approval, resume, `/compact`. It
  costs a few cents, so it never runs by default. Prints the cost.
- [ ] **B5 [M2] Terminal matrix and accessibility review.** Run a manual checklist on
  iTerm2, Ghostty, Terminal.app, kitty, WezTerm, the VS Code terminal, tmux, and SSH (OSC 52
  clipboard): shift+enter, image paste, notifications, title, progress bar, colours.
  - Accessibility: screen-reader flat mode (plan 07), reduced motion, daltonized and ANSI
    themes, keyboard-only flows through every dialog.
  - Record results in this plan.

  **Checklist (for the user, by hand).** About 10 minutes per terminal. Once, run
  `make install` in the repository (it installs the `mantle` launcher at
  `~/.local/bin/mantle` and a current build); then start each run with `mantle` from a
  scratch directory, signed in as usual. Without installing, `./bin/mantle-ui` after
  `make build` works too. Record ✓ / ✗ / n/a and a note per cell in the tables below
  (fill in the terminals you have); ✗ needs a one-line description (what you pressed,
  what you saw).

  Terminals: iTerm2 · Ghostty · Terminal.app · kitty · WezTerm · VS Code terminal · tmux
  (inside any of the above) · SSH (to a Linux host, mantle running remotely).

  | # | Check | How |
  |---|---|---|
  | T1 | Starts and draws | `mantle`, then type `hi` and enter. The banner, a reply, the prompt frame and the footer draw without garbage. |
  | T2 | shift+enter newline | Type `a`, press shift+enter, type `b`. Two lines in the prompt, not a submit. (If not: run `/terminal-setup` and retry; note whether that fixed it.) |
  | T3 | Image paste | Copy a screenshot, press ctrl+v (cmd+v in iTerm2/Ghostty) in the prompt. An `[Image #1]` chip appears. |
  | T4 | Notification | Ask "wait 20 seconds then say done" (or any long turn), switch to another app/tab. A desktop notification arrives when it finishes. |
  | T5 | Window title | The tab/window title shows the session name or the folder, and changes after `/rename parity test`. |
  | T6 | Progress bar | During a long turn, the tab or dock shows progress (Ghostty, iTerm2, Windows Terminal only; n/a elsewhere). |
  | T7 | Colours | `/theme`, pick Dark, then Light, then a daltonized theme. Text stays readable; diff colours (`/diff` after an edit) are distinguishable. |
  | T8 | Fullscreen | `/tui fullscreen`: mouse wheel scrolls, drag selects and copies (paste somewhere to check), PgUp/PgDn and ctrl+end work. `/tui default` returns to inline. |
  | T9 | Clipboard over SSH / tmux | In tmux and over SSH, select text in fullscreen (or `/copy`) and paste locally. OSC 52 must reach your local clipboard. |
  | T10 | Resize | Drag the window narrower and wider during a reply. No duplicated or torn lines in scrollback. |
  | T11 | Hand-off | `/mobile`, confirm with enter: Claude Code opens with the QR code; exit it (ctrl+c twice); mantle comes back with the conversation. |

  Accessibility (any one terminal):

  | # | Check | How |
  |---|---|---|
  | A1 | Screen-reader flat mode | `CLAUDE_AX_SCREEN_READER=1 mantle` (or the screen-reader setting in `/config`; `CLAUDE_CODE_ACCESSIBILITY=1` additionally keeps the real cursor visible): no box drawing, no spinner animation, every dialog readable as plain lines; with VoiceOver on, a reply is read out. |
  | A2 | Reduced motion | `/config` → reduce motion: spinner stops animating, no shimmer. |
  | A3 | Daltonized and ANSI themes | `/theme` → daltonized dark/light and the ANSI theme: modes, diffs and errors stay distinguishable. |
  | A4 | Keyboard-only dialogs | Without the mouse, open and close each: permission prompt (ask for a file edit), plan approval (shift+tab to plan, ask for a plan), AskUserQuestion (ask Claude to ask you a question), `/model`, `/help`, `/mcp`, `/resume`, `/rewind` (esc esc), `/config`, `/tasks`. Every option is reachable with arrows/tab/enter and esc closes. |

  Results:

  | Check | iTerm2 | Ghostty | Terminal.app | kitty | WezTerm | VS Code | tmux | SSH |
  |---|---|---|---|---|---|---|---|---|
  | T1 | | | | | | | | |
  | T2 | | | | | | | | |
  | T3 | | | | | | | | |
  | T4 | | | | | | | | |
  | T5 | | | | | | | | |
  | T6 | | | | | | | | |
  | T7 | | | | | | | | |
  | T8 | | | | | | | | |
  | T9 | | | | | | | | |
  | T10 | | | | | | | | |
  | T11 | | | | | | | | |

  | Check | Result | Note |
  |---|---|---|
  | A1 | | |
  | A2 | | |
  | A3 | | |
  | A4 | | |
- [ ] **B6 [M2] Close the parity audit.**
  - Go through every `docs/PARITY.md` row with `make parity` (generated status from plan
    checklists and `Parity:` tags) plus the side-by-side results.
  - Each row ends as **done** (with a scenario or test reference), **H** (hand-off verified
    working) or **X** (known gap with the reason).
  - Write `docs/parity-audit.md`: the summary, open requests, intentional differences, and
    the engine version audited (2.1.288, plus any newer version `make drift` flagged).
- [x] **B7 [M3] Fullscreen renderer** (`features/fullscreen`). When `tui` is `fullscreen`
  (setting, `/tui fullscreen`, plan 08's command), the host switches the layout mode to
  `Fullscreen`.
  - **Layout:** alt screen. Slot `header` at the top, a **virtual transcript viewport**,
    slots `aboveInput` / `input` / `belowInput` / `statusLine` fixed at the bottom, and
    optional `sidebarLeft` / `sidebarRight`.
  - **Viewport:** a virtual list over plan 03's transcript store (the same renderers).
    Per-item line cache keyed by `(item id, rev, width, view mode, themeRev)`. Render only
    the visible window plus a margin.
  - **Scrolling:** PgUp/PgDn, ctrl+home/end, wheel (with acceleration and `/scroll-speed`),
    auto-follow at the bottom, a "jump to bottom (N new)" pill.
  - **Sticky header:** shows the user prompt of the turn currently in view.
  - Honour `CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN` (fall back to inline) and
    `CLAUDE_CODE_DISABLE_VIRTUAL_SCROLL`.
- [x] **B8 [M3] Mouse and selection.**
  - `MouseModeCellMotion` (unless `CLAUDE_CODE_DISABLE_MOUSE`).
  - Hit-testing via the lipgloss Compositor to route clicks to the viewport, a sidebar, a
    dialog or the input.
  - Selection model over rendered lines: drag, double-click word, triple-click line,
    shift+arrows / home / end extend, `selection:clear`.
  - Copy-on-select (`copyOnSelect`) and ctrl+shift+c / cmd+c through plan 07's clipboard
    service. Copied text is the plain text (ANSI stripped, wrapped lines rejoined).
  - Click on a collapsed item expands it; hover highlights clickable items.
- [x] **B9 [M3] Search and sidebars.**
  - `/` search with n/N in fullscreen, reusing plan 03's viewer search logic through ext.
  - Sidebar slots via the Compositor with resizable widths (ctrl+x arrows, per Claude Code's
    `Pane` context). `/diff` uses the right sidebar at ≥110 columns (auto at 144);
    coordinate with plan 06 via ext IDs.
  - Write an `EXTENDING.md` example together with plan 10: a mod adding a left sidebar.

## Design notes
- **Same store, same renderers.** Fullscreen is another view over plan 03's transcript store,
  never a second rendering pipeline. Inline and fullscreen must show identical content.
- **Switching modes at runtime:** entering fullscreen uses the alt screen (scrollback is
  preserved). Leaving it calls the core `Reprint` only if the inline committed watermark is
  stale.
- **Selection and copy in fullscreen are mantle's own**, because mouse capture removes the
  terminal's selection. Inline mode keeps the mouse off and native selection on.
- **The parity harness is the referee.** Any claim of "parity done" in PARITY.md should
  point to a scenario checkpoint or a test.
- **Keep reports out of git** (`test/parity/out/`). Commit only scenarios, normalization
  rules, allowlists and the final `docs/parity-audit.md`.

## Interfaces you provide / consume
- **Provide:**
  - the `fullscreen` layout feature (viewport, scroll, selection, sidebars);
  - the parity harness (`test/parity`) and perf suite (`test/e2e/perf`);
  - `make parity-side-by-side`, `make perf`, `make e2e-real`;
  - `docs/parity-audit.md`.
- **Consume:**
  - plan 01's host layout modes, slots, Compositor integration and vt testkit;
  - plan 03's transcript store, renderers and search;
  - plan 07's clipboard service;
  - plan 06's `/diff` component;
  - plan 08's `/tui` and `/scroll-speed` settings;
  - plan 02's fakeapi and engine;
  - `make parity` (generated status).

## Tests and done criteria
- Harness self-test: the same scenario run twice against the same target gives identical
  normalized frames (no flakiness).
- The side-by-side suite runs offline at no cost. Every PARITY area has at least one
  scenario. All meaningful diffs are either fixed, filed as requests, or allowlisted with a
  reason.
- Perf targets are met or regressions filed: steady `View()` < 1 ms with 10k items; no
  ghost lines while streaming; startup to first frame no slower than `claude`'s.
- Fullscreen goldens (vt, alt screen) at 80×24, 120×40 and 200×60: scrolling, selection
  highlight, sticky header, jump-to-bottom, a sidebar layout.
- `docs/parity-audit.md` exists, and every PARITY.md row is done, H or X with a reference.
- `make test-12` passes; `go build -tags no_fullscreen ./cmd/mantle-ui` still builds.

## Parity coverage
- `VW-*`: the fullscreen renderer rows (mouse, selection, copy, virtual scroll, jump to
  bottom, sticky header, scroll speed, wheel acceleration, auto-scroll, fullscreen search).
- Audit of **all** areas (`ENG ED AC HI TC TR VW CH PD SE CU ST EC CF CLI CL GAP MT`).

## Out of scope
- **Fixing other plans' features yourself:** file requests instead, unless the fix is a
  trivial one-liner in a file whose owner agrees.
- **The inline renderer and committer:** plans 01 and 03.
- **`/diff` content:** plan 06.
- **Theme engine:** plan 01.
- **Windows terminals.**

**Reminder of the parallel rules:** edit mainly your primary paths; contracts are
additive-only; no repo-wide rewriters (`make fmt-12`); commit only your files with `[12]`;
per-area build tag `no_fullscreen`.
