package editor

import (
	"time"
	"unicode/utf8"

	"github.com/KaitouKid1412/mantle/pkg/ui/editor/history"
)

// HistoryEntry builds the history.jsonl line for the current content, the
// way Claude Code records it: chips stay as labels in display, paste text
// goes into pastedContents (inline up to InlinePasteMax characters, else by
// paste-cache hash), and images are not stored.
func (e *Editor) HistoryEntry(project, sessionID string, now time.Time) history.Entry {
	h := history.Entry{
		Display:   e.Display(),
		Timestamp: now.UnixMilli(),
		Project:   project,
		SessionID: sessionID,
	}
	for _, ch := range e.Chips() {
		if ch.Kind != ChipPaste {
			continue
		}
		pc := history.PastedContent{ID: ch.ID, Type: "text"}
		if utf8.RuneCountInString(ch.Text) > InlinePasteMax {
			pc.ContentHash = ch.Hash
			if pc.ContentHash == "" {
				pc.ContentHash = PasteHash(ch.Text)
			}
		} else {
			pc.Content = ch.Text
		}
		h.AddPaste(pc)
	}
	return h
}

// SetHistoryEntry replaces the content with a recalled history entry. Paste
// chips come back from pastedContents; hashed pastes are read through store
// (nil leaves their labels as plain text, as does a missing cache file).
func (e *Editor) SetHistoryEntry(h history.Entry, store PasteStore) {
	var chips []*Chip
	for _, pc := range h.Pasted() {
		if pc.Type != "" && pc.Type != "text" {
			continue
		}
		text := pc.Content
		if text == "" && pc.ContentHash != "" {
			if store == nil {
				continue
			}
			t, err := store.Get(pc.ContentHash)
			if err != nil {
				continue
			}
			text = t
		}
		ch := NewPasteChip(pc.ID, text)
		ch.Hash = pc.ContentHash
		chips = append(chips, ch)
	}
	e.SetValueWithChips(h.Display, chips)
}
