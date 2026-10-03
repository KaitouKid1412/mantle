package app

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Report is a problem found while setting up features: a Setup error, an ordering
// cycle, an override conflict or a bad override target. Reports appear in
// `mantle-ui catalog` and `selftest`.
type Report struct {
	Feature string
	ID      string
	Message string
	Fatal   bool // selftest fails on fatal reports
}

func (r Report) String() string {
	if r.ID != "" {
		return fmt.Sprintf("%s: %s: %s", r.Feature, r.ID, r.Message)
	}
	return fmt.Sprintf("%s: %s", r.Feature, r.Message)
}

// Host collects features, runs their Setup in a fixed order and resolves overrides.
// It owns the resolved registries the root model reads. A Host is used only on the UI
// goroutine once the program runs.
type Host struct {
	features []ext.Feature // in Setup order
	skipped  map[string]string
	entries  []*entry
	ops      []op
	aliases  map[string]string
	reports  []Report

	disabled map[string]string // feature → reason (runtime panics)
	onPanic  func(feature, where string, v any)
}

// HostOptions configure NewHost.
type HostOptions struct {
	// Safe skips user mods (Order >= ext.ModOrder): `mantle --safe`.
	Safe bool
	// Disabled lists feature IDs that panicked in an earlier run; mods among them
	// (Order >= ext.ModOrder) are skipped.
	Disabled []string
	// Core features are set up first, before any registered feature.
	Core []ext.Feature
}

// NewHost sets up features: core first, then built-ins and mods ordered by Order, then
// After, then ID. It then resolves Replace/Wrap/Remove/Alias.
func NewHost(features []ext.Feature, o HostOptions) *Host {
	h := &Host{skipped: map[string]string{}, aliases: map[string]string{}, disabled: map[string]string{}}
	var use []ext.Feature
	for _, f := range features {
		switch {
		case o.Safe && f.Order >= ext.ModOrder:
			h.skipped[f.ID] = "safe mode"
		case f.Order >= ext.ModOrder && slices.Contains(o.Disabled, f.ID):
			// Only mods stay off across runs; a built-in that panicked once is retried
			// (turning it off for good could leave mantle unusable).
			h.skipped[f.ID] = "disabled after an earlier crash"
		default:
			use = append(use, f)
		}
	}
	ordered := h.order(use)
	for _, f := range append(slices.Clone(o.Core), ordered...) {
		h.setup(f)
	}
	h.resolve()
	return h
}

func (h *Host) report(feature, id, msg string, fatal bool) {
	h.reports = append(h.reports, Report{Feature: feature, ID: id, Message: msg, Fatal: fatal})
}

// order sorts features by (Order, ID) and then topologically by After: a feature runs
// after everything it names in After. Unknown After targets and cycles are reported.
func (h *Host) order(fs []ext.Feature) []ext.Feature {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Order != fs[j].Order {
			return fs[i].Order < fs[j].Order
		}
		return fs[i].ID < fs[j].ID
	})
	byID := map[string]int{}
	for i, f := range fs {
		if _, dup := byID[f.ID]; dup {
			h.report(f.ID, "", "duplicate feature ID; the later registration is ignored", true)
			continue
		}
		byID[f.ID] = i
	}
	deps := make([][]int, len(fs))
	for i, f := range fs {
		for _, a := range f.After {
			j, ok := byID[a]
			if !ok {
				if _, skipped := h.skipped[a]; !skipped {
					h.report(f.ID, "", fmt.Sprintf("After names unknown feature %q", a), false)
				}
				continue
			}
			deps[i] = append(deps[i], j)
		}
	}
	done := make([]bool, len(fs))
	var out []ext.Feature
	for len(out) < len(byID) {
		progressed := false
		for i, f := range fs {
			if done[i] || byID[f.ID] != i {
				continue
			}
			ready := true
			for _, d := range deps[i] {
				if !done[d] {
					ready = false
					break
				}
			}
			if ready {
				done[i] = true
				out = append(out, f)
				progressed = true
				break // restart from the lowest (Order, ID)
			}
		}
		if !progressed {
			// Cycle: take the lowest remaining feature and report it.
			for i, f := range fs {
				if !done[i] && byID[f.ID] == i {
					h.report(f.ID, "", "After cycle; set up in Order instead", false)
					done[i] = true
					out = append(out, f)
					break
				}
			}
		}
	}
	return out
}

