package gates

import "strings"

// APIKeyStatus is the state of the ANTHROPIC_API_KEY approval.
type APIKeyStatus string

const (
	APIKeyNone     APIKeyStatus = "none" // no key in the environment
	APIKeyNew      APIKeyStatus = "new"  // ask the user
	APIKeyApproved APIKeyStatus = "approved"
	APIKeyRejected APIKeyStatus = "rejected"
)

// APIKeyState is the result of the API-key gate.
type APIKeyState struct {
	Status APIKeyStatus
	// Suffix is the key's last 20 characters, the form Claude Code shows and records.
	Suffix string
	// key is kept for RecordAPIKey and never leaves the package.
	key string
}

// Key returns the key itself, for recording the user's answer.
func (s APIKeyState) Key() string { return s.key }

// APIKeyApproval decides whether the one-time ANTHROPIC_API_KEY prompt is needed.
// Headless claude uses the key without asking, so mantle asks instead. An answer given in
// Claude Code (~/.claude.json customApiKeyResponses, read-only) or in mantle counts. gc
// and store may be nil.
func APIKeyApproval(env Env, gc *GlobalConfig, store *GateStore) APIKeyState {
	key := strings.TrimSpace(env.getenv("ANTHROPIC_API_KEY"))
	if key == "" {
		return APIKeyState{Status: APIKeyNone}
	}
	st := APIKeyState{Status: APIKeyNew, Suffix: keySuffix(key), key: key}
	if store != nil {
		switch APIKeyStatus(store.APIKeys[keyHash(key)]) {
		case APIKeyApproved:
			st.Status = APIKeyApproved
			return st
		case APIKeyRejected:
			st.Status = APIKeyRejected
			return st
		}
	}
	if gc != nil {
		for _, s := range gc.APIKeyApproved {
			if s == st.Suffix {
				st.Status = APIKeyApproved
				return st
			}
		}
		for _, s := range gc.APIKeyRejected {
			if s == st.Suffix {
				st.Status = APIKeyRejected
				return st
			}
		}
	}
	return st
}

// EngineEnvUnset lists the environment variables to remove from the engine's environment
// for this state: a rejected key must not reach the engine, which would otherwise use it.
func (s APIKeyState) EngineEnvUnset() []string {
	if s.Status == APIKeyRejected {
		return []string{"ANTHROPIC_API_KEY"}
	}
	return nil
}

func keySuffix(key string) string {
	if len(key) <= 20 {
		return key
	}
	return key[len(key)-20:]
}
