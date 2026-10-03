# mantle design review (architecture critique and refined plan)

> Internal research notes for mantle planning (Claude Code 2.1.288, collected 2026-10-03). Not shipped.
> Derived from the installed binary, the official docs and the Agent SDK; do not copy strings/schemas from here into shipped code.

Output of the planning agent that reviewed the initial mantle architecture. Checked against: bubbletea v2.0.10, bubbles v2.2.1, lipgloss v2, glamour v2, teatest v2 and x/vt (fetched from proxy.golang.org), `@anthropic-ai/claude-agent-sdk` 0.3.288 (matches CLI 2.1.288), and the installed `claude` 2.1.288 binary.

The approved master plan (`docs/plans/00-overview.md`) adopted most of this, with two changes: there is **no serial Phase 0** (contracts are the first milestones of plans 01 and 02, tagged `contracts-v1` / `proto-v1`), and plan numbering differs (08a/08b → 08/09, 09 → 10, 10 → 11, 11 → 12). Ownership became "primary owner" guidance because file-baton serializes same-file edits.

---

## 0. Summary of recommended changes

1. **The launcher supervises mantle-ui, not exec into it.** An exec'ing launcher is gone once the UI starts, so it can't notice a crash, roll back, relaunch, or repair a terminal left in raw/kitty mode. Run `claude` in its own process group.
2. **A contracts step before parallel work.** go.mod with every dependency, `pkg/proto`, `pkg/ext` v1, `pkg/theme` token names, the transcript `Item` model, slot/context/action ID tables, the testkit API and the ownership table. Replace "frozen after phase 1" with **additive-only, with a steward session**.
3. **Deterministic registration order.** `init()` only records a descriptor; the host runs Setup in a fixed order (core, built-ins, mods). Replace/Wrap/Remove resolve after all Setup calls. Group features by area package; one package per feature (~300 packages) is too many.
4. **The rendering core is a transcript store plus a committer.** Bubble Tea v2 inline mode has sharp edges (1.3): prints larger than the free space leave ghost lines, frames taller than the terminal lose their top lines, `View()` runs after every message. Needs a bounded live area, chunked prints, progressive commit of streamed text, delta coalescing and a reprint primitive. Temporary alt-screen for the ctrl+o viewer and heavy panels.
5. **Let the builder see what it builds.** `mantle-ui catalog --json` (every registered ID with kind, file, owner) and `mantle-ui story <id> --width N` (render a component as text). Stories double as golden tests.
6. **Self-mod changes:** config change before code change; protected paths enforced by a pipeline diff check (not only permission rules); API-compatibility check on `pkg/*`; boot-test the new build in a pty; a new build is on probation and auto-rolls back; each mod is one commit in a patch stack; `mods.json` derived from git.
7. **Copy the official SDK's spawn** (1.4). Add `--include-hook-events`, `--forward-subagent-text`, prompt suggestions. Pin the engine version and run a zero-cost conformance probe on Claude Code updates (the guard against `--bare` becoming default).
8. **Handoff fallback ("H")** for any panel not yet native: when idle, stop the engine, `tea.ExecProcess("claude --resume <sid>")`, restart the engine on exit. Gets to a usable daily driver months earlier.
9. **Fake Anthropic API** (`ANTHROPIC_BASE_URL` + isolated `CLAUDE_CONFIG_DIR`): deterministic, free end-to-end tests of the real engine and side-by-side comparison with interactive Claude Code.
10. **mantle settings never go in `~/.claude/settings.json`** (strict schema; `-p` silently ignores invalid files). Don't write `~/.claude.json` (no lockfile in 2.1.288); mantle keeps its own trust store.
11. **Tag every task M1 (daily driver), M2 (parity) or M3 (polish).** Fullscreen renderer moves to the last phase.

---

## 1. Critique

### 1.1 Process model and launcher

**Problem.** If `mantle` execs mantle-ui, "crash-loop auto-rollback and relaunch with notice" can't be built. Health markers alone only catch a bad build on the *next* manual launch, and can't restore the terminal after SIGKILL or a hard crash.

**Design:**
- **`mantle` (launcher):** stdlib only, built once by `make install`, never rebuilt by `/mantle`.
  - Starts mantle-ui with inherited stdio, **in the same process group**, and waits. Verified in bubbletea v2 `tty_unix.go`: suspend runs `syscall.Kill(0, SIGTSTP)` on the whole group — launcher and UI both stop, the shell sees the job stop, `fg` resumes both. The launcher must not ignore SIGTSTP.
  - Forwards SIGTERM/SIGHUP. Ignores SIGINT (in raw mode ctrl+c arrives as a key; only matters during ExecProcess).
  - Saves termios before starting the UI. On abnormal exit restores termios and writes reset sequences: leave alt screen, show cursor, mouse modes 1000/1002/1003/1006 off, bracketed paste off, pop kitty keyboard `CSI < u`, reset mode 2027, clear OSC 9;4 progress.
  - Handles `mantle versions|rollback|doctor|--safe` itself.
  - Passes claude subcommands and `-p` straight through with `syscall.Exec("claude", …)` so they work even when the UI build is broken.
- **Exit codes:** `0` normal; `75` restart requested (launcher re-reads `current`, relaunches with handoff args from `~/.mantle/run/<pid>.json`: session id, cwd, original argv); anything else = crash.
- **Probation:** a newly promoted build stays on probation until it writes "healthy" (first frame drawn, engine initialized, 20 s alive, or clean exit 0). Two failed probation launches → flip `current` to `last-good`, print a notice, relaunch with `--resume`.
- **Engine process:** spawn `claude` with `Setpgid: true` so terminal signals can't reach it (e.g. ctrl+c inside `$EDITOR` during ctrl+g) and it keeps working while mantle is suspended. mantle-ui writes the engine pgid to its run file; macOS has no PDEATHSIG, so the launcher kills that group if the UI dies abnormally.
- **Version store:** `~/.mantle/versions/<build-id>/{mantle-ui, manifest.json}` immutable (git sha, upstream base sha, mod list, Go version, pipeline report). `current` and `last-good` symlinks flipped atomically with rename(2). GC keeps the last 10 versions plus any referenced by a live run file; several mantle instances can run different versions at once.
- **"Restart now":** M1 — when idle and `background_tasks` is empty, exit 75 and restart claude with `--resume` (otherwise defer to next launch; restarting claude kills background shells and subagents). M3 — fd handoff: mantle-ui `syscall.Exec`s the new binary keeping the same PID so the claude child and pipes survive (`--attach-engine-fds=3,4,5`).

### 1.2 `pkg/ext` and "built-ins are mods too"