func (h *Host) setup(f ext.Feature) {
	h.features = append(h.features, f)
	if f.Setup == nil {
		return
	}
	r := &registrar{h: h, feature: f.ID, order: f.Order}
	before, beforeOps := len(h.entries), len(h.ops)
	err := func() (err error) {
		defer func() {
			if v := recover(); v != nil {
				err = fmt.Errorf("panic in Setup: %v", v)
			}
		}()
		return f.Setup(r)
	}()
	if err != nil {
		// Drop everything the failed Setup registered.
		h.entries = h.entries[:before]
		h.ops = h.ops[:beforeOps]
		h.report(f.ID, "", err.Error(), true)
		h.skipped[f.ID] = err.Error()
	}
}

// resolveAlias follows alias chains (bounded, cycle-safe).
func (h *Host) resolveAlias(id string) string {
	for range 16 {
		n, ok := h.aliases[id]
		if !ok {
			return id
		}
		id = n
	}
	return id
}

func (h *Host) find(id string) []*entry {
	var out []*entry
	for _, e := range h.entries {
		if e.id == id && !e.removed {
			out = append(out, e)
		}
	}
	return out
}

// resolve applies duplicates policy and the declarative overrides.
func (h *Host) resolve() {
	// Aliases first, so every other op can name an old ID.
	for _, o := range h.ops {
		if o.kind == "alias" {
			if prev, ok := h.aliases[o.target]; ok && prev != o.newID {
				h.report(o.feature, o.target, fmt.Sprintf("alias conflict: %s vs %s", prev, o.newID), false)
			}
			h.aliases[o.target] = o.newID
		}
	}

	// Duplicate registrations of the same (kind, id): the higher Order wins (a later
	// feature at the same Order wins too); the loser is reported.
	type key struct {
		k  Kind
		id string
	}
	winner := map[key]*entry{}
	for _, e := range h.entries {
		if e.kind == KindBinding || e.kind == KindSubscriber || e.kind == KindStart {
			continue // bindings layer; subscribers and start hooks may share IDs harmlessly
		}
		k := key{e.kind, e.id}
		if w, ok := winner[k]; ok {
			lose, win := w, e
			if w.order > e.order {
				lose, win = e, w
			}
			lose.removed = true
			winner[k] = win
			if lose.feature != win.feature {
				h.report(win.feature, e.id, fmt.Sprintf("%s registered twice (also by %s); %s wins", e.kind, lose.feature, win.feature), false)
			}
			continue
		}
		winner[k] = e
	}

	// Replace and Remove: per target, the highest-Order op decides.
	type decision struct {
		o     op
		other []op
	}
	decide := map[string]*decision{}
	var targets []string
	for _, o := range h.ops {
		if o.kind != "replace" && o.kind != "remove" {
			continue
		}
		t := h.resolveAlias(o.target)
		d, ok := decide[t]
		if !ok {
			decide[t] = &decision{o: o}
			targets = append(targets, t)
			continue
		}
		if o.order >= d.o.order {
			d.other = append(d.other, d.o)
			d.o = o
		} else {
			d.other = append(d.other, o)
		}
	}
	for _, t := range targets {
		d := decide[t]
		for _, lost := range d.other {
			if lost.feature != d.o.feature {
				h.report(d.o.feature, t, fmt.Sprintf("override conflict: %s's %s loses to %s's %s (higher Order wins)", lost.feature, lost.kind, d.o.feature, d.o.kind), false)
			}
		}
		h.applyOne(t, d.o)
	}

	// Wraps: innermost first (ascending Order), so the highest-Order wrapper is outermost.
	var wraps []op
	for _, o := range h.ops {
		if o.kind == "wrap" {
			wraps = append(wraps, o)
		}
	}
	sort.SliceStable(wraps, func(i, j int) bool { return wraps[i].order < wraps[j].order })
	for _, o := range wraps {
		h.applyWrap(h.resolveAlias(o.target), o)
	}
}

