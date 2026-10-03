# Plan 11: CLI parity & drift detection

| Session | Primary paths | Part B needs | Produces |
|---|---|---|---|
| mantle-11 | `internal/cli`, `scripts/drift` | tags `contracts-v1`, `proto-v1` | none |

## Goal

1. **CLI parity.** `mantle` accepts every `claude` 2.1.288 command-line flag and subcommand
   with the same meaning:
   - flags mantle needs are consumed or rewritten (resume, continue, name, verbose,
     screen reader, positional prompt, …);
   - engine flags are forwarded **verbatim**, and unknown flags are forwarded too;
   - `-p` and subcommands are exec'd straight to `claude`;
   - interactive-only flags are handled deliberately (forward, exec, or hand off).
2. **Drift detection.** Claude Code ships almost daily. Detect automatically when a new
   engine version adds or removes flags, slash commands, keybinding actions, protocol
   message types, settings keys or tools, so mantle never silently falls behind.

## Start prompt
> You are session 11 of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/11-cli-drift.md`, then execute that plan. Do Part A now. Start Part B once
> `git tag -l` shows the tags it needs. Commit with prefix `[11]`.

## Read first
- `docs/plans/00-overview.md`: the architecture (launcher passthrough), coverage codes, the
  parallel rules.
- `docs/research/inventory-binary-2.1.288.md`: section 1 (the complete `claude --help` and
  subcommand help, every flag with a one-line meaning) and section 3 (keybinding action IDs
  and contexts, as found in the binary).
- `docs/research/inventory-docs.md`: section 7 (CLI subcommands; flags not combinable with
  `-p`; interactive-only flags).
- `docs/research/protocol-2.1.288.md`: the flags the SDK passes, `initialize.commands`, and
  section 9 (slash commands in headless mode).
- `docs/plans/10-selfmod-launcher.md` (A4): the launcher's own small passthrough list must
  match yours (a test checks it).

## Facts you can rely on
- **Help behaviour.**
  - `claude <sub> <nested> --help` works on 2.1.288.
  - A **non-existent** subcommand path with `--help` prints the root help instead of an
    error.
  - Any unrecognised positional word becomes a **model prompt** in non-interactive mode,
    which costs usage. A research agent did this by accident (`claude "auth login" help`).
  - Drift tooling must only ever call `claude --help`, `claude <known-sub> --help` or
    `claude --version`, with argv elements passed separately.
- **Option parsing is commander.js:**
  - `--flag value` and `--flag=value`;
  - optional values (`-r [id]`, `-w [name]`, `--debug [filter]`, `--cloud [x]`,
    `--from-pr [n]`, `--remote-control [name]`, `--prompt-suggestions [bool]`) consume the
    next argument only if it doesn't start with `-`;
  - variadic options (`--add-dir <dirs...>`, `--allowedTools <tools...>`,
    `--disallowedTools`, `--tools`, `--betas`, `--mcp-config <configs...>`, `--file <…>`)
    consume until the next option, so a positional prompt after a variadic option is
    swallowed by it, just as Claude Code does;
  - `--plugin-dir`/`--plugin-url` are repeatable; `--` ends options.
- **Flags only valid with `-p`:** `--output-format`, `--input-format`,
  `--include-partial-messages`, `--include-hook-events`, `--replay-user-messages`,
  `--forward-subagent-text`, `--max-budget-usd`, `--json-schema`, `--no-session-persistence`,
  `--prompt-suggestions`, `--permission-prompts`, `--permission-prompt-tool`, `--max-turns`.
  mantle manages the stream flags itself for its engine.
- **Not combinable with `-p`:** `--bg` and `--cloud <task>` (`--cloud <session-id> -p`
  queues a message instead).
- **Interactive-only flags:** `--remote-control`, `--tmux`, `--teleport`, `--from-pr` (opens
  a picker), `--ax-screen-reader`, `--desktop`.
- **Subcommands:** `agents`, `attach`, `auth`, `auto-mode`, `doctor`, `gateway`, `import`,
  `install`, `logs`, `mcp`, `plugin`/`plugins`, `purge`, `respawn`, `rm`, `setup-token`,
  `stop`/`kill`, `ultrareview`, `update`/`upgrade`.
- **Settings schema:** `https://www.schemastore.org/claude-code-settings.json` (the binary
  doesn't embed a settings schema URL; `$schema` is accepted and ignored).
- **The SDK's spawn flags** are the safest set (see `protocol-2.1.288.md`, section 2).
  `scripts/sdk-diff` (plan 02) diffs SDK message and control unions against `pkg/proto`.

