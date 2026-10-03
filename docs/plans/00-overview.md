# mantle: overview and decisions

**Every session reads this file first.** It describes what mantle is, how the code is laid
out, and how the parallel sessions coordinate. Your own plan file
(`docs/plans/NN-*.md`) gives your detailed tasks.

## What mantle is

mantle is a new terminal UI for Claude Code, written in Go with Bubble Tea v2. It has two
goals:

1. **Parity.** Out of the box, mantle does everything the Claude Code 2.1.288 terminal UI
   does. It drives the real `claude` binary headlessly (stream-json over stdio). The engine
   is therefore unchanged: the agent loop, tools, MCP, hooks, skills, plugins, sessions,
   prompt caching and performance are exactly those of Claude Code. Only the UI is replaced.
2. **Self-modification.** `/mantle <request>` changes mantle itself:
   - a builder agent edits mantle's Go source;
   - a fixed pipeline vets, tests, builds and self-tests the result;
   - the next time the user types `mantle`, the new version runs.

   Any built-in feature can be changed this way, and new features can be added.

"mantle" is a **working name until the MVP**. Claude Code already uses "Mantle" internally
(`CLAUDE_CODE_USE_MANTLE`, an `apiProvider` value `mantle`), so a rename is likely. Keep the
name out of anything that would be painful to change. The module path is a single rewrite.

## User decisions

| Topic | Decision |
|---|---|
| Parity scope | Every local terminal feature is rebuilt natively. Cloud and product features (remote control, teleport, desktop and mobile hand-off, cloud sessions, agent view, …) **hand off** to the real `claude`. Infeasible features are listed as **known gaps** |
| Where `/mantle` changes live | A private clone at `~/.mantle/src` (branch `user`). The dev checkout `~/mantle` stays clean core code |
| Parallelism | Up to 11 Claude Code sessions in the same checkout. file-baton serializes edits to the same file |

## Decisions

| Topic | Decision |
|---|---|
| Name / module | `mantle` (working name); module `github.com/KaitouKid1412/mantle` |
| Language | Go 1.27, `toolchain` pinned in go.mod |
| UI stack | `charm.land/bubbletea/v2` (v2.0.10), `charm.land/bubbles/v2` (v2.2.1), `charm.land/lipgloss/v2` (v2.0.6). Upgrade the Charm set together |
| Libraries | `yuin/goldmark` + a custom ANSI renderer (glamour is reference only); `alecthomas/chroma/v2`; `sahilm/fuzzy`; `aymanbagabas/go-udiff` + `sergi/go-diff`; `charmbracelet/x/ansi`; `charmbracelet/x/editor`; `yuin/goldmark-emoji`; `fsnotify`; `google/uuid`; `dustin/go-humanize`; `creack/pty`; `charmbracelet/x/vt` + `charmbracelet/x/exp/golden` for tests |
| JSON / flags | Plain `encoding/json` (json/v2 is still experimental in 1.27). A hand-written, table-driven flag parser: unknown `claude` flags must be forwarded verbatim |
| Clipboard | `pbcopy`, and `osascript` for image paste (Claude Code does the same); OSC 52 when `SSH_TTY` is set; no cgo |
| Engine | The installed `claude`, minimum 2.1.288. Each version is checked by a zero-token conformance probe and pinned if it fails |
| Rendering | **Inline** by default, like Claude Code's classic renderer: finished items are committed into the terminal's own scrollback and only the live area redraws. Alt-screen sub-views for ctrl+o, `/diff` and big pickers. Fullscreen renderer in M3 (plan 12) |
| Extensibility | **Built-ins are mods too.** Every feature registers through `pkg/ext`, keyed by stable IDs. User mods can Replace, Wrap, Remove or Alias any built-in without editing core files |
| Self-modification | Rebuild from source: no Go `plugin` package, no interpreter. Needs a Go toolchain on the machine (MVP) |
| Settings | Read every Claude Code settings scope and honour its UI keys. mantle's own settings live only in `~/.mantle/settings.json` (Claude Code's settings schema is strict, and `-p` silently ignores invalid files). **Never write `~/.claude.json`.** Prefer control requests for changes |
| Keybindings | Claude Code's file format and action IDs, so `~/.claude/keybindings.json` works unchanged. `~/.mantle/keybindings.json` holds the `mantle:*` actions |
| Platforms (MVP) | macOS (arm64/amd64) and Linux. Windows later |
| Licensing hygiene | Don't commit strings, themes, palettes or schemas copied from the Claude Code binary or the SDK `.d.ts`. Read them at runtime or write our own. `docs/research/` holds internal notes only |

