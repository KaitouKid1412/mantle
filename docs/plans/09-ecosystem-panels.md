# Plan 09: ecosystem panels

| Session | Primary paths | Part B needs | Produces |
|---|---|---|---|
| mantle-09 | `features/ecosystem`, `internal/claudecli` | tags `contracts-v1`, `proto-v1` | none |

## Goal

Rebuild the panels that manage Claude Code's ecosystem: `/mcp`, `/plugin`, `/skills`,
`/hooks`, `/agents`, `/memory`, `/doctor`, `/login`, `/logout`, `/upgrade`, `/feedback`,
`/import`. They are `local-jsx` in Claude Code and refused in headless mode.

Also provide:
- the **hand-off (H) commands** for cloud and product features, which the user decided
  should open the real `claude`;
- the **safe `claude` subcommand runner** (`internal/claudecli`) that any plan can use to
  call `claude mcp …`, `claude plugin …`, `claude auth …` and similar without accidents.

## Start prompt
> You are session 09 of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/09-ecosystem-panels.md`, then execute that plan. Do Part A now. Start Part B
> once `git tag -l` shows the tags it needs. Commit with prefix `[09]`.

## Read first
- `docs/plans/00-overview.md`: coverage codes (H = hand off), the parallel rules.
- `docs/research/inventory-binary-2.1.288.md`:
  - section 1: every subcommand and flag of `claude mcp`, `claude plugin`, `claude auth`,
    `claude agents`, `claude import`, `claude doctor`, `claude update`;
  - section 2: which slash commands are `local-jsx`, `local` or `prompt`;
  - section 7: the `~/.claude` layout (plugins, skills, agents, projects/memory).
- `docs/research/inventory-docs.md`: sections 6 (skills), 10 (hooks), 11 (MCP), 13 (memory),
  21 (plugins), 22–23 (integrations, auth), and the "terminal-only" commands list.
- `docs/research/protocol-2.1.288.md`:
  - control requests `mcp_status`, `mcp_toggle`, `mcp_reconnect`, `reload_plugins`,
    `reload_skills`, `get_hooks_listing`, `register_repo_root`;
  - `initialize.agents`, `init.skills/plugins/plugin_errors/mcp_servers/memory_paths`,
    `commands_changed`.

## Facts you can rely on
- **A real accident to never repeat.** During research, a loop ran
  `claude "auth login" help` (zsh didn't split the words). Because stdout wasn't a TTY, the
  CLI treated the string as a **prompt** and ran a headless session that cost usage.
  - Any positional text the CLI doesn't recognise as a subcommand becomes a model prompt in
    non-interactive mode.
  - Also, `claude <sub> <nested> --help` with a non-existent path prints the root help
    instead of an error.
  - Therefore: always pass argv elements separately, keep subcommands on an allowlist, and
    never pass user text positionally.
- **MCP subcommands:** `claude mcp add [-s local|user|project] [-t stdio|sse|http] [-e KEY=V]
  [-H header] [--client-id] [--client-secret] [--callback-port] <name> <cmdOrUrl> [args...]`,
  `add-json`, `add-from-claude-desktop`, `get <name>`, `list`, `login [--no-browser] <name>`,
  `logout <name>`, `remove [-s] <name>`, `reset-project-choices`. `list` and `get` print
  text (no `--json` in 2.1.288).
- **Plugin subcommands:** `claude plugin list [--available] [--json]`, `details <name>`,
  `install|uninstall|enable|disable|update … [--json] [-s scope] [-y]`,
  `configure [--json] [--values-stdin] <plugin>`, `prune`, and
  `marketplace add|list|remove|update [--json]`. Plugin keybinding context `Plugin`: space
  toggle, `i` install, `f` favorite, ctrl+s cycle marketplace.
- **Auth subcommands:** `claude auth status [--json|--text]`, `claude auth login
  [--claudeai|--console|--sso|--email]`, `claude auth logout`. These are interactive flows,
  so run them with `tea.ExecProcess`.
- **Other subcommands:** `claude doctor` is the installation health check (interactive; run
  via ExecProcess). Note that `/doctor` *inside* a session is a bundled **skill** in 2.1.288
  (prompt type, E). `claude import [codex|gemini|cursor] [--dry-run] [--yes[=digest]]`.
  `claude update`. `claude agents [--json] [--all] [--cwd]`; `claude attach|logs|stop|rm
  <id>`.
- **Control requests:**
  - `mcp_status` returns `{mcpServers: McpServerStatus[]}` (name, status, scope/source,
    error, tool counts).
  - `mcp_toggle {serverName, enabled}`, `mcp_reconnect {serverName}`.
  - `reload_plugins {hold_on_cache_impact?}` returns `{commands, agents, plugins,
    mcpServers, error_count, …}`; plugin MCP changes may need an engine restart.
  - `reload_skills` returns `{skills}`.
  - `get_hooks_listing` returns the merged hooks configuration.
- **MCP OAuth isn't available headlessly** (no `/mcp` panel). Claude is told the server
  needs auth. Use `claude mcp login <name>` (`--no-browser` supported), then `mcp_reconnect`.
- **Discovery locations:**
  - Agents: `~/.claude/agents/*.md`, `.claude/agents/*.md`, plus plugin agents.
  - Skills: `~/.claude/skills/<name>/SKILL.md`, `.claude/skills/…` (nested and parent
    discovery), `.claude/commands/*.md`, plugin skills (`plugin:name`), synced claude.ai
    skills (`~/.claude/skills/synced/`, shown as `anthropic-skills:`). Visibility overrides
    in setting `skillOverrides`.
  - Memory: `~/.claude/CLAUDE.md`, project `CLAUDE.md` and `.claude/CLAUDE.md`,
    `CLAUDE.local.md`, `.claude/rules/`, AGENTS.md, `@imports`; auto memory in
    `~/.claude/projects/<slug>/memory/` (setting `autoMemoryEnabled`); `init.memory_paths`.
  - Hooks: `hooks` in every settings scope, plus plugin hooks.
- **Invalid settings files are silently ignored by `-p`**, so mantle should detect and warn
  (part of `/doctor` and the startup notices).

## Part A: start immediately (no other session needed)
- [x] **A1 `internal/claudecli`, the safe runner.**
  - `Run(ctx, sub Subcommand, args ...string) (stdout, stderr []byte, err)`, where
    `Subcommand` is a closed enum (`MCPList`, `MCPGet`, `MCPAdd`, `PluginList`, `AuthStatus`,
    `AgentsList`, `Import`, `AutoModeDefaults`, …) mapping to a fixed argv prefix.
  - Never more than the declared positional slots; stdin `/dev/null` unless declared; a
    timeout; `CLAUDECODE` removed from the environment; the binary resolved the same way
    as plan 02's engine (`MANTLE_CLAUDE_BIN` or `PATH`).
  - `Interactive(sub, args)` returns an `*exec.Cmd` for `tea.ExecProcess` flows (`auth
    login`, `mcp login`, `doctor`).
  - **Regression test:** a fake `claude` on `PATH` records argv. Assert that every
    Subcommand produces separate argv elements and that no call can produce a lone free-text
    positional argument.
- [x] **A2 Typed wrappers on A1:**
  - `mcp list/get` (text parsers with fixtures; tolerant of format drift), `mcp
    add/add-json/remove/login/logout`;
  - `plugin list --json [--available]`, `plugin marketplace list --json`,
    `install/uninstall/enable/disable/update --json -y`, `details`, `configure
    --values-stdin`;
  - `auth status --json`, `agents --json [--all]`, `import --dry-run`, `auto-mode
    defaults|config` (used by plan 08's `/permissions`).

  Fixture-based tests.
- [x] **A3 Discovery.** Scanners for agents, skills, commands, memory files and hooks at the
  locations above:
  - each item reports name, path, scope (user/project/local/plugin/managed) and parsed
    frontmatter (a small YAML-frontmatter parser for `name, description, tools, model,
    argument-hint, …`);
  - these enrich what the engine reports (`initialize.agents`, `init.skills`,
    `get_hooks_listing`) with file paths for editing;
  - tests use temp directory trees.
- [x] **A4 mantle doctor checks**, each returning ok/warn/fail plus a fix hint:
  - Go toolchain present and its version (needed for `/mantle`);
  - `claude` found, its version, and pinned or probe status (read plan 02's pin file when
    it exists);
  - terminal capabilities (kitty keyboard, truecolor, OSC 52, focus reporting);
  - every Claude Code settings file and `keybindings.json` parses (warn: `-p` ignores
    invalid files);
  - `~/.mantle` health (`current`/`last-good` symlinks valid, disk space);
  - a trust gate reminder for the cwd.

## Part B: after `contracts-v1` and `proto-v1`
- [x] **B1 [M2] `/mcp` panel.**
  - Server list from `mcp_status`: name, scope, status (connected / needs-auth / failed /
    disabled), tool counts, errors.
  - Actions: enable/disable (`mcp_toggle`), reconnect (`mcp_reconnect`), authenticate
    (`claude mcp login <name>` via ExecProcess, then reconnect), clear auth (`claude mcp
    logout`), add (a small form that maps to `claude mcp add`/`add-json`), remove.
  - `/mcp reconnect|enable|disable <name>` with arguments goes to the engine (E).
  - Publish a "needs auth" count message for plan 07's footer.
- [ ] **B2 [M2] `/plugin` manager** (aliases `/plugins`, `/marketplace`).
  - Tabs: Discover (`--available`), Installed, Marketplaces.
  - Plugin-context keys: space toggle, `i` install, `f` favorite, ctrl+s cycle marketplace.
  - Mutations via A2 with `-y`, then `reload_plugins`. If the result reports MCP or
    cache-impact changes, offer an engine restart (`Engine.Restart` with `--resume`).
  - Show `init.plugin_errors`.
- [x] **B3 [M2] `/skills`.** Skills from `init.skills`, refreshed with `reload_skills`;
  source and path from A3; cycle visibility, which writes `skillOverrides` through plan
  01's config writer; open SKILL.md in `$EDITOR`. `/skill-doctor` stays E.
- [x] **B4 [M2] `/hooks`.** A read-only browser over `get_hooks_listing`, grouped by event,
  then matcher, then source scope, showing handler type and command/url. "Edit" opens the
  owning settings file in `$EDITOR`.
- [ ] **B5 [M2] `/agents`.**
  - Browse agents from `initialize.agents` plus A3 paths (built-in agents are read-only).
  - Create: pick scope (user/project), open a template in `$EDITOR`; after saving, the
    engine picks the agent up (spike: is a restart needed? Check `commands_changed` and
    `initialize.agents` after a reload).
  - Edit and delete with confirmation. In 2.1.288 Claude Code's `/agents` is marked removed,
    so mantle's version is a convenience; keep it minimal.
- [ ] **B6 [M2] `/memory` and `/pause-memory`.**
  - List memory files (A3 plus `init.memory_paths`) and open one in `$EDITOR`.
  - Toggle auto memory (`autoMemoryEnabled`); open the auto-memory folder.
  - After editing, call `register_repo_root {directory, reload_claude_md: true}` if
    supported; otherwise tell the user the change applies next session.
  - `/pause-memory`: E if it's in `initialize.commands`; otherwise toggle the setting.
- [x] **B7 [M2] `/doctor`.**
  - Shows the A4 checks.
  - Offers "Run Claude Code's installation check" (`claude doctor` via ExecProcess, H) and
    "Ask Claude to diagnose" (sends the engine's bundled `/doctor` skill, E).
  - The native command takes the name `/doctor`; register the engine skill under its
    original name too so it stays reachable (coordinate the routing rule with plan 04's
    slash routing).
- [x] **B8 [M2] `/login` and `/logout`.** `claude auth login [--claudeai|--console|--sso]`
  (pick the method in a small dialog) or `claude auth logout` via ExecProcess, then restart
  the engine and show the new `initialize.account`.
- [x] **B9 [M2] `/upgrade`, `/feedback` (`/bug`), `/import`, `/install-github-app`.**
  - `/upgrade` (subscription) is H.
  - `/feedback` and `/bug` are H (consent and upload happen in Claude Code).
  - `/import`: dry-run preview from `claude import --dry-run`, confirm, then run with
    `--yes=<digest>`.
  - `/install-github-app` is H.
  - Engine binary updates: `claude update` via ExecProcess, then trigger plan 02's
    conformance probe.
- [x] **B10 [M2] H commands for cloud and product features.** Register each as a command
  that calls plan 06's generic hand-off action (by ext ID).
  - **Spike first:** does `claude --resume <sid> "/<cmd>"` run the command at startup in
    interactive mode? If yes, pass it through; if not, hand off and show a one-line hint to
    type the command.
  - **H list:** `/remote-control` (`/rc`), `/teleport` (`/tp`), `/desktop` (`/app`),
    `/mobile` (`/ios`, `/android`), `/session` (`/remote`), `/web-setup`, `/remote-env`,
    `/ultraplan`, `/autofix-pr`, `/background` (`/bg`), `/chrome`, `/ide`, `/artifacts`,
    `/design-login`, `/passes`, `/usage-credits`, `/setup-bedrock`, `/setup-vertex`,
    `/install-github-app`, `/cloud-plugins`, `/daemon`, `/powerup`, `/stickers`, `/radio`,
    `/wellbeing`.
  - **Exceptions:** anything that appears in `initialize.commands` (headless-capable, e.g.
    `/ultrareview`, `/list-agents`, `/stop`) is E, not H. Decide at runtime from the engine's
    list, with this table as the default.
- [ ] **B11 [M2] Agent view.** A read-only list of background sessions from `claude agents
  --json [--all]`. Attach is H via ExecProcess `claude attach <id>`; logs via `claude logs
  <id>`; stop via `claude stop <id>`.

## Design notes
- **Structure.** One feature per panel inside `features/ecosystem`. `internal/claudecli` is
  importable by any plan (only feature-to-feature imports are banned). Plan 08 uses it for
  `auto-mode`, plan 10's `mantle doctor` may reuse A4, and plan 11's passthrough uses the
  same binary resolution.
- **Prefer the control channel; fall back to the CLI.** Prefer control requests while an
  engine is running (`mcp_status`, `reload_*`, `get_hooks_listing`). Use the CLI only for
  what the control channel can't do (add/remove/login, plugin install). After a CLI
  mutation, always refresh via the control channel.
- **Restarts:** any action that requires an engine restart goes through `Engine.Restart`
  with `--resume`, and first warns if `background_tasks` is non-empty (restarting kills
  background shells and subagents).
- **ExecProcess flows** suspend mantle's renderer; when they return, request a redraw (core
  `Reprint` if needed).
- **Wording.** Labels are mantle's own wording.

## Interfaces you provide / consume
- **Provide:**
  - `internal/claudecli` (safe runner, typed wrappers, `Interactive`);
  - commands `/mcp`, `/plugin` (`/plugins`, `/marketplace`), `/skills`, `/hooks`, `/agents`,
    `/memory`, `/pause-memory` (fallback), `/doctor`, `/login`, `/logout`, `/upgrade`,
    `/feedback` (`/bug`), `/import`, and the H command set;
  - the agent-view dialog;
  - an `MCPNeedsAuth{count}` message type (declared in `pkg/ext` as an additive change, or
    use a Notice).
- **Consume:** `pkg/ext` (dialogs, commands, settings writer via ext), `pkg/proto` (MCP
  status, hooks listing types), the engine's `Control()`/`Restart()`, plan 06's hand-off
  action (by ID), plan 01's config writer.

## Tests and done criteria
- `internal/claudecli` argv regression tests (A1) and wrapper fixture tests (A2).
- Discovery tests over temp trees (A3); doctor check tests with fake binaries and broken
  settings files (A4).
- Every panel is tested against fakeclaude control responses (`mcp_status` with mixed
  states, `reload_plugins` results, hooks listing). Exec flows are tested with stub binaries
  on `PATH`.
- A Story and vt goldens per panel at 60/100/160 columns.
- `make test-09` passes; `go build -tags no_ecosystem ./cmd/mantle-ui` still builds.

## Parity coverage
- `EC-*`: `/mcp` (incl. OAuth), `/plugin`, `/skills`, `/hooks`, `/agents`, `/memory`,
  `/pause-memory`, `/doctor`, `/login`, `/logout`, `/upgrade`, `/feedback`, `/bug`,
  `/import`, and the E passthrough rows (skills, custom commands, MCP prompts, `/init`,
  `/insights`, `/security-review`, `/statusline`, `/reload-*`, `/skill-doctor`,
  `/ultrareview`). Their routing is plan 04's; you confirm they work.
- `CL-*`: every cloud and product command (H) and the agent view.
- `GAP-*`: note which items stay X (voice, Claude Code TS mods drawing, agent-team panes,
  computer use) if a user types them, with a one-line explanation.

## Out of scope
- **Generic hand-off mechanism:** plan 06 (you only register commands that use it).
- **`/permissions`, `/config`, `/model`, `/theme`:** plan 08.
- **Startup trust and `.mcp.json` approval gates:** plan 05.
- **`claude` CLI flag parsing and subcommand passthrough at the `mantle` command line:**
  plan 11 (shared binary resolution only).
- **`/tasks` and background task output:** plan 07.

**Reminder of the parallel rules:** edit mainly your primary paths; contracts are
additive-only; no repo-wide rewriters (`make fmt-09`); commit only your files with `[09]`;
per-area build tag `no_ecosystem`.

## Facts verified on 2.1.288 (Part A)

- **`--` before operands** works for the commander-based subcommands (`mcp get -- "<name>"`,
  `plugin configure --json -- <id>`): `mcp get -- -p` looks up a server named `-p`.
  `import`, `logs`, `attach` and `stop` parse argv themselves and reject `--`
  (`unknown option '--'`, or a usage error for `import`), so `internal/claudecli` gives
  them strict operands instead (an enum for `import`, `[A-Za-z0-9._-]` IDs for the rest).
- Flags are always emitted as `--long=value`. Commander's variadic options (`mcp add -e`,
  `-H`, `marketplace add --sparse`) swallow following words in the `-e A B` form but not
  in the `--env=A` form.
- `mcp list` / `mcp get` print text only. Row format `name: target - <glyph> <status>`;
  statuses seen in the binary: Connected, Connected · tools fetch failed, Needs
  authentication, Failed to connect, Pending approval, Rejected, Disabled for this
  project, Not configured (glyph `-`). No servers: a single "No MCP servers configured"
  line.
- Plugin and marketplace mutations with `--json` print one result line
  (`{command, outcome, plugin, message, failureCode}`); failures also exit 1. A
  marketplace-declared install command is reported as `shownCommand.sha256` and accepted
  with `--accept-command <sha256>`. mantle never passes a blanket `--yes` to
  install/update: the panel shows the command and re-runs with `--accept-command`.
- `plugin list --available --json` is `{installed: [...], available: [...]}`; `source`
  is a relative path string or an object keyed by `source` (`url`, `git-subdir`, …).
  `plugin marketplace list --json` omits the built-in directory and claude.ai-hosted
  marketplaces that the text output shows.
- `auth status --json`: `{loggedIn, authMethod, apiProvider, email, orgId, orgName,
  subscriptionType, …}`. `agents --json`: interactive sessions have no `id`; background
  ones have `id` and `state`.
- `import --dry-run` reports `(scan digest: <d>)`; apply with `import --yes=<d> [source]`.
- `skillOverrides` values: `on`, `name-only`, `user-invocable-only`, `off`.
- Managed policy directory: `/Library/Application Support/ClaudeCode` (macOS),
  `/etc/claude-code` (Linux); managed skills in `<dir>/.claude/skills`, rules in
  `<dir>/.claude/rules`, user rules in `~/.claude/rules`.
- `autoMemoryDirectory` is honoured from managed, local and user settings, not from the
  checked-in project file. Auto memory is keyed by the main checkout of the git repo
  (worktrees share it): `~/.claude/projects/<slug>/memory/MEMORY.md`.