## Part A: start immediately (no other session needed)
- [ ] **A1 Flag table** (`internal/cli/flags.go`). One entry per 2.1.288 flag (about 70)
  with: long name, short name, aliases (`--allowedTools`/`--allowed-tools`, …), arity
  (none / required / optional / variadic / repeatable), and **class**:
  - `consumed`: mantle uses it and doesn't forward it as-is. `-c/--continue`,
    `-r/--resume`, `--fork-session`, `--session-id`, `-n/--name`, `--verbose`,
    `--ax-screen-reader`, `-v/--version`, `-h/--help`, the positional prompt.
    - Resume and continue are resolved by mantle (plan 06 shows the history first), then
      passed to the engine as `--resume=<id>` [+ `--fork-session`] [+ `--session-id=`].
    - `--verbose` sets mantle's verbose view and is also forwarded.
  - `forwarded`: passed verbatim to the engine.
    - Model and effort: `--model`, `--fallback-model`, `--effort`, `--autocompact`,
      `--betas`.
    - Directories and agents: `--add-dir`, `--agent`, `--agents`.
    - Permissions and tools: `--permission-mode`, `--dangerously-skip-permissions` and
      `--allow-dangerously-skip-permissions` (both also feed plan 05's bypass gate),
      `--allowedTools`, `--disallowedTools`, `--tools`, `--restricted`.
    - System prompt: `--system-prompt[-file]`, `--append-system-prompt[-file]`,
      `--exclude-dynamic-system-prompt-sections`, `--system-prompt-snapshot`.
    - Config sources: `--settings`, `--setting-sources`, `--mcp-config`,
      `--strict-mcp-config`, `--plugin-dir`, `--plugin-url`, `--disable-slash-commands`.
    - Other: `--chrome`, `--no-chrome`, `--file`, `--brief`, `--debug`, `--debug-file`,
      `--ide`, `--safe-mode`, `-w/--worktree` (pending the B2 decision).
  - `print-only`: valid only with `-p`. Given to `mantle` without `-p`, they are an error
    with a clear message.
  - `exec`: mantle execs `claude` with argv unchanged. `-p/--print` anywhere, `--bg`,
    `--desktop`, `--cloud`, `--environment`, `--teleport`, `--remote-control`,
    `--remote-control-session-name-prefix`, `--tmux` (pending B2).
  - `warn`: `--bare`. It drops hooks, skills, plugins, MCP and CLAUDE.md; forward only after
    a one-line warning, never silently.
  - Unknown flags are forwarded verbatim, with a debug-log note. They are probably new
    engine flags.
- [ ] **A2 Parser** (`internal/cli/parse.go`). Hand-written, table-driven, commander-
  compatible tokenizer.
  - `Parse(argv) (Parsed, error)` with `Parsed{Mode, Mantle MantleOpts, EngineArgs []string,
    Prompt string, Exec []string}`.
  - `Mode` is one of `ui`, `exec-claude`, `version`, `help`.
  - `EngineArgs` keeps original order and spelling for every forwarded token. Table tests
    cover optional values, variadic swallowing, `=` forms, `--`, repeated flags, unknown
    flags, short-flag clusters if commander allows them, and the prompt position.
  - **No cobra, pflag or fang:** they reorder arguments and reject unknown flags.
- [ ] **A3 Subcommand passthrough and exec.**
  - `IsPassthroughSubcommand(argv)` uses the subcommand list above; `doctor` belongs to the
    launcher (`mantle doctor`, plan 10).
  - `ExecClaude(argv)` runs `syscall.Exec` with the binary resolved like plan 02's engine
    (`MANTLE_CLAUDE_BIN` or `PATH`) and the environment unchanged.
  - Export the list so plan 10's launcher test can assert its copy matches.
- [ ] **A4 `--version` and `--help`.**
  - `--version` prints `mantle <version> (<build id>), engine claude <version>`; the engine
    version comes from `claude --version`, with a timeout.
  - `--help` prints mantle's usage and mantle-only options (`--safe`, and the launcher
    commands `versions|rollback|doctor`), then "all claude options are accepted", followed
    by the **runtime** output of `claude --help`. Never commit a copy of Claude Code's help
    text.
- [ ] **A5 Drift tool skeleton** (`scripts/drift`, a Go program run with
  `go run ./scripts/drift`; `make drift`). Collectors, each producing a normalized JSON
  list:
  1. **flags:** parse `claude --help` and `claude <sub> --help` for the known subcommands
     only (safe argv);
  2. **keybinding action IDs and contexts:** regex over the embedded JS in the claude binary
     (`<binary>` from `claude --version` / symlink resolution);
  3. **settings keys:** fetch the schemastore JSON (cache it in `~/.mantle/cache/drift/`;
     offline-tolerant);
  4. placeholders for engine-reported data (B3).

  The comparator diffs each list against mantle's tables (this plan's flag table, plan 01's
  keymap table, plan 08's `/config` option table, PARITY.md IDs). Output is
  `docs/parity-drift.md` (generated, committed by the coordinator) plus JSON, with exit
  status 1 when something is unclassified. Snapshot tests on saved collector outputs
  (normalized lists, not raw help text).

