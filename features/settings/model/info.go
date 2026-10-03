// Package model turns the engine's model list into /model picker rows and resolves the
// effort level in effect for each model. It is pure: no engine, no files.
package model

import (
	"regexp"
	"strings"
)

// Info mirrors the engine's ModelInfo (initialize.models and list_models). It is written
// against the protocol notes; pkg/proto.ModelInfo replaces it once proto-v1 lands, with
// the same JSON names.
type Info struct {
	Value                    string   `json:"value"`
	ResolvedModel            string   `json:"resolvedModel,omitempty"`
	DisplayName              string   `json:"displayName"`
	Description              string   `json:"description"`
	SupportsEffort           bool     `json:"supportsEffort,omitempty"`
	SupportedEffortLevels    []string `json:"supportedEffortLevels,omitempty"`
	SupportsAdaptiveThinking bool     `json:"supportsAdaptiveThinking,omitempty"`
	SupportsFastMode         bool     `json:"supportsFastMode,omitempty"`
	SupportsAutoMode         bool     `json:"supportsAutoMode,omitempty"`
	// Disabled is set on entries of unavailable_models: visible but not selectable. The
	// reason is folded into Description by the engine.
	Disabled bool `json:"disabled,omitempty"`
}

// DefaultValue is the picker value that resets the session to the account default.
// set_model treats it like null.
const DefaultValue = "default"

// IsDefault reports whether this entry is the "use the default model" row.
func (i Info) IsDefault() bool { return i.Value == DefaultValue || i.Value == "" }

// Efforts returns the supported effort levels in ascending order. Unknown level names
// from a newer engine are dropped.
func (i Info) Efforts() []Effort {
	if !i.SupportsEffort && len(i.SupportedEffortLevels) == 0 {
		return nil
	}
	if len(i.SupportedEffortLevels) == 0 {
		return append([]Effort(nil), Levels...)
	}
	var out []Effort
	for _, l := range Levels {
		for _, s := range i.SupportedEffortLevels {
			if string(l) == s {
				out = append(out, l)
				break
			}
		}
	}
	return out
}

var dateSuffix = regexp.MustCompile(`-\d{8}$`)

// Family normalises a model ID to the key Claude Code uses for per-model settings:
// lower case, without a "[1m]"-style context suffix or a trailing release date.
// "claude-opus-5-5[1m]" and "claude-opus-5-5-20260901" both become "claude-opus-5-5".
func Family(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	if i := strings.IndexByte(s, '['); i > 0 && strings.HasSuffix(s, "]") {
		s = s[:i]
	}
	return dateSuffix.ReplaceAllString(s, "")
}

// SameModel reports whether a model reference (alias or ID, as stored in settings or
// reported by the engine) names this entry.
func (i Info) SameModel(ref string) bool {
	if ref == "" {
		return false
	}
	if strings.EqualFold(ref, i.Value) {
		return true
	}
	return i.ResolvedModel != "" && strings.EqualFold(ref, i.ResolvedModel)
}

// settingsKeys are the modelSettings keys that may hold this model's settings, most
// specific first.
func (i Info) settingsKeys() []string {
	var keys []string
	add := func(k string) {
		if k == "" {
			return
		}
		for _, have := range keys {
			if have == k {
				return
			}
		}
		keys = append(keys, k)
	}
	add(i.ResolvedModel)
	if !i.IsDefault() {
		add(i.Value)
	}
	add(Family(i.ResolvedModel))
	if !i.IsDefault() {
		add(Family(i.Value))
	}
	return keys
}

// SettingsKey is the modelSettings key mantle writes for this model.
func (i Info) SettingsKey() string {
	if i.ResolvedModel != "" {
		return Family(i.ResolvedModel)
	}
	if i.IsDefault() {
		return ""
	}
	return Family(i.Value)
}