## Facts that shape the design

Details and sources are in `docs/research/protocol-2.1.288.md`.

- **Spawn.** The engine runs as `claude --output-format stream-json --input-format
  stream-json --verbose --include-partial-messages --permission-prompt-tool stdio` (plus
  options), talking NDJSON in both directions.
  - Copy the official Agent SDK's spawn arguments: Anthropic must keep that path working.
  - **Do not pass `--system-prompt`**, so the default Claude Code system prompt is kept.
- **Rich control channel.**
  - Documented requests: `initialize`, `interrupt`, `set_permission_mode`, `set_model`,
    `set_max_thinking_tokens`, `apply_flag_settings`, `update_settings`,
    `get_context_usage`, `get_usage`, `list_models`,
    `mcp_status/mcp_toggle/mcp_reconnect`, `rewind_files`, `file_suggestions` (for `@`),
    `stop_task`, `background_tasks`, `get_task_output`, `reload_plugins/skills/output_styles`,
    `get_hooks_listing` and `end_session`.
  - The CLI asks mantle for decisions with `can_use_tool` (permissions, AskUserQuestion,
    ExitPlanMode), `hook_callback`, `elicitation` and `request_user_dialog`.
- **Interactive commands are refused.** `local-jsx` slash commands (`/theme`, `/resume`,
  `/login`, `/help`, `/config` panel, …) return "isn't available in this environment", so
  mantle rebuilds them. Prompt-type commands (skills, custom commands, MCP prompts) and
  headless-capable local commands (`/compact`, `/clear`, `/context`, `/model <m>`, …) are
  passed through.
- **Resume doesn't replay history.** mantle reads `~/.claude/projects/<slug>/<id>.jsonl`
  itself to show earlier turns. Switching sessions means restarting the engine with
  `--resume=<id>`.
- **Security: `-p` skips the workspace-trust dialog and `.mcp.json` approval**, and project
  hooks run as soon as `claude` starts. mantle gates **before** spawning the engine.
- **Session visibility.** Headless sessions get entrypoint `sdk-cli`, which hides them from
  Claude Code's own `/resume` picker. mantle's picker shows them all.
- **`--bare` risk.** The docs say `--bare` "will become the default for `-p` in a future
  release"; it drops hooks, skills, plugins, MCP and CLAUDE.md. mantle mirrors the SDK's
  spawn arguments, unsets `CLAUDE_CODE_SIMPLE`, and the conformance probe blocks a bad
  engine version.
- **Bubble Tea v2 inline rendering has sharp edges:**
  - `tea.Println` writes immediately; a print taller than the free space leaves ghost lines
    in scrollback;
  - an inline frame taller than the terminal loses its top lines;
  - `View()` runs after every message.

  That is why mantle has a committer with chunked prints, a bounded live area, delta
  coalescing and slot render caching.
- **bubbles' textarea** has no undo, kill ring, vim, atomic chips or ghost text, so mantle
  writes its own editor (`pkg/ui/editor`).
- **Release cadence.** Claude Code ships almost daily (2.1.286, 287 and 288 within days), so
  drift detection is a first-class feature (plan 11).

## Architecture

