# Extending mantle

> **2026-10-07:** `/mantle` and the `mods/` directory were removed pending a redesign.
> The `pkg/ext` guidance below still applies to built-in features; the parts about the
> builder, mods and the pipeline are out of date.

This guide is for anyone who changes mantle: people, and the `/mantle` builder agent.
Built-in features and user mods register the same way, through `pkg/ext`. A mod can
add anything a built-in can, and can Replace, Wrap, Remove or Alias any built-in by ID
without editing core files.

## 1. Mods: where code goes

- One mod is one package: `mods/<mod-id>/`, package name = the id without dashes
  (`mods/spinner-blue/` is `package spinnerblue`).
- Link it into mantle-ui with one file, `mods/link_<mod-id>.go`:

  ```go
  package mods

  import _ "github.com/KaitouKid1412/mantle/mods/spinner-blue"
  ```

- Import rules (enforced by `internal/archtest`): a mod imports only `pkg/...` from this
  module, the standard library and modules already in `go.mod`. Tests may also use
  `internal/testkit`. Features never import other features or mods.
- `/mantle` commits a mod as one unit: files under `mods/` get `Mantle-Kind: mod`, and
  any file outside `mods/` becomes a separate `core-seam` commit with the same
  `Mantle-Mod` id. Edit core only to add a missing seam, and keep it minimal.

## 2. The Feature lifecycle

```go
package spinnerblue

import "github.com/KaitouKid1412/mantle/pkg/ext"

func init() {
	ext.Register(ext.Feature{
		ID:    "mod.spinner-blue",
		Order: ext.ModOrder, // mods use 1000 and up; built-ins use 0-999
		Setup: setup,
	})
}

func setup(r ext.Registrar) error {
	// register commands, components, renderers, overrides, stories, ...
	return nil
}
```

- `init()` only records the descriptor. The host runs every `Setup` in order: core,
  then built-ins (by `Order`, then `After`), then mods.
- Overrides (Replace, Wrap, Remove, Alias) are declarative: they are resolved after
  every Setup has run, so registration order never decides a winner. When two features
  override the same ID, the higher `Order` wins and `mantle-ui catalog` reports the
  conflict.
- `mantle --safe` starts mantle without any feature with `Order >= ext.ModOrder`.
- A panicking component, command or Cmd is disabled with a notice and remembered in
  `~/.mantle/state/disabled.json`; mantle keeps running.

## 3. Finding IDs

`go run ./cmd/mantle-ui catalog --json` lists every registered ID with its kind
(feature, command, action, binding, renderer, component, dialog, theme, setting,
promptStage, interceptor, story, subscriber, start), feature, file and parity tags.
`--kind story` filters. Entries with `"removed": true` were overridden.

ID conventions:

| What | ID |
|---|---|
| feature | `<area>.<name>` (`chrome.footer`), mods `mod.<id>` |
| slash command | `ext.CommandID("model")` = `cmd.model` |
| renderer | `ext.RendererID(key)` = `render.tool.Bash` |
| dialog | `dialog.<name>` |
| component | `<feature>.<component>` |
| action | Claude Code's IDs verbatim (`chat:submit`); mantle-only `mantle:*` |

## 4. Overriding built-ins

```go
// Replace: same kind as the target.
r.Replace(ext.CommandID("clear"), ext.Command{Name: "clear", Run: myClear})
// Wrap: func(next T) T, for CommandFunc, ActionFunc, Renderer, Component,
// DialogFactory or PromptStage.
r.Wrap("chrome.footer", func(next ext.Component) ext.Component { return &bordered{next} })
r.Wrap(ext.RendererID("tool.Bash"), func(next ext.Renderer) ext.Renderer {
	return func(rc ext.RenderCtx, it *ext.Item) ext.Block { return next(rc, it) }
})
r.Remove("chrome.todos")            // drop a built-in
r.Alias("mod.old-name", "mod.new") // keep an old ID working after a rename
```

## 5. What you can register

- **Components and slots.** `r.AddComponent(slot, c, ext.SlotOpts{Weight, MaxHeight, Modes})`.
  Inline slots, top to bottom: `live`, `status`, `aboveInput`, `input`, `belowInput`,
  `statusLine`; fullscreen adds `header`, `sidebarLeft`, `sidebarRight` (use
  `Modes: []ext.LayoutMode{ext.Fullscreen}`). A component implements `ID`, `Init`,
  `Update` and `View(ctx, area) ext.Rendered`; output is cached until you call
  `ctx.Invalidate(id)`. Keep `Update` cheap. Keyboard input goes to `ext.Focusable`.
