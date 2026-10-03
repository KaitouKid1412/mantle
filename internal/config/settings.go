package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Snapshot is an immutable view of every settings source, read in one go. Build it off
// the UI goroutine (Load) and swap it in on the UI goroutine (Store.Apply).
type Snapshot struct {
	Sources   []ext.SettingsSource      // every scope, highest precedence first
	Scopes    map[string]map[string]any // scope → raw document
	Merged    map[string]any            // Claude Code settings with precedence applied
	Global    map[string]any            // ~/.claude.json, read-only (nil if unreadable)
	Mantle    map[string]any            // ~/.mantle/settings.json
	MantleErr error
}

// Load reads every scope. flag is the --settings value: inline JSON or a file path
// ("" = none).
func Load(p Paths, flag string) *Snapshot {
	s := &Snapshot{Scopes: map[string]map[string]any{}}
	for _, scope := range ext.SettingsScopes { // highest first
		src, doc := readScope(p, scope, flag)
		s.Sources = append(s.Sources, src)
		if doc != nil {
			s.Scopes[scope] = doc
		}
	}
	// Merge lowest precedence first so later scopes override.
	s.Merged = map[string]any{}
	for i := len(ext.SettingsScopes) - 1; i >= 0; i-- {
		if doc := s.Scopes[ext.SettingsScopes[i]]; doc != nil {
			mergeInto(s.Merged, doc)
		}
	}
	if raw, err := os.ReadFile(p.ClaudeJSON); err == nil {
		var g map[string]any
		if json.Unmarshal(raw, &g) == nil {
			s.Global = g
		}
	}
	s.Mantle = map[string]any{}
	if raw, err := os.ReadFile(p.MantleSettings()); err == nil {
		doc, _, err := Decode(raw)
		if err != nil {
			s.MantleErr = err
		} else {
			s.Mantle = normalize(doc).(map[string]any)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		s.MantleErr = err
	}
	return s
}

func readScope(p Paths, scope, flag string) (ext.SettingsSource, map[string]any) {
	src := ext.SettingsSource{Scope: scope, Path: p.ScopeFile(scope)}
	var raw []byte
	switch {
	case scope == ext.ScopeFlag:
		f := strings.TrimSpace(flag)
		if f == "" {
			return src, nil
		}
		if strings.HasPrefix(f, "{") {
			raw, src.Exists = []byte(f), true
		} else {
			src.Path = f
			b, err := os.ReadFile(f)
			if err != nil {
				src.Err = err
				return src, nil
			}
			raw, src.Exists = b, true
		}
	case scope == ext.ScopePolicy:
		doc := map[string]any{}
		found := false
		for _, path := range append([]string{p.Managed}, p.ManagedDropIns()...) {
			b, err := os.ReadFile(path)
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) && src.Err == nil {
					src.Err = err
				}
				continue
			}
			d, _, err := Decode(b)
			if err != nil {
				src.Err = err
				continue
			}
			found = true
			mergeInto(doc, normalize(d).(map[string]any))
		}
		src.Exists = found
		if !found {
			return src, nil
		}
		return src, doc
	default:
		if src.Path == "" {
			return src, nil
		}
		b, err := os.ReadFile(src.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return src, nil
		}
		src.Exists = true
		if err != nil {
			src.Err = err
			return src, nil
		}
		raw = b
	}
	doc, _, err := Decode(raw)
	if err != nil {
		src.Err = err // Claude Code ignores an invalid scope; so do we
		return src, nil
	}
	return src, normalize(doc).(map[string]any)
}

// normalize turns json.Number into float64 (or int-valued float64), matching
// encoding/json's default so values compare and type-switch the usual way.
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = normalize(x)
		}
		return t
	case []any:
		for i, x := range t {
			t[i] = normalize(x)
		}
		return t
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return t.String()
		}
		return f
	}
	return v
}

// mergeKeys are merged across scopes instead of replaced.
var mergeKeys = map[string]bool{"permissions": true, "hooks": true, "env": true, "enabledPlugins": true, "extraKnownMarketplaces": true}

// mergeInto applies src over dst with Claude Code's precedence rules: permission rule
// lists and hooks accumulate across scopes; env and plugin maps merge key by key;
// everything else is replaced by the higher scope.
func mergeInto(dst, src map[string]any) {
	for k, v := range src {
		if !mergeKeys[k] {
			dst[k] = deepCopyAny(v)
			continue
		}
		cur, ok := dst[k].(map[string]any)
		in, ok2 := v.(map[string]any)
		if !ok || !ok2 {
			dst[k] = deepCopyAny(v)
			continue
		}
		merged := deepCopy(cur)
		for kk, vv := range in {
			a, isArr := merged[kk].([]any)
			b, isArr2 := vv.([]any)
			if (k == "permissions" || k == "hooks") && isArr && isArr2 {
				merged[kk] = unionAppend(a, b)
				continue
			}
			merged[kk] = deepCopyAny(vv)
		}
		dst[k] = merged
	}
}

func unionAppend(a, b []any) []any {
	out := slices.Clone(a)
	for _, x := range b {
		dup := false
		for _, y := range out {
			if reflect.DeepEqual(x, y) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, deepCopyAny(x))
		}
	}
	return out
}

// Store implements ext.Settings, ext.ScopedSettings and ext.ClaudeSettingsWriter over
// a Snapshot. Use it on the UI goroutine only; file I/O happens in the Cmds it returns.
type Store struct {
	paths Paths
	flag  string
	snap  *Snapshot
	specs map[string]ext.SettingSpec
}