## Part B: after `contracts-v1` and `proto-v1`
- [ ] **B1 [M1] Wire `cli.Parse` into startup.**
  - Plan 01 owns `cmd/mantle-ui/main.go`, and the `cli.Parse` call site is part of the
    contracts. If it isn't there yet, add the minimal call yourself (file-baton serializes)
    or file a request.
  - Route:
    - `exec-claude` → `ExecClaude`;
    - `ui` → engine spawn options (plan 02) built from `EngineArgs`, plus resume handling
      (`-r` with no value opens plan 06's picker; `-r <id|search>` resolves via plan 06;
      `-c` continues the most recent session for the cwd, including `sdk-cli` sessions),
      plus the initial prompt (submitted as the first user message once gates pass), plus
      gate inputs (bypass flags → plan 05).
  - End-to-end tests with fakeclaude assert the exact engine argv.
- [ ] **B2 [M2] Interactive-only flag decisions.** Implement and document each in this
  file's checklist:
  - `-w/--worktree [name]`: forward to the engine, which creates the worktree and runs
    there; mantle follows `init.cwd`. Spike it with fakeapi.
  - `--tmux`: exec `claude` (tmux and iTerm2 panes are interactive).
  - `--bg`: exec `claude` (returns an id; watch with `claude agents`).
  - `--remote-control`, `--teleport`, `--cloud`, `--environment`, `--desktop`: exec
    `claude` (H).
  - `--from-pr [n|url]`: native if plan 06's picker supports PR-linked sessions by then,
    otherwise exec `claude` (H).

  Each decision is a row in the flag table with a test.
- [ ] **B3 [M2] Engine-reported drift.**
  - Add collectors that use a zero-token engine session (plan 02's conformance probe
    pattern: `initialize`, then `end_session`): `initialize.commands` (vs mantle's routing
    table: native / E / H), `init.tools` (vs plan 03's renderer keys; tools without a
    renderer fall back to the generic renderer and are flagged), output styles and models.
  - Integrate `scripts/sdk-diff` (plan 02) for protocol message and control subtypes.
- [ ] **B4 [M2] Runtime drift notice.** When plan 02 detects an engine version change
  (probe), compare engine-reported commands, tools and flags against the tables embedded in
  the binary. Show one notice, e.g. "Claude Code 2.1.290 adds 3 commands mantle doesn't know
  yet; they work as engine passthrough". Record the result in `~/.mantle/state/drift.json`.
  Never block startup.

## Design notes
- **Forward, don't reinterpret.** mantle only rewrites what it must (resume, continue,
  session id, prompt). Everything else reaches the engine byte-for-byte, so new or obscure
  engine flags keep working without mantle changes.
- **The launcher dispatches first.** It execs `claude` for `-p` and subcommands before
  mantle-ui even starts (plan 10), so a broken UI build never blocks `mantle -p`. mantle-ui
  handles the same cases again as a fallback.
- **Drift is advisory at runtime and strict in development.** `make drift` fails CI-style
  on unclassified items; the runtime check only informs.
- **Licensing hygiene.** Store normalized names (flag names, action IDs, setting keys), not
  copied help text or descriptions.

## Interfaces you provide / consume
- **Provide:**
  - `internal/cli` (`Parse`, the flag table, `ExecClaude`, the passthrough list, help and
    version);
  - `scripts/drift` and `make drift`; `docs/parity-drift.md` (generated);
  - a runtime drift check function for plan 02's probe hook.
- **Consume:**
  - plan 02's engine spawn options and probe;
  - plan 06's resume resolution and picker (by ext ID or a small interface in `pkg/ext`);
  - plan 05's gate inputs;
  - plan 01's keymap table; plan 08's option table; plan 03's renderer keys (read through
    `catalog --json`, no imports);
  - plan 09's `internal/claudecli` for safe `--help` calls if available (otherwise a local
    safe exec with the same rules).

## Tests and done criteria
- A **table test covering every 2.1.288 flag** (class, arity, forwarding).
- Parser edge cases: optional and variadic values, `=` forms, `--`, unknown flags, prompt
  detection.
- Exec routing tests with a fake `claude` that records argv. Passthrough lists match plan
  10's copy.
- Drift snapshot tests; the drift run works offline using cached inputs.
- B1 end-to-end: `mantle --model sonnet --add-dir ../x "hello"` spawns the engine with
  `--model sonnet --add-dir ../x` plus mantle's stream flags, and sends "hello" as the first
  user message after the gates.
- `make test-11` passes; `go build -tags no_cli ./cmd/mantle-ui` still builds (if
  `internal/cli` is wired through a feature).

## Parity coverage
- `CLI-*`: every flag, the positional prompt, `-p`, subcommand passthrough,
  `--version`/`--help`, interactive-only flag decisions, the drift detector.
- `CL-*`: the flag-level cloud and product entries (`--remote-control`, `--teleport`,
  `--cloud`, `--desktop`, `--bg`, `--tmux`).

## Out of scope
- **Slash command routing inside the UI:** plan 04 (you only feed drift results about
  commands).
- **The resume picker and JSONL reading:** plan 06.
- **Launcher-level dispatch and `mantle versions|rollback|doctor|--safe`:** plan 10.
- **The engine's own stream flags:** plan 02 decides those; they are never taken from user
  argv.

**Reminder of the parallel rules:** edit mainly your primary paths; contracts are
additive-only; no repo-wide rewriters (`make fmt-11`); commit only your files with `[11]`.