func (h *Host) applyOne(target string, o op) {
	es := h.find(target)
	if len(es) == 0 {
		h.report(o.feature, target, o.kind+": no such ID", false)
		return
	}
	if o.kind == "remove" {
		for _, e := range es {
			e.removed = true
		}
		return
	}
	e := pickByValue(es, o.value)
	if e == nil {
		h.report(o.feature, target, fmt.Sprintf("replace: %T does not match the target's kind (%s)", o.value, es[0].kind), true)
		return
	}
	switch v := o.value.(type) {
	case ext.Command:
		if v.ID == "" {
			v.ID = target
		}
		if v.Name == "" {
			v.Name = e.value.(ext.Command).Name
		}
		e.value = v
	case ext.CommandFunc:
		c := e.value.(ext.Command)
		c.Run = v
		e.value = c
	case ext.Action:
		v.ID = e.value.(ext.Action).ID
		e.value = v
	case ext.ActionFunc:
		a := e.value.(ext.Action)
		a.Run = v
		e.value = a
	case ext.Component:
		m := *e.value.(*mounted)
		m.comp = v
		e.value = &m
	case theme.Theme:
		v.Name = e.value.(theme.Theme).Name
		e.value = v
	default:
		e.value = o.value
	}
	e.feature, e.order, e.file = o.feature, o.order, o.file
}

// pickByValue finds the entry whose kind accepts v.
func pickByValue(es []*entry, v any) *entry {
	want := kindOfValue(v)
	for _, e := range es {
		if slices.Contains(want, e.kind) {
			return e
		}
	}
	return nil
}

var funcKinds = []reflect.Type{
	reflect.TypeFor[ext.CommandFunc](), reflect.TypeFor[ext.ActionFunc](),
	reflect.TypeFor[ext.Renderer](), reflect.TypeFor[ext.DialogFactory](),
	reflect.TypeFor[ext.PromptStage](), reflect.TypeFor[ext.Interceptor](),
}

// normalize converts a plain func value (a named function or a literal) to the ext
// func type with the same signature, so Replace(id, myRenderer) works without a
// conversion.
func normalize(v any) any {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Func {
		return v
	}
	for _, t := range funcKinds {
		if rv.Type() == t {
			return v
		}
	}
	for _, t := range funcKinds {
		if rv.Type().ConvertibleTo(t) {
			return rv.Convert(t).Interface()
		}
	}
	return v
}

func kindOfValue(v any) []Kind {
	switch v.(type) {
	case ext.Command, ext.CommandFunc:
		return []Kind{KindCommand}
	case ext.Action, ext.ActionFunc:
		return []Kind{KindAction}
	case ext.Renderer:
		return []Kind{KindRenderer}
	case ext.DialogFactory:
		return []Kind{KindDialog}
	case ext.PromptStage:
		return []Kind{KindStage}
	case ext.Interceptor:
		return []Kind{KindInterceptor}
	case theme.Theme:
		return []Kind{KindTheme}
	case ext.Story:
		return []Kind{KindStory}
	case ext.Component:
		return []Kind{KindComponent}
	}
	return nil
}

func (h *Host) applyWrap(target string, o op) {
	es := h.find(target)
	if len(es) == 0 {
		h.report(o.feature, target, "wrap: no such ID", false)
		return
	}
	ok := false
	for _, e := range es {
		switch w := o.value.(type) {
		case func(ext.CommandFunc) ext.CommandFunc:
			if c, is := e.value.(ext.Command); is {
				c.Run = w(c.Run)
				e.value, ok = c, true
			}
		case func(ext.ActionFunc) ext.ActionFunc:
			if a, is := e.value.(ext.Action); is {
				a.Run = w(a.Run)
				e.value, ok = a, true
			}
		case func(ext.Renderer) ext.Renderer:
			if r, is := e.value.(ext.Renderer); is {
				e.value, ok = w(r), true
			}
		case func(ext.Component) ext.Component:
			if m, is := e.value.(*mounted); is {
				nm := *m
				nm.comp = w(m.comp)
				e.value, ok = &nm, true
			}
		case func(ext.DialogFactory) ext.DialogFactory:
			if f, is := e.value.(ext.DialogFactory); is {
				e.value, ok = w(f), true
			}
		case func(ext.PromptStage) ext.PromptStage:
			if s, is := e.value.(ext.PromptStage); is {
				e.value, ok = w(s), true
			}
		}
	}
	if !ok {
		h.report(o.feature, target, fmt.Sprintf("wrap: %T does not fit the target's kind (%s)", o.value, es[0].kind), true)
	}
}