The idea is right; details:
1. **Ordering.** Go initializes packages in dependency order, otherwise (since 1.21) by sorted import path. "Mods register after built-ins" would silently depend on `features/` sorting before `mods/`. Fix: `init()` only appends a `Feature{ID, Order, After, Setup}`; the host calls Setup in order core → built-ins (by Order/After) → mods; Replace/Wrap/Remove are declarative and resolved afterwards. Two mods replacing the same ID → report it; higher Order wins.
2. **Granularity.** Registration unit = feature; package = area (`features/input`, `features/transcript`). Only heavy subsystems get sub-packages (editor, markdown). 300 packages slow builds and invite import tangles.
3. **"Frozen" is premature.** Make it additive-only, add `ext.APIVersion`, use `Alias(old,new)` for renamed IDs. The 01 session stays on as API steward and merges change requests at integration windows.
4. **Fit with the Elm architecture (verified in the bubbletea `eventLoop`).** Every message runs `Update` then `View()`; frames flush on a ticker (60 fps default). So: all state changes in Update on the loop goroutine; Cmds never touch state, only return messages; **View must be cheap** — the host caches each slot's last render and re-renders only invalidated/resized/theme-changed slots. Use pointer components internally; the root still satisfies `tea.Model`.
5. **No separate event bus.** `tea.Msg` is the bus. Add typed subscriptions (`Subscribe[T]`) so stream deltas don't fan out to 80 components, and interceptors (priority-ordered message middleware) as the general escape hatch for mods.
6. **Import discipline.** `internal/` is visible to everything in the module, so "public API" is convention only. Enforce with an arch test: `mods/...` may import only `pkg/...`. Shared widgets go in `pkg/ui`, the render kit in `pkg/render`, protocol types in `pkg/proto`.
7. **Add:** Stories (named fixtures rendering a component at a width: goldens, previews, builder), Catalog (every ID with kind, file, feature, parity IDs, description), SettingSpec (generates the mantle section of `/config`), Parity tags on registrations.
8. **Slots, not a layout engine.** Inline: a vertical stack. Fullscreen (M3): the same stack plus a transcript viewport, left/right sidebars, overlays via lipgloss v2 `Compositor` (has `Hit(x,y)` for mouse hit-testing, verified). Dialog placement is host policy: inline, a dialog replaces the input region (as Claude Code does for permission prompts and pickers); in alt-screen/fullscreen it is a centered layer.
9. **Several engines at once.** Main session, `/mantle` builder, `/btw` fork, possibly background sessions. Every engine message carries `EngineID`; every dialog shows which engine asks ("Builder mod-a3f wants to run…").
10. **Later: model-callable tools.** The control channel supports in-process SDK MCP servers (`initialize.sdkMcpServers` + `mcp_message`, verified in `sdk.d.ts`), so a mod could expose a tool. Opt-in only: by default mantle must not change what the model sees (no extra tools, no prompt changes).

### 1.3 Rendering: inline vs fullscreen (verified Bubble Tea v2.0.10 behaviour)

**Verified facts:**
- `tea.Println` and `tea.Printf` are Cmds (`Program.Println` also exists); documented no-ops in alt screen.
- `insertAbove` writes to the terminal immediately — not on the frame ticker, not wrapped in synchronized output. It scrolls with newlines, then `CursorUp(offset+h-1)`, then `InsertLine(offset)`. Correct only when the printed line count ≤ terminal height − live-frame height; larger prints leave a ghost copy of the live area in scrollback. Its wrap estimate (`lineWidth/w`) is approximate.
- An inline frame taller than the terminal silently loses its top lines (`cellbuf.Lines[frameHeight-height:]`).
- Declarative View fields: `AltScreen`, `MouseMode`, `KeyboardEnhancements`, `WindowTitle`, `ProgressBar` (native OSC 9;4), `Cursor`, `ReportFocus`, `DisableBracketedPasteMode`, `Background/ForegroundColor`, `OnMouse`.
- Negotiated automatically: synchronized output (2026) and grapheme widths (2027).

**Design:**
- **Transcript store** (owned by the transcript plan) is the single source of truth: ordered items with stable IDs and revisions, children grouped by `parent_tool_use_id`.
- **Committer** (primitive in core shell, policy in transcript):
  - Commit the longest prefix of finished items: render at current width/mode, pre-wrap, chunk to ≤ H − liveHeight − 1 lines, send with `tea.Sequence(tea.Println…)`, advance the watermark in the same Update.
  - **Progressive commit:** for the streaming text item at the watermark, commit closed top-level markdown blocks (paragraphs, closed code fences, finished lists) as they complete.
  - Committed lines are never re-rendered.
  - `supersedes` can replace content already in scrollback → print a dim "response replaced" marker plus the new content (spike: when does `supersedes` actually happen?).
- **Live area:** running items, spinner, todos, queue, input, footer, status line. Budget H − 1 rows. Running items beyond budget collapse into "+N more running"; the streaming tail shows its last K lines.
- **Reprint primitive:** `ESC[2J ESC[3J ESC[H`, then reprint the whole store in chunks. Used for /clear, ctrl+l, rewind, session switch, (optionally) theme change.
- **Alt-screen sub-views:** ctrl+o viewer (less keys, search, expand), `/diff`, large pickers. Entering alt screen preserves inline scrollback for free.
- **Mouse:** off in inline mode (capturing the wheel breaks native scrollback).
- **Fullscreen renderer (M3):** alt screen + `MouseModeCellMotion`, virtual list over the same store, per-item line cache keyed by (id, rev, width, mode, themeRev), mantle's own selection and copy.
- **Engine bridge coalescing:** merge `stream_event` deltas per content index; flush at most every ~16 ms, or immediately on any non-delta event, preserving order.

### 1.4 Engine client

- **Spawn like the SDK** (`sdk.mjs` 0.3.288): `--output-format stream-json --verbose --input-format stream-json` plus, as options require: `--thinking adaptive|disabled`, `--thinking-display`, `--max-thinking-tokens`, `--effort`, `--model`, `--permission-prompt-tool stdio`, `--permission-prompts`, `--permission-mode`, `--resume=`, `--fork-session`, `--resume-session-at=`, `--resume-drops-turn=`, `--session-id=`, `--no-session-persistence`, `--allowedTools`, `--disallowedTools`, `--tools`, `--mcp-config`, `--setting-sources=`, `--strict-mcp-config`, `--fallback-model`, `--include-hook-events`, `--include-partial-messages`, `--add-dir`, `--plugin-dir`. Sets `CLAUDE_CODE_ENTRYPOINT=sdk-ts` if unset; deletes `NODE_OPTIONS`, `DEBUG`. Anthropic has to keep that path working; flags the SDK never passes carry more risk.
- **Add:** `--include-hook-events`, `--forward-subagent-text`, `initialize.promptSuggestions: true` (ghost text), `--replay-user-messages` (acks for queued messages; needed for queue display and take-back), `-n/--name` passthrough.
- **Environment:** delete `CLAUDECODE`, `NODE_OPTIONS`, `DEBUG`, `CLAUDE_CODE_SIMPLE` (set by bare mode), `CLAUDE_CODE_SAFE_MODE` unless requested. Set `CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS=1`, `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true`, `CLAUDE_CODE_ENTRYPOINT=<spike>` (test `cli`, `sdk-cli`, `mantle` for picker visibility and feature gating).
- **Transport:** `bufio.Reader.ReadSlice` loop accumulating multi-MB lines; decode `{type, subtype}` envelope first, then the typed struct; keep raw JSON so unknown types survive. Writes through a writer goroutine with a queue; never block Update on a pipe.
- **Control requests:** correlator (request_id → pending, timeouts); capability cache (an "unsupported subtype" error disables it; features call `Supports()` to hide UI). Undocumented subtypes all in `internal/engine/unstable.go` with per-version notes.
- **CLI requests** (`can_use_tool`, `hook_callback`, `elicitation`, `request_user_dialog`, `mcp_message`) become `tea.Msg`s carrying a `Reply` func; `control_cancel_request` closes the matching dialog. Declare in `supportedDialogKinds` only kinds actually implemented (a declared-but-mishandled kind stays pending; answering `cancelled` = user dismissed).
- **Engine binary and conformance probe:** record which claude versions pass a probe (`~/.local/share/claude/versions/` keeps three). The probe costs no API tokens: initialize, check commands non-empty, plugins/skills/hooks present when settings say so (`get_hooks_listing`, init `plugins`, memory files in context usage), models listed; then `end_session`. Run on version change; if it fails, offer to pin the last passing version.
- **Protocol types:** hand-written in `pkg/proto`. `scripts/sdk-diff` fetches SDK `0.3.<patch>` for CLI `2.1.<patch>` from npm into a cache and diffs control subtypes and message unions against a table in `pkg/proto`. Don't commit the `.d.ts` (license).

