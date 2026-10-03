package gates

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

// Scope names a Claude Code settings source, as the engine names it in PermissionUpdate
// destinations and settings listings.
type Scope string

const (
	ScopeUser    Scope = "userSettings"
	ScopeProject Scope = "projectSettings"
	ScopeLocal   Scope = "localSettings"
	ScopeFlag    Scope = "flagSettings"
	ScopePolicy  Scope = "policySettings"
)

// SettingsFile is one parsed settings source.
type SettingsFile struct {
	Scope  Scope
	Path   string // "" for inline --settings JSON
	Exists bool
	Data   map[string]any
	// Err is a read, syntax or shape error. Headless claude silently ignores such a file,
	// so mantle reports it (PD-37).
	Err error
}

// Layers holds the settings sources in increasing precedence: user, project, local,
// flag, policy. Plan 01's config reader is the general-purpose one; this reader only
// serves the gates, which must run before anything else is set up.
type Layers []SettingsFile

// LoadSettings reads every settings scope for cwd. flagSettings is the value of
// --settings (inline JSON or a path, relative to cwd).
func LoadSettings(env Env, cwd, flagSettings string) Layers {
	l := Layers{
		readSettingsFile(ScopeUser, env.UserSettingsPath()),
		readSettingsFile(ScopeProject, filepath.Join(cwd, ".claude", "settings.json")),
		readSettingsFile(ScopeLocal, filepath.Join(cwd, ".claude", "settings.local.json")),
	}
	if fs := strings.TrimSpace(flagSettings); fs != "" {
		if strings.HasPrefix(fs, "{") {
			f := SettingsFile{Scope: ScopeFlag, Exists: true}
			f.Data, f.Err = parseSettings([]byte(fs))
			l = append(l, f)
		} else {
			p := fs
			if !filepath.IsAbs(p) {
				p = filepath.Join(cwd, p)
			}
			l = append(l, readSettingsFile(ScopeFlag, p))
		}
	}
	l = append(l, readManaged(env.managedDir()))
	return l
}

func readSettingsFile(scope Scope, path string) SettingsFile {
	f := SettingsFile{Scope: scope, Path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			f.Exists = true
			f.Err = err
		}
		return f
	}
	f.Exists = true
	f.Data, f.Err = parseSettings(data)
	return f
}

// readManaged merges managed-settings.json with the managed-settings.d drop-ins
// (alphabetical, later files win per key).
func readManaged(dir string) SettingsFile {
	base := readSettingsFile(ScopePolicy, filepath.Join(dir, "managed-settings.json"))
	entries, err := os.ReadDir(filepath.Join(dir, "managed-settings.d"))
	if err != nil {
		return base
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		d := readSettingsFile(ScopePolicy, filepath.Join(dir, "managed-settings.d", n))
		if d.Err != nil {
			if base.Err == nil {
				base.Err = d.Err
			}
			continue
		}
		if base.Data == nil {
			base.Data = map[string]any{}
		}
		for k, v := range d.Data {
			base.Data[k] = v
		}
		base.Exists = true
	}
	return base
}

func parseSettings(data []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, describeJSONError(data, err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("settings must be a JSON object, got %s", jsonKind(v))
	}
	return m, nil
}

func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case []any:
		return "an array"
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	}
	return "a value"
}

// Problems returns the sources that exist but couldn't be used.
func (l Layers) Problems() []SettingsFile {
	var out []SettingsFile
	for _, f := range l {
		if f.Err != nil {
			out = append(out, f)
		}
	}
	return out
}

// Scope returns the layer for s, if loaded.
func (l Layers) Scope(s Scope) (SettingsFile, bool) {
	for _, f := range l {
		if f.Scope == s {
			return f, true
		}
	}
	return SettingsFile{}, false
}

// Lookup returns the highest-precedence value at a dotted key path ("permissions.defaultMode"),
// considering only the given scopes (all scopes when none are given).
func (l Layers) Lookup(key string, scopes ...Scope) (any, Scope, bool) {
	for i := len(l) - 1; i >= 0; i-- {
		f := l[i]
		if f.Data == nil || !scopeAllowed(f.Scope, scopes) {
			continue
		}
		if v, ok := dig(f.Data, key); ok {
			return v, f.Scope, true
		}
	}
	return nil, "", false
}

// Bool returns the highest-precedence boolean at key; non-booleans are ignored.
func (l Layers) Bool(key string, scopes ...Scope) bool {
	for i := len(l) - 1; i >= 0; i-- {
		f := l[i]
		if f.Data == nil || !scopeAllowed(f.Scope, scopes) {
			continue
		}
		if v, ok := dig(f.Data, key); ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
	}
	return false
}

// String returns the highest-precedence string at key.
func (l Layers) String(key string, scopes ...Scope) string {
	for i := len(l) - 1; i >= 0; i-- {
		f := l[i]
		if f.Data == nil || !scopeAllowed(f.Scope, scopes) {
			continue
		}
		if v, ok := dig(f.Data, key); ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

// Strings returns the union of string arrays at key across scopes (arrays merge across
// settings scopes), in precedence order without duplicates.
func (l Layers) Strings(key string, scopes ...Scope) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range l {
		if f.Data == nil || !scopeAllowed(f.Scope, scopes) {
			continue
		}
		v, ok := dig(f.Data, key)
		if !ok {
			continue
		}
		arr, _ := v.([]any)
		for _, it := range arr {
			if s, ok := it.(string); ok && !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

func scopeAllowed(s Scope, scopes []Scope) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, x := range scopes {
		if x == s {
			return true
		}
	}
	return false
}

func dig(m map[string]any, key string) (any, bool) {
	var cur any = m
	for _, part := range strings.Split(key, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// userScopes are the settings sources a repository can't ship (local settings live in
// the repo and can be committed). One-time warnings are only suppressed from these, so a
// cloned repo can't silence them for itself.
var userScopes = []Scope{ScopeUser, ScopeFlag, ScopePolicy}