// live returns the entries of a kind that are not removed and whose feature is not
// disabled, in registration (Setup) order.
func (h *Host) live(k Kind) []*entry {
	var out []*entry
	for _, e := range h.entries {
		if e.kind == k && !e.removed && h.disabled[e.feature] == "" {
			out = append(out, e)
		}
	}
	return out
}

// Reports returns setup problems.
func (h *Host) Reports() []Report { return slices.Clone(h.reports) }

// Skipped returns features that were not set up, with the reason.
func (h *Host) Skipped() map[string]string { return h.skipped }

// Disable turns a feature off for the rest of the run (after a panic).
func (h *Host) Disable(feature, reason string) {
	if feature == "" || h.disabled[feature] != "" {
		return
	}
	h.disabled[feature] = reason
}

// Disabled reports whether a feature was disabled at runtime.
func (h *Host) Disabled(feature string) bool { return h.disabled[feature] != "" }

// featureOf returns the feature that registered a component ID (or "").
func (h *Host) featureOf(kind Kind, id string) string {
	for _, e := range h.entries {
		if e.kind == kind && e.id == id && !e.removed {
			return e.feature
		}
	}
	return ""
}

// Themes returns the registered themes (built-ins plus AddTheme), by name.
func (h *Host) Themes() map[string]theme.Theme {
	out := map[string]theme.Theme{}
	for _, t := range theme.Builtins() {
		out[t.Name] = t
	}
	for _, e := range h.live(KindTheme) {
		t := e.value.(theme.Theme)
		out[t.Name] = t
	}
	return out
}

// Settings returns the mantle setting specs.
func (h *Host) Settings() []ext.SettingSpec {
	var out []ext.SettingSpec
	for _, e := range h.live(KindSetting) {
		out = append(out, e.value.(ext.SettingSpec))
	}
	return out
}

// Stories returns every story, sorted by ID.
func (h *Host) Stories() []ext.Story {
	var out []ext.Story
	for _, e := range h.live(KindStory) {
		out = append(out, e.value.(ext.Story))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Bindings returns feature-registered default bindings, in Setup order.
func (h *Host) Bindings() []ext.Binding {
	var out []ext.Binding
	for _, e := range h.live(KindBinding) {
		out = append(out, e.value.(ext.Binding))
	}
	return out
}

// CatalogEntry describes one registered ID.
type CatalogEntry struct {
	Kind        Kind     `json:"kind"`
	ID          string   `json:"id"`
	Feature     string   `json:"feature"`
	File        string   `json:"file,omitempty"`
	Parity      []string `json:"parity,omitempty"`
	Description string   `json:"description,omitempty"`
	Order       int      `json:"order"`
	Removed     bool     `json:"removed,omitempty"`
}

// Catalog lists every feature and registration, sorted by kind then ID.
func (h *Host) Catalog() []CatalogEntry {
	parity := map[string][]string{}
	var out []CatalogEntry
	for _, f := range h.features {
		parity[f.ID] = f.Parity
		out = append(out, CatalogEntry{Kind: KindFeature, ID: f.ID, Feature: f.ID, Parity: f.Parity, Order: f.Order})
	}
	for _, e := range h.entries {
		ce := CatalogEntry{Kind: e.kind, ID: e.id, Feature: e.feature, File: e.file, Parity: parity[e.feature], Order: e.order, Removed: e.removed}
		switch v := e.value.(type) {
		case ext.Command:
			ce.Description = v.Description
		case ext.Action:
			ce.Description = v.Description
		case ext.SettingSpec:
			ce.Description = v.Description
		case ext.Binding:
			ce.Description = string(v.Action)
		}
		out = append(out, ce)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// renderer resolves a content key: exact, longest wildcard, then "default".
func (h *Host) renderer(k ext.ContentKey) (ext.Renderer, string) {
	byID := map[string]*entry{}
	for _, e := range h.live(KindRenderer) {
		byID[e.id] = e
	}
	for _, c := range k.Candidates() {
		if e, ok := byID[ext.RendererID(c)]; ok {
			return e.value.(ext.Renderer), e.feature
		}
	}
	return nil, ""
}

func typeName(t reflect.Type) string {
	if t == nil {
		return "<nil>"
	}
	return strings.TrimPrefix(t.String(), "*")
}
