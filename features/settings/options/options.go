// Package options is the table behind /config: one entry per user-facing setting, with
// where it lives, its type and allowed values, mantle's own label and description, and
// how a change is written. Plan turns a change into action effects, so every panel uses
// one apply path.
//
// Write precedence, most direct first:
//
//	control request (session)  >  update_settings (allowlisted keys)
//	  >  config writer (settings files)  >  engine "/config row=value" (global config)
//
// Where each key is stored follows Claude Code 2.1.288: several keys that used to live
// in ~/.claude.json are now written to user settings and only read back from the global
// config as a fallback (GlobalFallback).
package options

import (
	"errors"
	"fmt"
	"strings"
)

// Store is where a setting's value lives.
type Store int

const (
	// StoreUser is ~/.claude/settings.json (userSettings).
	StoreUser Store = iota
	// StoreLocal is <project>/.claude/settings.local.json (localSettings).
	StoreLocal
	// StoreGlobal is ~/.claude.json. mantle never writes it; the engine does.
	StoreGlobal
	// StoreMantle is ~/.mantle/settings.json.
	StoreMantle
)

func (s Store) String() string {
	switch s {
	case StoreUser:
		return "user settings"
	case StoreLocal:
		return "local settings"
	case StoreGlobal:
		return "global config"
	case StoreMantle:
		return "mantle settings"
	}
	return fmt.Sprintf("store(%d)", int(s))
}

// Write is how a persistent change reaches its store.
type Write int

const (
	// WriteConfigWriter edits a settings file through the locked, merging config writer.
	WriteConfigWriter Write = iota
	// WriteUpdateSettings sends update_settings; the engine allows only a few keys.
	WriteUpdateSettings
	// WriteEngineConfig sends "/config <row>=<value>" so the engine writes its own file.
	WriteEngineConfig
	// WriteMantle sets a key in ~/.mantle/settings.json.
	WriteMantle
	// WritePanel means the value is chosen in a dedicated panel (model, theme, …),
	// which owns the write.
	WritePanel
)

func (w Write) String() string {
	switch w {
	case WriteConfigWriter:
		return "config writer"
	case WriteUpdateSettings:
		return "update_settings"
	case WriteEngineConfig:
		return "engine /config"
	case WriteMantle:
		return "mantle settings"
	case WritePanel:
		return "panel"
	}
	return fmt.Sprintf("write(%d)", int(w))
}

// UpdateSettingsAllowlist is the set of keys the engine's update_settings accepts.
var UpdateSettingsAllowlist = map[string]bool{"outputStyle": true, "effortLevel": true}

// Type is a setting's value type.
type Type int

const (
	Bool Type = iota
	Enum
	Text
)

func (t Type) String() string {
	switch t {
	case Bool:
		return "bool"
	case Enum:
		return "enum"
	case Text:
		return "text"
	}
	return fmt.Sprintf("type(%d)", int(t))
}

// When says in which situations a row is offered.
type When int

const (
	Always When = iota
	FullscreenOnly
	IDEConnected
	NotInIDE
	InIDETerminal // running inside an IDE's integrated terminal
)

// Option is one setting.
type Option struct {
	// Key is the settings key; nested keys are dotted ("permissions.defaultMode").
	Key string
	// Row is the ID the engine's "/config row=value" accepts for this setting, or "".
	Row string
	// Section groups rows in the panel.
	Section string
	// Label and Description are mantle's own wording.
	Label, Description string

	Type    Type
	Values  []string // allowed values for Enum
	Default any      // value in effect when the key is absent

	Store Store
	Write Write
	// Panel names the dedicated panel for WritePanel options ("model", "theme", …).
	Panel string
	// UnsetWhen, when non-nil, makes writing that value remove the key instead, as the
	// engine does for keys whose absence means the default.
	UnsetWhen any
	// GlobalFallback reads ~/.claude.json when the settings files don't set the key.
	GlobalFallback bool
	// SessionOK allows applying the value for this session only via
	// apply_flag_settings (engine-side keys only).
	SessionOK bool
	When      When
}

