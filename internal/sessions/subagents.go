package sessions

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SubagentMeta is the agent-<id>.meta.json written next to a subagent transcript.
type SubagentMeta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	ToolUseID   string `json:"toolUseId"` // the Agent tool call that spawned it
	SpawnDepth  int    `json:"spawnDepth"`
}

// Subagent is one subagent transcript of a session.
type Subagent struct {
	AgentID string // the <id> of agent-<id>.jsonl
	Path    string
	Meta    SubagentMeta
	HasMeta bool
}

// Load parses the subagent's transcript.
func (s Subagent) Load() (*Transcript, error) { return Load(s.Path) }

// ListSubagents lists the subagent transcripts in a session's subagents dir (see
// SubagentsDir), sorted by file name. A missing dir is not an error.
func ListSubagents(dir string) ([]Subagent, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Subagent
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		base := strings.TrimSuffix(name, ".jsonl")
		s := Subagent{AgentID: strings.TrimPrefix(base, "agent-"), Path: filepath.Join(dir, name)}
		if b, err := os.ReadFile(filepath.Join(dir, base+".meta.json")); err == nil {
			if json.Unmarshal(b, &s.Meta) == nil {
				s.HasMeta = true
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// SubagentsByToolUse maps the spawning tool_use id to each subagent that records one.
func SubagentsByToolUse(dir string) (map[string]Subagent, error) {
	subs, err := ListSubagents(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Subagent, len(subs))
	for _, s := range subs {
		if s.Meta.ToolUseID != "" {
			out[s.Meta.ToolUseID] = s
		}
	}
	return out, nil
}

// Sidechains groups a transcript's inline sidechain entries by agent id, in file order.
// Older engines stored subagent messages in the main file this way.
func Sidechains(entries []*Entry) map[string][]*Entry {
	out := map[string][]*Entry{}
	for _, e := range entries {
		if e.IsSidechain {
			out[e.AgentID] = append(out[e.AgentID], e)
		}
	}
	return out
}
