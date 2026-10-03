package ext

import tea "charm.land/bubbletea/v2"

// Additions in contracts-v1.2.

// CommandsMsg replaces the runtime slash commands from one source, e.g. the engine's
// command list from initialize or commands_changed (Source SourceEngine). Built-in and
// mod commands with the same name win over runtime ones. Commands with a nil Run are
// listed in menus; the prompt pipeline decides how to send them.
type CommandsMsg struct {
	Source   string
	EngineID string
	Commands []Command
}

// TranscriptHistoryMsg hands finished items from an earlier session (plan 06's JSONL
// reader) to the transcript store of EngineID. With Reset the store is emptied first
// (session switch, rewind, return from a hand-off). Items with a ParentID nest under
// that item. The sender follows it with Ctx.Reprint() when the screen must be
// redrawn; otherwise the commit policy prints the new items as usual.
type TranscriptHistoryMsg struct {
	EngineID string
	Items    []*Item
	Reset    bool
}

// ThemePreviewMsg applies a theme temporarily (the /theme picker's live preview).
// Name "" ends the preview and restores the theme from settings. Nothing is persisted;
// the picker writes the "theme" setting on confirm. SyntaxHighlight, when set,
// previews syntaxHighlightingDisabled = !*SyntaxHighlight.
type ThemePreviewMsg struct {
	Name            string
	SyntaxHighlight *bool
}

// SpawnGateMsg is sent by whoever is about to spawn an engine (the host for "main",
// plan 10 for builders) instead of spawning, when a subscriber for it exists. The
// subscriber (the startup gates: trust, bypass warning, .mcp.json approval, API key)
// calls exactly one of Proceed or Abort. With no subscriber the engine spawns directly.
type SpawnGateMsg struct {
	EngineID string
	Opts     SpawnOpts                   // as built from the command line
	Proceed  func(SpawnOpts) tea.Cmd     // spawn with these (possibly amended) options
	Abort    func(reason string) tea.Cmd // don't spawn; for "main" the host exits
}

// QueuedPromptsMsg replaces the list of prompts sent while a turn runs that the engine
// has not started yet. The input feature sends it on every change.
type QueuedPromptsMsg struct {
	EngineID string
	Prompts  []QueuedPrompt
}

// QueuedPrompt is one queued prompt.
type QueuedPrompt struct {
	UUID, Text, Priority string
}

// EngineStartMsg asks the host to start (or restart) engine EngineID with Opts.
// EngineAttachMsg follows on success, EngineExitedMsg{Err} on failure.
type EngineStartMsg struct {
	EngineID string
	Opts     SpawnOpts
}

// EngineStopMsg asks the host to stop engine EngineID and forget it
// (EngineExitedMsg, then EngineDetachMsg follow).
type EngineStopMsg struct{ EngineID string }

// Contexts and actions found in the 2.1.288 binary after contracts-v1 (drift check).
const (
	ContextProactivityMenu = "ProactivityMenu"
	ContextTerminal        = "Terminal"

	ActStripJump1                  ActionID = "strip:jump1"
	ActStripJump2                  ActionID = "strip:jump2"
	ActStripJump3                  ActionID = "strip:jump3"
	ActStripJump4                  ActionID = "strip:jump4"
	ActStripJump5                  ActionID = "strip:jump5"
	ActStripJump6                  ActionID = "strip:jump6"
	ActStripJump7                  ActionID = "strip:jump7"
	ActStripJump8                  ActionID = "strip:jump8"
	ActStripJump9                  ActionID = "strip:jump9"
	ActStripNext                   ActionID = "strip:next"
	ActStripPrevious               ActionID = "strip:previous"
	ActStripToggle                 ActionID = "strip:toggle"
	ActStripNew                    ActionID = "strip:new"
	ActProactivityMenuPreviousMode ActionID = "proactivityMenu:previousMode"
	ActProactivityMenuNextMode     ActionID = "proactivityMenu:nextMode"
	ActPermissionToggleDebug       ActionID = "permission:toggleDebug"
)

func init() {
	Contexts = append(Contexts, ContextProactivityMenu, ContextTerminal)
	ClaudeActions = append(ClaudeActions,
		ActStripJump1, ActStripJump2, ActStripJump3, ActStripJump4, ActStripJump5,
		ActStripJump6, ActStripJump7, ActStripJump8, ActStripJump9,
		ActStripNext, ActStripPrevious, ActStripToggle, ActStripNew,
		ActProactivityMenuPreviousMode, ActProactivityMenuNextMode, ActPermissionToggleDebug)
	// app:help appears in 2.1.288 only inside an error message, not as a real action;
	// keep the constant (additive-only) but don't accept it in keybindings.json.
	for i, a := range ClaudeActions {
		if a == ActAppHelp {
			ClaudeActions = append(ClaudeActions[:i], ClaudeActions[i+1:]...)
			break
		}
	}
}

// Claude Code settings scopes, as the protocol and settings files name them.
const (
	ScopePolicy  = "policySettings"
	ScopeFlag    = "flagSettings"
	ScopeLocal   = "localSettings"
	ScopeProject = "projectSettings"
	ScopeUser    = "userSettings"
)

// SettingsScopes lists the scopes from highest to lowest precedence.
var SettingsScopes = []string{ScopePolicy, ScopeFlag, ScopeLocal, ScopeProject, ScopeUser}

// SettingsSource is one Claude Code settings file and how reading it went.
type SettingsSource struct {
	Scope  string
	Path   string // "" for flag settings passed inline
	Exists bool
	Err    error // parse or read error; the scope is then ignored, as Claude Code does
}

// ScopedSettings adds per-scope access to Settings. The host's Settings and
// exttest.Settings implement it; get it with ctx.Settings().(ext.ScopedSettings).
// (A separate interface, so test doubles written against contracts-v1 keep compiling.)
type ScopedSettings interface {
	Settings
	// ClaudeScope returns one scope's raw settings (nil when absent or invalid).
	ClaudeScope(scope string) map[string]any
	// ClaudeSources lists every scope file in precedence order.
	ClaudeSources() []SettingsSource
}