- **Renderers and ContentKeys.** A renderer is a pure function
  `func(ext.RenderCtx, *ext.Item) ext.Block` returning lines at most `rc.Width` wide.
  Keys: `user.prompt`, `assistant.text`, `assistant.thinking`, `tool.<Name>`,
  `tool.mcp.<server>.<tool>` (`ext.MCPToolKey`), `system.<subtype>`. Resolution: exact
  key, then the longest wildcard (`tool.mcp.*`), then `default`.
- **Commands.** `r.AddCommand(ext.Command{Name, Description, ArgHint, Source: ext.SourceMod, Run})`.
- **Actions and bindings.** `r.AddAction(ext.Action{ID: "mantle:my-action", Context, Run})`
  and `r.AddBinding(ext.Binding{Context: ext.ContextChat, Keys: "ctrl+x ctrl+k", Action: ...})`.
  Contexts are Claude Code's (`Global`, `Chat`, `Confirmation`, `Select`, ...). The
  user's `keybindings.json` always wins over defaults; reserved keys (ctrl+c, ctrl+d,
  ...) cannot be bound.
- **Dialogs.** `r.AddDialog("dialog.mine", factory)`, then `ctx.OpenDialog(id, args)`.
  A dialog is a Focusable with a `Placement` (inline over the input, centered, or an
  alt-screen view); implement `ext.Resulter` to hand a result back in
  `ext.DialogClosedMsg`.
- **Interceptors and prompt stages.** `r.AddInterceptor(id, priority, fn)` sees every
  `tea.Msg` first (return nil to consume it). `r.AddPromptStage(id, priority, fn)` is a
  step of the submit pipeline (`Continue`, `Consumed` or `Reject` a `*ext.Draft`).
- **Messages.** `ext.Subscribe(r, id, func(ctx ext.Ctx, m T) tea.Cmd)` delivers only
  messages of type `T` (engine events are `ext.EngineEventMsg`, carrying `EngineID`).
- **Settings and state.** `r.AddSetting(ext.SettingSpec{Key: "mod.spinner-blue.color", Type: "string", Default: "blue"})`
  declares a mantle setting (shown in `/config`); read it with
  `ctx.Settings().Mantle(key)`. `ctx.Store(featureID)` is a small persistent KV in
  `~/.mantle/state/`.
- **Threading.** `ext.Ctx` is valid only on the UI goroutine. Cmds run elsewhere: they
  must not capture the Ctx or touch component state; they return messages.

## 6. Stories and tests (required)

Every visible thing gets an `ext.Story`, and everything gets a test.

```go
r.AddStory(ext.Story{
	ID: "mod.spinner-blue/default",
	Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered { return view(ctx, a.Width) },
})
```

- `go run ./cmd/mantle-ui story mod.spinner-blue/default --width 100` prints it;
  `story --list` lists IDs.
- `mantle-ui selftest` renders every story at 60, 100 and 160 columns: a panic or a
  line wider than the width fails the build.
- In tests, use `pkg/ext/exttest`: `exttest.Setup(feature)` runs a Setup into a
  recording Registrar; `exttest.NewCtx()` is a fake Ctx that records prints, notices
  and dialogs. `internal/testkit` runs real UI tests in the vt emulator.

## 7. What /mantle checks, and what is protected

The pipeline runs, in order: the protected-path check, `gofmt -l` on changed files,
`go vet ./...`, the import rules, `apidiff` on every `pkg/*` package (additive changes
only), `go build -trimpath ./cmd/mantle-ui`, `go test ./...`, `mantle-ui selftest`,
and a smoke boot in a pseudo-terminal against a scripted engine.

Protected paths (any change fails step 1): `cmd/mantle/`, `internal/launcher/`,
`internal/selfmod/`, `.claude/`, `.mcp.json`, and the `toolchain` line of `go.mod`.
No new dependencies: ask the user instead of running `go get`.

## 8. Worked examples

### Custom spinner verbs (config only, no code)

The spinner already honours Claude Code's `spinnerVerbs` setting, so the right answer
is a config change. The builder replies with:

    MANTLE_CONFIG_PROPOSAL
    {"summary": "Pirate spinner verbs", "scope": "claude", "key": "spinnerVerbs",
     "value": {"mode": "replace", "verbs": ["Plundering", "Swashbuckling"]}}
    END_MANTLE_CONFIG_PROPOSAL

