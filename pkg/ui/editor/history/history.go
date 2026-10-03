// Package history reads and appends Claude Code's prompt history,
// ~/.claude/history.jsonl, so mantle and Claude Code share it.
//
// Each line is one JSON object:
//
//	{"display":"…","pastedContents":{"1":{"id":1,"type":"text","content":"…"}},
//	 "timestamp":1759…,"project":"/path/to/cwd","sessionId":"…"}
//
// Pastes longer than 1024 characters are stored in paste-cache and referenced
// by "contentHash" instead of "content". Images are not stored.
package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// Entry is one history line.
type Entry struct {
	Display        string                   `json:"display"`
	PastedContents map[string]PastedContent `json:"pastedContents"`
	Timestamp      int64                    `json:"timestamp"` // Unix milliseconds
	Project        string                   `json:"project"`
	SessionID      string                   `json:"sessionId,omitempty"`
}

// PastedContent is a paste referenced from Display as "[Pasted text #ID …]".
type PastedContent struct {
	ID          int    `json:"id"`
	Type        string `json:"type"`
	Content     string `json:"content,omitempty"`
	ContentHash string `json:"contentHash,omitempty"`
	MediaType   string `json:"mediaType,omitempty"`
	Filename    string `json:"filename,omitempty"`
}

// Pasted returns the pasted contents sorted by ID.
func (e Entry) Pasted() []PastedContent {
	out := make([]PastedContent, 0, len(e.PastedContents))
	for _, p := range e.PastedContents {
		out = append(out, p)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// AddPaste records a pasted text under its ID.
func (e *Entry) AddPaste(p PastedContent) {
	if e.PastedContents == nil {
		e.PastedContents = map[string]PastedContent{}
	}
	e.PastedContents[strconv.Itoa(p.ID)] = p
}

// ConfigDir is Claude Code's config directory: $CLAUDE_CONFIG_DIR, else
// ~/.claude.
func ConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// Path is the history file.
func Path() string { return filepath.Join(ConfigDir(), "history.jsonl") }

// PasteCacheDir is where long pastes are stored.
func PasteCacheDir() string { return filepath.Join(ConfigDir(), "paste-cache") }

// Load reads every entry, oldest first. A missing file is not an error;
// malformed lines are skipped.
func Load(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// Read parses history lines from r, skipping malformed ones.
func Read(r io.Reader) ([]Entry, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	var out []Entry
	for {
		line, err := br.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			var e Entry
			if json.Unmarshal(line, &e) == nil && e.Display != "" {
				out = append(out, e)
			}
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}

// Filter returns the entries for which keep is true, keeping order.
func Filter(entries []Entry, keep func(Entry) bool) []Entry {
	var out []Entry
	for _, e := range entries {
		if keep(e) {
			out = append(out, e)
		}
	}
	return out
}

// ForProject keeps entries recorded in project (a working directory).
func ForProject(entries []Entry, project string) []Entry {
	return Filter(entries, func(e Entry) bool { return e.Project == project })
}

// ForSession keeps entries from one session.
func ForSession(entries []Entry, sessionID string) []Entry {
	return Filter(entries, func(e Entry) bool { return e.SessionID == sessionID })
}

// Encode returns the entry as one JSON line (with the trailing newline),
// formatted like Claude Code writes it: no HTML escaping, and an empty
// pastedContents object rather than null.
func Encode(e Entry) ([]byte, error) {
	if e.PastedContents == nil {
		e.PastedContents = map[string]PastedContent{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Append adds one entry to the history file with a single O_APPEND write,
// so concurrent writers (Claude Code, other mantle sessions) never
// interleave within a line.
func Append(path string, e Entry) error {
	line, err := Encode(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(line)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// Navigator walks history newest first for up/down recall. Consecutive
// duplicates are collapsed. Position -1 is the user's draft.
type Navigator struct {
	items []Entry // newest first
	pos   int
}

// NewNavigator builds a navigator over entries given oldest first (as Load
// returns them).
func NewNavigator(entries []Entry) *Navigator {
	n := &Navigator{pos: -1}
	for i := len(entries) - 1; i >= 0; i-- {
		n.push(entries[i])
	}
	return n
}

func (n *Navigator) push(e Entry) {
	if k := len(n.items); k > 0 && n.items[k-1].Display == e.Display {
		return
	}
	n.items = append(n.items, e)
}

// Len is the number of distinct entries.
func (n *Navigator) Len() int { return len(n.items) }

// AtDraft reports whether the navigator is at the draft (not recalling).
// Hosts save their draft before the first Older call from here.
func (n *Navigator) AtDraft() bool { return n.pos < 0 }

// Pos is the recall position: -1 for the draft, 0 for the newest entry.
func (n *Navigator) Pos() int { return n.pos }

// Older moves to the next older entry.
func (n *Navigator) Older() (Entry, bool) {
	if n.pos+1 >= len(n.items) {
		return Entry{}, false
	}
	n.pos++
	return n.items[n.pos], true
}

// Newer moves to the next newer entry. When it moves past the newest entry
// it returns draft=true: the host restores its saved draft.
func (n *Navigator) Newer() (e Entry, ok, draft bool) {
	switch {
	case n.pos < 0:
		return Entry{}, false, false
	case n.pos == 0:
		n.pos = -1
		return Entry{}, true, true
	}
	n.pos--
	return n.items[n.pos], true, false
}

// Reset returns to the draft position.
func (n *Navigator) Reset() { n.pos = -1 }

// Add records a just-submitted prompt as the newest entry and resets.
func (n *Navigator) Add(e Entry) {
	n.pos = -1
	if len(n.items) > 0 && n.items[0].Display == e.Display {
		n.items[0] = e
		return
	}
	n.items = append([]Entry{e}, n.items...)
}
