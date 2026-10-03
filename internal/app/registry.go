package app

import (
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Kind is the kind of a registered thing (catalog "kind").
type Kind string

const (
	KindFeature     Kind = "feature"
	KindCommand     Kind = "command"
	KindAction      Kind = "action"
	KindBinding     Kind = "binding"
	KindRenderer    Kind = "renderer"
	KindComponent   Kind = "component"
	KindDialog      Kind = "dialog"
	KindTheme       Kind = "theme"
	KindSetting     Kind = "setting"
	KindStage       Kind = "promptStage"
	KindInterceptor Kind = "interceptor"
	KindStory       Kind = "story"
	KindSubscriber  Kind = "subscriber"
	KindStart       Kind = "start"
)

// entry is one registration.
type entry struct {
	kind     Kind
	id       string
	feature  string
	order    int
	file     string // file:line of the registering call
	priority int    // stages, interceptors
	value    any    // ext.Command, ext.Action, ext.Renderer, mounted, ext.DialogFactory, theme.Theme, ext.SettingSpec, ext.PromptStage, ext.Interceptor, ext.Story, subscription, startFn, ext.Binding
	removed  bool
}

type mounted struct {
	slot ext.Slot
	comp ext.Component
	opts ext.SlotOpts
}

type subscription struct {
	typ reflect.Type
	fn  func(ext.Ctx, tea.Msg) tea.Cmd
}

type startFn func(ext.Ctx) tea.Cmd

// op is a declarative override, resolved after every Setup.
type op struct {
	kind    string // "replace" | "wrap" | "remove" | "alias"
	target  string
	value   any
	newID   string // alias
	feature string
	order   int
	file    string
}

// registrar records one feature's Setup calls.
type registrar struct {
	h       *Host
	feature string
	order   int
}

var _ ext.Registrar = (*registrar)(nil)

func callerFile() string {
	// 0 callerFile, 1 add, 2 registrar method, 3 feature code (or ext.Subscribe)
	for skip := 3; skip < 6; skip++ {
		_, file, line, ok := runtime.Caller(skip)
		if !ok {
			break
		}
		if strings.HasSuffix(file, "/pkg/ext/ext.go") {
			continue // ext.Subscribe
		}
		return relFile(file) + ":" + itoa(line)
	}
	return ""
}

func relFile(file string) string {
	if i := strings.Index(file, "/mantle/"); i >= 0 {
		// Trim everything up to the module root (checkout, worktree or ~/.mantle/src).
		rest := file[i+len("/mantle/"):]
		for _, top := range []string{"features/", "mods/", "internal/", "pkg/", "cmd/"} {
			if j := strings.Index(rest, top); j >= 0 {
				return rest[j:]
			}
		}
		return rest
	}
	return filepath.Base(file)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func (r *registrar) add(k Kind, id string, prio int, v any) {
	r.h.entries = append(r.h.entries, &entry{kind: k, id: id, feature: r.feature, order: r.order, file: callerFile(), priority: prio, value: v})
}

func (r *registrar) addOp(o op) {
	o.feature, o.order, o.file = r.feature, r.order, callerFile()
	r.h.ops = append(r.h.ops, o)
}

func (r *registrar) FeatureID() string { return r.feature }

func (r *registrar) AddCommand(c ext.Command) {
	c.Name = strings.TrimPrefix(c.Name, "/")
	if c.ID == "" {
		c.ID = ext.CommandID(c.Name)
	}
	if c.Source == "" {
		if r.order >= ext.ModOrder {
			c.Source = ext.SourceMod
		} else {
			c.Source = ext.SourceBuiltin
		}
	}
	r.add(KindCommand, c.ID, 0, c)
}

func (r *registrar) AddAction(a ext.Action) { r.add(KindAction, string(a.ID), 0, a) }

func (r *registrar) AddBinding(b ext.Binding) {
	r.add(KindBinding, b.Context+"|"+b.Keys, 0, b)
}

func (r *registrar) AddRenderer(key ext.ContentKey, f ext.Renderer) {
	r.add(KindRenderer, ext.RendererID(key), 0, f)
}

func (r *registrar) AddComponent(s ext.Slot, c ext.Component, o ext.SlotOpts) {
	r.add(KindComponent, c.ID(), 0, &mounted{slot: s, comp: c, opts: o})
}

func (r *registrar) AddDialog(id string, f ext.DialogFactory) { r.add(KindDialog, id, 0, f) }

func (r *registrar) AddTheme(t theme.Theme) { r.add(KindTheme, "theme."+t.Name, 0, t) }

func (r *registrar) AddSetting(s ext.SettingSpec) { r.add(KindSetting, s.Key, 0, s) }

func (r *registrar) AddPromptStage(id string, prio int, s ext.PromptStage) {
	r.add(KindStage, id, prio, s)
}

func (r *registrar) AddInterceptor(id string, prio int, i ext.Interceptor) {
	r.add(KindInterceptor, id, prio, i)
}

func (r *registrar) AddStory(s ext.Story) { r.add(KindStory, s.ID, 0, s) }

func (r *registrar) SubscribeRaw(id string, sample tea.Msg, fn func(ext.Ctx, tea.Msg) tea.Cmd) {
	r.add(KindSubscriber, id, 0, subscription{typ: reflect.TypeOf(sample), fn: fn})
}

func (r *registrar) OnStart(id string, fn func(ext.Ctx) tea.Cmd) {
	r.add(KindStart, id, 0, startFn(fn))
}

func (r *registrar) Replace(id string, with any) {
	r.addOp(op{kind: "replace", target: id, value: normalize(with)})
}
func (r *registrar) Wrap(id string, w any) { r.addOp(op{kind: "wrap", target: id, value: w}) }
func (r *registrar) Remove(id string)      { r.addOp(op{kind: "remove", target: id}) }
func (r *registrar) Alias(oldID, newID string) {
	r.addOp(op{kind: "alias", target: oldID, newID: newID})
}