### 1.5 Self-mod pipeline

- **Config first.** The builder's first step decides whether settings can satisfy the request (`spinnerVerbs`, `statusLine`, `~/.claude/themes/*.json`, `keybindings.json`, output styles, mantle settings). If so, propose the config edit; nothing is rebuilt.
- **Give the builder a map and eyes:** `CLAUDE.md`, `docs/EXTENDING.md`, `mantle-ui catalog --json`, `mantle-ui story <id> --width N`. Builder rules via `--append-system-prompt-file` (builder session only): prefer `mods/<id>/` with Replace/Wrap; edit core only to add a seam, as a separate commit; always add a story and a test; never commit; never touch protected paths.
- **Builder permissions:** acceptEdits (edits inside its cwd only). Allowed Bash: `go build:*`, `go test:*`, `go vet:*`, `gofmt:*`, `go run ./cmd/mantle-ui:*`, `./bin/mantle-ui story:*|catalog:*`, `git diff:*`, `git status`. Not allowed: `go get` (new deps prompt the user). Denied: `git commit/push`, `rm -rf`. Everything else prompts via mantle dialogs labelled with the builder's ID.
- **Protected paths enforced by the pipeline** (shell commands can write anything in the worktree): check `git diff --name-only base..HEAD` against `cmd/mantle/**`, `internal/launcher/**`, `internal/selfmod/**`, the go.mod `toolchain` line, and the protected-path list itself. For `pkg/*` require `apidiff` (golang.org/x/exp/cmd/apidiff) to report a compatible change.
- **Deterministic pipeline** (`internal/selfmod`; each step with timeout and log in `~/.mantle/builds/<id>/`):
  1. protected-path check
  2. `gofmt -l`
  3. `go vet`
  4. arch test
  5. apidiff
  6. `GOTOOLCHAIN=local go build -trimpath`
  7. `go test ./...` with the fake engine (`-race` only on `internal/engine`)
  8. `mantle-ui selftest`: registry resolves; every story renders at 60/100/160 columns without panic and within width
  9. pty smoke boot of the new binary against fakeclaude: first frame, type a prompt, see the reply, open /help, quit with exit 0

  On failure feed trimmed errors back to the builder, up to 3 rounds; then keep the worktree and offer `/mantle retry|edit`.
- **Visual approval.** Intended UI changes change goldens. The builder may `-update` only packages it touched. Before promotion, show a before/after diff of changed stories. Setting `selfmod.confirm`: `always | on-visual-change | never`.
- **Commit and promote:** one commit per request with trailers `Mantle-Mod: <id>`, `Mantle-Request: …`, `Mantle-Kind: mod|core-seam`. Rebase onto `user` (if `user` moved, re-run steps 6–9). Build from `user` HEAD, install the version, flip `current`, all under `flock ~/.mantle/promote.lock`. `mods.json` is a cache derived from `git log --grep Mantle-Mod`.
- **Builder runs as a background task** with a collapsible progress block and cost display (`selfmod.maxBudgetUsd` → `--max-budget-usd`).
- **Dev mode.** `selfmod.source=<dev repo>` makes worktrees from the dev repo on branch `mantle/mod-<id>`; never auto-merge into `main` there (parallel sessions are dirtying it); install from the branch for testing. `/mantle upstream <id>` exports a mod commit from `~/.mantle/src` to the dev repo.

### 1.6 How mods override built-ins, and upstream merges

- **Two ways to change a built-in:** override by ID in `mods/<id>/` (Replace, Wrap, Remove, interceptor, settings) — merge-safe; or patch core directly — allowed, isolated in its own commit, more likely to conflict on update. When an extension point is missing, the builder adds a minimal seam commit (natural upstream candidates).
- **ID stability upstream:** renames via `Alias`; removal is a one-release deprecation. The catalog is the contract.
- **`/mantle update`:** fetch upstream, `git rebase upstream/main user` one commit at a time. On conflict the builder resolves it using that commit's `Mantle-Request` text as intent. Then the full pipeline, including each mod's own tests (required by builder rules) — that is what makes upstream refactors safe for mods.
- **`/mantle undo <id>`** reverts that commit, runs the pipeline, promotes. **`/mantle rollback`** only flips `current`.

### 1.7 Missing pieces (to add)

