package research

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// featureForTest returns the registered research feature.
func featureForTest() ext.Feature {
	for _, f := range ext.Pending() {
		if f.ID == FeatureID {
			return f
		}
	}
	panic("research feature not registered")
}

// rig is a feature set up against a recording registrar and a fullscreen fake Ctx.
type rig struct {
	t   *testing.T
	f   *feature
	ctx *exttest.Ctx
	reg *exttest.Registrar
}

func newRig(t *testing.T, nodes []fakeNode, w, h int) *rig {
	t.Helper()
	f := newFeature()
	reg := exttest.NewRegistrar()
	reg.Feature = FeatureID
	f.setup(reg)
	ctx := exttest.NewCtx()
	ctx.W, ctx.H, ctx.LayoutMode = w, h, ext.Fullscreen
	f.tree = fakeTree(nodes)
	return &rig{t: t, f: f, ctx: ctx, reg: reg}
}

// enter activates research mode at node viewing and returns the scope it sent.
func (r *rig) enter(viewing string) ext.TranscriptScopeMsg {
	r.t.Helper()
	return r.scope(r.f.activate(r.ctx, viewing))
}

// scope returns the last TranscriptScopeMsg among cmd's messages.
func (r *rig) scope(cmd tea.Cmd) ext.TranscriptScopeMsg {
	r.t.Helper()
	var out *ext.TranscriptScopeMsg
	for _, m := range exttest.Exec(cmd) {
		switch m := m.(type) {
		case ext.TranscriptScopeMsg:
			out = &m
		case scopeLoadedMsg:
			if s, ok := exttest.Exec(r.f.scopeLoaded(r.ctx, m))[0].(ext.TranscriptScopeMsg); ok {
				out = &s
			}
		}
	}
	if out == nil {
		r.t.Fatal("no TranscriptScopeMsg")
	}
	return *out
}

// exec runs cmd, feeding off-branch loads back to the feature as the host would.
func (r *rig) exec(cmd tea.Cmd) []tea.Msg {
	msgs := exttest.Exec(cmd)
	for _, m := range msgs {
		if l, ok := m.(scopeLoadedMsg); ok {
			msgs = append(msgs, r.exec(r.f.scopeLoaded(r.ctx, l))...)
		}
	}
	return msgs
}

// deliver hands msg to the feature's subscriptions for its type and runs the result.
func (r *rig) deliver(msg tea.Msg) []tea.Msg {
	var out []tea.Msg
	for _, s := range r.reg.Subs {
		if reflect.TypeOf(s.Sample) == reflect.TypeOf(msg) {
			out = append(out, r.exec(s.Fn(r.ctx, msg))...)
		}
	}
	return out
}

// action runs a registered action by ID.
func (r *rig) action(id ext.ActionID) (bool, tea.Cmd) {
	r.t.Helper()
	for _, a := range r.reg.Actions {
		if a.ID == id {
			return a.Run(r.ctx)
		}
	}
	r.t.Fatalf("action %s not registered", id)
	return false, nil
}

// bar returns the registered bar component with the given ID.
func (r *rig) bar(id string) ext.Component {
	r.t.Helper()
	for _, c := range r.reg.Components {
		if c.Component.ID() == id {
			return c.Component
		}
	}
	r.t.Fatalf("component %s not registered", id)
	return nil
}

// click sends a left click on row y of a bar.
func (r *rig) click(id string, y int) tea.Cmd {
	return r.bar(id).Update(r.ctx, ext.MouseEvent{Msg: tea.MouseClickMsg{X: 3, Y: y, Button: tea.MouseLeft}, X: 3, Y: y})
}

// screen renders ancestors, the viewed node and children as plain text.
func (r *rig) screen() string {
	return ansi.Strip(storyScreen(r.ctx, r.f, r.ctx.Area()).Text)
}

// styled renders the same with styles, for goldens.
func (r *rig) styled() string { return storyScreen(r.ctx, r.f, r.ctx.Area()).Text }

// scopeText lists a scope's items, one "key id" per line.
func scopeText(s ext.Transcript) string {
	if s == nil {
		return "<unscoped>"
	}
	var b strings.Builder
	for _, it := range s.Items() {
		fmt.Fprintf(&b, "%s %s\n", it.Key, it.ID)
	}
	return b.String()
}
