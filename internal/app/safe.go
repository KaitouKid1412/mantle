package app

import (
	"fmt"
	"reflect"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"
)

// panicMsg reports a panic caught inside a feature's Cmd (on another goroutine); the
// root disables the feature when it arrives.
type panicMsg struct {
	feature, where string
	value          any
	stack          []byte
}

// safe runs fn and turns a panic into a disabled feature. It returns false after a
// panic. Only call it on the UI goroutine.
func (r *Root) safe(feature, where string, fn func()) (ok bool) {
	defer func() {
		if v := recover(); v != nil {
			r.panicked(feature, where, v, debug.Stack())
			ok = false
		}
	}()
	fn()
	return true
}

// wrapCmd makes a feature's Cmd panic-safe, including nested Batch and Sequence Cmds.
func wrapCmd(feature, where string, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() (msg tea.Msg) {
		defer func() {
			if v := recover(); v != nil {
				msg = panicMsg{feature: feature, where: where + " (cmd)", value: v, stack: debug.Stack()}
			}
		}()
		msg = cmd()
		switch m := msg.(type) {
		case tea.BatchMsg:
			out := make(tea.BatchMsg, len(m))
			for i, c := range m {
				out[i] = wrapCmd(feature, where, c)
			}
			return out
		}
		// tea.Sequence's message type is unexported: a []tea.Cmd. Rebuild it with the
		// same type so Bubble Tea still runs it in order.
		v := reflect.ValueOf(msg)
		if v.IsValid() && v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
			out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
			for i := range v.Len() {
				c, _ := v.Index(i).Interface().(tea.Cmd)
				if w := wrapCmd(feature, where, c); w != nil {
					out.Index(i).Set(reflect.ValueOf(w))
				}
			}
			return out.Interface()
		}
		return msg
	}
}

func (r *Root) panicked(feature, where string, v any, stack []byte) {
	if feature == "" {
		feature = "unknown"
	}
	reason := fmt.Sprintf("panic in %s: %v", where, v)
	r.log().Error("feature panicked; disabling it", "feature", feature, "where", where, "panic", fmt.Sprint(v), "stack", string(stack))
	if r.host.Disabled(feature) {
		return
	}
	r.host.Disable(feature, reason)
	if r.opts.OnDisable != nil {
		r.opts.OnDisable(feature, reason)
	}
	// Drop focus and dialogs that belong to the feature.
	if r.focus != "" && r.compFeature(r.focus) == feature {
		r.focus = ""
		r.pickFocus()
	}
	for i := len(r.dialogs) - 1; i >= 0; i-- {
		if r.dialogs[i].feature == feature {
			r.dialogs = append(r.dialogs[:i], r.dialogs[i+1:]...)
		}
	}
	r.rebuild()
	r.addNotice(noticeFor(feature, reason))
	r.invalidateAll()
}
