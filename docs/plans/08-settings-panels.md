# Plan 08: settings & model panels

| Session | Primary paths | Part B needs | Produces |
|---|---|---|---|
| mantle-08 | `features/settings` (plus its own data packages under `features/settings/...`) | tags `contracts-v1`, `proto-v1` | none |

## Goal

Rebuild Claude Code's interactive settings and model panels natively. In headless mode they
are `local-jsx` commands and are refused, so mantle must provide them itself:
- `/model` (with effort, fast mode and thinking), `/effort`, `/fast`;
- `/config` (tabs Config / Status / Usage / Stats, plus a generated mantle section),
  `/status`;
- `/theme`, `/output-style`, `/permissions`, `/keybindings`, `/terminal-setup`;
- the small toggles `/focus`, `/tui`, `/scroll-speed`, `/advisor`, `/autocompact`, `/vim`;
- `/help`.

Everything persists **exactly where Claude Code persists it**, so switching between mantle
and `claude` is seamless.

## Start prompt
> You are session 08 of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/08-settings-panels.md`, then execute that plan. Do Part A now. Start Part B
> once `git tag -l` shows the tags it needs. Commit with prefix `[08]`.

## Read first
- `docs/plans/00-overview.md`: the settings decision and the parallel rules.
- `docs/research/inventory-binary-2.1.288.md`:
  - section 2: command list and which commands are `local-jsx`;
  - section 3: `ModelPicker`, `EffortSlider`, `ThemePicker` and `Settings` keybinding
    contexts;
  - section 4: all 192 settings keys and the `~/.claude.json` keys;
  - section 6: model aliases.
- `docs/research/inventory-docs.md`: sections 4 (permissions), 8 (settings), 16 (models),
  17 (output styles).
- `docs/research/protocol-2.1.288.md`:
  - control requests `list_models`, `set_model`, `set_max_thinking_tokens`,
    `apply_flag_settings`, `update_settings`, `get_settings`, `list_permission_rules`,
    `reload_output_styles`;
  - `initialize` response fields `models`, `available_output_styles`, `account`,
    `fast_mode_state`.

## Facts you can rely on
- **Models.**
  - `list_models` and `initialize.models` return `ModelInfo {value, resolvedModel?,
    displayName, description, supportsEffort?, supportedEffortLevels?,
    supportsAdaptiveThinking?, supportsFastMode?, supportsAutoMode?}`, plus
    `unavailable_models?`.
  - Aliases: `default`, `best`, `fable`, `opus`, `sonnet`, `haiku`, `[1m]` variants,
    `opusplan`.
- **Switching model:** `set_model {model}`, where null or `"default"` resets. It takes
  effect on the next request, so it is safe mid-turn. In headless mode `/model <m>` is
  session-only, so persisting a default is mantle's job: write `model` into user settings.
  `s` in the picker means "this session only".
- **Effort.**
  - Levels: `low | medium | high | xhigh | max`, plus the **ultracode** toggle (Tab in the
    effort slider).
  - Session-only: `apply_flag_settings {"effortLevel": …}`.
  - Persistent: `update_settings {source: "userSettings", settings: {effortLevel}}`. The
    `update_settings` allowlist is only `outputStyle` and `effortLevel`.
  - Per-model defaults live in `modelSettings.<model>.effortLevel`. The user's
    `~/.claude/settings.json` has `modelSettings` for `claude-opus-5-5` with effort `xhigh`.
- **Fast mode:** setting `fastMode`, plus `fastModePerSessionOptIn`. `init.fast_mode_state`
  and `fast_mode_disabled_reason` report it. `/fast` works in `-p` only if `fastMode` is set
  via flag settings; use `apply_flag_settings`. Spike in Part B.
- **Thinking:** toggled with `set_max_thinking_tokens {max_thinking_tokens,
  thinking_display}` or `alwaysThinkingEnabled`. It **cannot be turned off** on Opus 5.5,
  Sonnet 5.5 or Fable (`supportsAdaptiveThinking`), so show it as locked.
- **Keybindings** (Claude Code IDs, copied verbatim into plan 01's table):
  - `chat:modelPicker` (meta+p), `chat:fastMode` (meta+o), `chat:thinkingToggle` (meta+t),
    `chat:increaseEffort` / `chat:decreaseEffort`, `chat:defaultToNewerModel` (ctrl+y).
  - Context `ModelPicker`: left/right change effort, `s` session only.
  - Context `EffortSlider`: left/right, tab toggles ultracode, `s`.
  - Context `ThemePicker`: ctrl+t toggles syntax highlighting, ctrl+e edits the custom theme.
- **Themes:** `auto | dark | light | light-daltonized | dark-daltonized | light-ansi |
  dark-ansi | custom:<name>`. Custom themes are `~/.claude/themes/*.json` token overrides,
  live-reloaded. The theme engine itself is plan 01's; you build the picker and persistence
  (`theme` in user settings).
- **Output styles:** built-ins Default, Proactive, Concise, Explanatory and Learning, plus
  custom styles in `output-styles/` directories. Headless `/output-style [style]` works (E).
  Persist with `update_settings {source:"localSettings", settings:{outputStyle}}`; refresh
  with `reload_output_styles`.
- **Two config stores.**
  - **Settings files** (`~/.claude/settings.json`, `.claude/settings.json`,
    `.claude/settings.local.json`, plus managed (read-only) and flag scopes).
  - **Global config** (`~/.claude.json`): `copyOnSelect`, `copyFullResponse`,
    `externalEditorContext`, `diffTool`, `autoConnectIde`, `editorMode` (check which file
    holds it in 2.1.288), and others.
  - mantle **never writes `~/.claude.json`**. For keys that live there, send the headless
    `/config key=value` command (documented as working in `-p`); the engine then writes its
    own file.
- **Settings-file writes** go through plan 01's config writer: atomic, under flock,
  re-reading and merging just before writing. The engine live-reloads settings (the
  `ConfigChange` hook fires).
- **mantle's own settings** live only in `~/.mantle/settings.json`, declared with
  `ext.SettingSpec`. Never put unknown keys in Claude Code's settings files: the schema is
  strict and `-p` silently ignores invalid files.
- **Permission rules** (engine syntax): `Tool`, `Tool(spec)`, `Tool(param:value)`,
  wildcards, `mcp__server__*`, `Read`/`Edit` path anchors, `WebFetch(domain:…)`,
  `Agent(...)`, `Skill(...)`. Scopes: user, project, local; managed is read-only.
  `permissions.additionalDirectories`, `permissions.defaultMode` and the `autoMode
  {allow, soft_deny, hard_deny, environment}` rules are editable. `list_permission_rules`
  returns the effective state.

## Part A: start immediately (no other session needed)
- [x] **A1 Model data layer.**
  - Turn `[]ModelInfo` into picker rows: display name, description, effort levels,
    fast/auto support, a "Default (recommended)" row and the current marker.
  - Effort resolution order: session flag, then `modelSettings[model].effortLevel`, then
    `effortLevel`, then the model default. Clamp to `supportedEffortLevels`.
  - Tests use ModelInfo fixtures. Write the types locally against the spec;
    `pkg/proto.ModelInfo` replaces them after `proto-v1`.
- [x] **A2 `/config` option table.** One entry per user-facing setting:
  - fields: key, store (settings scope / global config / mantle), type, allowed values,
    default, label and description (in mantle's own words), and write path
    (`update_settings` / `apply_flag_settings` / config writer / engine `/config k=v`);
  - built from the research inventories (the UI keys in inventory-docs section 8 and the
    settings in inventory-binary section 4);
  - test that every key is unique and has a write path.
- [x] **A3 `/status` data assembly.** A struct and builder:
  - mantle and engine versions, model, account (email, org, subscription, `apiProvider`),
    cwd and additional dirs;
  - settings sources in effect, MCP servers with status, memory files, tools count, output
    style, permission mode, fast mode, sandbox status.
- [x] **A4 `/terminal-setup` installers.** Pure functions from (terminal, current config
  file contents) to (a proposed edit and a human-readable diff), then apply with a backup.
  - Terminals: Ghostty, iTerm2, VS Code / Cursor / Windsurf, Terminal.app, WezTerm, kitty,
    Alacritty.
  - Detect via `TERM_PROGRAM` and friends.
  - Note: Ghostty, kitty and WezTerm support the kitty keyboard protocol, which Bubble Tea
    v2 requests by default, so shift+enter already works there. The installer should say so
    instead of editing. The user's terminal is Ghostty.
  - Tests use fixture config files.
- [x] **A5 Permission rules model.**
  - Parse and print rule strings (round-trip tests).
  - A merged view across scopes with a source label on each rule.
  - Edit operations (add/remove allow, deny or ask; add/remove directory; set
    `defaultMode`) that produce settings patches per scope; managed rules are shown
    read-only.
- [x] **A6 Theme list model.** Built-in names plus `custom:*` from `~/.claude/themes`;
  current theme; preview-selection state machine (move, preview, confirm, cancel restores
  the original).
- [x] **A7 Help model.** Group commands by source (built-in native / engine / skills /
  plugins / MCP prompts / mantle mods), including aliases and argument hints; group key
  bindings by context from the keymap table. Pure function over plain lists.

## Part B: after `contracts-v1` and `proto-v1`
- [x] **B1 [M1] `/model` picker** (also `chat:modelPicker`, meta+p).
  - Opens with the current model highlighted. Left/right change effort for the highlighted
    model; Enter applies via `set_model` (+ `apply_flag_settings {effortLevel}`).
  - Persists `model` (and per-model effort) to user settings unless `s` was pressed.
  - Also handles `/model <name>` typed with an argument: native fast path, persisted the
    same way.
  - **Mid-session switch warning:** if the session has a large cached context, warn that
    switching models re-reads it (Claude Code's cache-warm confirmation).
- [x] **B2 [M1] `/help`.** Native help dialog from A7: commands (native registry plus
  `initialize.commands` plus `commands_changed`) and shortcuts, searchable. (`?` on an empty
  prompt is plan 04's shortcut panel; it may reuse your component by ID.)
- [ ] **B3 [M2] `/effort`, `/fast` and thinking.**
  - `/effort` slider with Tab for ultracode, `s` for session only, persisted via
    `update_settings`.
  - `/fast [on|off]` and `chat:fastMode` (meta+o), hidden when the model doesn't support it
    or `fast_mode_disabled_reason` is set.
  - `chat:thinkingToggle` (meta+t), locked where thinking can't be disabled.
  - `chat:increaseEffort` / `chat:decreaseEffort`; `chat:defaultToNewerModel` (ctrl+y)
    behaviour per Claude Code.
- [x] **B4 [M2] `/config`.**
  - Tabs: **Config** (from the A2 table: search with `/`, toggle with space or enter, enums
    cycle), **Status** (B5), **Usage** and **Stats**.
  - Usage and Stats are plan 06's components: look them up by ext ID; don't import
    `features/sessions`.
  - A generated **mantle** section from every registered `SettingSpec`.
  - `/config key=value` with an argument is passed straight to the engine (E).
- [x] **B5 [M2] `/status`** from A3. Refresh MCP state with `mcp_status`.
- [x] **B6 [M2] `/theme`.** Picker with live preview (send a theme-preview message the host
  applies; Esc restores the original), ctrl+t syntax-highlighting toggle, ctrl+e opens the
  custom theme file in `$EDITOR` (via `tea.ExecProcess`). Persists `theme`.
- [x] **B7 [M2] `/output-style`.** Picker over `available_output_styles` with descriptions;
  apply with `update_settings` (localSettings), then `reload_output_styles`. `/output-style
  <name>` with an argument goes to the engine (E).
- [x] **B8 [M2] `/permissions` (alias `/allowed-tools`).**
  - Tabs: Allow, Ask, Deny, Workspace directories, Auto-mode rules.
  - Add and remove per scope through the config writer; refresh from
    `list_permission_rules`.
  - The Auto-mode tab reads `claude auto-mode defaults` / `claude auto-mode config` (these
    print config and don't call the model) through plan 09's safe runner
    `internal/claudecli`. Never build `claude` argv by hand: a stray positional word becomes
    a paid prompt. `critique` does call the model, so it's opt-in.
- [x] **B9 [M2] `/keybindings`, `/terminal-setup`, `/vim`.**
  - `/keybindings` opens `~/.claude/keybindings.json` in `$EDITOR`, creating it with the
    `$schema`/`$docs` header and an empty `bindings` array if missing, and offers
    `~/.mantle/keybindings.json` for `mantle:*` actions. The hot reload is plan 01's.
  - `/terminal-setup` runs the A4 installer with a confirmation dialog.
  - `/vim` toggles `editorMode` between `normal` and `vim` (in 2.1.288 it is a "moved to
    /config" stub; mantle keeps it as a convenience).
- [x] **B10 [M2] Small commands.**
  - ~~`/focus`~~ and ~~`/scroll-speed`~~: dropped from plan 08 by the coordinator
    (2026-10-03). Plan 03 owns `/focus` (VW-10) and plan 12 owns `/scroll-speed` (VW-21);
    registering them here too would make the host report a command conflict.
  - `/tui [default|fullscreen]`: persists `tui`; fullscreen is plan 12's, so until it ships,
    show a notice.
  - `/advisor [model|off]` and `/autocompact [auto|tokens]`: E passthrough plus a small
    picker when called without arguments.
  - ~~`/privacy-settings`~~: plan 09 already registers it in its hand-off table
    (`features/ecosystem/handoff`, CL-24), so plan 08 does not.
  - Ownership: PARITY.md CU-08 (`/autocompact`) moved to plan 08 by the coordinator
    (2026-10-03); plan 06's file did not list it.
- [x] **B11 [M2] Read-back test harness.** In a temp HOME / `CLAUDE_CONFIG_DIR`, perform
  every write path, then read effective settings back through real `claude` with a
  zero-token `initialize` + `get_settings` (the probe pattern from plan 02). Values must
  match.

## Facts verified in Part A (2.1.288 binary)
- **Where `/config` writes.** Most keys go to user settings. `spinnerTipsEnabled`,
  `prefersReducedMotion`, `outputStyle` and `defaultView` go to *local* settings. Several
  keys moved from `~/.claude.json` to user settings and are only read back from the global
  config as a fallback: `theme`, `editorMode`, `verbose`, `preferredNotifChannel`,
  `autoCompactEnabled`, `autoScrollEnabled`, `fileCheckpointingEnabled`, `showTurnDuration`,
  `showMessageTimestamps`, `terminalProgressBarEnabled`, `todoFeatureEnabled`,
  `teammateMode`, `inputNeededNotifEnabled`, `agentPushNotifEnabled`. Still global-only:
  `respectGitignore`, `copyFullResponse`, `copyOnSelect`, `externalEditorContext`,
  `prStatusFooterEnabled`, `autoConnectIde`, `autoInstallIdeExtension`, `diffTool`,
  `claudeInChromeDefaultEnabled`. The table in `features/settings/options` encodes this.
- **Headless `/config k=v` matches panel row IDs, not setting keys** (case-insensitive):
  `gitignore`, `prStatus`, `chrome`, `turnDuration`, `editor`, `notifChannel`, … A single
  pair keeps spaces in its value; several pairs are whitespace-separated. Rows that the
  panel only shows conditionally (e.g. `copyOnSelect` outside fullscreen, `diffTool`
  without an IDE) may be refused.
- **Thinking off** is stored as `alwaysThinkingEnabled: false`; turning it on removes the key.
- **Effort.** `effortLevel` persists only low…xhigh (`max` is session-only).
  `CLAUDE_CODE_EFFORT_LEVEL` also feeds resolution, and `maxEffortLevel` (top level or per
  model) caps it. `ultracode` is a separate boolean setting.
- `set_model` resets with `"default"` (as typed in `pkg/proto`). `unavailable_models`
  entries are `ModelInfo` plus `disabled: true`.

## Facts verified by the B11 read-back harness (real 2.1.288 against fakeapi)
- Every `/config` key mantle writes to settings files, plus the `/model`, `/theme`,
  `/permissions`, `/sandbox`, `/fast`, `ultracode` and `/tui` writes, passes the engine's
  strict schema and is read back unchanged per source.
- On startup the engine **migrates `model: "opus"` to `"opus[1m]"`** and rewrites the user
  settings file in its own key order (it also adds `env` entries from the environment).
- `update_settings {source: userSettings, settings: {effortLevel}}` stores the effort on
  the **session model's `modelSettings` entry**, not as top-level `effortLevel`.
- After `/config row=value` the engine answers at once but writes `~/.claude.json`
  **lazily** (by the time it exits). The panel shows its own value meanwhile.
- `/config autoInstallIdeExtension=…` exists only inside an IDE terminal; `/config
  chrome=…` is refused (it needs the panel's consent flow), so the Chrome row is not in
  mantle's table (plan 09's `/chrome` hand-off covers it).

## Design notes
- **Structure.** One feature per panel (`settings.model`, `settings.config`,
  `settings.theme`, …) inside the `features/settings` area package. Heavy pure logic lives
  in subpackages (`features/settings/model`, `.../perms`, `.../termsetup`) so it can be
  tested without the host.
- **Panels are dialogs** registered with `AddDialog`. Use plan 01's `pkg/ui` widgets
  (select with fuzzy filter, tabs, table, dialog frame) and Claude Code's keybinding
  contexts (`ModelPicker`, `EffortSlider`, `ThemePicker`, `Settings`, `Select`, `Tabs`), so
  the user's keybindings.json applies.
- **Write precedence:** control request (session) > `update_settings` (allowlisted keys) >
  config writer (settings files) > engine `/config key=value` (global config). Encode this
  in the A2 table so each panel calls a single `apply(option, value, persist)` helper.
- **Session vs persistent:** every panel that has an `s` key (model, effort) applies to the
  session with control requests and only writes files when persisting.
- **Wording.** Labels and descriptions are mantle's own wording (licensing hygiene). Setting
  *keys* are API and are used verbatim.

## Interfaces you provide / consume
- **Provide:**
  - commands `/model`, `/effort`, `/fast`, `/config`, `/status`, `/theme`, `/output-style`,
    `/permissions` (`/allowed-tools`), `/keybindings`, `/terminal-setup`, `/vim`, `/focus`,
    `/tui`, `/scroll-speed`, `/advisor`, `/autocompact`, `/help`;
  - actions `chat:modelPicker`, `chat:fastMode`, `chat:thinkingToggle`,
    `chat:increaseEffort`, `chat:decreaseEffort`, `chat:defaultToNewerModel`;
  - the `settings.help` component (reusable by plan 04's `?` panel).
- **Consume:**
  - `pkg/ext` (dialogs, settings API, SettingSpec registry, keymap table);
  - `pkg/proto` (ModelInfo, control request types);
  - the engine's `Control()`;
  - plan 01's config reader and writer and theme engine;
  - plan 06's Usage/Stats components and generic handoff action (by ID).

## Tests and done criteria
- Unit tests for A1–A7 (fixtures for ModelInfo, rule round-trips, terminal config
  fixtures, option-table integrity).
- A key-flow test per panel with fakeclaude responses (open, navigate, apply, persist,
  cancel).
- A Story and vt goldens at 60/100/160 columns for each panel.
- **B11 read-back harness passes:** values mantle writes are read back correctly by real
  Claude Code.
- No code path writes `~/.claude.json`. A test fails if any writer targets it.
- `make test-08` passes; `go build -tags no_settings ./cmd/mantle-ui` still builds.

## Parity coverage
- `ST-*`: `/model`, effort, fast, thinking, model shortcuts, `/config` tabs, `/status`,
  `/theme`, `/output-style`, `/permissions` (incl. auto-mode tab), `/keybindings`,
  `/terminal-setup`, `/vim`, `/focus`, `/tui`, `/scroll-speed`, `/advisor`, `/autocompact`,
  `/help`, model-switch cache warning.
- `CF-*`: the settings keys these panels write, and `~/.claude/themes`.

Tag each registration with its PARITY IDs.

## Out of scope
- **Theme engine, config reader/writer, keymap engine:** plan 01.
- **`/color`:** plan 07.
- **`/context`, `/usage`, `/cost`, `/stats` content:** plan 06.
- **`/mcp`, `/plugin`, `/skills`, `/hooks`, `/agents`, `/memory`, `/login`:** plan 09.
- **Sandbox UI (`/sandbox`):** plan 05 owns the permission/safety dialogs. If it isn't
  listed there, file a request; don't build it here.

**Reminder of the parallel rules:** edit mainly your primary paths; contracts are
additive-only; no repo-wide rewriters (`make fmt-08`); commit only your files with `[08]`;
per-area build tag `no_settings`.
