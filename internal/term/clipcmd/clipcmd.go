// Package clipcmd is the clipboard service for features: Copy returns a tea.Cmd that
// copies text and reports a CopiedMsg. When only OSC 52 can reach the clipboard (ssh,
// no local tool), the Cmd also writes the sequence to the terminal with tea.Raw.
//
// Features import this package (never features/chrome); internal/term/clipboard holds
// the Bubble Tea-free implementation.
package clipcmd

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/clipboard"
)

// CopiedMsg reports a finished copy. Tag is the caller's value from Copy, so several
// features can tell their results apart.
type CopiedMsg struct {
	Tag    string
	Method clipboard.Method
	Bytes  int
	Err    error
}

// Copier is the clipboard implementation; tests replace it.
var Copier = clipboard.Copier{}

// Copy copies text in the background and delivers a CopiedMsg.
func Copy(tag, text string) tea.Cmd {
	return func() tea.Msg {
		res, err := Copier.Copy(context.Background(), text)
		done := CopiedMsg{Tag: tag, Method: res.Method, Bytes: len(text), Err: err}
		if err != nil || res.Sequence == nil {
			return done
		}
		// OSC 52: write the sequence, then report.
		return tea.BatchMsg{
			tea.Raw(string(res.Sequence)),
			func() tea.Msg { return done },
		}
	}
}

// Notice is a short confirmation for a CopiedMsg, in mantle's words.
func (m CopiedMsg) Notice() string {
	switch {
	case m.Err != nil:
		return "Couldn't copy: " + m.Err.Error()
	case m.Method == clipboard.OSC52:
		return "Sent to the terminal's clipboard"
	}
	return "Copied to clipboard"
}
