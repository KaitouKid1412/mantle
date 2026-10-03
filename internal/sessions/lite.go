package sessions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// liteChunk is how much of each end of a transcript the index reads. The engine
// re-stamps title, last-prompt and similar metadata near the end of the file, so the
// tail has them; the head has the envelope and the first prompt.
const liteChunk = 64 << 10

type headTail struct {
	head, tail []byte
	size       int64
	mtime      time.Time
}

func readHeadTail(path string) (headTail, error) {
	f, err := os.Open(path)
	if err != nil {
		return headTail{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return headTail{}, err
	}
	if !fi.Mode().IsRegular() {
		return headTail{}, fmt.Errorf("%s: not a regular file", path)
	}
	ht := headTail{size: fi.Size(), mtime: fi.ModTime()}
	ht.head = make([]byte, min(ht.size, liteChunk))
	n, err := io.ReadFull(f, ht.head)
	ht.head = ht.head[:n]
	if err != nil && err != io.ErrUnexpectedEOF {
		return ht, err
	}
	if ht.size <= liteChunk {
		ht.tail = ht.head
		return ht, nil
	}
	ht.tail = make([]byte, liteChunk)
	n, err = f.ReadAt(ht.tail, ht.size-liteChunk)
	ht.tail = ht.tail[:n]
	if err != nil && err != io.EOF {
		return ht, err
	}
	return ht, nil
}

// headLines splits a head chunk into lines, dropping a final line that the chunk cut.
// If the cut line is the only one, it is kept so field extraction can still try it.
func (ht headTail) headLines() [][]byte {
	lines := bytes.Split(ht.head, []byte{'\n'})
	if int64(len(ht.head)) < ht.size && len(lines) > 1 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// tailLines splits a tail chunk into lines, dropping a first line that the chunk cut.
func (ht headTail) tailLines() [][]byte {
	lines := bytes.Split(ht.tail, []byte{'\n'})
	if int64(len(ht.tail)) < ht.size && len(lines) > 1 {
		lines = lines[1:]
	}
	return lines
}

var (
	metaPrefix     = []byte(`{"type":"`)
	userMarker     = []byte(`"type":"user"`)
	assistMarker   = []byte(`"type":"assistant"`)
	toolResMarker  = []byte(`"tool_result"`)
	isMetaMarker   = []byte(`"isMeta":true`)
	compactMarker  = []byte(`"isCompactSummary":true`)
	turnDurMarker  = []byte(`"subtype":"turn_duration"`)
	maxMetaLineLen = 16 << 10
)

// isMetaLine is a cheap test for a metadata record: the engine writes "type" first for
// those, and message records start with parentUuid.
func isMetaLine(l []byte) bool {
	l = trimLine(l)
	return len(l) <= maxMetaLineLen && bytes.HasPrefix(l, metaPrefix)
}

// SessionMeta is what the session picker needs about one transcript, read from the head
// and tail of the file only.
type SessionMeta struct {
	ID          string    `json:"id"`
	Path        string    `json:"path"`
	ProjectDir  string    `json:"projectDir"`
	Cwd         string    `json:"cwd,omitempty"` // the relocated cwd if the session moved
	GitBranch   string    `json:"gitBranch,omitempty"`
	Version     string    `json:"version,omitempty"`
	Entrypoint  string    `json:"entrypoint,omitempty"`
	SessionKind string    `json:"sessionKind,omitempty"`
	Created     time.Time `json:"created"`    // first entry's timestamp
	LastActive  time.Time `json:"lastActive"` // last entry's timestamp
	Modified    time.Time `json:"modified"`   // file mtime
	Size        int64     `json:"size"`

	FirstPrompt    string `json:"firstPrompt,omitempty"`
	LastPrompt     string `json:"lastPrompt,omitempty"`
	LeafUUID       string `json:"leafUuid,omitempty"`
	AITitle        string `json:"aiTitle,omitempty"`
	CustomTitle    string `json:"customTitle,omitempty"`
	AgentName      string `json:"agentName,omitempty"`
	Summary        string `json:"summary,omitempty"`
	Tag            string `json:"tag,omitempty"`
	Mode           string `json:"mode,omitempty"`
	PermissionMode string `json:"permissionMode,omitempty"`
	PRNumber       int    `json:"prNumber,omitempty"`
	PRURL          string `json:"prUrl,omitempty"`
	PRRepository   string `json:"prRepository,omitempty"`
	InWorktree     bool   `json:"inWorktree,omitempty"`

	// MessageCount is the message count from the last turn_duration record (0 if none in
	// the tail).
	MessageCount int         `json:"messageCount,omitempty"`
	Cost         *CostTotals `json:"cost,omitempty"`
	// HasMessages is false for files holding only metadata.
	HasMessages bool `json:"hasMessages"`
	// Hidden marks SDK and daemon sessions, which Claude Code's own picker hides.
	Hidden bool `json:"hidden,omitempty"`
}

// Title is the display title: custom title, generated title, agent name, legacy
// summary, first prompt, last prompt.
func (m SessionMeta) Title() string {
	for _, s := range []string{m.CustomTitle, m.AITitle, m.AgentName, m.Summary, m.FirstPrompt, m.LastPrompt} {
		if s != "" {
			return s
		}
	}
	return ""
}

// programmaticEntrypoints are entrypoints Claude Code's picker hides.
var programmaticEntrypoints = map[string]bool{"sdk-cli": true, "sdk-ts": true, "sdk-py": true}

// ReadMeta reads a session's metadata from the head and tail of its file.
func ReadMeta(path string) (SessionMeta, error) {
	ht, err := readHeadTail(path)
	if err != nil {
		return SessionMeta{}, err
	}
	return metaFrom(path, ht), nil
}

func metaFrom(path string, ht headTail) SessionMeta {
	m := SessionMeta{ID: SessionIDFromPath(path), Path: path, Size: ht.size, Modified: ht.mtime}
	var md Metadata

	// Head: envelope from the first main-chain entry, the first prompt, and any metadata
	// records written up front by older engines.
	var envelope *Entry
	cmdFallback := ""
	for _, l := range ht.headLines() {
		if isMetaLine(l) {
			if r, err := Decode(l, Pos{}); err == nil {
				md.Apply(r)
			}
			continue
		}
		needPrompt := m.FirstPrompt == "" && bytes.Contains(l, userMarker) &&
			!bytes.Contains(l, toolResMarker) && !bytes.Contains(l, isMetaMarker) &&
			!bytes.Contains(l, compactMarker)
		if envelope != nil && !needPrompt {
			continue
		}
		r, err := Decode(l, Pos{})
		if err != nil {
			continue
		}
		e, ok := r.(*Entry)
		if !ok || e.IsSidechain {
			continue
		}
		if envelope == nil {
			envelope = e
		}
		if needPrompt {
			if p, c, ok := PromptPreview(e); ok {
				m.FirstPrompt = p
			} else if cmdFallback == "" {
				cmdFallback = c
			}
		}
	}
	if m.FirstPrompt == "" {
		m.FirstPrompt = cmdFallback
	}
	if envelope != nil {
		m.Cwd, m.GitBranch, m.Version = envelope.Cwd, envelope.GitBranch, envelope.Version
		m.Entrypoint, m.SessionKind, m.Created = envelope.Entrypoint, envelope.SessionKind, envelope.Timestamp
		if envelope.SessionID != "" {
			m.ID = envelope.SessionID
		}
	} else if len(ht.head) > 0 {
		// The first line is longer than the chunk: pick fields out of the raw text.
		m.Cwd = firstField(ht.head, "cwd")
		m.GitBranch = firstField(ht.head, "gitBranch")
		m.Version = firstField(ht.head, "version")
		m.Entrypoint = firstField(ht.head, "entrypoint")
		m.Created = parseTime(firstField(ht.head, "timestamp"))
	}

	// Tail: metadata in file order (last wins), the last entry, the last turn_duration.
	lines := ht.tailLines()
	for _, l := range lines {
		if isMetaLine(l) {
			if r, err := Decode(l, Pos{}); err == nil {
				md.Apply(r)
				if _, ok := r.(*WorktreeState); ok {
					m.InWorktree = true
				}
			}
		}
	}
	var last *Entry
	for i := len(lines) - 1; i >= 0 && (last == nil || m.MessageCount == 0); i-- {
		l := lines[i]
		isTurn := m.MessageCount == 0 && bytes.Contains(l, turnDurMarker)
		if (last != nil && !isTurn) || isMetaLine(l) || len(trimLine(l)) == 0 {
			continue
		}
		r, err := Decode(l, Pos{})
		if err != nil {
			continue
		}
		e, ok := r.(*Entry)
		if !ok || e.IsSidechain {
			continue
		}
		if last == nil {
			last = e
		}
		if e.Subtype == "turn_duration" && m.MessageCount == 0 {
			m.MessageCount = e.MessageCount
		}
	}
	if last != nil {
		m.LastActive = last.Timestamp
		if last.GitBranch != "" {
			m.GitBranch = last.GitBranch
		}
		if m.Cwd == "" {
			m.Cwd = last.Cwd
		}
	}

	m.LastPrompt, m.LeafUUID = md.LastPrompt, md.LeafUUID
	m.AITitle, m.CustomTitle, m.AgentName, m.Summary = md.AITitle, md.CustomTitle, md.AgentName, md.Summary
	m.Tag, m.Mode, m.PermissionMode = md.Tag, md.Mode, md.PermissionMode
	if md.RelocatedCwd != "" {
		m.Cwd = md.RelocatedCwd
	}
	if md.PR != nil {
		m.PRNumber, m.PRURL, m.PRRepository = md.PR.PRNumber, md.PR.PRURL, md.PR.PRRepository
	}
	if md.Cost != nil {
		t := md.Cost.Totals()
		m.Cost = &t
	}
	if m.LastPrompt != "" {
		m.LastPrompt = truncate(oneLine(m.LastPrompt), MaxPromptPreview)
	}
	if m.Created.IsZero() {
		m.Created = m.Modified
	}
	if m.LastActive.IsZero() {
		m.LastActive = m.Modified
	}
	m.HasMessages = ht.size > 2*liteChunk ||
		bytes.Contains(ht.head, userMarker) || bytes.Contains(ht.head, assistMarker) ||
		bytes.Contains(ht.tail, userMarker) || bytes.Contains(ht.tail, assistMarker)
	m.Hidden = programmaticEntrypoints[m.Entrypoint] || m.SessionKind == "daemon" || m.SessionKind == "daemon-worker"
	return m
}

func oneLine(s string) string {
	return string(bytes.TrimSpace(bytes.ReplaceAll([]byte(s), []byte{'\n'}, []byte{' '})))
}

// firstField returns the first string value of "key" in text, found by scanning the raw
// JSON. It is for text that does not parse (a line cut by the chunk), so it may also
// match a nested key.
func firstField(text []byte, key string) string {
	for _, pat := range [][]byte{[]byte(`"` + key + `":"`), []byte(`"` + key + `": "`)} {
		i := bytes.Index(text, pat)
		if i < 0 {
			continue
		}
		if s, ok := jsonStringAt(text, i+len(pat)-1); ok {
			return s
		}
	}
	return ""
}

// lastFieldOfType returns key's string value from the last complete line of text whose
// record type is typ.
func lastFieldOfType(text []byte, typ, key string) string {
	lines := bytes.Split(text, []byte{'\n'})
	typePat := []byte(`"type":"` + typ + `"`)
	keyPat := []byte(`"` + key + `":`)
	for i := len(lines) - 1; i >= 0; i-- {
		l := trimLine(lines[i])
		if !bytes.Contains(l, typePat) || !bytes.Contains(l, keyPat) {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(l, &obj) != nil || rawString(obj["type"]) != typ {
			continue
		}
		if s := rawString(obj[key]); s != "" {
			return s
		}
	}
	return ""
}

// jsonStringAt decodes the JSON string starting at text[i] (a '"').
func jsonStringAt(text []byte, i int) (string, bool) {
	for j := i + 1; j < len(text); j++ {
		switch text[j] {
		case '\\':
			j++
		case '"':
			var s string
			if json.Unmarshal(text[i:j+1], &s) != nil {
				return "", false
			}
			return s, true
		}
	}
	return "", false
}
