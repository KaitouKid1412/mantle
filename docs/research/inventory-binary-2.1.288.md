# Claude Code feature inventory: local install (2.1.288)

> Internal research notes for mantle planning (Claude Code 2.1.288, collected 2026-10-03). Not shipped.
> Derived from the installed binary, the official docs and the Agent SDK; do not copy strings/schemas from here into shipped code.

Binary: `~/.local/share/claude/versions/2.1.288` (symlinked from `~/.local/bin/claude`), built 2026-10-02T16:42:03Z, git `17fe1eb7`. A ~229 MB Bun-compiled Mach-O (Bun v1.4.3). The readable JS source sits around byte offset 179M, with bytecode copies at ~72–81M, so `strings` + grep works. UI is React + Ink (`ink-box`, `ink-text`, `useInput` markers).

Evidence labels: **help** = `--help` output, **bin** = found in the embedded JS, **disk** = files under `~/.claude`.

Useful quirk: on this version `claude <sub> <nested> --help` works, but a non-existent subcommand path with `--help` prints the root help instead of an error. **Never** run `claude "<words>"` without `--help` in a non-TTY context — it becomes a headless prompt.

---

## 1. CLI surface (help)

### `claude [options] [command] [prompt]`
Starts an interactive session by default; `-p/--print` gives non-interactive output.

**Context and directories**
- `--add-dir <dirs...>`: extra directories tools may access.
- `-w, --worktree [name]`: create a git worktree for this session.
- `--tmux`: create a tmux session for the worktree (needs `--worktree`); uses iTerm2 native panes when available, `--tmux=classic` for plain tmux.

**Agents and models**
- `--agent <agent>`: agent for this session (overrides the `agent` setting).
- `--agents <json-or-file>`: define custom agents inline (with `-p`, a path to a file).
- `--model <model>`: alias (`fable`, `opus`, `sonnet`) or full model name.
- `--fallback-model <models>`: comma-separated fallback chain used when the model is overloaded; the primary is retried each turn.
- `--effort <level>`: `low|medium|high|xhigh|max`.
- `--autocompact <auto|tokens>`: auto-compact window (`auto`, or 100k–1M tokens).
- `--betas <betas...>`: API beta headers (API-key users only).

**Permissions and tools**
- `--permission-mode <mode>`: `acceptEdits|auto|bypassPermissions|manual|dontAsk|plan`.
- `--dangerously-skip-permissions`: bypass all permission checks.
- `--allow-dangerously-skip-permissions`: make bypass mode available without defaulting to it.
- `--permission-prompts <host|none>`: in `-p` mode, who answers permission prompts (`none` auto-denies).
- `--allowedTools/--allowed-tools`, `--disallowedTools/--disallowed-tools <tools...>`: rule lists, e.g. `"Bash(git *) Edit"`.
- `--tools <tools...>`: restrict the built-in tool set (`""` = none, `default` = all).
- `--restricted`: drops code-running tools and WebFetch; ignores user/project/local settings; confines file tools to the working directories; refuses bypass mode.

**System prompt**
- `--system-prompt <p>`: replace the system prompt.
- `--append-system-prompt <p>`: append to the default system prompt.
- `--exclude-dynamic-system-prompt-sections`: move per-machine sections (cwd, env, memory paths, git status) into the first user message for cache reuse.
- `--system-prompt-snapshot <on|off>`: record the system prompt once per conversation and reuse it verbatim until compaction.

**Sessions**
- `-c, --continue`: continue the most recent conversation in this directory.
- `-r, --resume [id|search]`: resume by ID, or open the picker.
- `--fork-session`: when resuming, use a new session ID.
- `--session-id <uuid>`: use a specific session ID.
- `-n, --name <name>`: display name (prompt box, `/resume` picker, terminal title).
- `--from-pr [n|url]`: resume a session linked to a PR.
- `--no-session-persistence`: don't save the session (`-p` only).
- `--bg, --background`: start in the background and print an id.

**Print / SDK mode**
- `-p, --print`: print the response and exit. Skips the trust dialog; invalid settings files are silently ignored.
- `--output-format text|json|stream-json`; `--input-format text|stream-json`.
- `--json-schema <schema>`: validate structured output against a schema.
- `--include-partial-messages`, `--include-hook-events`, `--replay-user-messages`, `--forward-subagent-text`: extra stream-json events.
- `--max-budget-usd <n>`: spending cap (`-p` only).
- `--prompt-suggestions [bool]`: emit predicted next prompts.

**Config sources**
- `--settings <file-or-json>`: extra settings.
- `--setting-sources user,project,local`: which settings files to load.
- `--mcp-config <configs...>`: load MCP servers from files or JSON strings.
- `--strict-mcp-config`: use only `--mcp-config` servers.
- `--plugin-dir <path>` (repeatable): load a plugin from a directory or .zip for this session.
- `--plugin-url <url>` (repeatable): fetch a plugin .zip for this session.
- `--disable-slash-commands`: disable all skills.
- `--bare`: minimal mode. Skips hooks, LSP, plugin sync, auto-memory and CLAUDE.md discovery; sets `CLAUDE_CODE_SIMPLE=1`; auth is API key / apiKeyHelper only.
- `--safe-mode`: disable all customizations (CLAUDE.md, skills, plugins, hooks, MCP, agents, styles, themes, keybindings…); sets `CLAUDE_CODE_SAFE_MODE=1`.

**Integrations**
- `--ide`: auto-connect to the IDE if exactly one is available.
- `--chrome` / `--no-chrome`: Claude in Chrome on/off.
- `--desktop`: open in the Claude Desktop app.
- `--cloud [desc|id|url]`: create or attach a cloud session.
- `--environment <id>`: cloud session on a self-hosted environment.
- `--teleport [session]`: resume a teleport session.
- `--remote-control [name]`, `--remote-control-session-name-prefix <p>`: Remote Control.
- `--file <file_id:path...>`: download file resources at startup.
- `--brief`: enable the SendUserMessage tool.

**Debug and display**
- `-d, --debug [filter]` (e.g. `"api,hooks"` or `"!1p,!file"`); `--debug-file <path>`.
- `--verbose`: override the verbose setting.
- `--ax-screen-reader`: flat screen-reader output.
- `-v, --version`; `-h, --help`.

### Subcommands

