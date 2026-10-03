package gates

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// GateStore is mantle's record of one-time gate answers (~/.mantle/state/gates.json).
// Claude Code keeps its own answers in settings and ~/.claude.json; mantle reads those
// but writes only here.
type GateStore struct {
	Version int `json:"version"`
	// BypassAcceptedAt is set once the user accepted the bypass-permissions warning.
	BypassAcceptedAt string `json:"bypassAcceptedAt,omitempty"`
	// AutoModeAcceptedAt is set once the user accepted the auto-mode first-use prompt.
	AutoModeAcceptedAt string `json:"autoModeAcceptedAt,omitempty"`
	// APIKeys maps sha256(trimmed key) to "approved" or "rejected". mantle stores a hash,
	// never the key or a slice of it.
	APIKeys map[string]string `json:"apiKeys,omitempty"`
	// Mcp holds .mcp.json server choices per project key. Claude Code keeps these in
	// local settings; mantle keeps its own until the settings writer can merge safely.
	Mcp map[string]McpChoices `json:"mcp,omitempty"`
}

// McpChoices are a project's answers to the .mcp.json approval dialog.
type McpChoices struct {
	Approved  []string `json:"approved,omitempty"`
	Rejected  []string `json:"rejected,omitempty"`
	EnableAll bool     `json:"enableAll,omitempty"`
}

// RecordMcp remembers the approval dialog's answers for cwd's project.
func RecordMcp(env Env, cwd string, approved map[string]bool, enableAll bool) error {
	_, key := projectRoots(absClean(cwd))
	if key == "" {
		key = absClean(cwd)
	}
	return updateJSON(env.GateStorePath(), func(s *GateStore) error {
		s.Version = 1
		if s.Mcp == nil {
			s.Mcp = map[string]McpChoices{}
		}
		c := s.Mcp[key]
		c.EnableAll = c.EnableAll || enableAll
		for name, ok := range approved {
			c.Approved = removeString(c.Approved, name)
			c.Rejected = removeString(c.Rejected, name)
			if ok {
				c.Approved = append(c.Approved, name)
			} else {
				c.Rejected = append(c.Rejected, name)
			}
		}
		sort.Strings(c.Approved)
		sort.Strings(c.Rejected)
		s.Mcp[key] = c
		return nil
	})
}

func removeString(list []string, s string) []string {
	var out []string
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// LoadGateStore reads mantle's gate answers; a missing or corrupt file is empty.
func LoadGateStore(env Env) *GateStore {
	var s GateStore
	if readJSON(env.GateStorePath(), &s) != nil {
		return &GateStore{}
	}
	return &s
}

// RecordBypassAccepted remembers that the bypass warning was accepted.
func RecordBypassAccepted(env Env) error {
	return updateJSON(env.GateStorePath(), func(s *GateStore) error {
		s.Version = 1
		s.BypassAcceptedAt = time.Now().UTC().Format(time.RFC3339)
		return nil
	})
}

// RecordAutoModeAccepted remembers that the auto-mode prompt was accepted.
func RecordAutoModeAccepted(env Env) error {
	return updateJSON(env.GateStorePath(), func(s *GateStore) error {
		s.Version = 1
		s.AutoModeAcceptedAt = time.Now().UTC().Format(time.RFC3339)
		return nil
	})
}

// RecordAPIKey remembers the user's answer for key.
func RecordAPIKey(env Env, key string, approved bool) error {
	return updateJSON(env.GateStorePath(), func(s *GateStore) error {
		s.Version = 1
		if s.APIKeys == nil {
			s.APIKeys = map[string]string{}
		}
		v := string(APIKeyRejected)
		if approved {
			v = string(APIKeyApproved)
		}
		s.APIKeys[keyHash(key)] = v
		return nil
	})
}

func keyHash(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}
