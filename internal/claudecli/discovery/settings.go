package discovery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SettingsFile is one Claude Code settings file.
type SettingsFile struct {
	Scope  Scope
	Path   string
	Exists bool
	Err    error // read error, or why the file is not a JSON object
	Data   map[string]json.RawMessage
}

// Get decodes key into v and reports whether it was present and decodable.
func (s SettingsFile) Get(key string, v any) bool {
	raw, ok := s.Data[key]
	return ok && json.Unmarshal(raw, v) == nil
}

// SettingsFiles returns the settings files in increasing precedence: user,
// project, local, then managed (the policy file and its drop-in directory).
// Missing files are included with Exists false so a panel can offer to create
// them.
func SettingsFiles(r Roots) []SettingsFile {
	var files []SettingsFile
	if r.ConfigDir != "" {
		files = append(files, ReadSettings(ScopeUser, filepath.Join(r.ConfigDir, "settings.json")))
	}
	if r.CWD != "" {
		files = append(files,
			ReadSettings(ScopeProject, filepath.Join(r.CWD, ".claude", "settings.json")),
			ReadSettings(ScopeLocal, filepath.Join(r.CWD, ".claude", "settings.local.json")))
	}
	if r.ManagedDir != "" {
		files = append(files, ReadSettings(ScopeManaged, filepath.Join(r.ManagedDir, "managed-settings.json")))
		dropIns, _ := filepath.Glob(filepath.Join(r.ManagedDir, "managed-settings.d", "*.json"))
		sort.Strings(dropIns)
		for _, p := range dropIns {
			files = append(files, ReadSettings(ScopeManaged, p))
		}
	}
	return files
}

// ReadSettings reads one settings file. The CLI's -p mode silently ignores an
// invalid file, so Err is worth surfacing.
func ReadSettings(scope Scope, path string) SettingsFile {
	s := SettingsFile{Scope: scope, Path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s
	}
	s.Exists = true
	if err != nil {
		s.Err = err
		return s
	}
	s.Data, s.Err = ParseJSONObject(b)
	return s
}

// ParseJSONObject parses a JSON document that must be an object, with an error
// that names the line and column of a syntax error.
func ParseJSONObject(b []byte) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, errors.New("empty file")
	}
	var m map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&m); err != nil {
		var syn *json.SyntaxError
		if errors.As(err, &syn) {
			line, col := lineCol(b, syn.Offset)
			return nil, fmt.Errorf("line %d, column %d: %v", line, col, syn)
		}
		var typ *json.UnmarshalTypeError
		if errors.As(err, &typ) && typ.Field == "" {
			return nil, fmt.Errorf("top level must be a JSON object, not %s", typ.Value)
		}
		return nil, err
	}
	if dec.More() {
		return m, errors.New("unexpected data after the JSON object")
	}
	if m == nil {
		return nil, errors.New("top level must be a JSON object, not null")
	}
	return m, nil
}

func lineCol(b []byte, offset int64) (int, int) {
	if offset > int64(len(b)) {
		offset = int64(len(b))
	}
	line, col := 1, 1
	for _, c := range b[:offset] {
		if c == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

// Effective returns the value of key from the highest-precedence file that
// sets it, and that file's scope.
func Effective(files []SettingsFile, key string) (json.RawMessage, Scope) {
	for i := len(files) - 1; i >= 0; i-- {
		if raw, ok := files[i].Data[key]; ok {
			return raw, files[i].Scope
		}
	}
	return nil, ""
}

// effectiveBool is Effective for a boolean key, with a default.
func effectiveBool(files []SettingsFile, key string, def bool) bool {
	raw, _ := Effective(files, key)
	var v bool
	if raw == nil || json.Unmarshal(raw, &v) != nil {
		return def
	}
	return v
}

// mergedBoolMap merges an object of booleans across files, later files winning
// per key (enabledPlugins).
func mergedBoolMap(files []SettingsFile, key string) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		var m map[string]any
		if !f.Get(key, &m) {
			continue
		}
		for k, v := range m {
			switch b := v.(type) {
			case bool:
				out[k] = b
			case string:
				out[k] = strings.EqualFold(b, "true")
			}
		}
	}
	return out
}

// Visibility is a skillOverrides value: how a skill is listed to the model and
// in the / menu.
type Visibility string

const (
	VisibilityOn                Visibility = "on"
	VisibilityNameOnly          Visibility = "name-only"
	VisibilityUserInvocableOnly Visibility = "user-invocable-only"
	VisibilityOff               Visibility = "off"
)

// Visibilities is the cycle order used by the /skills panel.
var Visibilities = []Visibility{VisibilityOn, VisibilityNameOnly, VisibilityUserInvocableOnly, VisibilityOff}

// Next returns the next value in the cycle.
func (v Visibility) Next() Visibility {
	for i, x := range Visibilities {
		if x == v {
			return Visibilities[(i+1)%len(Visibilities)]
		}
	}
	return VisibilityOn
}

// SkillOverride is one skillOverrides entry and the file that sets it.
type SkillOverride struct {
	Value Visibility
	Scope Scope
	Path  string
}

// SkillOverrides merges skillOverrides across files; higher precedence wins.
func SkillOverrides(files []SettingsFile) map[string]SkillOverride {
	out := map[string]SkillOverride{}
	for _, f := range files {
		var m map[string]string
		if !f.Get("skillOverrides", &m) {
			continue
		}
		for k, v := range m {
			out[k] = SkillOverride{Value: Visibility(v), Scope: f.Scope, Path: f.Path}
		}
	}
	return out
}