- **`agents`**: manage background agents (agent view).
  - Session defaults: `--add-dir`, `--agent`, `--effort`, `--model`, `--permission-mode`, `--dangerously-skip-permissions`, `--allow-dangerously-skip-permissions`.
  - Listing: `--json` (active sessions as JSON), `--all` (include completed, with `--json`), `--cwd <path>` (filter).
  - Config: `--mcp-config`, `--plugin-dir`, `--restricted`, `--setting-sources`, `--settings`, `--strict-mcp-config`.
- **`attach <id>`**: open a background session here. ← returns to agent view; Ctrl+Z returns to the shell.
- **`auth`**
  - `login`: `--claudeai` (default), `--console`, `--email <e>`, `--sso`.
  - `logout`.
  - `status`: `--json` (default) or `--text`.
- **`auto-mode`**
  - `config`: print the effective config.
  - `critique [--model]`: AI feedback on your rules.
  - `defaults [--label <prefix>]`: print default environment / allow / soft_deny / hard_deny rules.
  - `reset [-y]`: remove `autoMode` from user settings.
- **`doctor`**: installation health check.
- **`gateway [--config <yaml>]`**: enterprise auth/telemetry gateway.
- **`import [codex|gemini|cursor] [--dry-run] [--yes[=digest]]`**: import config from another agent.
- **`install [stable|latest|<ver>] [--force]`**: install the native build.
- **`logs <id>`**: a background session's recent output.
- **`mcp`**
  - `add [-s local|user|project] [-t stdio|sse|http] [-e KEY=V] [-H header] [--client-id] [--client-secret] [--callback-port] <name> <cmdOrUrl> [args...]`
  - `add-json [-s] [--client-secret] <name> <json>`
  - `add-from-claude-desktop [-s]`
  - `get <name>`; `list`
  - `login [--no-browser] <name>`; `logout <name>`
  - `remove [-s] <name>`
  - `reset-project-choices`
  - `serve [-d] [--verbose]`: run Claude Code as an MCP server.
- **`plugin` / `plugins`**
  - `configure [--json] [--values-stdin] <plugin>`
  - `details <name>`: component inventory and token cost.
  - `disable [-a] [--json] [-s] [plugin]`; `enable [--json] [-s] <plugin>`
  - `eval [target]`: run a plugin's eval suite. Flags: `--ablation`, `--allow-real-servers`, `--allow-tools`, `--case`, `-j`, `--eval-dir`, `--json`, `--judge-model`, `--keep-temp`, `--max-cost-usd`, `--mocks`, `--model`, `--no-publish`, `--no-scaffold`, `--output-dir`, `--publish-report`, `--report`, `--runs`, `--scaffold`, `--tag`, `--threshold`, `--trust-plugin`, `--verbose`.
    - `eval init [--bare] [--eval-dir] [-i] [name]`
  - `init|new [--author] [--author-email] [--description] [-f] [--with skills,agents,hooks,mcp,lsp,output-style,channel] <name>`: scaffolds into `~/.claude/skills/<name>/`.
  - `install|i [--accept-command sha] [--config k=v] [--json] [--registry] [-s] [-y] <plugin>`
  - `list [--available] [--data-size] [--json]`
  - `marketplace`: `add [--claudeai] [--json] [--scope] [--sparse] <source>`, `list [--json]`, `remove|rm [--json] [--scope] <name>`, `update [--json] [name]`
  - `prune|autoremove [--dry-run] [-s] [-y]`
  - `tag [--dry-run] [-f] [-m] [--push] [--remote] [path]`
  - `test [dir]`: run a mod's `*.test.ts(x)`.
  - `uninstall|remove [--json] [--keep-data] [--prune] [-s] [-y] <plugin>`
  - `update [--accept-command] [--json] [-s] [-y] <plugin>`
  - `validate [--json] [--strict] <path>`
- **`purge [path] [--all] [--dry-run] [-i] [-y]`**: delete a project's state.
- **`respawn <id>|--all`**: restart background sessions on the current binary.
- **`rm <id> [--discard-unpushed …] [--force-remove-worktree …]`**: delete a background session (and its worktree when safe).
- **`setup-token`**: long-lived auth token.
- **`stop|kill <id>`**: stop a background session; the conversation is kept.
- **`ultrareview [target] [--json] [--post|--no-post] [--timeout <min>]`**: cloud multi-agent review.
- **`update|upgrade`**: check for and install updates.

Built-in marketplace: `anthropic-plugin-directory`; official marketplace `claude-plugins-official`.

---

## 2. Built-in slash commands (bin)

Many commands come in two variants: `local-jsx` (Ink UI) and `local` (a text twin used in non-interactive / thin-client mode, hidden in the TUI). Aliases are in brackets.

