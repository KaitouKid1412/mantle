package app

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/keymap"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// keyTarget is the component that receives keys: the top dialog, else the focused
// component.
func (r *Root) keyTarget() (ext.Focusable, string) {
	if d := r.topDialog(); d != nil {
		return d.d, d.feature
	}
	if c := r.byID[r.focus]; c != nil && !r.host.Disabled(c.feature) {
		if f, ok := c.m.comp.(ext.Focusable); ok {
			return f, c.feature
		}
	}
	return nil, ""
}

// activeContexts is the keybinding context stack, most specific first: the target's
// contexts, then ambient contexts (sorted, for determinism), then Global.
func (r *Root) activeContexts() []string {
	var out []string
	if t, feature := r.keyTarget(); t != nil {
		if cs, ok := t.(ext.ContextStack); ok {
			var ks []string
			r.safe(feature, t.ID()+".KeyContexts", func() { ks = cs.KeyContexts() })
			out = append(out, ks...)
		} else {
			var k string
			r.safe(feature, t.ID()+".KeyContext", func() { k = t.KeyContext() })
			if k != "" {
				out = append(out, k)
			}
		}
	}
	for _, k := range sortedKeys(r.ambient) {
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	if !slices.Contains(out, ext.ContextGlobal) {
		out = append(out, ext.ContextGlobal)
	}
	return out
}

func (r *Root) routeKey(k tea.KeyPressMsg) tea.Cmd {
	keys := keymap.EventKeys(k)
	res := r.resolver.Resolve(r.activeContexts(), keys, r.opts.Clock.Now())
	outcome := res.Outcome
	if outcome == keymap.Abandoned {
		outcome = res.Retry
	}
	switch outcome {
	case keymap.Pending:
		return nil
	case keymap.Matched:
		for _, m := range res.Matches {
			if handled, cmd := r.runAction(m.Action); handled {
				return cmd
			}
		}
	}
	t, feature := r.keyTarget()
	if t != nil {
		var handled bool
		var cmd tea.Cmd
		r.safe(feature, t.ID()+".HandleKey", func() { handled, cmd = t.HandleKey(r.ctx, k) })
		if handled {
			return wrapCmd(feature, t.ID(), cmd)
		}
	}
	if len(keys) > 0 && keys[0] == "ctrl+z" {
		return tea.Suspend
	}
	return nil
}

// runAction offers an action to the key target's HandleAction, then to the registered
// action. It reports whether anything handled it.
func (r *Root) runAction(a ext.ActionID) (bool, tea.Cmd) {
	if t, feature := r.keyTarget(); t != nil {
		if ah, ok := t.(ext.ActionHandler); ok {
			var handled bool
			var cmd tea.Cmd
			r.safe(feature, t.ID()+".HandleAction", func() { handled, cmd = ah.HandleAction(r.ctx, a) })
			if handled {
				return true, wrapCmd(feature, t.ID(), cmd)
			}
		}
	}
	e, ok := r.actions[a]
	if !ok {
		return false, nil
	}
	act := e.value.(ext.Action)
	if act.Run == nil {
		return false, nil
	}
	var handled bool
	var cmd tea.Cmd
	if !r.safe(e.feature, string(a), func() { handled, cmd = act.Run(r.ctx) }) {
		return true, nil
	}
	return handled, wrapCmd(e.feature, string(a), cmd)
}

func (r *Root) routePaste(p tea.PasteMsg) tea.Cmd {
	t, feature := r.keyTarget()
	if t == nil {
		return nil
	}
	var handled bool
	var cmd tea.Cmd
	r.safe(feature, t.ID()+".HandlePaste", func() { handled, cmd = t.HandlePaste(r.ctx, p) })
	_ = handled
	return wrapCmd(feature, t.ID(), cmd)
}

func (r *Root) routeWheel(m tea.MouseWheelMsg) tea.Cmd {
	up := m.Button == tea.MouseWheelUp
	res := r.resolver.Resolve(append([]string{ext.ContextScroll}, r.activeContexts()...), []string{keymap.WheelKey(up)}, r.opts.Clock.Now())
	if res.Outcome == keymap.Matched {
		for _, mt := range res.Matches {
			if handled, cmd := r.runAction(mt.Action); handled {
				return cmd
			}
		}
	}
	return r.broadcast(m)
}