```
mantle (launcher, stdlib only, never rebuilt by /mantle)
  └─ supervises ~/.mantle/current/mantle-ui  (same process group; restores terminal on crash;
       │                                     exit 75 = restart; probation → auto-rollback)
       ├─ engine "main"    → claude (own process group, stdio NDJSON)
       ├─ engine "builder" → claude (cwd = ~/.mantle/work/<mod>, for /mantle)
       └─ engine "btw"/bg  → claude (forked side sessions)
```

### Launcher (plan 10)
- **Supervises instead of exec-ing.** `mantle` starts `mantle-ui` and waits for it, so it
  can notice crashes and roll back.
  - Both share a process group: Bubble Tea's suspend sends SIGTSTP to the group.
  - The launcher saves termios first. On an abnormal exit it restores the terminal (leave
    alt screen, show cursor, mouse off, bracketed paste off, pop kitty keyboard, clear
    OSC 9;4) and kills the engine's process group.
- **Exit codes:** `0` normal, `75` restart requested, anything else a crash.
- **Probation.** A newly promoted build is on probation until it writes a healthy marker.
  Two failed launches flip `current` back to `last-good`.
- **Passthrough.** `mantle -p …` and `claude` subcommands (`mcp`, `plugin`, `auth`,
  `doctor`, …) go straight to `claude`, so they work even when the UI build is broken.
- **Own commands:** `mantle versions|rollback|doctor|--safe`.
- **Version store.** Immutable folders `~/.mantle/versions/<build-id>/{mantle-ui,
  manifest.json}`. `current` and `last-good` are symlinks flipped atomically with rename(2).

### Extension API: `pkg/ext` (plan 01 defines it; the full sketch is in `01-contracts-shell.md`)
- **Registration.**
  - `init()` only records a descriptor: `ext.Register(Feature{ID, Order, After, Parity,
    Setup})`.
  - The host runs Setup in a fixed order: core, then built-ins (by Order/After), then mods
    (Order ≥ 1000).
  - Replace, Wrap, Remove and Alias are declarative and resolved after every Setup has run.
    When two mods target the same ID, the conflict is reported and the higher Order wins.
- **Registries:**

  | Registry | Keyed by |
  |---|---|
  | Commands (slash) | name; `Source` = builtin/engine/mod |
  | Actions + Bindings | Claude Code contexts (`Chat`, `Global`, `Confirmation`, …) and action IDs (`chat:submit`, …); mantle-only actions are `mantle:*` |
  | Renderers | `ContentKey`: `user.prompt`, `assistant.text`, `thinking`, `tool.Bash`, `tool.Edit`, `tool.mcp.*`, `system.compact_boundary`, … |
  | Components | fixed Slots: `live`, `status`, `aboveInput`, `input`, `belowInput`, `statusLine`; `header`, `sidebarLeft`, `sidebarRight` (fullscreen only) |
  | Dialogs | ID; the host decides placement (inline replaces the input; centered; alt screen) |
  | Themes | token names (`pkg/theme`) |
  | SettingSpecs | mantle setting keys, which generate the mantle section of `/config` |
  | PromptStages | the submit pipeline (slash routing, `!` mode, attachments, priority) |
  | Interceptors | priority-ordered `tea.Msg` middleware: the escape hatch for mods |
  | Stories | a component rendered at a given width: goldens, previews, and the builder's "eyes" |

- **Messaging.** No separate event bus: `tea.Msg` is the bus. Typed `ext.Subscribe[T]` means
  stream deltas reach only the components that want them. Every engine message carries an
  `EngineID`, and every dialog says which engine is asking.
- **`Ctx`** is valid only on the UI goroutine (Update, View, Run). Cmds never touch state.
- **Safety.** Every component call and Cmd runs under `recover`. A panicking feature is
  disabled with a notice and remembered in `~/.mantle/state/disabled.json`.
- **Introspection.** `mantle-ui catalog --json` lists every registered ID with its kind,
  file, feature and parity IDs. `mantle-ui story <id> --width N` renders a story as text.
  `mantle-ui selftest` checks that the registry resolves and that every story renders at
  60/100/160 columns.
