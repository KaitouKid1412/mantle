package exttest

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Registrar records a feature's registrations. It does not resolve overrides; use the
// real host (internal/app) for that.
type Registrar struct {
	Feature      string
	Commands     []ext.Command
	Actions      []ext.Action
	Bindings     []ext.Binding
	Renderers    map[ext.ContentKey]ext.Renderer
	Components   []SlotComponent
	Dialogs      map[string]ext.DialogFactory
	Themes       []theme.Theme
	Settings     []ext.SettingSpec
	Stages       []Named
	Interceptors []Named
	Stories      []ext.Story
	Replaced     map[string]any
	Wrapped      map[string][]any
	Removed      []string
	Aliases      map[string]string
	Subs         []Subscription
	Starts       []Named
}

// SlotComponent is one AddComponent call.
type SlotComponent struct {
	Slot      ext.Slot
	Component ext.Component
	Opts      ext.SlotOpts
}

// Named is a prompt stage, interceptor or start hook with its ID and priority.
type Named struct {
	ID       string
	Priority int
	Value    any
}

// Subscription is one SubscribeRaw call.
type Subscription struct {
	ID     string
	Sample tea.Msg
	Fn     func(ext.Ctx, tea.Msg) tea.Cmd
}

var _ ext.Registrar = (*Registrar)(nil)

// NewRegistrar returns an empty recorder.
func NewRegistrar() *Registrar {
	return &Registrar{
		Renderers: map[ext.ContentKey]ext.Renderer{},
		Dialogs:   map[string]ext.DialogFactory{},
		Replaced:  map[string]any{},
		Wrapped:   map[string][]any{},
		Aliases:   map[string]string{},
	}
}

// Setup runs a feature's Setup against a new recorder.
func Setup(f ext.Feature) (*Registrar, error) {
	r := NewRegistrar()
	r.Feature = f.ID
	if f.Setup == nil {
		return r, nil
	}
	return r, f.Setup(r)
}

func (r *Registrar) FeatureID() string                            { return r.Feature }
func (r *Registrar) AddCommand(c ext.Command)                     { r.Commands = append(r.Commands, c) }
func (r *Registrar) AddAction(a ext.Action)                       { r.Actions = append(r.Actions, a) }
func (r *Registrar) AddBinding(b ext.Binding)                     { r.Bindings = append(r.Bindings, b) }
func (r *Registrar) AddRenderer(k ext.ContentKey, f ext.Renderer) { r.Renderers[k] = f }
func (r *Registrar) AddDialog(id string, f ext.DialogFactory)     { r.Dialogs[id] = f }
func (r *Registrar) AddTheme(t theme.Theme)                       { r.Themes = append(r.Themes, t) }
func (r *Registrar) AddSetting(s ext.SettingSpec)                 { r.Settings = append(r.Settings, s) }
func (r *Registrar) AddStory(s ext.Story)                         { r.Stories = append(r.Stories, s) }
func (r *Registrar) Replace(id string, with any)                  { r.Replaced[id] = with }
func (r *Registrar) Wrap(id string, w any)                        { r.Wrapped[id] = append(r.Wrapped[id], w) }
func (r *Registrar) Remove(id string)                             { r.Removed = append(r.Removed, id) }
func (r *Registrar) Alias(oldID, newID string)                    { r.Aliases[oldID] = newID }

func (r *Registrar) AddComponent(s ext.Slot, c ext.Component, o ext.SlotOpts) {
	r.Components = append(r.Components, SlotComponent{s, c, o})
}

func (r *Registrar) AddPromptStage(id string, prio int, s ext.PromptStage) {
	r.Stages = append(r.Stages, Named{id, prio, s})
}

func (r *Registrar) AddInterceptor(id string, prio int, i ext.Interceptor) {
	r.Interceptors = append(r.Interceptors, Named{id, prio, i})
}

func (r *Registrar) SubscribeRaw(id string, sample tea.Msg, fn func(ext.Ctx, tea.Msg) tea.Cmd) {
	r.Subs = append(r.Subs, Subscription{id, sample, fn})
}

func (r *Registrar) OnStart(id string, fn func(ext.Ctx) tea.Cmd) {
	r.Starts = append(r.Starts, Named{ID: id, Value: fn})
}

// Command returns the recorded command with the given name.
func (r *Registrar) Command(name string) (ext.Command, bool) {
	for _, c := range r.Commands {
		if c.Name == name {
			return c, true
		}
	}
	return ext.Command{}, false
}

// Dispatch delivers msg to every recorded subscription whose sample has the same
// dynamic type, and returns the batched Cmds.
func (r *Registrar) Dispatch(c ext.Ctx, msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	want := fmt.Sprintf("%T", msg)
	for _, s := range r.Subs {
		if fmt.Sprintf("%T", s.Sample) == want {
			cmds = append(cmds, s.Fn(c, msg))
		}
	}
	return tea.Batch(cmds...)
}
