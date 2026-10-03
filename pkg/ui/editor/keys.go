package editor

import (
	tea "charm.land/bubbletea/v2"
)

// Binding is a set of keystrokes, written like tea.Key.Keystroke ("ctrl+a",
// "alt+b", "home"), that trigger one editor operation.
type Binding []string

// Matches reports whether keystroke k is in the binding.
func (b Binding) Matches(k string) bool {
	for _, s := range b {
		if s == k {
			return true
		}
	}
	return false
}

// KeyMap lists the keys the editor handles itself in HandleKey. These are
// the readline keys Claude Code hardcodes in its prompt input; keys that are
// keymap actions in Claude Code (chat:newline, chat:undo, history:previous,
// ...) are included so the editor also works standalone, and hosts that route
// them through a keymap clear those entries (see ClearActionKeys).
type KeyMap struct {
	CharForward, CharBackward Binding
	WordForward, WordBackward Binding
	LineStart, LineEnd        Binding
	DocStart, DocEnd          Binding
	LineUp, LineDown          Binding

	DeleteBackward, DeleteForward Binding
	KillLineEnd, KillLineStart    Binding
	KillWordBackward              Binding // to whitespace (ctrl+w)
	KillWordBackwardAlnum         Binding // to word start (alt+backspace)
	KillWordForward               Binding
	Yank, YankPop                 Binding
	Transpose                     Binding

	Undo, Redo Binding
	Newline    Binding

	// Attachment navigation (Claude Code context "Attachments").
	AttachPrev, AttachNext, AttachRemove, AttachExit Binding
}

// DefaultKeyMap returns readline-style defaults matching Claude Code's prompt.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		CharForward:  Binding{"right", "ctrl+f"},
		CharBackward: Binding{"left", "ctrl+b"},
		WordForward:  Binding{"alt+f", "ctrl+right", "alt+right"},
		WordBackward: Binding{"alt+b", "ctrl+left", "alt+left"},
		LineStart:    Binding{"ctrl+a", "home"},
		LineEnd:      Binding{"ctrl+e", "end"},
		DocStart:     Binding{"ctrl+home", "alt+<"},
		DocEnd:       Binding{"ctrl+end", "alt+>"},
		LineUp:       Binding{"up"},
		LineDown:     Binding{"down"},

		DeleteBackward:        Binding{"backspace", "ctrl+h", "shift+backspace"},
		DeleteForward:         Binding{"delete", "ctrl+d"},
		KillLineEnd:           Binding{"ctrl+k"},
		KillLineStart:         Binding{"ctrl+u"},
		KillWordBackward:      Binding{"ctrl+w"},
		KillWordBackwardAlnum: Binding{"alt+backspace", "ctrl+backspace"},
		KillWordForward:       Binding{"alt+d", "alt+delete", "ctrl+delete"},
		Yank:                  Binding{"ctrl+y"},
		YankPop:               Binding{"alt+y"},

		Undo:    Binding{"ctrl+_", "ctrl+-", "ctrl+shift+-", "ctrl+shift+_"},
		Newline: Binding{"shift+enter", "alt+enter", "ctrl+j"},

		AttachPrev:   Binding{"left"},
		AttachNext:   Binding{"right"},
		AttachRemove: Binding{"backspace", "delete"},
		AttachExit:   Binding{"down", "esc"},
	}
}

// ClearActionKeys removes the keys that Claude Code routes through keymap
// actions (chat:undo, chat:newline via ctrl+j, history:previous/next), for
// hosts that bind those actions to editor operations themselves. Shift+enter
// and alt+enter stay: they are hardcoded newline keys in Claude Code.
func (k *KeyMap) ClearActionKeys() {
	k.Undo = nil
	k.LineUp, k.LineDown = nil, nil
	k.Newline = Binding{"shift+enter", "alt+enter"}
}

// Keystroke returns the editor's name for a key press.
func Keystroke(msg tea.KeyPressMsg) string { return msg.Keystroke() }

// HandleKey applies a key press. It reports whether the editor used the
// key; unhandled keys (enter, esc, tab, keymap chords, up on the first row)
// belong to the host. The returned Cmd is non-nil only for vim timeouts.
func (e *Editor) HandleKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if e.vim != nil {
		return e.vim.handle(e, msg)
	}
	return e.handleBasicKey(msg), nil
}

// handleBasicKey is HandleKey without vim: readline keys and typing.
func (e *Editor) handleBasicKey(msg tea.KeyPressMsg) bool {
	k := msg.Keystroke()
	km := &e.KeyMap
	if e.attach >= 0 {
		switch {
		case km.AttachPrev.Matches(k):
			e.AttachmentPrev()
			return true
		case km.AttachNext.Matches(k):
			e.AttachmentNext()
			return true
		case km.AttachRemove.Matches(k):
			e.RemoveAttachment()
			return true
		case km.AttachExit.Matches(k):
			e.ExitAttachments()
			return true
		}
		e.ExitAttachments()
	}
	switch {
	case km.Newline.Matches(k):
		e.Newline()
	case km.CharForward.Matches(k):
		e.CharForward()
	case km.CharBackward.Matches(k):
		e.CharBackward()
	case km.WordForward.Matches(k):
		e.WordForward()
	case km.WordBackward.Matches(k):
		e.WordBackward()
	case km.LineStart.Matches(k):
		e.LineStart()
	case km.LineEnd.Matches(k):
		e.LineEnd()
	case km.DocStart.Matches(k):
		e.DocStart()
	case km.DocEnd.Matches(k):
		e.DocEnd()
	case km.LineUp.Matches(k):
		return e.CursorUp()
	case km.LineDown.Matches(k):
		return e.CursorDown()
	case km.DeleteBackward.Matches(k):
		e.DeleteBackward()
	case km.DeleteForward.Matches(k):
		if e.Empty() {
			return false // ctrl+d on an empty prompt belongs to the host (exit)
		}
		e.DeleteForward()
	case km.KillLineEnd.Matches(k):
		e.KillLineEnd()
	case km.KillLineStart.Matches(k):
		e.KillLineStart()
	case km.KillWordBackward.Matches(k):
		e.KillWordBackward()
	case km.KillWordBackwardAlnum.Matches(k):
		e.KillWordBackwardAlnum()
	case km.KillWordForward.Matches(k):
		e.KillWordForward()
	case km.Yank.Matches(k):
		return e.Yank()
	case km.YankPop.Matches(k):
		return e.YankPop()
	case km.Transpose.Matches(k):
		e.TransposeChars()
	case km.Undo.Matches(k):
		e.Undo()
	case km.Redo.Matches(k):
		e.Redo()
	default:
		if t := typedText(msg); t != "" {
			e.InsertString(t)
			return true
		}
		return false
	}
	return true
}

// typedText returns the text a key press types, or "" for commands.
func typedText(msg tea.KeyPressMsg) string {
	if msg.Text == "" || msg.Mod.Contains(tea.ModCtrl) || msg.Mod.Contains(tea.ModAlt) ||
		msg.Mod.Contains(tea.ModSuper) || msg.Mod.Contains(tea.ModMeta) || msg.Mod.Contains(tea.ModHyper) {
		return ""
	}
	return msg.Text
}