- **Arch test** (`internal/archtest`): `features/a` never imports `features/b`, and `mods/`
  imports only `pkg/...`.
- **Stability.** Additive-only once tagged `contracts-v1`. `ext.APIVersion` is bumped only
  for a deliberate break.

### Rendering (plans 01, 03)
- **Transcript store** (03). Ordered items with stable IDs and revisions, children grouped
  by `parent_tool_use_id`. It is the single source for inline commit, the ctrl+o viewer and
  fullscreen. Renderers are pure functions over items.
- **Committer** (primitive in 01, policy in 03).
  - Commits the longest finished prefix of items into scrollback: rendered at the current
    width, pre-wrapped, and chunked to at most H − liveHeight − 1 lines per print via
    `tea.Sequence(tea.Println…)`.
  - Progressive commit: closed markdown blocks are committed while text is still streaming.
  - Committed lines are never re-rendered.
  - A reprint primitive (`ESC[2J ESC[3J ESC[H` plus a chunked reprint) handles /clear,
    ctrl+l, rewind and session switches.
- **Live area** (running items, spinner, todos, queue, input, footer, status line) is capped
  at H − 1 rows: extra running items collapse to "+N more", and the streaming tail shows its
  last K lines.
- **Coalescing.** The engine bridge merges `stream_event` deltas per content block and
  flushes at most every ~16 ms, or immediately on any non-delta event.
- **Mouse is off in inline mode**, because capturing the wheel breaks native scrollback.

### Engine (plan 02)
- **Spawn and environment.**
  - Flags copy the SDK's, plus `--include-hook-events`, `--forward-subagent-text`,
    `--replay-user-messages` and `-n`, with `initialize.promptSuggestions: true`.
  - Environment: remove `CLAUDECODE`, `NODE_OPTIONS`, `DEBUG`, `CLAUDE_CODE_SIMPLE` and
    `CLAUDE_CODE_SAFE_MODE` (unless the user asked for safe mode). Set
    `CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS=1` and
    `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true`.
  - `Setpgid: true`. stderr is drained into a ring buffer, never the terminal.
- **Transport and decoding.**
  - The reader handles multi-MB lines (never the default `bufio.Scanner`).
  - Decoding is typed but keeps the raw JSON, so unknown types survive.
  - Writes go through a writer goroutine.
- **Control requests.**
  - A request_id correlator with timeouts.
  - A capability cache: features call `Supports(subtype)` and hide their UI if it's false.
  - Undocumented subtypes (`rewind_conversation`, `side_question`, `get_workspace_diff`, …)
    live only in `internal/engine/unstable.go`, each with a fallback.
- **Conformance probe.** Zero tokens. It runs initialize, checks that commands, skills,
  plugins, hooks, memory and models appear as settings say they should, then ends the
  session. It runs whenever the engine version changes.

### Startup gates (plan 05; before any engine spawn)
1. **Workspace trust.** Read `~/.claude.json` `projects[path].hasTrustDialogAccepted`
   (read-only). mantle records its own acceptances in `~/.mantle/trust.json`.
2. **`.mcp.json` server approval.** The result is passed via
   `--settings '{"disabledMcpjsonServers":[…]}'`.
3. **Bypass-permissions warning.**
4. **Engine version check**, plus the conformance probe on change.

A test asserts that the engine never spawns before the gates pass.

### Self-modification: `/mantle` (plan 10)
- **Workspace.** `~/.mantle/src` is a git clone; branch `user` is upstream plus one commit
  per mod. Each request gets a worktree `~/.mantle/work/<id>` on branch `mod/<id>`.
- **Triage.** Config first: if a setting, theme, keybinding, statusLine or output-style
  change satisfies the request, mantle proposes that and rebuilds nothing.
