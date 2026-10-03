package gates

import (
	"crypto/sha256"
	"encoding/hex"
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