### local-jsx (interactive UI)
- `/add-dir <path>`: add a working directory.
- `/advisor`: let Claude consult a stronger model.
- `/artifacts`: browse published artifacts.
- `/auto-mode-setup`: teach auto mode your environment.
- `/autocompact [auto|<tokens>]`: how full context gets before auto-summarizing.
- `/autofix-pr`: monitor and autofix the current PR (claude.ai only).
- `/background [bg] [prompt]`: send the session to the background.
- `/branch [name]`: branch the conversation at this point.
- `/brief`: toggle brief-only mode.
- `/btw [question]`: side question without interrupting.
- `/bug [share] [report]`: report a bug or share the conversation.
- `/cd <path>`: move the session to a new directory.
- `/chrome`: Claude in Chrome settings.
- `/cloud-plugins`: whether cloud sessions use local plugins.
- `/color [<color>|default]`: prompt bar color.
- `/config [settings] [key=value]`: settings UI.
- `/context [all]`: context usage as a colored grid.
- `/copy [N]`: copy the last (or Nth-latest) response.
- `/daemon`: background services and routines.
- `/design-login`: design-system access.
- `/desktop [app]`: continue in Claude Desktop.
- `/diff`: uncommitted changes and per-turn diffs (a panel in fullscreen).
- `/effort`: effort level.
- `/exit [quit]`: exit, or detach when backgrounded. `:q`, `:q!`, `:wq`, `:wq!` are also recognized.
- `/export [filename]`: export to file or clipboard.
- `/fast [on|off]`: fast mode.
- `/feedback [report]`.
- `/focus`: focus view (prompt, summary, response only).
- `/fork [prompt]`: copy the conversation into a background session (a second `/fork <directive>` variant spawns an agent that inherits the conversation).
- `/goal [<condition>|clear]`: a condition checked before stopping; implemented as a session Stop hook.
- `/help`.
- `/hooks`: view hook configurations.
- `/ide [open]`: IDE integrations.
- `/import [codex|gemini|cursor]`.
- `/install [options]`.
- `/install-github-app`.
- `/login`, `/logout`.
- `/loops`: present but disabled.
- `/mcp [reconnect|enable|disable …]`.
- `/memory`: CLAUDE.md files and memory settings.
- `/mobile [ios, android]`: QR code for the app.
- `/model [model]`.
- `/passes`: share a free week of Claude Code.
- `/permissions [allowed-tools]`: allow/deny rules.
- `/plan [open|<desc>]`: enable plan mode or view the plan.
- `/plugin [plugins, marketplace]`.
- `/powerup`: interactive lessons.
- `/privacy-settings`.
- `/rate-limit-options`.
- `/release-notes`.
- `/remote-control [rc]`.
- `/remote-env`.
- `/rename [name] [name]`.
- `/resume [continue] [id|search]`.
- `/sandbox`: status in the description, e.g. "sandbox enabled (auto-allow)".
- `/scroll-speed`.
- `/session [remote]`: cloud session URL and QR.
- `/setup-bedrock`, `/setup-vertex`.
- `/skill-doctor`: unused skills that cost context.
- `/skills`.
- `/status`: version, model, account, connectivity, tools.
- `/stop`: stop this background session.
- `/subtask <task>`: subagent with full context.
- `/tasks [bashes]`: everything running in the background.
- `/teleport [tp]`.
- `/terminal-setup`: Shift+Enter / Option+Enter setup.
- `/theme`.
- `/tui [default|fullscreen]`: renderer.
- `/ultraplan <prompt>`: editable plan in the cloud.
- `/ultrareview`.
- `/upgrade`.
- `/usage [cost, stats]`: cost, plan usage, activity stats.
- `/usage-credits`.
- `/version`.
- `/web-setup`.
- `/wellbeing [breaks, break-reminder, downtime]`.
- `/workflows`.
- Hidden: `/extra-usage` (renamed to `/usage-credits`), `/pro-trial-expired`, `/vim` and `/output-style` stubs ("moved to /config", behind a flag).

### local (text output)
- `/clear [reset, new] [name]`: new session; the old one stays resumable.
- `/compact [instructions]`: summarize to free context.
- `/keybindings`: open the shortcuts file.
- `/rewind [checkpoint, undo]`: restore code and/or conversation.
- `/list-agents [peers]`.
- `/recap`: one-line session recap.
- `/reload-plugins [--force]`, `/reload-skills`.
- `/restart [update]`.
- `/output-style [style]`.
- `/pause-memory [memory-pause, toggle-memory]`.
- `/voice [hold|tap|off]`.
- `/radio`: lo-fi radio.
- `/stickers`.
- `/install-slack-app`.
- Hidden: `/heapdump`, `/design-consent`, `/design-revoke`, `/agents` (now "(removed)"), `/__remote-workflow`, `/workflow-launch-exec`.

### prompt (expands into a model prompt)
- `/init`: create CLAUDE.md.
- `/insights`: report analyzing your sessions.
- `/statusline`: set up the status line.
- `/team-onboarding`.
- `/commit-push-pr`.
- `/security-review`.

### Bundled skills (registered as `prompt` commands)
- Dev workflow: `/code-review [review]` (subcommand `ultra` → ultrareview), `/simplify`, `/commit`, `/pr`, `/verify`, `/debug`, `/batch`, `/loop [proactive]`, `/schedule [routines]`, `/run`, `/run-skill-generator`, `/doctor [checkup]`, `/fewer-permission-prompts`.
- Help and config: `/claude-code-docs`, `/update-config`, `/keybindings-help` (model-only), `/explain-usage`, `/memory-types`.
- Integrations: `/claude-api`, `/claude-in-chrome`, `/design`, `/design-sync`, `/setup-claude [setup-cowork]`.
- Artifacts: `/artifact-design`, `/artifact-diagramming`, `/artifact-capabilities`, `/artifact-components`, `/artifact-pr-review`, `/dataviz`, `/prototype`, `/whiteboard`, `/workshop`, `artifact-<kind>` (doc, …).
- Authoring: `/workflow-authoring`, `/plugin-authoring`, `cowork-plugin`.
- Cloud stubs (`dj(...)`): `teleport`, `remote-control`, `schedule`, `autofix-pr`, `ultraplan`, `ultrareview`.

### Command object fields (bin)
`type`, `name`, `description` (or getter), `menuDescription`, `aliases`, `argumentHint`, `argNames`, `isEnabled`, `isHidden`, `immediate`, `availability: ["claude-ai","console"]`, `policyGate`, `requires:{workspace,ink}`, `supportsNonInteractive`, `thinClientDispatch`, `terminalOriented`, `allowedTools` / `disallowedTools`, `whenToUse`, `userInvocable`, `disableModelInvocation`, `context:"fork"`, `agent`, `effort`, `model`, `subcommands`, `getArgumentCompletions`, `progressMessage`, `source:"builtin"`.

---

## 3. Keybindings (bin)

**File:** `~/.claude/keybindings.json` (absent by default). Format:

```json
{"$schema":"https://www.schemastore.org/claude-code-keybindings.json",
 "$docs":"https://code.claude.com/docs/en/keybindings",
 "bindings":[{"context":"Chat","bindings":{"ctrl+e":"chat:externalEditor","ctrl+s":null}}]}
```

- User bindings are added after the defaults; `null` unbinds a key.
- Chords are space-separated keystrokes with a 3-second timeout.
- Modifiers: `ctrl` (control), `alt`/`opt`/`option`, `shift`, `meta` (same as alt in terminals), `cmd`/`command`/`super`/`win`.
- Special keys: escape/esc, enter/return, tab, space, backspace, delete, arrow keys; defaults also use home, end, pageup, pagedown, wheelup, wheeldown.
- Validation warnings go to the debug log.

