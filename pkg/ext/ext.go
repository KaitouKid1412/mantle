package ext

import (
	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// APIVersion is bumped only for a deliberate breaking change to this package.
const APIVersion = 1

// ModOrder is the lowest Order a user mod may use; built-ins use 0-999.
const ModOrder = 1000

// Feature is the registration unit. init() calls Register (records only); the host
// runs Setup in order core < built-ins (Order/After) < mods, then resolves overrides.
type Feature struct {
	ID     string   // "chrome.footer", "mod.pirate-spinner"
	Order  int      // built-ins 0-999, mods >= 1000; higher wins override conflicts
	After  []string // features that must Setup first
	Parity []string // PARITY.md IDs covered
	Setup  func(Registrar) error
}

var pending []Feature

// Register records a feature. Call it from init(); it does nothing else.
func Register(f Feature) { pending = append(pending, f) }

// Pending returns every registered feature, in registration order. Host only.
func Pending() []Feature { return append([]Feature(nil), pending...) }

// Registrar is what a feature's Setup receives. Only mantle implements it; it may gain
// methods (additive).
//
// Override targets are IDs (see the package doc). Replace takes a value of the same
// kind as the target (Command, Action, Renderer, Component, DialogFactory, PromptStage,
// Interceptor, theme.Theme). Wrap takes func(next T) T for T in CommandFunc, ActionFunc,
// Renderer, Component, DialogFactory or PromptStage. Overrides are resolved after every
// Setup ran; registration order never decides the winner.
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
	Wrap(id string, w any)       // func(next T) T for T in {CommandFunc, Renderer, ActionFunc, Component, DialogFactory, PromptStage}
	Remove(id string)
	Alias(oldID, newID string)
	SubscribeRaw(id string, sample tea.Msg, fn func(Ctx, tea.Msg) tea.Cmd)

	// OnStart runs fn once on the UI goroutine after every feature is set up and the
	// program has started (before the first engine spawn). id names the hook.
	OnStart(id string, fn func(Ctx) tea.Cmd)
	// FeatureID is the ID of the feature whose Setup is running.
	FeatureID() string
}

// Subscribe delivers only messages of type T (host dispatches by type). id names the
// subscription so it can be listed, replaced or removed.
func Subscribe[T tea.Msg](r Registrar, id string, fn func(Ctx, T) tea.Cmd) {
	var zero T
	r.SubscribeRaw(id, zero, func(c Ctx, m tea.Msg) tea.Cmd { return fn(c, m.(T)) })
}

// Msg wraps a message in a Cmd.
func Msg(m tea.Msg) tea.Cmd { return func() tea.Msg { return m } }

// ID helpers for override targets.

// CommandID is the override ID of a slash command ("cmd.model").
func CommandID(name string) string { return "cmd." + name }

// RendererID is the override ID of a renderer ("render.tool.Bash").
func RendererID(k ContentKey) string { return "render." + string(k) }