- **Builder engine.**
  - A second headless claude with cwd set to the worktree. Guidance: `CLAUDE.md`,
    `docs/EXTENDING.md`, `catalog` and `story`, plus a builder rules file via
    `--append-system-prompt-file`.
  - Rules: prefer `mods/<id>/` with Replace/Wrap; core seams go in separate commits; always
    add a story and a test; never commit.
  - Permissions: acceptEdits. Bash is limited to go build/test/vet, gofmt, story and
    catalog. No `go get`. Other prompts go to the user's dialogs, labelled "Builder <id>".
  - Runs as a background task with progress, cost and `--max-budget-usd`.
- **Deterministic pipeline** (Go code, not the model; logs in `~/.mantle/builds/<id>/`):
  1. Protected-path diff check (`cmd/mantle/**`, `internal/launcher/**`, `internal/selfmod/**`,
     the go.mod `toolchain` line, the protected list itself)
  2. `gofmt -l`
  3. `go vet ./...`
  4. arch test
  5. `apidiff` on `pkg/*` (must be compatible)
  6. `GOTOOLCHAIN=local go build -trimpath`
  7. `go test ./...` against fakeclaude
  8. `mantle-ui selftest`
  9. pty smoke boot against fakeclaude: first frame, prompt, reply, `/help`, quit 0

  On failure, trimmed errors go back to the builder, up to 3 rounds. After that the worktree
  is kept for `/mantle retry|edit`.
- **Promote.**
  - Commit with trailers `Mantle-Mod: <id>`, `Mantle-Request: …`, `Mantle-Kind: mod|core-seam`.
  - Rebase onto `user` (re-run steps 6–9 if `user` moved), build, install the version and
    flip `current`, all under `flock ~/.mantle/promote.lock`.
  - Active on the next launch. When idle, mantle offers "restart now" (exit 75, then
    `--resume <session>`).
  - `selfmod.confirm` = `always | on-visual-change | never`. The default shows a
    before/after story diff when the look changes.
- **Commands.** `/mantle <request>`, `list`, `show <id>`, `undo <id>` (revert, rebuild,
  promote), `rollback` (flip `current` only), `retry`, `edit <id> <request>`, `status`,
  `update`.
  - `update` rebases the mods onto new upstream; the builder resolves conflicts using each
    mod's `Mantle-Request` intent, then the full pipeline runs.
- **Dev mode.** `selfmod.source=~/mantle` works on `mantle/mod-<id>` branches and never
  auto-merges into `main`. `/mantle upstream <id>` exports a mod from `~/.mantle/src` to the
  dev repo.

## Repo layout and primary paths

file-baton serializes edits to the same file, so **anyone may make small edits anywhere**.
The primary owner drives the design of their paths. For larger changes in someone else's
area, leave a request note (rule 2).

| Paths | Primary | Plan file |
|---|---|---|
| `pkg/ext`, `pkg/theme`, `cmd/mantle-ui`, `internal/app`, `internal/keymap`, `internal/config`, `internal/testkit`, `internal/archtest`, `pkg/ui` (base widgets) | 01 | [01-contracts-shell.md](01-contracts-shell.md) |
| `pkg/proto`, `internal/engine`, `internal/testkit/enginefake`, `cmd/fakeclaude`, `cmd/fakeapi`, `scripts/record-fixture.sh`, `scripts/sdk-diff` | 02 | [02-proto-engine.md](02-proto-engine.md) |
| `pkg/render`, `pkg/ui/diffview`, `features/transcript` | 03 | [03-render-transcript.md](03-render-transcript.md) |
| `pkg/ui/editor`, `features/input` | 04 | [04-input.md](04-input.md) |
| `features/turn` | 05 | [05-turn-permissions.md](05-turn-permissions.md) |
| `internal/sessions`, `features/sessions` | 06 | [06-sessions-context.md](06-sessions-context.md) |
| `internal/term`, `features/chrome` | 07 | [07-chrome-terminal.md](07-chrome-terminal.md) |
| `features/settings` | 08 | [08-settings-panels.md](08-settings-panels.md) |
| `features/ecosystem`, `internal/claudecli` (safe `claude` subcommand runner, usable by any plan) | 09 | [09-ecosystem-panels.md](09-ecosystem-panels.md) |
| `cmd/mantle`, `internal/launcher`, `internal/selfmod`, `features/selfmod`, `mods/`, `docs/EXTENDING.md`, `scripts/install.sh` | 10 | [10-selfmod-launcher.md](10-selfmod-launcher.md) |
| `internal/cli`, `scripts/drift` | 11 | [11-cli-drift.md](11-cli-drift.md) |
| `features/fullscreen`, `test/e2e`, `test/parity` | 12 | [12-fullscreen-audit.md](12-fullscreen-audit.md) |
| Shared: `go.mod`, `go.sum`, `Makefile`, `CLAUDE.md`, `features/all/all_<area>.go`, `internal/deps` | anyone (small, additive edits) | n/a |
| `docs/plans/NN-*.md`, `testdata/fixtures/<NN>/` | that plan | n/a |