var (
	_ ext.ScopedSettings       = (*Store)(nil)
	_ ext.ClaudeSettingsWriter = (*Store)(nil)
)

// NewStore loads every source.
func NewStore(p Paths, flagSettings string) *Store {
	return &Store{paths: p, flag: flagSettings, snap: Load(p, flagSettings), specs: map[string]ext.SettingSpec{}}
}

// Paths returns the store's paths.
func (s *Store) Paths() Paths { return s.paths }

// Snapshot returns the current snapshot (read-only).
func (s *Store) Snapshot() *Snapshot { return s.snap }

// SetSpecs registers mantle setting specs (defaults for Mantle()).
func (s *Store) SetSpecs(specs []ext.SettingSpec) {
	for _, sp := range specs {
		s.specs[sp.Key] = sp
	}
}

// globalFallback are UI keys that Claude Code's /config stores in ~/.claude.json;
// they are read from there when no settings scope sets them.
var globalFallback = map[string]bool{
	"theme": true, "verbose": true, "editorMode": true, "preferredNotifChannel": true,
	"autoCompactEnabled": true, "todoFeatureEnabled": true, "messageIdleNotifThresholdMs": true,
	"autoConnectIde": true, "diffTool": true, "copyFullResponse": true, "copyOnSelect": true,
	"externalEditorContext": true,
}

// Claude returns a merged Claude Code setting.
func (s *Store) Claude(key string) (any, bool) {
	if v, ok := s.snap.Merged[key]; ok {
		return v, true
	}
	if globalFallback[key] && s.snap.Global != nil {
		if v, ok := s.snap.Global[key]; ok {
			return v, true
		}
	}
	return nil, false
}

// Global reads a key from ~/.claude.json (read-only).
func (s *Store) Global(key string) (any, bool) {
	v, ok := s.snap.Global[key]
	return v, ok
}

// Mantle returns a mantle setting or its spec default.
func (s *Store) Mantle(key string) any {
	if v, ok := s.snap.Mantle[key]; ok {
		return v
	}
	if sp, ok := s.specs[key]; ok {
		return sp.Default
	}
	return nil
}

// SetMantle updates a mantle setting now and writes ~/.mantle/settings.json in a Cmd.
func (s *Store) SetMantle(key string, v any) tea.Cmd {
	next := *s.snap
	next.Mantle = maps.Clone(s.snap.Mantle)
	if v == nil {
		delete(next.Mantle, key)
	} else {
		next.Mantle[key] = v
	}
	s.snap = &next
	w, path := Writer{Paths: s.paths}, s.paths.MantleSettings()
	return func() tea.Msg {
		if err := w.Set(path, key, v); err != nil {
			return WriteFailedMsg{Scope: "mantle", Key: key, Err: err}
		}
		return ext.SettingsMsg{Changed: []string{"mantle:" + key}}
	}
}

// ClaudeScope returns one scope's raw settings.
func (s *Store) ClaudeScope(scope string) map[string]any { return s.snap.Scopes[scope] }

// ClaudeSources lists the scope files, highest precedence first.
func (s *Store) ClaudeSources() []ext.SettingsSource { return slices.Clone(s.snap.Sources) }

// SetClaude writes one top-level key of a Claude Code settings scope (user, project
// or local) under the file lock, then reloads. nil deletes the key.
func (s *Store) SetClaude(scope, key string, value any) tea.Cmd {
	path := s.paths.ScopeFile(scope)
	switch scope {
	case ext.ScopeUser, ext.ScopeProject, ext.ScopeLocal:
	default:
		path = ""
	}
	p, flag := s.paths, s.flag
	return func() tea.Msg {
		if path == "" {
			return WriteFailedMsg{Scope: scope, Key: key, Err: errors.New("scope " + scope + " is not writable")}
		}
		if err := (Writer{Paths: p}).Set(path, key, value); err != nil {
			return WriteFailedMsg{Scope: scope, Key: key, Err: err}
		}
		return ReloadMsg{Snap: Load(p, flag)}
	}
}

// WriteFailedMsg reports a failed settings write.
type WriteFailedMsg struct {
	Scope, Key string
	Err        error
}

// ReloadMsg carries a freshly loaded snapshot to the UI goroutine.
type ReloadMsg struct{ Snap *Snapshot }

// Apply swaps in a new snapshot and returns the changed top-level keys (Claude keys
// plain, mantle keys prefixed "mantle:"), sorted.
func (s *Store) Apply(m ReloadMsg) []string {
	old := s.snap
	s.snap = m.Snap
	var changed []string
	diff := func(prefix string, a, b map[string]any) {
		keys := map[string]bool{}
		for k := range a {
			keys[k] = true
		}
		for k := range b {
			keys[k] = true
		}
		for k := range keys {
			if !reflect.DeepEqual(a[k], b[k]) {
				changed = append(changed, prefix+k)
			}
		}
	}
	diff("", old.Merged, m.Snap.Merged)
	for k := range globalFallback {
		if _, inMerged := m.Snap.Merged[k]; inMerged {
			continue
		}
		if !reflect.DeepEqual(old.Global[k], m.Snap.Global[k]) && !slices.Contains(changed, k) {
			changed = append(changed, k)
		}
	}
	diff("mantle:", old.Mantle, m.Snap.Mantle)
	sort.Strings(changed)
	return changed
}

// Reload re-reads every source synchronously (tests, startup).
func (s *Store) Reload() []string { return s.Apply(ReloadMsg{Snap: Load(s.paths, s.flag)}) }
