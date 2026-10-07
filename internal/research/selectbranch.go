package research

import (
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/KaitouKid1412/mantle/internal/sessions"
)

// The engine resumes a session on one chain: the branch named by the newest
// last-prompt record's leafUuid (or the newest entry, when that descends from it), and
// --resume-session-at only finds messages on that chain. A follow-up to a node on
// another branch therefore needs that branch selected first. The engine records an
// explicit choice itself the same way (a rewind writes a last-prompt record with
// "explicit": true), so SelectBranch appends one such record. It is the only write
// mantle makes to a session file; call it only while the engine is idle, just before
// restarting it.

// ErrNoEntry is returned when the entry to select is not in the session file.
var ErrNoEntry = errors.New("research: entry not in the session file")

// SelectBranch makes session file path resume on the branch ending at entry at. It
// appends a last-prompt record whose leafUuid is the nearest message (user or
// assistant entry) at or above at, and returns that message's uuid: the entry to
// resume at (the engine's chain ends at a message, so a trailing attachment is not on
// it). prompt is the record's lastPrompt text (the picker's preview).
func SelectBranch(path, at, prompt string) (string, error) {
	tr, err := sessions.Load(path)
	if err != nil {
		return "", err
	}
	anchor := ""
	for n := tr.Tree.Node(at); n != nil; n = n.Parent {
		if k := n.Entry.Kind(); k == sessions.KindUser || k == sessions.KindAssistant {
			anchor = n.Entry.UUID
			break
		}
	}
	if anchor == "" {
		return "", ErrNoEntry
	}
	rec, err := json.Marshal(struct {
		Type       string `json:"type"`
		LastPrompt string `json:"lastPrompt"`
		LeafUUID   string `json:"leafUuid"`
		Explicit   bool   `json:"explicit"`
		SessionID  string `json:"sessionId"`
	}{sessions.KindLastPrompt, prompt, anchor, true, sessions.SessionIDFromPath(path)})
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	// One write, newline-terminated; start a new line if the file does not end in one.
	if fi, err := f.Stat(); err == nil && fi.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, fi.Size()-1); err != nil && err != io.EOF {
			return "", err
		}
		if last[0] != '\n' {
			rec = append([]byte{'\n'}, rec...)
		}
	}
	if _, err := f.Write(append(rec, '\n')); err != nil {
		return "", err
	}
	return anchor, f.Close()
}
