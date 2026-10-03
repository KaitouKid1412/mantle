# Plan 10: self-modification & launcher

| Session | Primary paths | Part B needs | Produces |
|---|---|---|---|
| mantle-10 | `cmd/mantle`, `internal/launcher`, `internal/selfmod`, `features/selfmod`, `mods/`, `docs/EXTENDING.md`, `scripts/install.sh` | tags `contracts-v1`, `proto-v1` | none |

## Goal

Build the part of mantle that makes it mantle:

1. **The launcher** (`mantle`): a small, stable, standard-library-only supervisor. It starts
   the current UI build, survives its crashes, restores the terminal, rolls back bad builds
   automatically, and passes `-p` and `claude` subcommands straight to `claude`.
2. **`/mantle <request>`**:
   - a builder agent (a second headless Claude engine) edits mantle's own source in a
     private clone;
   - a **deterministic pipeline** vets, tests, builds and smoke-boots the result;
   - the build is promoted as a new immutable version, and the next `mantle` launch runs it
     (or "restart now" when idle).

   Also `list`, `show`, `undo`, `rollback`, `retry`, `status`, `edit`, `update` and
   `upstream`.
3. **`docs/EXTENDING.md`**: the guide the builder (and humans) follow to write mods.

## Start prompt
> You are session 10 of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/10-selfmod-launcher.md`, then execute that plan. Do Part A now. Start Part B
> once `git tag -l` shows the tags it needs. Commit with prefix `[10]`.

## Read first
- `docs/plans/00-overview.md`: the architecture (launcher, `/mantle`), the user decision
  that mods live in `~/.mantle/src`, and the parallel rules.
- `docs/research/design-review.md`: sections 1.1 (launcher), 1.5 (pipeline), 1.6 (mods and
  upstream merges), 7 (spike S16).
- `docs/plans/01-contracts-shell.md`: `pkg/ext` (Feature, Order ≥ 1000 for mods, Replace,
  Wrap, Remove, Alias, Story, catalog, selftest) and the arch test.
- `docs/plans/02-proto-engine.md`: the engine API (several engines, `EngineID`, `Restart`,
  fakeclaude, fakeapi).
- `docs/plans/05-turn-permissions.md`: permission dialogs (the builder's prompts are shown
  there with an engine label).

## Facts you can rely on
- **Why supervise:** a launcher that `exec`s the UI no longer exists once the UI starts, so
  it can't detect crashes or roll back. The launcher must **supervise**.
- **Process groups** (verified in Bubble Tea v2 `tty_unix.go`): suspend sends `kill(0,
  SIGTSTP)` to the whole group.
  - Launcher and UI share a group. The launcher **must not ignore SIGTSTP**, so both stop
    and the shell's `fg` resumes both.
  - The engine runs in its own group (`Setpgid`) and keeps working while mantle is
    suspended.
  - macOS has no PDEATHSIG, so the launcher must kill the engine's group if the UI dies
    abnormally. The UI writes the engine pgid into its run file.
- **Terminal reset sequences** after an abnormal exit, once termios is restored:
  - leave alt screen `ESC[?1049l`, show cursor `ESC[?25h`;
  - mouse modes off `ESC[?1000l ESC[?1002l ESC[?1003l ESC[?1006l`;
  - focus reporting off `ESC[?1004l`, bracketed paste off `ESC[?2004l`;
  - pop kitty keyboard `ESC[<u`, mode 2027 off `ESC[?2027l`;
  - clear OSC 9;4 progress `ESC]9;4;0BEL`.
- **Exit codes:** `0` normal, `75` restart requested, anything else a crash.
- **Prompt-mode hazard:** `claude` treats an unrecognised positional word as a **prompt** in
  non-interactive mode, which costs usage. Passthrough must forward argv exactly, and the
  launcher's own commands must be matched before anything is forwarded.
- **The builder engine** is a normal headless `claude` (plan 02's engine) with cwd set to
  the mod worktree.
  - It gets the repo's `CLAUDE.md` automatically.
  - `--append-system-prompt-file` adds the builder rules (supported in 2.1.288).
  - `--max-budget-usd` caps its spend.
  - `--allowedTools` / `--disallowedTools` take rule lists such as `"Bash(go test:*)"` and
    `"Edit(cmd/mantle/**)"`.
- **Permission rules don't stop shell writes** (a Bash command can write any file), so
  protected paths are enforced by a **diff check in the pipeline**, not only by permission
  rules.
- **apidiff:** Go 1.24+ supports `tool` directives in go.mod. Pin
  `golang.org/x/exp/cmd/apidiff` as a tool and run it with `go tool apidiff`. Adding it is a
  dependency change: follow rule 4 (`go get -tool …`, then commit go.mod and go.sum on their
  own).
- **The user's decision:** mods live in a **private clone `~/.mantle/src`** (branch
  `user`), not in the dev checkout `~/mantle`.

## `~/.mantle` layout (create in A7)
```
~/.mantle/
  bin/mantle                 launcher (installed by make install; ~/.local/bin/mantle → here)
  src/                       git clone of mantle; branch `user` = upstream + one commit per mod
  work/<mod-id>/             git worktrees for in-flight /mantle requests (branch mod/<id>)
  versions/<build-id>/       immutable: mantle-ui, manifest.json
  current -> versions/<id>   symlink, flipped atomically (rename(2))
  last-good -> versions/<id> last build that passed probation
  builds/<id>/               pipeline logs and reports
  run/<pid>.json             live run files: session id, cwd, original argv, engine pgid, version
  state/                     healthy markers, probation counters, disabled.json, feature stores
  logs/<pid>.log             mantle-ui stderr
  settings.json  keybindings.json  trust.json (plan 05)
  promote.lock               flock for promotion
  mods.json                  cache derived from `git log --grep Mantle-Mod` (never the source of truth)
```
`manifest.json` holds: build id, git sha, upstream base sha, mod list, Go version, the
`claude` version it was tested with, and the pipeline report summary.

## Part A: start immediately (no other session needed)
- [ ] **A1 `internal/launcher`: the supervisor.** Standard library only.
  - Save termios (`syscall` ioctl `TIOCGETA` on darwin, `TCGETS` on linux) and start
    `current/mantle-ui` with inherited stdio in the **same process group**. Wait for it.
  - Forward SIGTERM and SIGHUP. Ignore SIGINT in the launcher (the UI reads ctrl+c as a
    key). Never ignore SIGTSTP.
  - On a non-zero, non-75 exit: restore termios, write the reset sequences, kill the engine
    process group from the run file, print a short notice with the log path.
  - Tests use a fake child binary built in `TestMain`.
- [ ] **A2 Version store.**
  - Install a build into `versions/<id>/` (write to a temp dir, then rename); write the
    manifest.
  - Flip `current` and `last-good` atomically.
  - List versions.
  - GC: keep the last 10, plus any version referenced by a live run file (pid alive).
- [ ] **A3 Probation and rollback** (a pure state machine plus files in `state/`).
  - A newly promoted build is on probation until mantle-ui writes
    `state/healthy-<build-id>`. That happens after the first frame, engine initialized and
    20 s alive, or on a clean exit 0.
  - Two failed probation launches: flip `current` to `last-good`, print a notice, relaunch
    with `--resume <session-id>` from the run file.
  - Exit 75: re-read `current` and relaunch with the run file's handoff args.
- [ ] **A4 `cmd/mantle` main: dispatch.**
  - Launcher commands, matched first: `mantle versions`, `mantle rollback [<id>]`,
    `mantle doctor` (mantle checks, then offer `claude doctor`), `mantle --safe` (run
    `last-good` with `MANTLE_SAFE=1`, so mantle-ui skips `mods/` registrations).
  - **Passthrough** via `syscall.Exec` of `claude` with argv unchanged: `-p` / `--print`
    anywhere in argv, and the subcommands `mcp`, `plugin`/`plugins`, `auth`, `agents`,
    `attach`, `logs`, `stop`/`kill`, `rm`, `respawn`, `update`/`upgrade`, `install`,
    `import`, `purge`, `ultrareview`, `setup-token`, `auto-mode`, `gateway`.
  - Everything else is supervised mantle-ui with the original argv.
  - Plan 11 owns the canonical flag and subcommand table (`internal/cli`). Keep a small copy
    here (the launcher must stay standard-library-only and is never rebuilt by `/mantle`),
    plus a `_test.go` that imports `internal/cli` and asserts the two lists match.
- [ ] **A5 `internal/selfmod`: the pipeline runner library.** Steps as functions, each with
  a timeout, a log file under `builds/<id>/` and a structured result:
  1. **Protected-path check:** `git diff --name-only <base>` plus untracked files
     (`git status --porcelain`) against `cmd/mantle/**`, `internal/launcher/**`,
     `internal/selfmod/**`, the go.mod `toolchain` line and the protected list itself
     (`internal/selfmod/protected.go`).
  2. `gofmt -l` on changed Go files.
  3. `go vet ./...`
  4. arch test (`go test ./internal/archtest`)
  5. `go tool apidiff` between base and candidate for each `pkg/*` package (incompatible
     changes fail)
  6. `GOTOOLCHAIN=local go build -trimpath -o <tmp>/mantle-ui ./cmd/mantle-ui`
  7. `go test ./...` (fakeclaude; `-race` only for `internal/engine`)
  8. `<tmp>/mantle-ui selftest`
  9. **pty smoke boot** (`creack/pty`): start `<tmp>/mantle-ui` against fakeclaude; wait
     for the first frame, type a prompt, see the scripted reply, open `/help`, quit, expect
     exit 0, all within a timeout.

  Steps 8–9 call commands from plans 01 and 02. Make the commands configurable and test the
  runner with stub commands now.
  `TrimForBuilder(results)` produces compact failure text (first N errors per step) to feed
  back to the builder.
- [ ] **A6 Git plumbing** (exec `git`; tests in temp repos):
  - worktree add and remove;
  - commit with trailers `Mantle-Mod: <id>`, `Mantle-Request: <one line>`,
    `Mantle-Kind: mod|core-seam`;
  - rebase onto `user` and detect conflicts; revert; cherry-pick;
  - list mods from `git log --grep '^Mantle-Mod:'`.
  - **Commit-split rule:** files only under `mods/` give one commit with `Mantle-Kind: mod`.
    Files outside `mods/` give a `core-seam` commit for them first, then the `mods/` commit.
    Both carry the same `Mantle-Mod` id.
- [ ] **A7 Install.**
  - `make install`, via `scripts/install.sh`:
    - build the launcher into `~/.mantle/bin/mantle` and symlink `~/.local/bin/mantle`;
    - create or update `~/.mantle/src` (clone the dev repo `~/mantle` as `origin`; create
      branch `user` from `origin/main`);
    - build mantle-ui from `user`, install it as a version, flip `current` and
      `last-good`.
  - Re-running is idempotent.
  - `promote.lock` helper with `syscall.Flock`.
  - Check that `go` exists and print an install hint if not (the MVP requires a Go
    toolchain).

## Part B: after `contracts-v1` and `proto-v1`
- [ ] **B1 [M1] Builder engine.**
  - Spawn through the engine API with `EngineID = "builder-<id>"` and cwd set to the
    worktree.
  - Flags:
    - `--permission-mode acceptEdits`;
    - `--allowedTools` for `go build:*`, `go test:*`, `go vet:*`, `gofmt:*`,
      `go run ./cmd/mantle-ui:*`, `./bin/mantle-ui story:*`, `./bin/mantle-ui catalog:*`,
      `git diff:*`, `git status`;
    - `--disallowedTools` for `git commit:*`, `git push:*`, `go get:*`, `rm -rf:*`, and
      `Edit(...)`/`Write(...)` on the protected paths;
    - `--append-system-prompt-file <embedded builder rules>`;
    - `--max-budget-usd <selfmod.maxBudgetUsd>`;
    - `--model <selfmod.model or the user's model>`.
  - Any other permission request goes to plan 05's dialogs, labelled "Builder <id>".
  - **Builder rules** (embed `internal/selfmod/builder_rules.md`):
    1. **Triage first:** if a setting, theme (`~/.claude/themes`), keybinding, statusLine or
       output style satisfies the request, reply with a `MANTLE_CONFIG_PROPOSAL` block and
       stop.
    2. Prefer a new `mods/<id>/` package using `pkg/ext` (Replace, Wrap, Remove,
       Interceptor, new components and commands). Find target IDs with
       `./bin/mantle-ui catalog --json`.
    3. Edit core only to add a missing seam, minimally.
    4. Always add a Story and a test for what you build.
    5. Verify with `go test ./mods/<id>/...` and `./bin/mantle-ui story <id> --width 100`.
    6. Never commit, never run `go get` (ask instead), never touch protected paths.
- [ ] **B2 [M1] `/mantle` commands** (`features/selfmod`):
  - `/mantle <request>`: create a worktree from `user`, start the builder in the background,
    show a collapsible progress block (renderer key `mantle.build`: phase, last tool, cost),
    run the pipeline when the builder finishes, and loop failures back up to 3 rounds;
  - `list`: id, request, date, kind, state;
  - `show <id>`: request, files, diffstat, pipeline report;
  - `undo <id>`: revert its commits in a worktree, run the pipeline, promote;
  - `rollback`: flip `current` to the previous version, no rebuild;
  - `retry [<id>]`: rerun the builder on the kept worktree with the last failure text;
  - `status`: running builds, current version, probation state.
- [ ] **B3 [M1] Promote.**
  - Under `promote.lock`: commit (A6), rebase onto `user` (if `user` moved, re-run steps
    6–9), fast-forward `user`, build from `user` HEAD, install the version, flip `current`.
  - Notice: "<id> is ready; active next launch".
  - **Restart now** (only when idle and `background_tasks` is empty; otherwise offer it for
    next launch): write the run file and exit 75, so the launcher relaunches with
    `--resume <session-id>`.
- [ ] **B4 [M1] mantle-ui side of the launcher protocol** (in `features/selfmod`,
  subscribing to messages; no core edits needed):
  - write `run/<pid>.json` at start (session id, cwd, argv, version) and update it with the
    engine pgid and session id when the engine initializes or `conversation_reset` happens;
  - write the healthy marker after the first frame, engine init and 20 s;
  - honour `MANTLE_SAFE=1` (the host skips features with Order ≥ 1000; coordinate the flag
    with plan 01 via a request file if it's not in `pkg/ext`);
  - remove the run file on clean exit.
- [ ] **B5 [M2] Config-first triage UI.** When the builder returns a
  `MANTLE_CONFIG_PROPOSAL`, show the exact change and apply it through plan 01's config
  writer or the right control request. No rebuild.
- [ ] **B6 [M2] Visual preview.** For stories whose rendered output differs between
  `current` and the candidate, show before/after side by side. Setting
  `selfmod.confirm = always | on-visual-change | never` (default `on-visual-change`), declared
  as an `ext.SettingSpec`.
- [ ] **B7 [M2] `edit <id> <request>`.** A builder session on top of the mod with the
  original and new requests. The result is squashed into the mod's commits, keeping one mod
  as one unit.
- [ ] **B8 [M2] `update`.**
  - Fetch `origin`; in a worktree, rebase `user` onto `origin/main` **one commit at a
    time**.
  - On conflict, the builder resolves it using that commit's `Mantle-Request` as intent
    (rules: keep the user's intent, adapt to the new core).
  - Run the full pipeline, including every mod's own tests; promote.
- [ ] **B9 [M2] `upstream <id>` and dev mode.**
  - `upstream <id>` exports a mod's commits to the dev repo `~/mantle` on branch
    `mantle/mod-<id>`. Never `main`, because parallel sessions are dirtying it.
  - Dev mode `selfmod.source=~/mantle`: worktrees come from the dev repo, branches are
    `mantle/mod-<id>`, never auto-merged; install from the branch for testing.
- [ ] **B10 [M2] `docs/EXTENDING.md`.** Start as soon as `contracts-v1` exists, and keep it
  in sync with plan 01 (the API steward). It covers:
  - the Feature lifecycle and Order;
  - finding IDs with `catalog`; Replace, Wrap, Remove and Alias semantics;
  - slots and components; renderers and ContentKeys; commands, actions, bindings and
    keybinding contexts; dialogs and placement; interceptors and prompt stages;
    SettingSpecs; the per-feature store;
  - Stories and tests (required); what's protected; import rules (`mods/` imports only
    `pkg/`);
  - three worked examples: custom spinner verbs (config only), a custom renderer for one MCP
    tool, a sidebar component (fullscreen).
- [ ] **B11 [M3] fd-handoff instant restart.** mantle-ui calls `syscall.Exec` on the new
  binary with `--attach-engine-fds=3,4,5` (plus session state). The PID stays the same, so
  the claude child, its pipes and background tasks survive.

## Design notes
- **The launcher never imports anything outside the standard library** and never changes
  through `/mantle`. If it breaks, `/mantle` can't fix it, so keep it small and heavily
  tested.
- **The pipeline is plain Go code** with deterministic steps and logs. The model never
  decides whether a build is good.
- **One mod is one git unit**, identified by its `Mantle-Mod` trailer. Undo, show, update
  and upstream all work from git history. `mods.json` is only a cache.
- **Concurrency.** Several `/mantle` requests can run at once, each in its own worktree.
  Promotion is serialized by `promote.lock`, and the later promotion re-runs steps 6–9 after
  rebasing.
- **Cost.** The builder runs with a budget cap and shows its running cost. Three failed fix
  rounds stop the attempt.
- **Several mantle instances** can run different versions at once. GC keeps any version a
  live run file references.

## Interfaces you provide / consume
- **Provide:**
  - the `mantle` binary (launcher) and the `~/.mantle` layout;
  - `internal/selfmod` (pipeline, git plumbing, builder rules);
  - `/mantle` commands; the `mantle.build` renderer;
  - the run-file and healthy-marker protocol;
  - `docs/EXTENDING.md`;
  - the setting specs `selfmod.confirm`, `selfmod.maxBudgetUsd`, `selfmod.model`,
    `selfmod.source`.
- **Consume:**
  - `pkg/ext` (commands, renderer, Subscribe, SettingSpec, Notice);
  - the engine API (second engine, `Restart`, `background_tasks`);
  - plan 05's dialogs (engine label);
  - plan 01's `story`, `catalog` and `selftest` commands and arch test;
  - plan 02's fakeclaude and fakeapi;
  - plan 11's `internal/cli` (test-only cross-check).

## Tests and done criteria
- Unit tests:
  - supervisor signal handling and terminal restore (fake child);
  - version store (atomic flips, GC);
  - the probation state machine;
  - dispatch (launcher commands vs passthrough vs UI, with argv preserved);
  - protected-path checker; commit-split rule; trailers parsing.
- **End-to-end with fakeapi** (no real API cost):
  - a scripted builder creates `mods/demo/` (a trivial command plus a story and a test); the
    pipeline passes and promotes; the launcher runs the new version and it passes probation;
  - a deliberately crashing build is rolled back automatically to `last-good` with a
    notice;
  - an edit to `internal/launcher/` is rejected by step 1;
  - `/mantle undo demo` reverts and promotes.
- Spike S16 results (from plan 01) are reflected in A1. Manually check ctrl+z and `fg`, and
  ctrl+g with `$EDITOR` while supervised.
- `make test-10` passes; `go build -tags no_selfmod ./cmd/mantle-ui` still builds.

## Parity coverage
- `MT-*`: every mantle-only row (`/mantle` and its subcommands, launcher, versions,
  rollback, `--safe`, catalog, story, selftest, update, upstream).
- `CLI-*`: launcher-level passthrough of `-p` and subcommands (shared with plan 11).

## Out of scope
- **The `pkg/ext` API itself, `story`/`catalog`/`selftest` implementation:** plan 01.
- **The engine client, fakeclaude, fakeapi:** plan 02.
- **The full `claude` flag table:** plan 11.
- **Permission dialog UI:** plan 05.
- **A bundled Go toolchain** (post-MVP).
- **Windows.**

**Reminder of the parallel rules:** edit mainly your primary paths; contracts are
additive-only; no repo-wide rewriters (`make fmt-10`); new tool dependency (apidiff): `go
get -tool …`, then commit `go.mod`/`go.sum` alone; commit only your files with `[10]`;
per-area build tag `no_selfmod`.
