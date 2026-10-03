package ext

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Command sources.
const (
	SourceBuiltin = "builtin" // implemented natively by mantle
	SourceEngine  = "engine"  // passed through to the engine (skills, custom commands, MCP prompts)
	SourceMod     = "mod"     // added by a user mod
)

// CommandFunc runs a slash command. args is everything after the name, trimmed.
type CommandFunc func(ctx Ctx, args string) tea.Cmd

// Command is a slash command. Its override ID is ID, which defaults to CommandID(Name).
type Command struct {
	ID, Name, Description, ArgHint string
	Aliases                        []string
	Hidden                         bool
	Source                         string // "builtin" | "engine" | "mod"
	Run                            CommandFunc
	Complete                       func(Ctx, string) []Completion
}

// Completion is one argument completion for a command.
type Completion struct {
	Value       string // inserted text
	Display     string // shown in the menu ("" = Value)
	Description string
}

// ActionID is a keymap action: Claude Code IDs verbatim ("chat:submit"); mantle-only
// actions use the "mantle:" prefix.
type ActionID string

// IsMantle reports whether a is a mantle-only action.
func (a ActionID) IsMantle() bool { return strings.HasPrefix(string(a), MantleActionPrefix) }

// ActionFunc runs an action. Returning handled=false lets the key fall through to the
// focused component (history:previous declines while the cursor is not on the first
// line, so up arrow moves the cursor).
type ActionFunc func(Ctx) (handled bool, cmd tea.Cmd)

// Action is a named behaviour bound to keys. Context is where it is documented to
// apply; the keymap decides where it fires.
type Action struct {
	ID                   ActionID
	Context, Description string
	Run                  ActionFunc
}

// Binding is a default key binding. User keybindings.json files win over defaults.
type Binding struct {
	Context, Keys string // Keys: "ctrl+x ctrl+k"
	Action        ActionID
}