- Contracts step, steward role, integration windows.
- Stories, catalog, arch test.
- **Handoff (H)** to interactive `claude` for any local-jsx panel not yet native. Stop the engine first so two processes never write the same session JSONL.
- **Fake API + isolated `CLAUDE_CONFIG_DIR`** for free, deterministic end-to-end tests and side-by-side parity runs.
- Engine pinning, conformance probe, SDK diff.
- **Startup gates before spawn:** trust (read `~/.claude.json` `hasTrustDialogAccepted`; spike S10 confirms whether a trusted parent dir counts; keep acceptances in mantle's own store); `.mcp.json` approval (pass disabled servers in `--settings '{"disabledMcpjsonServers":[…]}'`); bypass warning; version check; Go toolchain check for `/mantle`. **Security:** in `-p`, an untrusted repo's project hooks run as soon as claude starts, so the gate must precede spawn.
- **Settings write policy:** prefer control requests (`set_model`, `set_permission_mode`, `apply_flag_settings`, `update_settings`); let the engine persist "don't ask again" rules via `updatedPermissions`; otherwise write atomically under flock, re-reading and merging just before the write; never write `~/.claude.json`.
- Write `history.jsonl` ourselves if headless doesn't (spike S3); single-line `O_APPEND` appends.
- Logs: UI stderr to `~/.mantle/logs/<pid>.log`, engine stderr ring buffer, `mantle doctor`.
- Clock injection and `testing/synctest` (Go 1.27) for spinner, coalescer and timeout tests.
- Screen-reader flat mode (`--ax-screen-reader`) needs an owner (chrome plan).
- Decide `--worktree`, `--tmux`, `--bg`, `--remote-control`, `--teleport`, `--cloud`: pass through or hand off (CLI plan).

### 1.8 Cut or defer

- General layout engine → fixed slots.
- Separate event bus → `tea.Msg`.
- Per-feature SelfTest hooks → stories plus normal tests.
- Per-mod KV store → trivial helper (`~/.mantle/state/<id>.json`).
- fd-handoff instant restart and the fullscreen renderer → M3.
- Vim mode → M2, but design the editor for modal input from day one.
- Spellcheck → M3.
- Novelty commands (/stickers, /radio, /passes, /powerup, /wellbeing) and agent view → H.

---

## 2. Verified libraries (latest versions on 2026-10-03)

| Need | Module @ version | Caveats |
|---|---|---|
| Core TUI, inline + scrollback | `charm.land/bubbletea/v2` **v2.0.10** | `tea.Println/Printf` Cmds. Chunk prints, budget the live area, coalesce deltas (1.3) |
| Alt screen + mouse, styling | same + `charm.land/lipgloss/v2` **v2.0.6** (bubbles 2.2.1 needs ≥ 2.0.5) | `View.AltScreen`, `MouseModeCellMotion`, `View.OnMouse`. lipgloss `Canvas`/`Layer`/`Compositor` with `Hit(x,y)` for overlays and hit-testing |
| Paste and keyboard | bubbletea v2 | Bracketed paste on by default: `PasteStartMsg`, `PasteMsg`, `PasteEndMsg`. Basic kitty key disambiguation requested by default → shift+enter / ctrl+enter work on kitty, Ghostty, WezTerm, iTerm2 (CSI u). `KeyboardEnhancementsMsg` reports support; `ReportEventTypes` adds release/repeat. Terminal.app has none: fall back to `\`+Enter, alt+enter (ESC CR), ctrl+j; `/terminal-setup` installs mappings like Claude Code |
| Widgets | `charm.land/bubbles/v2` **v2.2.1** | Use key, help, textinput, viewport. **textarea has no undo, kill ring, vim, atomic chips or ghost text** → custom `pkg/ui/editor` |
| Golden-frame tests | `github.com/charmbracelet/x/vt` **v0.0.0-20261001101533-953920dd3285** + `github.com/charmbracelet/x/exp/golden` **v0.1.0** | `vt.Emulator` exposes `Scrollback()`, `CellAt`, `Render`, `SendKeys`, `Paste`. Run `tea.NewProgram(m, WithInput, WithOutput(emu), WithWindowSize(w,h))` and assert screen and scrollback — the only way to test the Println logic. `x/exp/teatest/v2` exists (same pseudo-version) but gives raw bytes only and its go.mod names bubbletea rc.1 (MVS lifts it); optional |
| PTY end-to-end | `github.com/creack/pty` **v1.1.24** | Pipeline smoke boot; side-by-side harness |
| Markdown | `github.com/yuin/goldmark` **v1.8.6** (GFM extensions) + custom ANSI renderer | `charm.land/glamour/v2` **v2.0.1** exists (crush uses it) but renders whole documents with margins unlike Claude Code's, pins chroma 2.14 and goldmark 1.7.8, no incremental API. Reference/fallback only |
| Highlighting | `github.com/alecthomas/chroma/v2` **v2.27.0** | `lexers.Match(filename)`; custom formatter mapping to theme tokens; cache by content hash; skip above ~2k lines (regex lexers are slow) |
| Fuzzy matching | `github.com/sahilm/fuzzy` **v0.1.3** | Commands, pickers, history. For `@` files use the engine's `file_suggestions` (honours `respectGitignore` and `fileSuggestion`); debounce |
| Clipboard | none (exec) | The 2.1.288 binary uses `pbcopy` and `osascript … «class PNGf»`; do the same. OSC 52 (`tea.SetClipboard`) when `SSH_TTY` is set, with tmux passthrough. Detect image file paths in `PasteMsg` for drag-and-drop. Avoid `golang.design/x/clipboard` (cgo). `tea.ReadClipboard` (OSC 52 read) mostly denied by terminals |
| Diffs | `github.com/aymanbagabas/go-udiff` **v0.4.1**, `github.com/sergi/go-diff` **v1.4.0** | Prefer the engine's `tool_use_result.structuredPatch` for Edit/Write hunks; go-diff for word-level highlights |
| ANSI utilities | `github.com/charmbracelet/x/ansi` **v0.11.8** | Wrap, Truncate, StringWidth, Strip, OSC 8 links |
| Emoji shortcodes | `github.com/yuin/goldmark-emoji` **v1.0.6** (`definition` package) | GitHub emoji set |
| External editor | `github.com/charmbracelet/x/editor` **v0.2.0** + `tea.ExecProcess` | ctrl+g |
| Misc | `fsnotify` v1.10.1 (hot reload), `google/uuid` v1.6.0, `dustin/go-humanize` v1.1.0, `rsc.io/qr` v0.2.0 (M3) | JSON: plain `encoding/json` (`encoding/json/v2` still behind `GOEXPERIMENT=jsonv2` in Go 1.27.1). Flags: hand-written table-driven parser (unknown claude flags must be forwarded verbatim; cobra/fang get in the way). Locks: `syscall.Flock` |
| Reference code | `github.com/charmbracelet/crush` v0.97.1 | bubbletea v2.0.9 agent UI using glamour v2, chroma 2.27, sahilm/fuzzy, x/editor, golden. Worth reading |

Pin pseudo-versions (x/vt, ultraviolet) exactly and upgrade the Charm set together.

---

## 3. Phases, dependencies, ownership and parallel rules (as proposed)

### Phases (original proposal; superseded by the master plan's parallel layout)

```
Phase 0  Contracts (1 session, serial, 1-2 days)
           └─> Phase 1 (3 parallel): 01 core shell | 02 engine | 03A render kit
                  └─> Phase 2 (parallel, M1 tasks first in every plan):
                        03B transcript, 04 input, 05 turn/permissions/gates, 09 selfmod+launcher,
                        06 sessions, 07 chrome, 08a settings panels, 08b ecosystem panels, 10 CLI/drift
                        └─> Phase 3: 11 fullscreen + accessibility + parity audit + perf
```

- Real Phase 2 dependencies: 05's diff preview needs 03A's `pkg/ui/diffview`; 06 produces transcript items 03B renders (`Item` model frozen in contracts).
- Recommended at most 5 concurrent sessions: Wave A (M1 critical path) 03B, 04, 05, 09, plus M1 parts of 06/07; Wave B the rest.

**M1 "daily driver":** trust gate; new/resume/continue sessions; editor with multiline, history, paste collapse, images, @-files, slash menu; engine passthrough commands plus native /model, /clear, /compact, /resume, /help, /mantle; streaming markdown and main tool renderers, collapsed thinking, spinner, durations; permission prompts, AskUserQuestion, plan approval, shift+tab modes, esc interrupt, queue; the user's statusLine script, footer, todos, title, basic notifications; H handoff for everything else; `/mantle` with launcher rollback.

### Ownership table (proposal numbering)

| Path | Owner | Notes |
|---|---|---|
| `go.mod`, `go.sum`, `Makefile`, `CLAUDE.md`, `.gitignore`, `internal/deps/` | 00, then coordinator | All dependencies added up front; `deps.go` blank-imports them so tidy keeps them |
| `pkg/ext/`, `pkg/theme/` | 00, then 01 (steward) | Additive-only |
| `pkg/proto/` | 00, then 02 (steward) | Additive-only |
| `features/all/all.go`, `features/*/area.go` stubs | 00 | |
| `cmd/mantle-ui/`, `internal/app`, `internal/keymap`, `internal/config`, `internal/testkit` (except `enginefake`), `pkg/ui/` (except `editor`, `diffview`) | 01 | |
| `internal/engine`, `internal/testkit/enginefake`, `cmd/fakeclaude`, `cmd/fakeapi`, `scripts/record-fixture.sh`, `scripts/sdk-diff` | 02 | |
| `pkg/render`, `pkg/ui/diffview`, `features/transcript` | 03 | |
| `pkg/ui/editor`, `features/input` | 04 | |
| `features/turn` | 05 | Permissions, dialogs, modes, gates, turn control |
| `internal/sessions`, `features/sessions` | 06 | |
| `features/chrome`, `internal/term` | 07 | Notifications, OSC, clipboard service |
| `features/settings` | 08a | |
| `features/ecosystem` | 08b | |
| `cmd/mantle`, `internal/launcher`, `internal/selfmod`, `features/selfmod`, `mods/`, `docs/EXTENDING.md`, `scripts/install.sh` | 09 | |
| `internal/cli`, `scripts/drift` | 10 | `cmd/mantle-ui/main.go` calls `cli.Parse`; call site frozen |
| `features/fullscreen`, `test/e2e`, `test/parity` | 11 | |

### Rules for safe parallel sessions with file-baton (proposal)

1. Edit only owned paths; requests to another plan go in new files `docs/plans/requests/<from>-<to>-<slug>.md`.
2. Never run repo-wide rewriters during a phase (`go mod tidy`, `go get`, `gofmt -w .`, `goimports -w .`, `go generate ./...`, cross-directory `sed -i`); file-baton can't see shell edits.
3. Dependency additions batched by the coordinator at integration windows.
4. No cross-feature imports; arch test (`internal/archtest`, `go list -deps -json`) enforces it and that `mods/` imports only `pkg/`.
5. Your packages compile at the end of every turn.
6. Per-area build tags: `features/all/all_input.go` carries `//go:build !no_input`, so `go build -tags no_input ./cmd/mantle-ui` works while an area is broken.
7. Scoped tests (`make test-NN`); full `go test ./...` at integration windows.
8. Commit only your files, explicitly named, `[NN]` prefix; file-baton's commit guard backs this up.
9. Parity status not tracked in a shared file; `make parity` generates `docs/parity-status.md` from plan checklists and `Parity:` tags.
10. Golden `-update` only on your own packages.
11. Fixtures in `testdata/fixtures/<plan>/`, sanitized.
12. Risky contract change: `git worktree`, merge at a window, announce in `00-overview.md`.

### Frozen before parallel integration (additive-only afterwards)
`pkg/ext` v1 (registrar, Ctx, Component/Focusable/Dialog, slot IDs, Command/Action/Binding, PromptStage, Interceptor, Story, SettingSpec, Engine, message types with EngineID), `pkg/proto` structs, the `Item`/`ContentKey` model and naming, keymap contexts and action IDs (Claude Code's verbatim), `pkg/theme` token names, testkit Harness API and fakeclaude script format, render-kit signatures, notice API, ID naming convention, `cli.Parse` call site, `features/all` list.

---

## 4. Per-plan details (proposal numbering)

### 00 Contracts
Goal: every later session can start without blocking. Tasks: module path independent of the product name (rename never rewrites imports) with `go 1.27` + `toolchain`; pin all deps from section 2 and add `internal/deps/deps.go`; skeleton packages each with a `doc.go` naming its owner; Makefile `build test lint archtest test-NN parity`; `pkg/proto` (envelopes and every known type/subtype from 2.1.288 `sdk.d.ts`, each with `Raw json.RawMessage`); `pkg/ext` v1 (section 5) plus slot list and context/action ID table (families `chat:*`, `app:*`, `confirm:*`, `select:*`, `scroll:*`, `footer:*`, `diff:*`, `settings:*`, `tabs:*`, `attachments:*`, `plugin:*`, `autocomplete:*`, `transcript:*`, `messageSelector:*`, `modelPicker:*`, `history:*`, `theme:*`, `task:*`, `voice:*`); `pkg/theme` token names and `Item` model; testkit API signatures and fakeclaude script format; `PARITY.md`; `00-overview.md` and plan files; `CLAUDE.md`. Done: `go build ./...`, `go vet ./...`, arch test pass on stubs.

### 01 Core shell
1. Host: descriptor collection, deterministic Setup, override resolution with conflict report; `safeCall` with recover around every component call and Cmd; a panicking component is disabled, notice shown, ID saved to `~/.mantle/state/disabled.json`.
2. Root `tea.Model`: routing order interceptors → dialog stack → focused component → keymap actions → typed subscribers; slot render cache; cursor composition; merged View fields (title, progress, keyboard enhancements).
3. Inline layout with height budget; committer primitives `Print(blocks)` chunked with `tea.Sequence`, and `Reprint()`.
4. Dialog stack with placement policy (inline replaces input; centered; alt-screen sub-view).
5. Keymap: Claude Code contexts, chords with timeout, `null` unbinds; reads `~/.claude/keybindings.json` (Claude Code actions only) plus `~/.mantle/keybindings.json` (`mantle:*`); hot reload; conflict report.
6. Config: managed/user/project/local/flag scopes with Claude Code precedence; read-only `~/.claude.json`; `~/.mantle/settings.json`; writer atomic + flock + re-read/merge; fsnotify → `SettingsMsg`.
7. Theme engine: six built-ins plus `~/.claude/themes/*.json`; colour-profile downgrade; light/dark auto-detect via `BackgroundColorMsg.IsDark()`. Don't commit palettes/strings extracted from the Claude Code binary.
8. `pkg/ui` base widgets: select/list with fuzzy filter, tabs, dialog frame matching Claude Code, text field, scroll pane, table, key-hint bar, spinner primitive.
9. Testkit: vt harness (screen + scrollback), golden helpers, story runner; `mantle-ui story|catalog|selftest`.
10. Notice/toast API; Clock.
11. Spikes S15, S16.
12. `internal/archtest`.

Done: vt tests prove chunked Println leaves no ghost lines at 80×24, 120×40, 200×60 with live heights 3–20; chord/unbind parsing tests; override-conflict tests; panicking demo component disabled; `catalog --json` lists demo IDs.

### 02 Engine
1. Spikes S1–S14 first; findings into a "Facts verified on 2.1.288" section of the plan file (file-baton style).
2. Transport (large lines), decoder (typed + raw), writer goroutine.
3. Process supervisor: SDK-style flag builder; env cleanup; `Setpgid`; stderr ring buffer; restart with resume/fork-session/resume-session-at; on unexpected exit a notice with the stderr tail and "restart with --resume".
4. Correlator, capability cache, `unstable.go`; CLI requests become messages with `Reply`.
5. Coalescer.
6. `ext.Engine` implementation and tea bridge; EngineID support for several engines.
7. Session tracker: session id (incl. `conversation_reset`), model, mode, state, cwd, init data (tools, commands, skills, plugins, MCP, output style, fast mode).
8. **fakeclaude:** scripted engine (`emit`, `expect`, `respond`, `request`, `delay`), in-process or binary; fixture recorder that tees both directions and sanitizes.
9. **fakeapi:** Messages API SSE mock (scripted text, thinking, tool_use; generic replies for title/Haiku calls), isolated `CLAUDE_CONFIG_DIR`.
10. Conformance probe and engine pinning.
11. `scripts/sdk-diff`.

Done: fixtures for plain answer; tool use allow/deny/always; AskUserQuestion; ExitPlanMode; interrupt during a tool; queued message with priority; /compact; /clear; resume; subagent; background task; elicitation; local slash command; rate_limit_event; api_retry. Every fixture decodes and round-trips with unknown types preserved; `-race` correlator tests; with fakeapi, real headless claude answers a scripted prompt and runs a Bash tool in a temp dir, offline, free.

### 03 Render kit (A) and transcript (B)
A: ANSI-aware wrapping (hyperlinks, CJK/emoji widths); goldmark renderer in Claude Code's look (headings, lists, quotes, tables, fenced code with highlighting, inline code, OSC 8 links, task lists, `maxProseWidth`); streaming (split into blocks, cache finished blocks, re-render only the open tail); chroma highlighter with caches and size caps; diff view (from `structuredPatch` or old/new strings, word-level highlights, gutters, Claude Code colours); stories and goldens.

B: [M1] transcript store from stream events and from 06's JSONL normalizer, commit policy incl. progressive commit using 01's primitives; [M1] renderers — user prompt (chips, images), assistant text, thinking (shown or collapsed by mode), Bash (output collapse), Read, Write, Edit/MultiEdit (diff), Glob/Grep, WebFetch/WebSearch, Task with nested subagent items, TodoWrite, NotebookEdit, ExitPlanMode plan, AskUserQuestion answers, MCP calls grouped as `mcp__server__tool`, Skill, background shell tools, errors, interrupted markers, permission denials, system events (compact_boundary, api_retry, informational, hook lines, rate limit, model refusal, local_command_output); [M1] spinner line (verbs from `spinnerVerbs`, tips, elapsed, token arrows, thinking tokens, "esc to interrupt"), turn duration ("Cooked for 1m 6s"), timestamps; [M1] live-area overflow policy; [M2] view modes (verbose, brief, focus) and the ctrl+o alt-screen viewer with search and expand.

Done: goldens from fixture replays at 60/100/160 columns; vt test that scrollback after a full replay matches exactly; benchmark 10k items with steady-state `View()` < 1 ms; streaming a 2k-token answer leaves no ghost lines.

### 04 Input
1. [M1] Editor core: grapheme-aware buffer, wrapping, virtual and real cursor; newline on shift+enter, ctrl+j, `\`+Enter, alt+enter; readline keys, kill ring, undo/redo, atomic chips.
2. [M1] Submit pipeline: slash routing (native registry vs engine commands from `initialize`/`init`), `!` bash mode, attachments, priority.
3. [M1] `/` menu (aliases, hidden, argument hints, mid-prompt).
4. [M1] `@` completion via `file_suggestions` plus MCP resources.
5. [M1] Per-project history from `~/.claude/history.jsonl`, writing it if headless doesn't.
6. [M1] Paste collapse + `~/.claude/paste-cache`; image paste and drag-drop chips.
7. [M2] ctrl+r with scope cycling, ctrl+s stash, ctrl+g external editor, `:emoji:`, invisible-character stripping, ultrathink highlight, prompt-suggestion ghost text, vim mode (`editorMode`, remaps).
8. [M3] Spellcheck.

Done: table-driven key-sequence tests; vim tests; 1 MB paste test; `history.jsonl` compatibility tests against the real format; goldens.

### 05 Turn control, permissions, dialogs, gates
1. [M1] Permission prompts by tool type (Bash command, Edit diff preview, Write preview, WebFetch domain, MCP, generic): "don't ask again" → `updatedPermissions`; deny with feedback; tab to amend.
2. [M1] AskUserQuestion (multiple questions, multi-select, free text) via `updatedInput.answers`.
3. [M1] ExitPlanMode approval (auto-accept / manual / keep planning with feedback).
4. [M1] shift+tab cycling via `set_permission_mode` (bypass only if allowed; auto only if available).
5. [M1] esc interrupt; double ctrl+c / ctrl+d exit; queue display and take-back; ctrl+enter send-now (`priority:now`).
6. [M1] Startup gates: trust, bypass warning, `.mcp.json` approval. **Engine not spawned before gates pass** (asserted by a test).
7. [M2] Elicitation forms from JSON schema; `request_user_dialog` kinds (declare only implemented); `control_cancel_request`; ctrl+b; ctrl+x ctrl+k (`stop_task`); usage-limit wait and auto-continue via `rate_limit_event.resetsAt`; permission_denied notices.

Done: fixture-driven keyboard-flow tests for every dialog; gate tests with a temp HOME.

### 06 Sessions and context
1. [M1] Tolerant streaming JSONL reader + normalizer to `Item`s, incl. subagent files.
2. [M1] Resume: print history in chunks, then spawn with `--resume`; also `-c`.
3. [M1] `/clear` (`conversation_reset`, reprint) and `/compact` status.
4. [M1] `/resume` picker: all sessions incl. sdk-cli ones; search, preview, rename, all-projects, branch filter; index cached in `~/.mantle/cache/sessions.idx` keyed by (path, size, mtime), metadata from file tails.
5. [M2] Rewind (esc esc selector; restore code and/or conversation via `rewind_files` + `--resume-session-at`; summarize from/up to).
6. [M2] `/branch`, `/fork`, `/export`, `/copy` (code-block picker), `/rename`, `/recap`; `/btw` (`side_question` if supported, else forked one-shot engine); `/diff` panel (`get_workspace_diff` or `git diff`); `/goal`, `/plan`, `/add-dir`, `/cd`.
7. [M2] `/context` grid (`get_context_usage`); `/usage`, `/cost`, `/stats` (`get_usage`, `get_session_cost`, local JSONL stats); rate limits; auto-compact warnings.

Done: parses all real local sessions read-only behind an opt-in env var; picker < 200 ms warm with 1000 sessions; rewind fixture tests.

### 07 Chrome and terminal
1. [M1] Prompt-box frame, footer (mode indicator, hints, notice area).
2. [M1] statusLine runner: full JSON on stdin (`session_id`, `transcript_path`, `cwd`, `model`, `workspace`, `version`, `output_style`, `cost`, `context_window.*` incl. `remaining_percentage`/`used_percentage`, `exceeds_200k_tokens`); padding, `refreshInterval`, debounce, timeout, ANSI passthrough.
3. [M1] Todo panel (ctrl+t), window title, OSC 9;4 progress, clipboard service.
4. [M2] Subagent panel + `subagentStatusLine`; `/tasks` (`background_tasks`, `get_task_output`, `stop_task`); PR badge (gh/glab, cached); footer links.
5. [M2] Notifications per `preferredNotifChannel` (iterm2, kitty, ghostty, bell) via `tea.Raw`, triggered by idle and requires_action.
6. [M2] Welcome banner, release notes, ctrl+z suspend, `--ax-screen-reader` flat mode.

Done: statusLine parity test — point Claude Code's `statusLine` at a script that saves stdin, do the same under mantle, diff the key sets; notification escape-sequence tests in vt.

### 08a Settings and model panels
1. [M1] `/model`: `list_models`, effort slider (per-model `modelSettings.effortLevel`), fast mode, thinking toggle via `set_model`/`apply_flag_settings`; persist the way Claude Code does.
2. [M1] `/help`.
3. [M2] `/config` tabs (Config/Status/Usage/Stats + auto-generated mantle section), `/status`, `/theme` with live preview, `/output-style`, `/permissions` editor, `/keybindings`, `/terminal-setup`, `/effort`, `/fast`, `/color`, `/vim`. `/privacy-settings` is H.

Done: story and key-flow test per panel; values written by mantle read back correctly by real Claude Code (temp HOME).

### 08b Ecosystem panels
1. [M1] Generic **H handoff** command for every local-jsx command not yet native.
2. [M2] `/mcp`: `mcp_status`/toggle/reconnect; login via ExecProcess of `claude mcp login`; add/remove via `claude mcp …`.
3. [M2] `/plugin` (via `claude plugin …` + `reload_plugins`), `/skills`, `/hooks` (`get_hooks_listing`), `/agents` (agent files), `/memory`.
4. [M2] `/doctor` (`claude doctor` + mantle checks), `/login`/`/logout` (`claude auth …`, then restart engine), `/upgrade`, `/feedback`. Novelty commands: H or N.

Done: panels tested against fakeclaude responses; exec flows tested with stub binaries.

### 09 Self-mod and launcher
1. [M1] Launcher supervisor per 1.1 (probation, rollback, exit 75, terminal restore, engine-group cleanup, passthrough, `versions|rollback|doctor|--safe`).
2. [M1] `make install`: clone/update `~/.mantle/src`, build, install a version, link `~/.local/bin/mantle`.
3. [M1] Pipeline steps 1–9 with logs.
4. [M1] Builder engine: rules file, permissions, background progress block, cost.
5. [M1] `/mantle <req>|list|show|undo|rollback|retry|status`.
6. [M2] Config-first triage; visual preview before promote; `update` with conflict resolution; `edit <id>`; `upstream <id>`; dev mode.
7. [M3] fd-handoff instant restart.
8. Write `docs/EXTENDING.md`.

Done (end to end with fakeapi): a scripted builder adds a trivial mod, the pipeline promotes it, the launcher runs the new version which passes probation; a deliberately crashing build rolls back automatically; a protected-path edit is rejected; undo works.

### 10 CLI parity, packaging, drift
1. [M1] Flag table: every flag in `claude --help` 2.1.288 classified as consumed by mantle, forwarded, or interactive-only (warn or H). Unknown flags forwarded verbatim. Positional prompt; `-p` execs claude; `--resume` with no value opens the picker; `-c`; `--version`/`--help` for both.
2. [M2] Subcommand passthrough list; agent view and `attach` are H; decide `--worktree`/`--tmux`/`--bg`.
3. [M2] `make drift`: compares `claude --help` flags, `initialize.commands`, keybinding action IDs found in the binary, `sdk.d.ts` subtypes, settings keys from https://www.schemastore.org/claude-code-settings.json against mantle's tables; runs automatically on Claude Code version change.

Done: table test covering every 2.1.288 flag; drift report runs offline.

### 11 Fullscreen, accessibility, parity audit
1. Fullscreen renderer (`/tui fullscreen`, `tui` setting): virtual scroll, mouse selection and copy, sidebar slots.
2. Performance benchmarks.
3. **Side-by-side harness:** interactive `claude` and mantle both against fakeapi in x/vt emulators with identical scripts; normalized frame-diff report.
4. Opt-in end-to-end run against real claude (a few cents).
5. Close out the parity audit.

---

## 5. `pkg/ext` API sketch (verbatim)

```go
// Package ext is mantle's extension API. Built-ins and user mods use exactly this
// surface. Additive-only after v1; IDs are stable (use Alias for renames).
package ext

import (
	"encoding/json"
	"time"

	tea "charm.land/bubbletea/v2"

	"example.com/mantle/pkg/proto"
	"example.com/mantle/pkg/theme"
)

const APIVersion = 1

// Feature is the registration unit. init() calls Register (records only); the host
// runs Setup in order core < built-ins (Order/After) < mods, then resolves overrides.
type Feature struct {
	ID     string   // "chrome.footer", "mod.pirate-spinner"
	Order  int      // built-ins 0-999, mods >= 1000; higher wins override conflicts
	After  []string // features that must Setup first
	Parity []string // PARITY.md IDs covered
	Setup  func(Registrar) error
}

func Register(f Feature) { pending = append(pending, f) }

var pending []Feature

type Registrar interface {
	AddCommand(Command)
	AddAction(Action)
	AddBinding(Binding) // default; keybindings.json wins
	AddRenderer(key ContentKey, r Renderer)
	AddComponent(s Slot, c Component, o SlotOpts)
	AddDialog(id string, f DialogFactory)
	AddTheme(theme.Theme)
	AddSetting(SettingSpec)
	AddPromptStage(id string, priority int, s PromptStage)
	AddInterceptor(id string, priority int, i Interceptor)
	AddStory(Story)
	// Resolved after every Setup ran; registration order never decides the winner.
	Replace(id string, with any) // same kind as target
	Wrap(id string, w any)       // func(next T) T, T in {CommandFunc, Renderer, ActionFunc, ViewFunc}
	Remove(id string)
	Alias(oldID, newID string)
	subscribe(id string, sample tea.Msg, fn func(Ctx, tea.Msg) tea.Cmd)
}

// Subscribe delivers only messages of type T (host dispatches by type).
func Subscribe[T tea.Msg](r Registrar, id string, fn func(Ctx, T) tea.Cmd) {
	var zero T
	r.subscribe(id, zero, func(c Ctx, m tea.Msg) tea.Cmd { return fn(c, m.(T)) })
}

// Ctx is valid only on the UI goroutine (Update/View/Run). Cmds must not use it.
type Ctx interface {
	Engine(id string) Engine // "" = main session
	Session() SessionInfo
	Transcript() Transcript
	Settings() Settings
	Theme() *theme.Theme
	Clock() Clock
	Size() (w, h int)
	Layout() LayoutMode
	Invalidate(componentID string)
	Print(blocks ...string) tea.Cmd // commit to scrollback (inline), chunked by host
	Notify(Notice) tea.Cmd
	OpenDialog(id string, args any) tea.Cmd
	CloseDialog(id string) tea.Cmd
	Run(ActionID) tea.Cmd
	Store(featureID string) KV
}

type LayoutMode int

const (Inline LayoutMode = iota; Fullscreen; AltView)

type Slot string

const (
	SlotLive       Slot = "live"       // running/streaming transcript items
	SlotStatus     Slot = "status"     // spinner line
	SlotAboveInput Slot = "aboveInput" // todos, queued messages, notices
	SlotInput      Slot = "input"
	SlotBelowInput Slot = "belowInput" // footer, mode indicator
	SlotStatusLine Slot = "statusLine"
	SlotHeader     Slot = "header"     // fullscreen only
	SlotSidebarL   Slot = "sidebarLeft"
	SlotSidebarR   Slot = "sidebarRight"
)

type SlotOpts struct{ Weight, MaxHeight int; Modes []LayoutMode }
type Area struct{ Width, MaxHeight int; Focused bool; Mode LayoutMode }
type Rendered struct {
	Text   string      // styled, pre-wrapped to Area.Width
	Cursor *tea.Cursor // relative to this block; host translates
}

type Component interface {
	ID() string
	Init(Ctx) tea.Cmd
	Update(Ctx, tea.Msg) tea.Cmd // subscribed + own messages only
	View(Ctx, Area) Rendered     // cached by host until Invalidate/resize/theme
}

type Focusable interface {
	Component
	KeyContext() string // Claude Code context: "Chat", "Select", "Confirmation", ...
	HandleKey(Ctx, tea.KeyPressMsg) (handled bool, cmd tea.Cmd)
	HandlePaste(Ctx, tea.PasteMsg) (handled bool, cmd tea.Cmd)
}

type Placement int

const (PlaceInline Placement = iota; PlaceCentered; PlaceAltScreen)

type Dialog interface {
	Focusable
	Placement() Placement // host may override per layout mode
}
type DialogFactory func(Ctx, any) (Dialog, error)

type CommandFunc func(ctx Ctx, args string) tea.Cmd
type Command struct {
	ID, Name, Description, ArgHint string
	Aliases                        []string
	Hidden                         bool
	Source                         string // "builtin" | "engine" | "mod"
	Run                            CommandFunc
	Complete                       func(Ctx, string) []Completion
}

type ActionID string // Claude Code IDs verbatim ("chat:submit"); mantle-only "mantle:*"
type ActionFunc func(Ctx) (handled bool, cmd tea.Cmd)
type Action struct {
	ID                   ActionID
	Context, Description string
	Run                  ActionFunc
}
type Binding struct {
	Context, Keys string // Keys: "ctrl+x ctrl+k"
	Action        ActionID
}

// ---- transcript ----
type ContentKey string // "user.prompt", "assistant.text", "thinking", "tool.Edit", "tool.mcp.*", "system.compact_boundary"
type ItemState int

const (Streaming ItemState = iota; Running; Done; Failed; Interrupted)

type Item struct {
	ID, ParentID string // ParentID = parent_tool_use_id
	EngineID     string
	Key          ContentKey
	Data         any // *proto.TextBlock | *proto.ToolUse | *proto.SystemEvent | ...
	Result       *proto.ToolResult
	State        ItemState
	Rev          int
	Start, End   time.Time
}
type ViewMode int

const (Normal ViewMode = iota; Verbose; Brief; Focus; FullTranscript)

type RenderCtx struct {
	Width    int
	Mode     ViewMode
	Theme    *theme.Theme
	Expanded bool
	Now      time.Time
	Children []*Item
}
type Block struct {
	Lines       []string
	Collapsible bool
}
type Renderer func(RenderCtx, *Item) Block
type Transcript interface {
	Items() []*Item
	Get(id string) *Item
	Committed() int
}

// ---- engine ----
type Engine interface {
	Send(Prompt) tea.Cmd
	Interrupt() tea.Cmd
	Control(subtype string, req any) tea.Cmd // -> ControlResultMsg
	Supports(subtype string) bool
	Restart(SpawnOpts) tea.Cmd // resume / fork / resume-at
}
type Prompt struct {
	Blocks   []proto.ContentBlock
	Priority string // "now" | "next" | "later"
	UUID     string
	Composed bool // client_composed
}
type (
	EngineEventMsg struct {
		EngineID string
		Event    proto.Event // deltas coalesced
	}
	ControlResultMsg struct {
		EngineID, Subtype, RequestID string
		Resp                         json.RawMessage
		Err                          error
	}
	PermissionMsg struct {
		EngineID string
		Req      proto.CanUseTool
		Reply    func(proto.PermissionResult) tea.Cmd
	}
	SettingsMsg struct{ Changed []string }
)

// ---- pipeline / misc ----
type Draft struct {
	Text           string
	Attachments    []Attachment
	Mode, Priority string
}
type Verdict int

const (Continue Verdict = iota; Consumed; Reject)

type PromptStage func(Ctx, *Draft) (Verdict, tea.Cmd)
type Interceptor func(Ctx, tea.Msg) (tea.Msg, tea.Cmd) // nil msg = consumed

type SettingSpec struct {
	Key, Type, Description string
	Default                any
}
type Settings interface {
	Claude(key string) (any, bool) // merged Claude Code settings, read-only
	Mantle(key string) any
	SetMantle(key string, v any) tea.Cmd
}
type Story struct {
	ID     string // "chrome.footer/plan-mode"
	Widths []int
	Render func(Ctx, Area) Rendered
}
// SessionInfo, Notice, Completion, Attachment, KV, Clock, SpawnOpts elided.
```

Note: the module path in the real code is `github.com/KaitouKid1412/mantle`, not `example.com/mantle`.

Design choices behind the sketch:
- `Ctx` is passed into calls rather than stored, keeping state changes on the Update goroutine.
- Typed `Subscribe` keeps high-rate messages cheap.
- `Rendered.Cursor` lets the editor place the real terminal cursor (for IME); the host translates it to screen coordinates.
- Dialog placement is chosen by the host, not the dialog.
- Renderers are pure functions over `Item`s, so inline commit, the ctrl+o viewer and fullscreen share them.

---

## 6. Top risks and mitigations

| Risk | Mitigation |
|---|---|
| **Claude Code releases almost daily** (286, 287, 288 within days): protocol, flags, commands drift | Tolerant decoding with raw retention; conformance probe and engine pinning on version change; `sdk-diff` against matching SDK `0.3.<patch>`; `make drift` (flags, commands, action IDs, settings schema); fixtures re-recorded per version via fakeapi; capability detection hides UI that won't work |
| **`--bare` becomes the default for `-p`** (hooks, skills, plugins, MCP, CLAUDE.md dropped) | Copy the SDK's spawn args (the SDK must keep full-featured mode and will add any opt-out flag first; `sdk-diff` shows it). Unset `CLAUDE_CODE_SIMPLE`. The probe notices missing hooks/plugins/memory and blocks that engine version, offering the last good one |
| **Undocumented control requests change or disappear** | All in `unstable.go` behind `Supports()`, each with a fallback: `rewind_conversation` → `--resume-session-at`; `side_question` → forked engine; `get_workspace_diff` → `git diff`. Never on the M1 critical path |
| **Rendering performance on long transcripts** | Inline commit (scrollback never re-rendered); delta coalescing; slot caching; progressive commit; bounded live area; chroma size caps; fullscreen virtualization with line cache; 10k-item benchmarks in CI |
| **Inline-mode artifacts** (ghost lines, resize reflow, wide characters, tmux, Terminal.app) | Pre-wrap; chunked prints; reprint on ctrl+l; vt-emulator tests; manual matrix: iTerm2, Ghostty, Terminal.app, kitty, WezTerm, VS Code, tmux |
| **~300-item parity scope** | M1/M2/M3 tags; H handoff covers the long tail from day one; novelty items N/H; parity status generated from code tags |
| **A self-mod breaks the UI** | Protected launcher; pty smoke boot; probation and auto-rollback; `mantle --safe` runs last-good; panicking components isolated |
| **Security** | Trust gate before spawn (`-p` skips trust while project hooks still run); `.mcp.json` approval via flag settings; narrow builder permissions, dependency additions confirmed; protected paths checked by diff |
| **Races with claude on config files** | Never write `~/.claude.json`; prefer control requests; atomic flock writes with re-read and merge for `settings*.json` |
| **Unknown keys in `~/.claude/settings.json`** | Strict schema and `-p` silently ignores invalid files → mantle settings live only in `~/.mantle/` |
| **Parallel sessions in one checkout break each other** | Contracts first; no cross-feature imports (arch test); per-area build tags; steward plus integration windows; rewriters scoped to own paths |
| **Restarting the engine kills background tasks** | Check `background_tasks` first; warn or defer; fd handoff in M3 |
| **Session JSONL format drift** | Tolerant reader; opt-in test parsing all local sessions on every engine version change |
| **Licensing, if mantle is ever distributed** | Don't commit strings, themes or schemas extracted from the binary or SDK; read at runtime or write your own |
| **Builder cost and runaway loops** | `--max-budget-usd`; at most 3 fix rounds; cost shown per mod |

---

## 7. Spikes to run before writing much code

| ID | Owner (master plan) | Question |
|---|---|---|
| S1 | 02 | What does an interrupted turn produce, during text vs during a tool? What does `interrupt(cancel_queued)` do? |
| S2 | 02 | How do `priority` now/next/later behave? Does `--replay-user-messages` give acks? Can `cancel_async_message` take a queued message back? |
| S3 | 02 | Does headless write `history.jsonl` and `paste-cache`? |
| S4 | 02 | Does the Notification hook fire in `-p`? What does `--include-hook-events` emit? |
| S5 | 02 | Which `CLAUDE_CODE_ENTRYPOINT` values change `/resume` visibility or gate features? |
| S6 | 02 | Can the zero-API conformance probe see skills, plugins, hooks and CLAUDE.md? |
| S7 | 02 | Exact semantics of `--resume-session-at`, `--fork-session`, `--resume-drops-turn`; `rewind_files` dry run |
| S8 | 02 | Local slash-command output: `local_command_output` vs a synthetic assistant message |
| S9 | 02 | Does headless honour `disabledMcpjsonServers` passed via `--settings`? |
| S10 | 02 | Trust semantics (do trusted parent directories count?) and exactly what `-p` loads in an untrusted repo |
| S11 | 02 | `file_suggestions` latency and shape; how to list MCP resources for `@` |
| S12 | 02 | `--forward-subagent-text` streams and `parent_tool_use_id` |
| S13 | 02 | Thinking deltas and `--thinking-display` |
| S14 | 02 | Real claude, headless and interactive, against fakeapi with an isolated `CLAUDE_CONFIG_DIR` (API key approval pre-seeded) |
| S15 | 01 | Println chunking, flicker on commit-and-remove, resize behaviour, tmux, shift+enter per terminal |
| S16 | 01 | Launcher supervisor plus claude in its own process group: ctrl+z, `fg`, `$EDITOR` exec, crash restore |
| S17 | 02 | CPU at peak delta rates; pick the coalescing interval |
