package gates

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// GlobalConfig is the read-only subset of Claude Code's global state file
// (~/.claude.json) that the gates need. mantle never writes that file: it has no lock and
// Claude Code rewrites it constantly.
type GlobalConfig struct {
	Path     string
	Exists   bool
	Projects map[string]ProjectEntry
	// APIKeyApproved and APIKeyRejected hold the last 20 characters of keys the user
	// answered in Claude Code's one-time ANTHROPIC_API_KEY prompt.
	APIKeyApproved []string
	APIKeyRejected []string
	// Err is set when the file exists but can't be read or parsed. The gates then treat
	// the file as empty (so every gate asks) and the UI reports it without touching it.
	Err error
}

// ProjectEntry is one value of the projects map, keyed by absolute project path.
type ProjectEntry struct {
	HasTrustDialogAccepted     bool
	EnabledMcpjsonServers      []string
	DisabledMcpjsonServers     []string
	EnableAllProjectMcpServers bool
}

// LoadGlobalConfig reads the global state file tolerantly: a missing file is empty, a
// malformed file sets Err, and fields of the wrong type are ignored one by one.
func LoadGlobalConfig(env Env) *GlobalConfig {
	gc := &GlobalConfig{Path: env.GlobalConfigPath(), Projects: map[string]ProjectEntry{}}
	data, err := os.ReadFile(gc.Path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			gc.Exists = true
			gc.Err = err
		}
		return gc
	}
	gc.Exists = true
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		gc.Err = fmt.Errorf("%s: %w", gc.Path, describeJSONError(data, err))
		return gc
	}
	var projects map[string]json.RawMessage
	if json.Unmarshal(top["projects"], &projects) == nil {
		for path, raw := range projects {
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil {
				continue
			}
			gc.Projects[path] = ProjectEntry{
				HasTrustDialogAccepted:     asBool(fields["hasTrustDialogAccepted"]),
				EnabledMcpjsonServers:      asStrings(fields["enabledMcpjsonServers"]),
				DisabledMcpjsonServers:     asStrings(fields["disabledMcpjsonServers"]),
				EnableAllProjectMcpServers: asBool(fields["enableAllProjectMcpServers"]),
			}
		}
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(top["customApiKeyResponses"], &keys) == nil {
		gc.APIKeyApproved = asStrings(keys["approved"])
		gc.APIKeyRejected = asStrings(keys["rejected"])
	}
	return gc
}

// trusted reports whether the projects map marks path as trusted.
func (gc *GlobalConfig) trusted(path string) bool {
	if gc == nil {
		return false
	}
	return gc.Projects[path].HasTrustDialogAccepted
}

func asBool(raw json.RawMessage) bool {
	var b bool
	return raw != nil && json.Unmarshal(raw, &b) == nil && b
}

// asStrings decodes a JSON array, keeping only its string elements.
func asStrings(raw json.RawMessage) []string {
	var items []json.RawMessage
	if raw == nil || json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var out []string
	for _, it := range items {
		var s string
		if json.Unmarshal(it, &s) == nil {
			out = append(out, s)
		}
	}
	return out
}

// describeJSONError adds a line and column to syntax errors.
func describeJSONError(data []byte, err error) error {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		line, col := lineCol(data, se.Offset)
		return fmt.Errorf("line %d, column %d: %w", line, col, err)
	}
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		line, col := lineCol(data, te.Offset)
		return fmt.Errorf("line %d, column %d: %w", line, col, err)
	}
	return err
}

func lineCol(data []byte, off int64) (line, col int) {
	line, col = 1, 1
	for i := int64(0); i < off && i < int64(len(data)); i++ {
		if data[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}
