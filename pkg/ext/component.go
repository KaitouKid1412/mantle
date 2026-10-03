package ext

import (
	tea "charm.land/bubbletea/v2"
)

// Slot is a fixed region of the layout. Inline mode stacks the slots vertically in the
// order below; fullscreen adds header and sidebars.
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

// InlineSlots is the inline stacking order, top to bottom.
var InlineSlots = []Slot{SlotLive, SlotStatus, SlotAboveInput, SlotInput, SlotBelowInput, SlotStatusLine}

// SlotOpts places a component within its slot.
type SlotOpts struct {
	Weight    int          // order within slot (lower first)
	MaxHeight int          // 0 = host decides
	Modes     []LayoutMode // nil = every mode
}

// Area is the space a component may draw into.
type Area struct {
	Width, MaxHeight int // MaxHeight 0 = unbounded (stories, scrollback)
	Focused          bool
	Mode             LayoutMode
}

// Rendered is a component's output.
type Rendered struct {
	Text   string      // styled, pre-wrapped to Area.Width; "" = takes no rows
	Cursor *tea.Cursor // relative to this block; host translates (IME, real cursor)
}

// Component is anything that draws into a slot (or is a dialog).
//
// Update receives every message the root does not consume (keys and pastes go to
// Focusable.HandleKey/HandlePaste instead), plus messages addressed to it with
// [Address]. Keep Update cheap: type-switch and return. Call Ctx.Invalidate(ID()) when
// your state changes so the host re-renders you; View output is cached until then (or
// until a resize, theme change or settings change).
type Component interface {
	ID() string
	Init(Ctx) tea.Cmd
	Update(Ctx, tea.Msg) tea.Cmd // broadcast + addressed messages; see above
	View(Ctx, Area) Rendered     // cached by host until Invalidate/resize/theme
}

// Focusable is a component that takes keyboard input.
//
// Key routing: the host resolves the key through the keymap for the active contexts
// (top dialog or focused component, then ambient contexts, then "Global"). A matched
// action is offered first to the focused component's HandleAction (if it implements
// [ActionHandler]), then to the registered Action. If nothing handles it, the raw key
// goes to HandleKey.
type Focusable interface {
	Component
	KeyContext() string // Claude Code context: "Chat", "Select", "Confirmation", ...
	HandleKey(Ctx, tea.KeyPressMsg) (handled bool, cmd tea.Cmd)
	HandlePaste(Ctx, tea.PasteMsg) (handled bool, cmd tea.Cmd)
}

// ActionHandler is an optional interface for Focusables that implement keymap actions
// themselves (a Select widget handles "select:next" for whichever instance is focused).
type ActionHandler interface {
	HandleAction(Ctx, ActionID) (handled bool, cmd tea.Cmd)
}

// ContextStack is an optional interface for Focusables whose active keybinding
// contexts change with state (the prompt with its autocomplete menu open is
// ["Autocomplete", "Chat"]). Most specific first; "Global" is implied.
type ContextStack interface {
	KeyContexts() []string
}

// FocusAware is an optional interface: the host calls OnFocus/OnBlur when focus moves.
type FocusAware interface {
	OnFocus(Ctx) tea.Cmd
	OnBlur(Ctx) tea.Cmd
}

// Placement is where the host shows a dialog.
type Placement int

const (
	PlaceInline    Placement = iota // replaces the input slot (permission prompts, pickers)
	PlaceCentered                   // a layer over the screen (fullscreen/alt views)
	PlaceAltScreen                  // a full-screen sub-view (ctrl+o, /diff)
)

// Dialog is a focusable component the host stacks above everything else.
type Dialog interface {
	Focusable
	Placement() Placement // host may override per layout mode
}

// DialogFactory builds a dialog instance from OpenDialog's args.
type DialogFactory func(Ctx, any) (Dialog, error)

// Resulter is an optional Dialog interface: on close, the host reads Result() into
// DialogClosedMsg.Result.
type Resulter interface {
	Result() any
}

// DialogOpenedMsg is sent after a dialog instance is pushed.
type DialogOpenedMsg struct {
	ID       string // registry ID passed to OpenDialog ("dialog.permission")
	Instance string // the instance's Component.ID()
}

// DialogClosedMsg is sent after a dialog instance is popped.
type DialogClosedMsg struct {
	ID       string
	Instance string
	Result   any // from Resulter, if implemented
}

// AddressedMsg delivers Msg only to the component (or dialog instance) with ID To.
type AddressedMsg struct {
	To  string
	Msg tea.Msg
}

// Address returns a Cmd delivering m to one component's Update.
func Address(to string, m tea.Msg) tea.Cmd {
	return func() tea.Msg { return AddressedMsg{To: to, Msg: m} }
}