`features/all/all_<area>.go` blank-imports one area and carries a build tag
(`//go:build !no_<area>`), so `go build -tags no_input ./cmd/mantle-ui` still builds while
`features/input` is broken.

## Parallel session layout

### How the split works
1. **No serial Phase 0.** The shared contracts are produced as the *first milestone* of two
   sessions:
   - S01 writes `pkg/ext` v1, `pkg/theme` tokens, the transcript `Item`/`ContentKey` model
     and the keymap ID table, then runs `git tag contracts-v1`.
   - S02 writes `pkg/proto`, then runs `git tag proto-v1`.

   The exact API shape is already in `01-contracts-shell.md` and `02-proto-engine.md`, so
   every session codes against the same spec from the start.
2. **Every plan has two parts.**
   - **Part A** needs nothing from other sessions (pure packages, spikes, data layers,
     widgets) and starts immediately.
   - **Part B** integrates through `pkg/ext` and the engine. It starts once the tags it
     needs exist: `git tag -l contracts-v1 proto-v1` (same checkout, no fetch needed).

   Every Part A is hours of work, so in practice nobody waits.
3. **The bootstrap commit** already contains `go.mod` with every dependency pinned and
   `doc.go` stubs for every package, so `go build ./...` works from minute one.

### Sessions

| Session | Plan file | Part A (start immediately) | Part B needs | Produces |
|---|---|---|---|---|
| S01 | `01-contracts-shell.md` | `pkg/ext` v1, `pkg/theme` tokens, `Item` model, keymap ID table, testkit/vt harness, archtest | (none: continues with its own host) | tag `contracts-v1` |
| S02 | `02-proto-engine.md` | Spikes S1–S14/S17, `pkg/proto`, transport/decoder, fakeclaude format, fixture recorder | contracts-v1 | tag `proto-v1` |
| S03 | `03-render-transcript.md` | `pkg/render` (wrap, markdown, streaming cache, chroma), `pkg/ui/diffview` | both | n/a |
| S04 | `04-input.md` | `pkg/ui/editor` (buffer, readline, kill ring, undo, chips, vim state machine), history.jsonl and paste-cache I/O, image clipboard | both | n/a |
| S05 | `05-turn-permissions.md` | Gate logic (trust, `.mcp.json`, bypass), dialog view-models | both | n/a |
| S06 | `06-sessions-context.md` | `internal/sessions`: tolerant JSONL reader, slugs, index cache, subagent files | both | n/a |
| S07 | `07-chrome-terminal.md` | `internal/term`: statusLine runner, notifications, OSC 9;4, title, clipboard, PR badge fetcher | both | n/a |
| S08 | `08-settings-panels.md` | Panel data layers: model and effort items, /config option table, /status data, /terminal-setup installers | both | n/a |
| S09 | `09-ecosystem-panels.md` | Wrappers for `claude mcp/plugin/auth/doctor`; discovery of agents, skills, memory and hooks | both | n/a |
| S10 | `10-selfmod-launcher.md` | Launcher (stdlib) with probation and rollback, versions store, pipeline runner library, install script | both | n/a |
| S11 | `11-cli-drift.md` | Flag table for every 2.1.288 flag, forwarding parser, subcommand passthrough, drift scripts | both | n/a |
| S12 | `12-fullscreen-audit.md` | Starts after M1 | M1 done | n/a |

