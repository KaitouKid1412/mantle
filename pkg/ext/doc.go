// Package ext is mantle's extension API. Built-in features and user mods register
// through exactly this surface ("built-ins are mods too").
//
// # Registration
//
// A feature's init() calls [Register] with a [Feature] descriptor; nothing else happens
// at init time. The host then runs every Setup in a fixed order (core, then built-ins by
// Order/After, then mods with Order >= 1000) and resolves the declarative overrides
// (Replace, Wrap, Remove, Alias) after every Setup has run, so registration order never
// decides a winner. When two features override the same ID, the higher Order wins and
// the conflict is reported in `mantle-ui catalog`.
//
// # IDs
//
//   - features: "<area>.<name>" for built-ins ("chrome.footer"), "mod.<id>" for mods
//   - commands: "cmd.<name>" (see [CommandID])
//   - renderers: "render.<ContentKey>" (see [RendererID])
//   - dialogs: "dialog.<name>"
//   - components: "<feature>.<component>"
//   - actions: Claude Code's action IDs verbatim ("chat:submit"); mantle-only "mantle:*"
//
// IDs are stable: rename with [Registrar.Alias], never by editing callers.
//
// # Messages
//
// tea.Msg is the bus. Use [Subscribe] for typed delivery to a function, and the
// component Update method for messages a component cares about. Every engine message
// carries an EngineID.
//
// # Threading
//
// [Ctx] is valid only on the UI goroutine (Update, View, Run, Setup callbacks). Cmds run
// on other goroutines: they must not capture Ctx or touch component state; they only
// return messages.
//
// # Stability
//
// Additive-only after the contracts-v1 tag: no renames, removals or signature changes.
// Interfaces that only mantle implements ([Registrar], [Ctx], [Settings], [KV], [Clock])
// may gain methods; interfaces features implement ([Component], [Focusable], [Dialog])
// never do. New optional behaviour arrives as new optional interfaces
// ([ActionHandler], [ContextStack], [FocusAware], [Resulter], …). [APIVersion] is
// bumped only for a deliberate break.
//
// Primary owner: plan 01 (docs/plans/01-contracts-shell.md).
package ext
