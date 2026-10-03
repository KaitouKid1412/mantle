# Plan 01: Contracts & core shell

| Session | Primary paths | Part B needs | Produces |
|---|---|---|---|
| mantle-01 | `pkg/ext`, `pkg/theme`, `pkg/ui` (base widgets), `cmd/mantle-ui`, `internal/app`, `internal/keymap`, `internal/config`, `internal/testkit` (except `enginefake`), `internal/archtest` | nothing (Part B continues your own host) | tag `contracts-v1`; afterwards you are the **API steward** |

## Goal

Two jobs:
1. Publish the shared contracts every other session codes against: `pkg/ext` v1, `pkg/theme`
   tokens, the transcript `Item` model, the keymap ID table, the testkit and the arch test.
   Tag them `contracts-v1` **as early as possible**, because Part B of plans 02–11 waits for
   it.
2. Build the host that runs features: deterministic registration, override resolution,
   message routing, inline layout with a scrollback committer, a dialog stack, the keymap
   engine, config, themes and base widgets.

## Start prompt

> You are session 01 of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/01-contracts-shell.md`, then execute that plan. Do Part A now. Start Part B once
> `git tag -l` shows the tags it needs. Commit with prefix `[01]`.

## Read first

- `docs/plans/00-overview.md`: architecture, rules, the session table.
- `docs/research/design-review.md` §1.2 (ext API critique), §1.3 (Bubble Tea v2 inline
  behaviour), §2 (libraries), §5 (API sketch).
- `docs/research/inventory-binary-2.1.288.md` §3 (keybinding contexts, actions, defaults)
  and §4 (settings keys and scopes).
- `docs/research/inventory-docs.md` §8 (settings), §9 (keybindings).

## Facts you can rely on

- **Module and toolchain:** module `github.com/KaitouKid1412/mantle`, Go 1.27, pinned
  toolchain. The bootstrap already pins every dependency (`internal/deps/deps.go`); don't
  run `go mod tidy`.
- **Charm versions:** `charm.land/bubbletea/v2` v2.0.10, `charm.land/bubbles/v2` v2.2.1,
  `charm.land/lipgloss/v2` v2.0.6.
- **Test libraries:** `github.com/charmbracelet/x/vt` (pinned pseudo-version) and
  `github.com/charmbracelet/x/exp/golden` v0.1.0.
- **Bubble Tea v2 inline mode (verified in v2.0.10 source):**
  - `tea.Println` / `tea.Printf` are Cmds (no-ops in alt screen). `insertAbove` writes
    immediately: it is not tied to the frame ticker and not wrapped in synchronized output.
  - A print taller than terminal height minus live-frame height leaves a **ghost copy** of
    the live area in scrollback. The wrap estimate is approximate, so pre-wrap everything.
  - An inline frame taller than the terminal silently loses its **top** lines.
  - Every message runs `Update` then `View()`; the frame is flushed on a ~60 fps ticker.
    So `View` must be cheap.
  - The View struct carries `AltScreen`, `MouseMode`, `KeyboardEnhancements`, `WindowTitle`,
    `ProgressBar` (OSC 9;4), `Cursor`, `ReportFocus`, `OnMouse` and background/foreground
    colours. Synchronized output (2026) and grapheme widths (2027) are negotiated.
  - Bracketed paste is on by default (`PasteStartMsg`, `PasteMsg`, `PasteEndMsg`). Kitty key
    disambiguation is requested by default; `KeyboardEnhancementsMsg` reports support.
  - Suspend sends `SIGTSTP` to the whole process group (`syscall.Kill(0, SIGTSTP)`).
  - lipgloss v2 has `Canvas`, `Layer` and `Compositor` with `Hit(x,y)`, for overlays and
    mouse hit-testing.
- **Go package initialization** follows dependency order, then sorted import path. Never
  rely on `init()` order for semantics.
- **Claude Code keybindings** (`~/.claude/keybindings.json`):
  - Format: `{"$schema":…, "bindings":[{"context":"Chat","bindings":{"ctrl+e":"chat:externalEditor","ctrl+s":null}}]}`.
  - User bindings apply after the defaults; `null` unbinds.
  - Chords are space-separated with a 3 s timeout.
  - Modifiers: `ctrl`, `alt`/`opt`/`option`, `shift`, `meta` (= alt), `cmd`/`command`/`super`/`win`.
  - Reserved: ctrl+c, ctrl+d, ctrl+m, ctrl+i, ctrl+h, ctrl+[, `ctrl+\`; ctrl+z only warns.
  - Contexts: Global, Chat, Autocomplete, Confirmation, Settings, Tabs, Transcript,
    HistorySearch, Task, ThemePicker, Scroll, Help, Attachments, Footer, AbovePrompt,
    AbovePromptInput, AbovePromptSelect, Pane, PaneField, DiffDialog, DiffPanel,
    ModelPicker, EffortSlider, Select, Plugin, Agents, MessageSelector.
- **Settings scopes and precedence:** managed (policy) > flag (`--settings`) > local
  (`.claude/settings.local.json`) > project (`.claude/settings.json`) > user
  (`~/.claude/settings.json`). Permissions and hooks merge across scopes.
  - `-p` silently ignores invalid settings files, and the schema is strict. **Never** put
    mantle keys in Claude Code settings files. mantle's own settings live in
    `~/.mantle/settings.json`.
  - **Never write `~/.claude.json`.** It has no lockfile in 2.1.288.

## Part A: start immediately (goal: `contracts-v1`)

- [ ] **A1 `pkg/ext` v1.** Types and interfaces exactly as in the sketch below (`pkg/ext/*.go`),
  with doc comments. Keep implementations out of `pkg/ext`, except `Register`, `Subscribe`
  and small helpers.
- [ ] **A2 `pkg/theme`.**
  - Token names: semantic names, not palette names. Include text, dim/inactive, subtle,
    primary/claude accent, success, error, warning, permission, planMode, autoAccept,
    bashBorder, promptBorder, diffAdded, diffRemoved, diffAddedWord, diffRemovedWord,
    suggestion, remember, userMessageBg, selectionBg, spinner shimmer pair, and
    syntax-highlight token classes.
  - A `Theme` struct (name, isDark, token → `color.Color`, style helpers).
  - Placeholder built-in themes. Write our own palettes; don't copy Claude Code's.
- [ ] **A3 Transcript model in `pkg/ext`.** `Item`, `ContentKey`, `ItemState`, `ViewMode`,
  `RenderCtx`, `Block`, `Renderer`, `Transcript`, plus the naming convention below.
- [ ] **A4 Keymap ID table in `pkg/ext/keys.go`.**
  - Context constants and action ID constants, using Claude Code's IDs verbatim (`chat:submit`,
    `chat:cancel`, `chat:newline`, `chat:cycleMode`, `chat:modelPicker`, `chat:fastMode`,
    `chat:thinkingToggle`, `chat:externalEditor`, `chat:stash`, `chat:imagePaste`,
    `chat:killAgents`, `chat:queueSubmit`, `chat:sendNow`, `chat:undo`, `chat:clearInput`,
    `app:interrupt`, `app:exit`, `app:toggleTodos`, `app:toggleTranscript`,
    `app:toggleBrief`, `app:redraw`, `history:search`, `history:previous`, `history:next`,
    the `autocomplete:*`, `confirm:*`, `select:*`, `transcript:*`, `historySearch:*`,
    `task:background`, `footer:*`, `scroll:*`, `selection:*`, `modelPicker:*`,
    `effortSlider:*`, `settings:*`, `tabs:*`, `attachments:*`, `diff:*`, `plugin:*` and
    `messageSelector:*` families).
  - mantle-only actions use `mantle:*`.
  - Default chords live in a table here, so plans 03–07 register behaviour, not keys.
- [ ] **A5 Testkit API (`internal/testkit`).**
  - `Harness`: start a `tea.Program` on an `x/vt` emulator (`WithInput`,
    `WithOutput(emu)`, `WithWindowSize`). Helpers: `Send(keys…)`, `Paste(s)`,
    `Resize(w,h)`, `Screen() string`, `Scrollback() []string`, `WaitFor(pred, timeout)`.
  - `golden.RequireEqual` wrappers that normalise trailing spaces.
  - `RunStory(t, story, widths)`.
  - Commit the API with an example test, even if internals are rough. Others depend on the
    signatures.
- [ ] **A6 `internal/archtest`.** A `go list -deps -json ./...` based test enforcing:
  - `features/X` must not import `features/Y` (X ≠ Y);
  - `mods/...` imports only `pkg/...`, the stdlib and allowed third-party modules;
  - `pkg/...` never imports `internal/...` or `features/...`.
- [ ] **A7 Freeze and tag.** `go build ./... && go vet ./...` must pass. Commit with `[01]`,
  run `git tag contracts-v1`, and add a line under "Milestones" in
  `docs/plans/00-overview.md` (small edit). After this, every `pkg/ext`/`pkg/theme` change
  is additive-only.

## Part B: continue immediately after A7

- [ ] **B1 [M1] Host (`internal/app/host.go`).**
  - Collect `ext.Feature` descriptors; topologically sort by `Order`, then `After`
    (core < 1000 ≤ mods); run `Setup`.
  - Resolve Replace/Wrap/Remove/Alias **after** every Setup has run. Conflicts produce a
    report, and the higher `Order` wins.
  - `safeCall` wraps every component, renderer, command and Cmd in `recover`. A panicking
    feature is disabled, a notice is shown, and the ID goes into
    `~/.mantle/state/disabled.json`.
- [ ] **B2 [M1] Root `tea.Model` (`internal/app/root.go`).**
  - Routing order: interceptors (priority), then the dialog stack (top), then the focused
    component, then keymap actions, then typed subscribers.
  - Slot render cache, invalidated by `Ctx.Invalidate`, resize, theme revision or settings
    revision.
  - Merge View fields: window title, progress bar, cursor from the focused component,
    keyboard enhancements.
- [ ] **B3 [M1] Inline layout and committer primitives (`internal/app/layout.go`, `commit.go`).**
  - Vertical stack of slots with a height budget (live area ≤ H−1 rows; ask each slot for
    `MaxHeight`).
  - `Print(blocks…)`: pre-wrapped, chunked to ≤ H − liveHeight − 1 lines per
    `tea.Println`, sent with `tea.Sequence`.
  - `Reprint()`: `ESC[2J ESC[3J ESC[H`, then reprint the whole store in chunks.
  - The commit *policy* (what is finished) belongs to plan 03. You provide the primitives
    and the watermark plumbing.
- [ ] **B4 [M1] Dialog stack (`internal/app/dialogs.go`).** Placement policy:
  `PlaceInline` replaces the input slot (like Claude Code permission prompts),
  `PlaceCentered` is a layer, `PlaceAltScreen` is a sub-view. `CloseDialog` and the result
  are delivered as messages.
- [ ] **B5 [M1] Keymap engine (`internal/keymap`).**
  - Parse both `~/.claude/keybindings.json` (Claude Code actions only) and
    `~/.mantle/keybindings.json` (`mantle:*`).
  - Context stack, chords with timeout, `null` unbind, reserved-key validation.
  - fsnotify hot reload; conflict report into `catalog`.
- [ ] **B6 [M1] Config (`internal/config`).**
  - Read all scopes with precedence, including managed paths
    (`/Library/Application Support/ClaudeCode/managed-settings.json` and the
    `managed-settings.d/`); read-only access to `~/.claude.json`; read `~/.mantle/settings.json`.
  - Writer: atomic temp file + rename under `flock`, re-read and merge just before writing,
    for `settings*.json` only.
  - fsnotify sends `ext.SettingsMsg{Changed}`.
  - Typed accessors for the UI keys mantle honours: theme, editorMode,
    vimInsertModeRemaps, verbose, viewMode, tui, statusLine, spinnerVerbs,
    spinnerTipsEnabled, spinnerTipsOverride, prefersReducedMotion, showTurnDuration,
    showMessageTimestamps, timeFormat, timeZone, maxProseWidth, syntaxHighlightingDisabled,
    terminalProgressBarEnabled, terminalTitleFromRename, preferredNotifChannel,
    respectGitignore, fileSuggestion, emojiCompletionEnabled, promptSuggestionEnabled,
    respondToBashCommands, autoScrollEnabled, axScreenReader, footerLinksRegexes,
    companyAnnouncements, todoFeatureEnabled.
- [ ] **B7 [M1] Theme engine (`internal/app/theme.go` + `pkg/theme`).**
  - Built-ins: dark, light, dark-daltonized, light-daltonized, dark-ansi, light-ansi.
  - Custom themes from `~/.claude/themes/*.json` (token overrides), hot reload.
  - `auto` via `tea.BackgroundColorMsg.IsDark()`; colour-profile downgrade.
- [ ] **B8 [M1] `pkg/ui` base widgets.** Select/list with `sahilm/fuzzy` filter, tabs, a
  dialog frame matching Claude Code's look, a single-line text field, scroll pane, table,
  key-hint bar, spinner primitive. Each widget implements `ext.Focusable`, has stories, and
  uses `Select`/`Tabs`/`Confirmation` contexts.
- [ ] **B9 [M1] `cmd/mantle-ui` subcommands.** `story <id> --width N`, `catalog --json`
  (every registered ID with kind, feature, file, parity tags), `selftest` (registry resolves;
  every story renders at 60/100/160 without panic and within width). Call `cli.Parse` (owned
  by plan 11) at a fixed call site.
- [ ] **B10 [M1] Notices and Clock.** Notice/toast API (`Ctx.Notify`, levels, timeout, key
  dedupe); injectable Clock (tests use `testing/synctest`).
- [ ] **B11 [M1] Spike S15.** Println chunking at 80×24, 120×40, 200×60 with live heights
  3–20. Flicker on commit-and-remove; resize reflow; tmux; shift+enter and ctrl+enter
  detection in iTerm2, Ghostty, Terminal.app, kitty, WezTerm, VS Code. Write up in
  "Facts verified" in this file.
- [ ] **B12 [M1] Spike S16 (with plan 10).** UI plus claude in a separate process group:
  ctrl+z and `fg`, `tea.ExecProcess($EDITOR)` with ctrl+c inside the editor, crash restore
  of termios.
- [ ] **B13 [M2] API steward duties.** Review `docs/plans/requests/*-01-*.md`, add additive
  API, keep `Alias` for renamed IDs, bump `ext.APIVersion` only for breaking changes (avoid
  them). Hold integration windows: full `go test ./...`, tag `integration-N`.
- [ ] **B14 [M3] Fullscreen layout hooks** (sidebar slots, Compositor layering) for plan 12.

## `pkg/ext` v1 sketch (implement this shape)

```go
// Package ext is mantle's extension API. Built-ins and user mods use exactly this surface.
// Additive-only after contracts-v1; IDs are stable (use Alias for renames).
package ext

import (
	"encoding/json"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

const APIVersion = 1

// Feature is the registration unit. init() calls Register (records only); the host runs
// Setup in order core < built-ins (Order/After) < mods, then resolves overrides.
type Feature struct {
	ID     string   // "chrome.footer", "mod.pirate-spinner"
	Order  int      // built-ins 0-999, mods >= 1000; higher wins override conflicts
	After  []string // features that must Setup first
	Parity []string // PARITY.md IDs covered
	Setup  func(Registrar) error
}

func Register(f Feature) { pending = append(pending, f) }
func Pending() []Feature { return append([]Feature(nil), pending...) } // host only

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
	Replace(id string, with any) // resolved after all Setup; same kind as target
	Wrap(id string, w any)       // func(next T) T for T in {CommandFunc, Renderer, ActionFunc}
	Remove(id string)
	Alias(oldID, newID string)
	SubscribeRaw(id string, sample tea.Msg, fn func(Ctx, tea.Msg) tea.Cmd)
}

// Subscribe delivers only messages of type T (host dispatches by type).
func Subscribe[T tea.Msg](r Registrar, id string, fn func(Ctx, T) tea.Cmd) {
	var zero T
	r.SubscribeRaw(id, zero, func(c Ctx, m tea.Msg) tea.Cmd { return fn(c, m.(T)) })
}

// Ctx is valid only on the UI goroutine (Update/View/Run). Cmds must not capture it.
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
	Reprint() tea.Cmd
	Notify(Notice) tea.Cmd
	OpenDialog(id string, args any) tea.Cmd
	CloseDialog(id string) tea.Cmd
	Run(ActionID) tea.Cmd
	Store(featureID string) KV // ~/.mantle/state/<featureID>.json
}

type LayoutMode int

const (
	Inline LayoutMode = iota
	Fullscreen
	AltView
)

type Slot string

const (
	SlotLive       Slot = "live"       // running / streaming transcript items
	SlotStatus     Slot = "status"     // spinner line
	SlotAboveInput Slot = "aboveInput" // todos, queued messages, subagent panel, notices
	SlotInput      Slot = "input"      // prompt editor (dialogs with PlaceInline replace it)
	SlotBelowInput Slot = "belowInput" // footer, mode indicator, hints
	SlotStatusLine Slot = "statusLine" // user statusLine command output
	SlotHeader     Slot = "header"     // fullscreen only
	SlotSidebarL   Slot = "sidebarLeft"
	SlotSidebarR   Slot = "sidebarRight"
)

type SlotOpts struct {
	Weight    int // order within slot (lower first)
	MaxHeight int // 0 = host decides
	Modes     []LayoutMode
}
type Area struct {
	Width, MaxHeight int
	Focused          bool
	Mode             LayoutMode
}
type Rendered struct {
	Text   string      // styled, pre-wrapped to Area.Width
	Cursor *tea.Cursor // relative to this block; host translates (IME, real cursor)
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

const (
	PlaceInline Placement = iota
	PlaceCentered
	PlaceAltScreen
)

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
type ContentKey string
type ItemState int

const (
	Streaming ItemState = iota
	Running
	Done
	Failed
	Interrupted
)

type Item struct {
	ID, ParentID string // ParentID = parent_tool_use_id
	EngineID     string
	Key          ContentKey
	Data         any // *proto.TextBlock | *proto.ThinkingBlock | *proto.ToolUse | *proto.SystemEvent | ...
	Result       *proto.ToolResult
	State        ItemState
	Rev          int
	Start, End   time.Time
}
type ViewMode int

const (
	Normal ViewMode = iota
	Verbose
	Brief
	Focus
	FullTranscript
)

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
	Interrupt(cancelQueued bool) tea.Cmd
	Control(subtype string, req any) tea.Cmd // -> ControlResultMsg
	Supports(subtype string) bool
	Restart(SpawnOpts) tea.Cmd // resume / fork / resume-at; stop+handoff via SpawnOpts
}
type Prompt struct {
	Blocks   []proto.ContentBlock
	Priority string // "now" | "next" | "later" ("" = engine default)
	UUID     string
	Composed bool // client_composed: no @/slash expansion
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
	Mode, Priority string // Mode: "prompt" | "bash" ; Priority as in Prompt
}
type Verdict int

const (
	Continue Verdict = iota
	Consumed
	Reject
)

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
// Also define: SessionInfo, Notice, Completion, Attachment, KV, Clock, SpawnOpts.
```

The sketch imports `pkg/proto`, which plan 02 writes. To avoid blocking:
- Create **minimal placeholder types** in `pkg/proto/placeholder.go` (`Event`,
  `ContentBlock`, `ToolResult`, `CanUseTool`, `PermissionResult`) if they don't exist when
  you reach A1.
  - Give them only fields whose names and JSON tags match the real protocol in
    `docs/research/protocol-2.1.288.md` (for example `ToolName string \`json:"tool_name"\``).
    Then 02's real types are a superset and nothing in `pkg/ext` breaks.
  - Put nothing else in that file.
- Note this in `docs/plans/requests/01-02-proto-placeholders.md`. Plan 02 replaces them
  with real types of the same names (an additive change).

## Design notes

- **ContentKey naming:** `<source>.<kind>[.<detail>]`.
  - Base keys: `user.prompt`, `user.bash`, `assistant.text`, `assistant.thinking`.
  - Tools: `tool.<ToolName>` (for example `tool.Bash`, `tool.Edit`, `tool.Agent`);
    `tool.mcp.<server>.<tool>`, with a wildcard renderer `tool.mcp.*`.
  - System events: `system.<subtype>` (`system.compact_boundary`, `system.api_retry`,
    `system.informational`, `system.hook`, `system.rate_limit`, `system.local_command`,
    `system.error`).
  - Resolution order: exact key, then the longest wildcard, then `default`.
- **Feature IDs:** `<area>.<name>` for built-ins (`chrome.footer`, `input.editor`); `mod.<id>`
  for mods. Command IDs are `cmd.<name>`; renderer IDs `render.<ContentKey>`; dialog IDs
  `dialog.<name>`; components `<feature>.<component>`.
- **Areas are packages, features are registrations.** One package per area
  (`features/input`); sub-packages only for heavy subsystems.
- **Pointer components internally;** the root satisfies `tea.Model`. All state changes
  happen on the Update goroutine; Cmds only return messages.
- **Several engines:** every engine message carries `EngineID`; dialogs show who is asking
  ("Builder mod-a3f wants to run…").
- **Mouse is off in inline mode.** Capturing the wheel breaks native scrollback.
- **Licensing hygiene:** no strings, palettes or schemas copied from the Claude Code binary
  or SDK. Write your own text and colours.

## Interfaces you provide / consume

- **Provide:** `pkg/ext` (everything above), `pkg/theme`, `pkg/ui` base widgets, testkit,
  archtest, host behaviours (`Print`, `Reprint`, dialog placement, keymap, config, notices),
  and the `cmd/mantle-ui` subcommands.
- **Consume:** `pkg/proto` types (plan 02; placeholders until `proto-v1`), `cli.Parse`
  (plan 11), commit policy (plan 03), engine implementation of `ext.Engine` (plan 02).
- **Requests:** watch `docs/plans/requests/*-01-*.md` and answer them by adding API
  additively.

## Tests and done criteria

- vt tests prove chunked `Print` leaves **no ghost lines** at 80×24, 120×40 and 200×60 with
  live heights 3–20, and that the scrollback text equals what was printed.
- Keymap tests: chords, timeout, `null` unbind, reserved keys rejected, context stacking,
  user-over-default precedence.
- Override tests: Replace/Wrap/Remove/Alias resolved after Setup; conflict report; Order
  wins.
- A panicking demo component is disabled with a notice, and the app keeps running.
- Config tests with a temp HOME cover precedence, atomic merge-write and fsnotify reload.
- `mantle-ui catalog --json` lists demo IDs; `selftest` passes; archtest passes.
- `go build ./... && go vet ./...` stay green at the end of every turn.

## Parity coverage

- `CF-*`: config compatibility (settings scopes, UI keys, keybindings.json, themes directory,
  hot reload).
- `KB-*`: keymap engine and defaults table.
- `TH-*`: themes engine; the `/theme` panel itself is 08.
- `CORE-*`: host, dialogs, notices, catalog/story/selftest.

## Out of scope

- Commit policy and renderers: 03. Engine and proto: 02. Editor: 04. Dialog contents: 05,
  08 and 09.
- Statusline and footer: 07. Fullscreen renderer: 12. Launcher: 10. CLI flags: 11.

## Parallel rules (reminder)

- No repo-wide rewriters (`go mod tidy`, `gofmt -w .`, `go generate ./...`,
  cross-directory `sed -i`); use `make fmt-01`.
- After `contracts-v1`, `pkg/ext` and `pkg/theme` are **additive-only**.
- Commit only your own files, named explicitly, with prefix `[01]`. Run scoped tests
  (`make test-01`). No cross-feature imports.