// Path splits Key into its nested path.
func (o Option) Path() []string { return strings.Split(o.Key, ".") }

// Validate checks a value against the option's type.
func (o Option) Validate(v any) error {
	switch o.Type {
	case Bool:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s: want true or false, got %v", o.Key, v)
		}
	case Enum:
		s, ok := v.(string)
		if !ok || !o.hasValue(s) {
			return fmt.Errorf("%s: want one of %s, got %v", o.Key, strings.Join(o.Values, ", "), v)
		}
	case Text:
		if _, ok := v.(string); !ok {
			return fmt.Errorf("%s: want text, got %v", o.Key, v)
		}
	}
	return nil
}

func (o Option) hasValue(s string) bool {
	for _, v := range o.Values {
		if v == s {
			return true
		}
	}
	return false
}

// Parse converts typed text (from "/config key=value" or a search box) to a value.
func (o Option) Parse(raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	switch o.Type {
	case Bool:
		switch strings.ToLower(raw) {
		case "true", "on", "yes", "1":
			return true, nil
		case "false", "off", "no", "0":
			return false, nil
		}
		return nil, fmt.Errorf("%s: %q is not on or off", o.Key, raw)
	case Enum:
		for _, v := range o.Values {
			if strings.EqualFold(v, raw) {
				return v, nil
			}
		}
		return nil, fmt.Errorf("%s: %q is not one of %s", o.Key, raw, strings.Join(o.Values, ", "))
	}
	if o.UnsetWhen == "" && strings.EqualFold(raw, "default") {
		return "", nil
	}
	return raw, nil
}

// Format renders a value for "/config row=value" and for display.
func Format(v any) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

// Next is the value a toggle or enum cycle moves to.
func (o Option) Next(cur any) any {
	switch o.Type {
	case Bool:
		b, _ := cur.(bool)
		return !b
	case Enum:
		s, _ := cur.(string)
		for i, v := range o.Values {
			if v == s {
				return o.Values[(i+1)%len(o.Values)]
			}
		}
		if len(o.Values) > 0 {
			return o.Values[0]
		}
	}
	return cur
}

// Value reads the option's effective value: merged settings first, then the global
// config where the engine keeps or falls back to it, then the default.
func (o Option) Value(settings, global map[string]any) any {
	if v, ok := lookup(settings, o.Path()); ok && o.Validate(v) == nil {
		return v
	}
	if o.Store == StoreGlobal || o.GlobalFallback {
		if v, ok := lookup(global, o.Path()); ok && o.Validate(v) == nil {
			return v
		}
	}
	return o.Default
}

func lookup(doc map[string]any, path []string) (any, bool) {
	var cur any = doc
	for _, seg := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[seg]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// ErrUnknown is returned by Find for names that match no option.
var ErrUnknown = errors.New("no such setting")

// Find looks an option up by settings key or engine row ID, ignoring case.
func Find(name string) (Option, error) {
	var hits []Option
	for _, o := range Table {
		if strings.EqualFold(o.Key, name) || (o.Row != "" && strings.EqualFold(o.Row, name)) {
			if o.Key == name || o.Row == name {
				return o, nil
			}
			hits = append(hits, o)
		}
	}
	switch len(hits) {
	case 0:
		return Option{}, fmt.Errorf("%w: %s", ErrUnknown, name)
	case 1:
		return hits[0], nil
	}
	return Option{}, fmt.Errorf("%s is ambiguous", name)
}

// Sections lists section names in table order.
func Sections() []string {
	var out []string
	seen := map[string]bool{}
	for _, o := range Table {
		if !seen[o.Section] {
			seen[o.Section] = true
			out = append(out, o.Section)
		}
	}
	return out
}