**Reserved keys**
- Errors: ctrl+c, ctrl+d (hardcoded), ctrl+m, ctrl+i, ctrl+h, ctrl+[ (identical to Enter/Tab/Backspace/Escape), `ctrl+\` (SIGQUIT), capslock, cmd+c/v/x/q/w/tab/space.
- Warning: ctrl+z (SIGTSTP).

**Default bindings by context**
- **Global**: ctrl+c interrupt; ctrl+d exit; ctrl+t toggleTodos; ctrl+o toggleTranscript; ctrl+shift+b toggleBrief; ctrl+r history:search; ctrl/meta+up and ctrl/meta+down diff file list; ctrl+] openArtifact. Also referenced: meta+j app:toggleTerminal; app:help, app:redraw, app:toggleReplTab, app:toggleDiffNoiseFilter, app:toggleDiffPreSession.
- **Chat**: escape cancel; enter submit; ctrl+j newline; ctrl+l clearInput; cmd+k clearScreen; ctrl+x ctrl+k killAgents; shift+tab cycleMode (meta+m on old Windows runtimes); meta+p modelPicker; meta+o fastMode; meta+t thinkingToggle; meta+w workflowKeywordToggle; ctrl+y defaultToNewerModel; ctrl+x enter queueSubmit; ctrl+enter or ctrl+x ctrl+s sendNow; up/down history:previous/next; ctrl+_ / ctrl+- / ctrl+shift+- / ctrl+shift+_ undo; ctrl+g or ctrl+x ctrl+e externalEditor; ctrl+s stash; ctrl+v imagePaste (alt+v on Windows/WSL); ctrl+x ctrl+a abovePrompt:toggle; ctrl+x tab abovePrompt:focus; space voice:pushToTalk. Also: chat:attentionUp/Down, chat:cycleProactivity, chat:increase/decreaseEffort.
- **Autocomplete**: tab accept, esc dismiss, up/down.
- **Confirmation**: enter yes, esc no, up/down, tab nextField, space toggle, shift+tab cycleMode.
- **Settings**: esc; up/down, j/k, ctrl+p/n; home/end; pageup/pagedown; space/enter accept; `/` search; r retry; d/w period day/week; t sortByTokens; ctrl+u/ctrl+d half-page.
- **Tabs**: tab/shift+tab, right/left.
- **Transcript**: ctrl+e toggleShowAll; ctrl+c / esc / q exit; less-style scrolling (ctrl+u/d/b/f/n/p, g, G, j, k, space, b, arrows, home, end).
- **HistorySearch**: ctrl+r next; esc/tab accept; ctrl+c cancel; enter execute; ctrl+s cycleScope.
- **Task**: ctrl+b or ctrl+x ctrl+b background the running task.
- **ThemePicker**: ctrl+t toggle syntax highlighting; ctrl+e edit custom theme.
- **Scroll** (fullscreen): pageup/pagedown, mouse wheel, ctrl+home/end; ctrl+shift+c or cmd+c copy; shift+arrows / shift+home / shift+end extend selection.
- **Help**: esc.
- **Attachments**: left/right, backspace/delete remove, down/esc exit.
- **Footer**: up/down (also ctrl+p/n), left/right, enter open, esc clear, x close.
- **AbovePrompt / AbovePromptInput / AbovePromptSelect / Pane / PaneField** (plugin panes): tab / shift+tab, enter or space press, esc leave, scrolling; ctrl+x arrows grow/shrink; ctrl+x x close.
- **DiffDialog**: esc; left/right source; up/down or j/k file; paging keys.
- **ModelPicker**: left/right effort; s this-session-only.
- **EffortSlider**: left/right; tab toggleUltracode; s.
- **Select**: arrows, j/k, ctrl+n/p, paging, home/end, enter, esc.
- **Plugin**: space toggle, i install, f favorite, ctrl+s cycle marketplace.
- **Agents** (agent view): ctrl+s switchView, ctrl+t togglePin, ctrl+f find, ctrl+r rename, ctrl/meta+up/down group.
- **DiffPanel**: ctrl+x b cycleDiffBase.
- **Also defined:** a `MessageSelector` context (the rewind picker: up/down/top/bottom/select) and `selection:clear`.

Action-ID families present in the binary: `chat:*`, `app:*`, `confirm:*`, `select:*`, `scroll:*`, `footer:*`, `diff:*`, `settings:*`, `tabs:*`, `attachments:*`, `plugin:*`, `autocomplete:*`, `transcript:*`, `messageSelector:*`, `modelPicker:*`, `history:*`, `historySearch:*`, `theme:*`, `task:*`, `voice:*`, `effortSlider:*`, `agents:*`, `selection:*`.

**Vim mode**
- Setting `editorMode: "normal"|"vim"`.
- Modes: INSERT, NORMAL, VISUAL, VISUAL LINE, REPLACE.
- An `-- INSERT --` indicator shows below the prompt; hide with `statusLine.hideVimModeIndicator`.
- `vimInsertModeRemaps`, e.g. `{"jj":"<Esc>"}`.
- `keybindingFlavor` is deprecated; word-editing keys always follow readline.

---

## 4. Settings (bin + disk)

**Scopes**
- `userSettings` (`~/.claude/settings.json`)
- `projectSettings` (`.claude/settings.json`)
- `localSettings` (`.claude/settings.local.json`)
- `flagSettings` (`--settings`)
- `policySettings` (managed): `/Library/Application Support/ClaudeCode/managed-settings.json`, a `managed-settings.d/` drop-in directory, MDM plist / HKLM, server-managed settings, or a `policyHelper`; combined via `managedSourcesBehavior`.
- Internal sources also include `cliArg`, `session` and `command`.
- MCP config files: `.mcp.json` and `managed-mcp.json`.
- The keybindings schema URL is embedded; **no settings-schema URL is embedded** (`$schema` accepted but ignored). Public schema: https://www.schemastore.org/claude-code-settings.json.
- The settings schema is strict; `-p` silently ignores invalid settings files → never add unknown keys to Claude Code's settings files.

**Top-level settings keys (192, from schema function `Ta`)**
- **Auth and helpers:** `$schema`, apiKeyHelper, proxyAuthHelper, awsCredentialExport, awsAuthRefresh, gcpAuthRefresh, processWrapper, policyHelper(s), otelHeadersHelper, forceLoginMethod (claudeai|console|gateway), forceLoginGatewayUrl, forceLoginOrgUUID, gatewayInternalNetworks, allowedProviders, forceRemoteSettingsRefresh, parentSettingsBehavior, managedSourcesBehavior, wslInheritsWindowsSettings.
- **Permissions:** `permissions{allow, deny, ask, defaultMode, disableBypassPermissionsMode, disableAutoMode, blockReadsOutsideWorkingDirectories, additionalDirectories}`, allowManagedPermissionRulesOnly, skipDangerousModePermissionPrompt, skipAutoPermissionPrompt, useAutoModeDuringPlan, `autoMode{allow, soft_deny, hard_deny, environment, classifyAllShell}`.
- **Model:** model, fallbackModel, availableModels, enforceAvailableModels, availableModelsMatch, deniedModels, modelOverrides, modelPicker, modelPricing, modelSettings (per model: effortLevel, autoCompactWindow…), effortLevel, maxEffortLevel, alwaysThinkingEnabled, showThinkingSummaries, fastMode, fastModePerSessionOptIn, advisorModel, ultracode, agent, promptCacheTtl, subagentPromptCacheTtl, switchModelsOnFlag, autoContinueAtUsageLimit.
- **Hooks:** hooks, disableAllHooks, allowManagedHooksOnly, allowedHttpHookUrls, httpHookAllowedEnvVars.
- **MCP:** enableAllProjectMcpServers, enabledMcpjsonServers, disabledMcpjsonServers, disableClaudeAiConnectors, managedMcpServers, allowedMcpServers, deniedMcpServers, allowManagedMcpServersOnly, allowAllClaudeAiMcps, allowClaudeInChromeWithManagedMcp.
- **Plugins and skills:** enabledPlugins, prependPlugins, appendPlugins, extraKnownMarketplaces / additionalMarketplaces, strictKnownMarketplaces / allowedMarketplaces, blockedMarketplaces, disableCommandPluginSources, disableSideloadFlags, pluginSuggestionMarketplaces, pluginConfigs, pluginTrustMessage, strictPluginOnlyCustomization, skillOverrides, disableBundledSkills, disableSkillShellExecution, skillListingMaxDescChars, skillListingBudgetFraction, syncClaudeAiSkills, syncClaudeAiPlugins.
- **UI:**
  - Appearance: theme (auto|dark|light|light-daltonized|dark-daltonized|light-ansi|dark-ansi|`custom:*`), editorMode, vimInsertModeRemaps, keybindingFlavor, syntaxHighlightingDisabled, maxProseWidth, prefersReducedMotion, axScreenReader.
  - Views: verbose, viewMode (default|verbose|focus), defaultView (chat|transcript), tui (default|fullscreen), outputStyle, language.
  - Status line and footer: statusLine `{type:"command", command, padding, refreshInterval, hideVimModeIndicator}`, subagentStatusLine, prUrlTemplate, footerLinksRegexes.
  - Spinner and tips: spinnerTipsEnabled, spinnerVerbs `{mode: append|replace, verbs}`, spinnerTipsOverride `{tips, tipsFile}`, companyAnnouncements.
  - Input helpers: spellcheck `{enabled, checker, color}`, emojiCompletionEnabled, promptSuggestionEnabled.
  - Time and scrolling: showTurnDuration, showMessageTimestamps, timeFormat, timeZone, autoScrollEnabled, wheelScrollAccelerationEnabled.
  - Terminal: terminalProgressBarEnabled (OSC 9;4), terminalTitleFromRename, todoFeatureEnabled.
- **Notifications:** preferredNotifChannel (auto|iterm2|terminal_bell|iterm2_with_bell|kitty|ghostty|notifications_disabled), inputNeededNotifEnabled, agentPushNotifEnabled, voiceEnabled, `voice{autoSubmit…}`, breakReminder, quietHours, feedbackSurveyRate, feedbackDrafts.
- **Context and memory:** autoCompactEnabled, precomputeCompactionEnabled, autoCompactWindow, autoMemoryEnabled, autoMemoryDirectory, autoDreamEnabled, awaySummaryEnabled, claudeMd, claudeMdExcludes, cleanupPeriodDays (default 30), desktopSessionCleanupPeriodDays, fileCheckpointingEnabled, plansDirectory.
- **Shell:** defaultShell, bashEditDiffEnabled, bashOutputMaxChars (default 30000), taskOutputMaxChars (deprecated), respondToBashCommands, `sandbox{…}` (enabled, filesystem/network allow and deny lists, proxies, credentials masking, excludedCommands, …).
- **Workflows and agents:** disableAgentView, disableRemoteControl, disableWorkflows, enableWorkflows, workflowSizeGuideline, workflowKeywordTriggerEnabled, disableArtifact, enableArtifact, teammateMode (tmux|iterm2|in-process|auto), `worktree{symlinkDirectories, baseRef, bgIsolation, location}`, `remote{defaultEnvironmentId}`, remoteControlAtStartup, remoteControl, isolatePeerMachines, daemonColdStart, crossSessionInbound, autoUploadSessions, channelsEnabled, allowedChannelPlugins, sshConfigs, remoteTools.
- **Misc:** env, attribution `{commit, pr, sessionUrl}`, includeCoAuthoredBy (deprecated), includeGitInstructions, fileSuggestion, respectGitignore, autoUpdatesChannel, minimumVersion, requiredMinimumVersion, requiredMaximumVersion, skipWebFetchPreflight, showClearContextOnPlanAccept, askUserQuestionTimeout, dialogExpiry, modelProposedGoals, disableDeepLinkRegistration.
- **Internal:** doneMeansMerged, totalTokensReminder(+Budget / AfterUserTurn), skipWorkflowUsageWarning.

**Hook entry shape**
- `{matcher, hooks:[…]}`.
- Hook types: `command` (command, args, shell bash|powershell, timeout, statusMessage, once, async, asyncRewake), `prompt` (prompt with `$ARGUMENTS`, model, continueOnBlock), `http` (url, headers, allowedEnvVars), `mcp_tool` (server, tool, input with `${path}` interpolation). Internal types: script, function, callback.

**`~/.claude.json` (global state; key names only)**
- Top level: numStartups, installMethod, autoUpdates, tipsHistory, cachedGrowthBookFeatures, machineID, userID, oauthAccount (accountUuid, emailAddress, organization…, seat / rate-limit tiers), hasCompletedOnboarding, lastReleaseNotesSeen, projects, skillUsage, pluginUsage, githubRepoPaths, diffSidebarOpen, cachedUsageUtilization, promptQueueUseCount, btwUseCount, and others.
- Per-project entries: allowedTools, mcpServers, enabled/disabledMcpjsonServers, hasTrustDialogAccepted, lastCost, lastDuration, last token totals, lastModelUsage, lastSessionId, lastFpsAverage, …
- No lockfile in 2.1.288 → mantle must never write `~/.claude.json`.

---

## 5. Environment variables (bin)

- **`CLAUDE_CODE_*`** (~700 names). Key ones:
  - Providers: USE_BEDROCK, USE_VERTEX, USE_FOUNDRY, USE_GATEWAY, USE_MANTLE, USE_ANTHROPIC_AWS, USE_ANTHROPIC_GOOGLE_CLOUD, USE_POWERSHELL_TOOL; SKIP_*_AUTH.
  - Auth: OAUTH_TOKEN, API_KEY_HELPER_TTL_MS, CLIENT_CERT / CLIENT_KEY.
  - Limits: MAX_OUTPUT_TOKENS, MAX_RETRIES, MAX_TURNS, MAX_TOOL_USE_CONCURRENCY, MAX_CONTEXT_TOKENS, FILE_READ_MAX_OUTPUT_TOKENS, AUTO_COMPACT_WINDOW, EFFORT_LEVEL, SUBAGENT_MODEL.
  - Modes: SIMPLE, SAFE_MODE, RESTRICTED, NO_FLICKER, SHELL, SHELL_PREFIX, TMPDIR, ENTRYPOINT.
  - Disables: DISABLE_TERMINAL_TITLE, DISABLE_MOUSE, DISABLE_ALTERNATE_SCREEN, DISABLE_VIRTUAL_SCROLL, DISABLE_THINKING, DISABLE_ADAPTIVE_THINKING, DISABLE_AUTO_MEMORY, DISABLE_CLAUDE_MDS, DISABLE_FILE_CHECKPOINTING, DISABLE_BACKGROUND_TASKS, DISABLE_NONESSENTIAL_TRAFFIC, DISABLE_FAST_MODE, DISABLE_WORKFLOWS, DISABLE_AGENT_VIEW, DISABLE_GIT_INSTRUCTIONS, DISABLE_1M_CONTEXT, and more.
  - Features: ENABLE_TELEMETRY, ENABLE_TASKS, ENABLE_TODO_TOOLS, ENABLE_PROMPT_SUGGESTION, ENABLE_AUTO_MODE, EXPERIMENTAL_AGENT_TEAMS, SYNTAX_HIGHLIGHT, SCROLL_SPEED, ACCESSIBILITY, AUTO_CONNECT_IDE, IDE_SKIP_AUTO_INSTALL, TMUX_*, GIT_BASH_PATH, GLOB_*, WEBFETCH_*.
  - SDK/headless: EMIT_SESSION_STATE_EVENTS, SDK_READS_SESSION_STATE, ENABLE_SDK_FILE_CHECKPOINTING, FORK_SUBAGENT, PRINT_BG_WAIT_CEILING_MS.
- **`ANTHROPIC_*`:** API_KEY, AUTH_TOKEN, BASE_URL, MODEL, SMALL_FAST_MODEL (+_AWS_REGION), DEFAULT_{OPUS,SONNET,HAIKU,FABLE}_MODEL (+_NAME / _DESCRIPTION / _SUPPORTED_CAPABILITIES), CUSTOM_MODEL_OPTION*, CUSTOM_HEADERS, BETAS, BEDROCK_BASE_URL, VERTEX_PROJECT_ID, VERTEX_BASE_URL, FOUNDRY_*, GOOGLE_CLOUD_*, AWS_*, CONFIG_DIR, LOG, UNIX_SOCKET, ORGANIZATION_ID, WORKSPACE_ID, …
- **`DISABLE_*`:** AUTO_COMPACT, AUTOUPDATER, UPDATES, COMPACT, COST_WARNINGS, ERROR_REPORTING, TELEMETRY, GROWTHBOOK, PROMPT_CACHING (+ per model family), INTERLEAVED_THINKING, INSTALLATION_CHECKS, COLORS, PAGE_SKIPPING, and {BUG, DOCTOR, FEEDBACK, LOGIN, LOGOUT, UPGRADE, EXTRA_USAGE, INSTALL_GITHUB_APP}_COMMAND.
- **`ENABLE_*`:** TOOL_SEARCH, LSP_TOOL, PROMPT_CACHING_1H (+_BEDROCK), CLAUDEAI_MCP_SERVERS, SESSION_BACKGROUNDING, SESSION_PERSISTENCE, MCP_LARGE_OUTPUT_FILES, …
- **`MAX_*`:** MAX_THINKING_TOKENS, MAX_MCP_OUTPUT_TOKENS.
- **`MCP_*`:** MCP_TIMEOUT, MCP_TOOL_TIMEOUT, MCP_CONNECT_TIMEOUT_MS, MCP_CLIENT_SECRET, MCP_OAUTH_CALLBACK_PORT, MCP_SERVER_CONNECTION_BATCH_SIZE, …
- **Bash:** BASH_DEFAULT_TIMEOUT_MS, BASH_MAX_TIMEOUT_MS, BASH_MAX_OUTPUT_LENGTH.
- **Other `CLAUDE_*`:** CLAUDE_CONFIG_DIR, CLAUDE_PROJECT_DIR, CLAUDE_PLUGIN_ROOT, CLAUDE_PLUGIN_DATA, CLAUDE_PLUGIN_OPTION_*, CLAUDE_ENV_FILE, CLAUDE_SKILL_DIR, CLAUDE_SESSION_ID, CLAUDE_AX_SCREEN_READER, CLAUDE_AUTOCOMPACT_PCT_OVERRIDE, CLAUDE_BASH_MAINTAIN_PROJECT_WORKING_DIR, CLAUDE_STREAM_IDLE_TIMEOUT_MS, CLAUDE_BG_*, CLAUDE_RUNNER_*, CLAUDE_PID (exported to hooks), …
- **Misc:** SLASH_COMMAND_TOOL_CHAR_BUDGET, TASK_MAX_OUTPUT_LENGTH, AWS_BEARER_TOKEN_BEDROCK, VERTEX_REGION_CLAUDE_*, USE_BUILTIN_RIPGREP, FORCE_AUTOUPDATE_PLUGINS, IS_DEMO, IS_SANDBOX, HTTP(S)_PROXY, NO_PROXY, NODE_EXTRA_CA_CERTS, NO_COLOR, FORCE_COLOR, EDITOR, VISUAL, BAT_THEME, OTEL_* (full OTLP exporter set plus OTEL_LOG_USER_PROMPTS / TOOL_DETAILS / TOOL_CONTENT / ASSISTANT_RESPONSES, OTEL_METRICS_INCLUDE_*).

---

## 6. Modes, hooks, tools (bin)

**Permission modes**
- `default` (shown as "Manual", ⏸, alias `manual`), `acceptEdits` (⏵⏵), `plan` (⏸), `auto` (⏵⏵), `bypassPermissions` (⏵⏵), `dontAsk`.
- `bubble` is internal (subagents defer to the parent).
- **Shift+Tab cycle:** default → acceptEdits → plan → (bypassPermissions if enabled, else auto if available, else default). From bypassPermissions: auto if available, else default. dontAsk → default.

**Hook events (33)**
- Tool: PreToolUse (before tool execution), PostToolUse (after), PostToolUseFailure (after a tool fails), PostToolBatch (after a batch of tool calls resolves).
- Permission: PermissionRequest (a permission dialog is displayed), PermissionDenied (auto-mode classifier denied a call).
- Prompt and turn: UserPromptSubmit, UserPromptExpansion (a typed slash command expands into a prompt), Stop (right before Claude concludes), StopFailure (turn ends on API error), Notification, MessageDisplay (while assistant text is displayed).
- Session: SessionStart, SessionEnd, Setup (repo setup hooks for init and maintenance).
- Compaction and model: PreCompact / PostCompact, PreModelSwitch (before a requested switch), PostModelSwitch (after the model changes for any reason).
- Agent and task: SubagentStart / SubagentStop, TeammateIdle, TaskCreated / TaskCompleted.
- MCP: Elicitation, ElicitationResult.
- Config and environment: ConfigChange, InstructionsLoaded, CwdChanged, FileChanged, DirectoryAdded, WorktreeCreate / WorktreeRemove.

Hook and status-line input JSON includes session_id, transcript_path, cwd and permission_mode.

**Built-in tools (current names)**
- Shell and files: Bash, PowerShell, Read, Write, Edit, Glob, Grep, NotebookEdit, LSP.
- Agents and messaging: Agent (alias Task), SendMessage, ListAgents (alias ListPeers), SubagentHandback.
- Web: WebFetch, WebSearch.
- Tasks and todos: TodoWrite, TaskCreate, TaskUpdate, TaskGet, TaskList, TaskStop (aliases KillShell, KillBash), GetTask, Monitor.
- Plan and worktrees: EnterPlanMode, ExitPlanMode, EnterWorktree, ExitWorktree.
- User interaction: AskUserQuestion, Skill, ToolSearch, SendUserMessage (alias Brief), SendUserFile, SendFile, PushNotification.
- Scheduling: CronCreate, CronDelete, CronList, ScheduleWakeup, RemoteTrigger.
- Other: Workflow, StructuredOutput, ReportFindings, ProposeGoal, EndConversation, Poll, ReadNotifications, FetchInboxMessage.
- MCP: ListMcpResourcesTool, ReadMcpResourceTool, ReadMcpResourceDirTool.
- Product-specific: Artifact, AppifactRepl, Projects, ClaudeDesign, DesignSync, SuggestSkills, SuggestConnectors, SearchMcpRegistry, ListConnectors, SuggestPluginInstall, SendFeedback, ShareOnboardingGuide, ShowOnboardingRolePicker, REPL, memory_list/read/write.
- Legacy names: MultiEdit, NotebookRead, LS, TaskOutput, BashOutput.

**Built-in agent types:** claude, general-purpose, Explore, Plan, statusline-setup (also fork, teammate, workflow-subagent).
**Output styles:** Default, Explanatory, Learning, Concise, Proactive.
**Model aliases:** sonnet, opus, haiku, fable, best, `sonnet[1m]`, `opus[1m]`, `fable[1m]`, opusplan, default.

---

## 7. `~/.claude` layout (disk)

- `settings.json` (user settings); optional status line script referenced by `statusLine.command`.
- `history.jsonl`: prompt history for ↑ and ctrl+r. Fields: `{display, pastedContents, timestamp, project, sessionId}`.
- `paste-cache/<hash>.txt`: large pastes, referenced as `[Pasted text #N +L lines]`.
- `projects/<cwd with non-alphanumerics replaced by ->/`:
  - `<sessionId>.jsonl`: the transcript.
  - `<sessionId>/subagents/agent-<id>.jsonl` plus `.meta.json` `{agentType, description, toolUseId, spawnDepth, requestShape}`.
  - `<sessionId>/tool-results/*.txt`: large tool outputs.
  - `memory/*.md`: auto-memory.
- `file-history/<sessionId>/<hash>@vN`: pre-edit file backups used by `/rewind`.
- `plans/<slug>.md`: plan-mode plans with random slugs.
- `sessions/<pid>.json`: live-session registry `{pid, sessionId, cwd, version, kind, entrypoint, messagingSocketPath, name, status…}`, plus `.key` files.
- `session-env/<sessionId>/`: per-session env directory (hooks / `CLAUDE_ENV_FILE`).
- `shell-snapshots/snapshot-zsh-*.sh`: user shell snapshot for the Bash tool.
- `plugins/`: `installed_plugins.json` (v2), `known_marketplaces.json`, `marketplaces/<name>/` (git clones), `cache/<mkt>/<plugin>/<ver>/`, `data/<id>/`, `store/`, `synced/`, `plugin-directory-cache-v2.json`.
- `skills/synced/`: claude.ai-synced skills.
- `debug/<sessionId>.txt` and a `latest` symlink.
- `backups/.claude.json.backup.*`.
- `cache/changelog.md` (release notes source), `cache/model-catalog/`.
- `chrome/chrome-native-host`, `dev-mods/`, `feedback/drafts/`, `state/mcp-discover-verdicts.json`, `telemetry/`, `downloads/`.
- Dotfiles: `.last-cleanup`, `.last-update-result.json`.
- Optional (created on use): `keybindings.json`, `todos/`, `agents/`, `commands/`, `output-styles/`, `themes/`, `CLAUDE.md`.

### Session JSONL records (important for /resume)

- **Message records:** `user`, `assistant`, `attachment`, `system`.
  - Shared envelope: `parentUuid` (linked list / tree; the resume leaf comes from `last-prompt.leafUuid`), `uuid`, `isSidechain`, `timestamp`, `sessionId`, `cwd`, `gitBranch`, `version`, `entrypoint` (cli / sdk-cli), `userType`, `slug`.
  - `assistant`: `message` is the raw API message (id, model, content blocks `text`/`thinking`/`tool_use`, `stop_reason`, `usage`), plus `requestId`, `effort`, `isApiErrorMessage`.
  - `user`: `message.content` is a string or blocks (`text`, `tool_result`), plus `toolUseResult` (structured, e.g. `{stdout, stderr, interrupted, isImage}`), `sourceToolAssistantUUID`, `permissionMode`, `promptId`, `promptSource`, `isMeta`.
  - `attachment`: `attachment.type` ∈ environment, model, deferred_tools_delta, agent_listing_delta, mcp_instructions_delta, skill_listing, auto_mode, plan_mode, plan_mode_exit, instructions, session_context, date, total_tokens_reminder, edited_text_file, queued_command, command_permissions, prompt_snapshot, remote_session_change, todo_reminder, task_status, nested_memory, diagnostics, mcp_resource, invoked_skills, hook_success, … Each carries `rendered:[{content:"<system-reminder>…"}]`.
  - `system`: `subtype` ∈ turn_duration, api_error, away_summary, local_command, informational, compact_boundary, model_refusal_*, …
- **Metadata records:** `last-prompt` `{lastPrompt, leafUuid}`, `ai-title` `{aiTitle}`, `cost-state` (totalCostUSD, durations, lines added/removed, `modelUsage`), `mode` `{mode}`, `permission-mode`, `queue-operation` `{operation: enqueue…, content}`, `file-history-snapshot` `{messageId, snapshot{trackedFileBackups}}`, `file-history-delta`, `atis-latch`.

---

## 8. Interactive UI features (bin)

- **Prompt input**
  - `/` commands with autocomplete; `@` file paths (custom source via `fileSuggestion`, respects .gitignore).
  - `!` shell mode (border color `bashBorder`; `respondToBashCommands`).
  - Double-tap esc clears the input; ctrl+_ undoes.
  - Newline via `\`+⏎, ctrl+j, Shift+Enter or Option+Enter (`/terminal-setup` installs these).
  - ctrl+g opens `$EDITOR`; ctrl+s stashes the prompt.
  - `:emoji:` completion, spellcheck underline, prompt suggestions.
  - Pastes collapse to `[Pasted text #N +L lines]`; images to `[Image #N]` (ctrl+v paste, drag-and-drop).
  - `ultrathink` keyword highlighting; `ultracode` triggers the Workflow tool (meta+w toggles it).
- **Queueing:** Enter while Claude works queues a message ("Press up to edit queued messages, Enter to send them immediately"); ctrl+enter sends now to steer mid-turn.
- **History:** ↑/↓ recall; ctrl+r search with ctrl+s to change scope.
- **Transcript:** ctrl+o "Showing detailed transcript" with less-style keys; ctrl+e shows all.
- **Views:** focus view (`/focus`), brief mode (ctrl+shift+b), fullscreen renderer (`/tui`: alt screen, mouse, selection auto-copy, virtual scroll).
- **Rewind:** double-tap esc opens the message selector. Options: "Restore code and conversation", "Restore code", "Restore conversation", "Summarize from here", "Summarize up to here". Backed by file-history checkpoints.
- **Modes:** shift+tab cycling with a footer indicator ("plan mode on", "accept edits on"); thinking toggle (meta+t) with "Thought for Ns"; effort slider and model picker (meta+p, ←/→ effort, `s` this-session-only); fast mode (meta+o).
- **Permission dialogs:** yes; "Yes, and don't ask again for …"; "No, and tell Claude what to do differently"; "Tab to amend"; esc cancels; shift+tab changes mode from inside the dialog.
- **Tasks:** todo panel (ctrl+t, `todoFeatureEnabled`); background tasks (ctrl+b, `/tasks`, `/background`); agent view (`claude agents`); kill agents (ctrl+x ctrl+k).
- **Status and footer:** status line command (stdin JSON includes session_name, model, workspace, version, output_style, cost, context_window, exceeds_200k_tokens, fast_mode, effort, thinking, rate_limits, vim.mode, agent, pr, worktree; `refreshInterval`); footer PR / link badges.
- **Feedback during work:** spinner with ~189 verbs (customizable) and tips (40+ IDs such as prompt-queue, double-esc-code-restore, shift-tab, image-paste, …); "Cooked for Nm Ns" turn duration; message timestamps.
- **Startup and notifications:** welcome screen with the Clawd mascot, What's new / release notes, company announcements; OS notifications (iTerm2, Kitty, Ghostty, bell), push to mobile, OSC 9;4 progress bar, terminal title from `/rename` (`CLAUDE_CODE_DISABLE_TERMINAL_TITLE`).
- **Exit:** "Press Ctrl-C again to exit"; ctrl+z suspends.
- **Context and cost:** auto-compact with "Compacting conversation"; `/context` grid; `/usage` cost and plan limits; usage-limit dialogs (`autoContinueAtUsageLimit`); away recap after 5+ minutes away.
- **Theming:** 6 themes plus custom (ctrl+e in the picker), syntax-highlight toggle, session prompt-bar colors (`/color`), reduced motion, screen-reader mode.
- **IDE and others:** VS Code, Cursor, Windsurf, JetBrains family, Zed and others (`ide_selection`, `opened_file_in_ide`, `--ide`, auto-connect); Claude in Chrome; Remote Control; voice push-to-talk (hold space); mobile QR.
- **Clipboard (bin):** uses `pbcopy` and `osascript … «class PNGf»` for image paste on macOS.

---

## 9. What could not be determined

- **No sign of:** the `#` memory shortcut and the `&` background prefix (likely removed; backgrounding is now `/background` and ctrl+b), and a settings JSON-schema URL.
- **Flag-gated:** several commands and bindings are compiled to `...{}` or behind GrowthBook flags (e.g. app:toggleTerminal meta+j, the MessageSelector bindings, `/loops`), so liveness can't be decided statically.
- **Dynamic text:** some descriptions are getters (`/ultrareview`, `/terminal-setup`, `/sandbox`), so exact text depends on runtime state.
- **Partial lists:** the vim motion set and the full attachment-type list are only partly enumerated.
- **Not enumerated:** exact text of every spinner tip and the full sandbox sub-schema.
