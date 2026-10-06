package sessions

import (
	"bufio"
	"encoding/json"
	"os"
)

// Persisted reports whether `claude --resume <id>` run in cwd can load session id: one of
// cwd's project dirs holds its transcript with at least one message. The headless engine
// writes a transcript only after the session's first prompt, so a fresh session is not
// resumable yet, and a transcript holding only metadata (a title set before the first
// prompt) is not either. Restarts of the current session must check this first.
func (l Layout) Persisted(cwd, id string) bool {
	if !ValidID(id) || cwd == "" {
		return false
	}
	for _, dir := range l.ProjectDirs(cwd) {
		if hasMessage(SessionFile(dir, id)) {
			return true
		}
	}
	return false
}

// hasMessage reports whether the transcript at path has a user or assistant record. It
// stops at the first one, which is near the top of any real transcript.
func hasMessage(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var rec struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(line, &rec) == nil && (rec.Type == "user" || rec.Type == "assistant") {
				return true
			}
		}
		if err != nil { // io.EOF or a read error: no message found
			return false
		}
	}
}