### Starting a session
Two ways:
- **Script:** `scripts/start-sessions.sh` starts plans 01–11 as Claude Code background
  sessions; watch and answer them with `claude agents`. `scripts/start-sessions.sh 01 02 04`
  starts only those plans. `scripts/start-sessions.sh --print` prints one command per plan
  to paste into terminal tabs. Extra claude flags go in `CLAUDE_ARGS`.
- **By hand:** in `~/mantle`, run `claude -n mantle-NN` and paste:

> You are session NN of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/NN-<name>.md`, then execute that plan. Do Part A now. Start Part B once
> `git tag -l` shows the tags it needs. Commit with prefix `[NN]`.

**Running fewer sessions at once?** Start them in this order: 01, 02, 04, 03, 05, 10, 06,
07, 11, 08, 09.

**Cost.** Eleven parallel sessions use a lot of plan usage. Plan 02's spikes run a few real
`claude` prompts (a few cents). Everything else tests against fakeclaude and fakeapi at no
cost.

### Milestones
- **M1, daily driver:** every `[M1]` task in Part B is done. The user can use `mantle`
  instead of `claude` for real work:
  - trust gate; new, resume and continue sessions;
  - editor (multiline, history, paste collapse, images, `@` files, `/` menu);
  - engine passthrough commands, plus native `/model`, `/clear`, `/compact`, `/resume`,
    `/help` and `/mantle`;
  - streaming markdown, main tool renderers, spinner and turn durations;
  - permission prompts, AskUserQuestion, plan approval, shift+tab modes, esc interrupt,
    queue;
  - the user's statusLine script, footer, todos and window title;
  - generic hand-off to `claude` for every panel not yet native;
  - launcher rollback.

  After M1, start S12. The other sessions continue with their M2 tasks.
- **M2, full parity:** every PARITY.md row is done or explicitly H/X.
- **M3, polish:** fullscreen renderer, fd-handoff instant restart, spellcheck.

## Rules for parallel sessions (also in CLAUDE.md)

1. **Read first.** Before starting, read this file and your plan.
2. **Edit mainly your primary paths.** Editing someone else's files is allowed (file-baton
   serializes it), but keep it small. For larger changes in another area, write a note in
   `docs/plans/requests/<from>-<to>-<slug>.md`.
3. **Contracts are additive-only after their tags.** In `pkg/ext` and `pkg/proto`: no
   renames, no removals, no signature changes. file-baton can't catch semantic breaks across
   files.
4. **Shell commands file-baton can't see.** Never run repo-wide rewriters (`go mod tidy`,
   `gofmt -w .`, `goimports -w .`, `go generate ./...`, cross-directory `sed -i`). Scope
   formatters to your own paths with `make fmt-NN` (or `gofmt -w ./features/input`). For a new dependency,
   run `go get <mod>@<ver>`, then commit `go.mod`/`go.sum` on their own right away.
5. **No cross-feature imports.** `features/a` never imports `features/b` (the arch test
   enforces this). Talk through ext IDs and messages.
6. **Your packages compile at the end of every turn.** file-baton releases your files when
   the turn ends. Per-area build tags (`-tags no_<area>`) let others build while yours is
   broken.
7. **Scoped tests.** Run `make test-NN`. Run golden `-update` only in your own packages.
8. **Commit only your files.** Name them explicitly (never `git add -A`) and use the prefix
   `[NN]`. The file-baton commit guard backs this up.
9. **Parity status** lives in your plan's checklist plus `Parity:` tags in registrations.
   `make parity` generates the report. Don't edit PARITY.md per feature.
10. **Fixtures** go in `testdata/fixtures/<NN>/`, sanitized (no paths, usernames or tokens).

## Coverage codes and milestones (used in PARITY.md)

| Code | Meaning |
|---|---|
| **E** | Engine passthrough: sent as a prompt or command; mantle renders the output |
| **R** | Rendered by mantle from engine data |
| **N** | Native mantle implementation (settings files, control requests, `claude` subcommands, JSONL) |
| **H** | Hand off to the real `claude`: stop the engine, `tea.ExecProcess("claude --resume <sid> …")`, then restart the engine |
| **X** | Known gap: infeasible from outside the binary; documented with the reason |

**PARITY.md ID prefixes:**

| Prefix | Area | Prefix | Area |
|---|---|---|---|
| ENG | engine plumbing | ED | prompt editor |
| AC | autocomplete | HI | history |
| TC | turn control | TR | transcript |
| VW | views | CH | chrome |
| PD | permissions & dialogs | SE | sessions |
| CU | context & usage | ST | settings & model |
| EC | ecosystem | CF | config compatibility |
| CLI | CLI | CL | cloud & product (H) |
| GAP | known gaps | MT | mantle-only |

## Top risks

| Risk | Mitigation |
|---|---|
| Near-daily Claude Code releases drift the protocol, flags or commands | Tolerant decoding with raw retention; conformance probe and pinning; `scripts/sdk-diff`; `make drift`; capability-gated UI |
| `--bare` becomes the `-p` default | Copy SDK spawn args; unset `CLAUDE_CODE_SIMPLE`; probe blocks bad versions and offers the last good one |
| Undocumented control requests change | Isolated in `unstable.go` behind `Supports()` with fallbacks; never on the M1 path |
| Inline rendering artifacts and slowness | Committer with chunked prints, progressive commit, coalescing, slot cache, vt-emulator tests, terminal matrix (iTerm2, Ghostty, Terminal.app, kitty, WezTerm, VS Code, tmux) |
| A self-mod breaks mantle | Protected launcher, pty smoke boot, probation and auto-rollback, `mantle --safe`, panic isolation |
| Security (`-p` skips trust while project hooks run) | Gates before spawn; narrow builder permissions; protected-path diff check |
| Races with claude on config files | Never write `~/.claude.json`; prefer control requests; atomic flock'd merge-writes for `settings*.json` |
| Restarting the engine kills background tasks | Check `background_tasks` first; warn or defer; fd handoff in M3 |
| Parallel sessions break each other's APIs | Spec in the plan files, additive-only contracts after tags, arch test, build tags |

## Further reading

- [`docs/PARITY.md`](../PARITY.md): every Claude Code feature, with ID, code, milestone,
  owner and part.
- [`docs/research/protocol-2.1.288.md`](../research/protocol-2.1.288.md): headless protocol
  spec (flags, env, messages, control requests, sessions).
- [`docs/research/inventory-binary-2.1.288.md`](../research/inventory-binary-2.1.288.md):
  features found in the installed binary (CLI, slash commands, keybindings, settings, env,
  tools, `~/.claude` layout, JSONL records).
- [`docs/research/inventory-docs.md`](../research/inventory-docs.md): features from the
  official docs, tagged engine / UI / unclear, and the "not in headless" list.
- [`docs/research/design-review.md`](../research/design-review.md): architecture critique,
  verified library versions and APIs, the `pkg/ext` sketch, spikes S1–S17.
- Plans: [01](01-contracts-shell.md) · [02](02-proto-engine.md) ·
  [03](03-render-transcript.md) · [04](04-input.md) · [05](05-turn-permissions.md) ·
  [06](06-sessions-context.md) · [07](07-chrome-terminal.md) · [08](08-settings-panels.md) ·
  [09](09-ecosystem-panels.md) · [10](10-selfmod-launcher.md) · [11](11-cli-drift.md) ·
  [12](12-fullscreen-audit.md)
