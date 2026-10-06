package input

import (
	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor"
)

// action runs a keymap action for the prompt. It returns handled=false to
// let the key fall through (to HandleKey, or to another feature's action).
func (s *state) action(c ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	if s.search != nil {
		return s.searchAction(c, a)
	}
	switch a {
	// ---- submitting ----
	case ext.ActChatSubmit:
		return s.submitAction(c, "")
	case ext.ActChatSendNow:
		return s.submitAction(c, proto.PriorityNow)
	case ext.ActChatQueueSubmit:
		return s.submitAction(c, proto.PriorityLater)

	// ---- editing ----
	case ext.ActChatNewline:
		s.ed.Newline()
		return true, s.changed(c)
	case ext.ActChatUndo:
		s.ed.Undo()
		return true, s.changed(c)
	case ext.ActChatClearInput:
		s.ed.Clear()
		s.mode = modePrompt
		return true, s.changed(c)
	case ext.ActChatStash:
		return true, s.toggleStash(c)
	case ext.ActChatExternalEditor:
		return true, s.openExternalEditor(c)
	case ext.ActChatImagePaste:
		return true, editor.PasteImageCmd()
	case ext.ActChatWorkflowKeywordToggle:
		return true, s.toggleWorkflowKeyword(c)

	// ---- history ----
	case ext.ActHistoryPrevious:
		return s.historyPrev(c)
	case ext.ActHistoryNext:
		return s.historyNext(c)
	case ext.ActHistorySearch:
		return true, s.openSearch(c)

	// ---- autocomplete ----
	case ext.ActAutocompleteAccept:
		if !s.comp.open() {
			return false, nil
		}
		return true, s.comp.accept(c, s, false)
	case ext.ActAutocompleteDismiss:
		if !s.comp.open() {
			return false, nil
		}
		s.comp.dismiss(s)
		s.invalidate(c)
		return true, nil
	case ext.ActAutocompleteNext:
		if !s.comp.open() {
			return false, nil
		}
		s.comp.move(1)
		s.invalidate(c)
		return true, nil
	case ext.ActAutocompletePrevious:
		if !s.comp.open() {
			return false, nil
		}
		s.comp.move(-1)
		s.invalidate(c)
		return true, nil

	// ---- attachments ----
	case ext.ActAttachmentsNext:
		return s.inAttachments(c, s.ed.AttachmentNext)
	case ext.ActAttachmentsPrevious:
		return s.inAttachments(c, s.ed.AttachmentPrev)
	case ext.ActAttachmentsRemove:
		return s.inAttachments(c, func() { s.ed.RemoveAttachment() })
	case ext.ActAttachmentsExit:
		return s.inAttachments(c, s.ed.ExitAttachments)

	// ---- shared with turn control (plan 05) ----
	case ext.ActChatCancel:
		return s.cancel(c)
	case ext.ActAppInterrupt:
		// ctrl+c: clear a non-empty prompt (undoable); otherwise plan 05
		// interrupts the turn or arms exit.
		if s.comp.open() {
			s.comp.dismiss(s)
		}
		if s.ed.Empty() && s.mode == modePrompt {
			return false, nil
		}
		s.ed.Clear()
		s.mode = modePrompt
		return true, s.changed(c)
	case ext.ActAppExit:
		// ctrl+d deletes forward while there is text.
		if s.ed.Empty() {
			return false, nil
		}
		s.ed.DeleteForward()
		return true, s.changed(c)
	}
	return false, nil
}

func (s *state) inAttachments(c ext.Ctx, f func()) (bool, tea.Cmd) {
	if !s.ed.InAttachments() {
		return false, nil
	}
	f()
	return true, s.changed(c)
}

// cancel is esc: dismiss what is open; otherwise double esc clears a
// non-empty prompt or (on an empty one) opens rewind. While a turn runs and
// nothing is open, esc belongs to plan 05 (interrupt).
func (s *state) cancel(c ext.Ctx) (bool, tea.Cmd) {
	switch {
	case s.help:
		s.help = false
		s.invalidate(c)
		return true, nil
	case s.comp.open() || s.comp.noMatch != "":
		s.comp.dismiss(s)
		s.invalidate(c)
		return true, nil
	case s.ed.InAttachments():
		s.ed.ExitAttachments()
		return true, s.changed(c)
	}
	if s.ed.VimEnabled() {
		// Let vim leave INSERT/VISUAL or cancel a pending command first.
		if ok, cmd := s.ed.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape}); ok {
			return true, tea.Batch(cmd, s.changed(c))
		}
	}
	if s.mode == modeBash && s.ed.Empty() {
		s.mode = modePrompt
		return true, s.changed(c)
	}
	if s.busy {
		return false, nil
	}
	now := c.Clock().Now()
	double := !s.escAt.IsZero() && now.Sub(s.escAt) <= doubleEscWindow
	if !double {
		s.escAt = now
		if !s.ed.Empty() {
			return true, c.Notify(ext.Notice{Key: "input.esc", Text: "Esc again to clear", Timeout: doubleEscWindow, Source: FeatureID})
		}
		return true, nil
	}
	s.escAt = timeZero
	if s.ed.Empty() {
		return true, c.Run(ActRewind)
	}
	s.ed.Clear()
	s.mode = modePrompt
	return true, s.changed(c)
}

// ---- history recall ----

func (s *state) historyPrev(c ext.Ctx) (bool, tea.Cmd) {
	if s.ed.Empty() && len(s.queue) > 0 {
		return true, s.takeBackQueue(c)
	}
	if !s.ed.OnFirstRow() {
		return false, nil // the cursor moves up instead
	}
	if s.hist.AtDraft() {
		s.draft = s.save()
	}
	e, ok := s.hist.Older()
	if !ok {
		return true, nil
	}
	s.loadEntry(e)
	return true, s.changed(c)
}

func (s *state) historyNext(c ext.Ctx) (bool, tea.Cmd) {
	if !s.ed.OnLastRow() {
		return false, nil
	}
	if s.hist.AtDraft() {
		// Nothing newer: down moves on to the panels below the prompt.
		return true, c.Run(ActFooterSelect)
	}
	e, ok, draft := s.hist.Newer()
	switch {
	case !ok:
		return true, nil
	case draft:
		s.restore(s.draft)
		s.draft = nil
		s.ed.DocEnd()
	default:
		s.loadEntry(e)
	}
	return true, s.changed(c)
}