and `/mantle apply <id>` writes it to the user's Claude Code settings. Nothing is
rebuilt.

### A renderer for one MCP tool

```go
package weatherview

import (
	"fmt"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

var key = ext.MCPToolKey("weather", "forecast") // mcp__weather__forecast

func init() {
	ext.Register(ext.Feature{ID: "mod.weather-view", Order: ext.ModOrder, Setup: func(r ext.Registrar) error {
		r.AddRenderer(key, render)
		r.AddStory(ext.Story{ID: "mod.weather-view/done", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
			b := render(ext.RenderCtx{Width: a.Width, Theme: ctx.Theme()}, sample())
			return ext.Rendered{Text: b.Lines[0]}
		}})
		return nil
	}})
}

func render(rc ext.RenderCtx, it *ext.Item) ext.Block {
	var in struct{ City string }
	if tu, ok := it.Data.(*proto.ToolUse); ok {
		tu.DecodeInput(&in)
	}
	line := fmt.Sprintf("☁ forecast for %s", in.City)
	if it.Result != nil {
		line += ": " + it.Result.Content.PlainText()
	}
	if len(line) > rc.Width { // keep within width (use ansi.Truncate for styled text)
		line = line[:rc.Width]
	}
	return ext.Block{Lines: []string{line}}
}

// sample is a finished call with fixed data, for the story and the test.
func sample() *ext.Item {
	return &ext.Item{
		ID: "toolu_1", Key: key, State: ext.Done,
		Data:   &proto.ToolUse{Type: "tool_use", ID: "toolu_1", Name: "mcp__weather__forecast", Input: []byte(`{"City":"Lisbon"}`)},
		Result: &proto.ToolResult{ToolUseID: "toolu_1", Content: proto.TextContent("sunny, 24°C")},
	}
}
```

### A sidebar pane (fullscreen)

In the fullscreen layout (`tui: "fullscreen"`) the host draws two extra slots beside
the transcript: `ext.SlotSidebarL` and `ext.SlotSidebarR`. A pane is an ordinary
component registered for `ext.Fullscreen` only, so inline mode never shows it. The host
gives it the full height between the header and the prompt, and delivers mouse events
under it as `ext.MouseEvent` (coordinates relative to the pane). This example, written
with plan 12, is tested in the real host's fullscreen layout
(`features/fullscreen/sidebar_example_test.go`).

```go
package notespane

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

func init() {
	ext.Register(ext.Feature{
		ID: "mod.notes-pane", Order: ext.ModOrder,
		Setup: func(r ext.Registrar) error {
			r.AddComponent(ext.SlotSidebarL, &pane{},
				ext.SlotOpts{Modes: []ext.LayoutMode{ext.Fullscreen}})
			return nil
		},
	})
}

type pane struct{ clicks int }

func (p *pane) ID() string           { return "mod.notes-pane.pane" }
func (p *pane) Init(ext.Ctx) tea.Cmd { return nil }

func (p *pane) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(ext.MouseEvent); ok {
		if _, click := m.Msg.(tea.MouseClickMsg); click {
			p.clicks++
			ctx.Invalidate(p.ID())
		}
	}
	return nil
}

func (p *pane) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	t := ctx.Theme()
	return ext.Rendered{Text: t.Paint(theme.Accent, "Notes") + "\n" +
		t.Paint(theme.Inactive, fmt.Sprintf("clicked %d times", p.clicks))}
}
```

- Width: the host decides. Draw within `Area.Width` and at most `Area.MaxHeight` lines.
- Wheel events also arrive as `scroll:*` actions for the transcript; a pane that
  scrolls should handle `ext.MouseEvent` wheel messages itself and ignore the actions.
- Test a pane like any component: a Story, plus `app.NewHost` and `testkit.New` with
  `app.Options{Layout: ext.Fullscreen}` for an end-to-end check.
- To show live data (the session's model, say), subscribe to `ext.SessionChangedMsg`
  and call `ctx.Invalidate(p.ID())`.

## 9. After you build

`/mantle` shows progress, runs the checks (up to three fix rounds), and installs the
result as a new version: active on the next launch, or now with `/mantle restart`
when mantle is idle. A build that crashes twice on probation rolls back to the last
good one by itself. `/mantle list`, `show`, `undo`, `edit`, `retry`, `rollback`,
`update` and `upstream` manage mods afterwards.
