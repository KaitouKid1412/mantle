package model

import (
	"strings"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

// Effort is a reasoning effort level, named as the engine names it.
type Effort string

const (
	Low    Effort = "low"
	Medium Effort = "medium"
	High   Effort = "high"
	XHigh  Effort = "xhigh"
	Max    Effort = "max"
)

// Levels lists every effort level in ascending order.
var Levels = []Effort{Low, Medium, High, XHigh, Max}

// DefaultEffort is assumed when nothing sets an effort for a model that supports it.
// The engine does not report per-model defaults; it is clamped to the model's levels.
var DefaultEffort = High

// ParseEffort accepts a level name in any case.
func ParseEffort(s string) (Effort, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, l := range Levels {
		if string(l) == s {
			return l, true
		}
	}
	return "", false
}

// Index is the level's position in Levels, or -1.
func (e Effort) Index() int {
	for i, l := range Levels {
		if l == e {
			return i
		}
	}
	return -1
}

// Persistable reports whether the level may be saved to a settings file. The engine
// only accepts low through xhigh in effortLevel; max is a per-session choice.
func (e Effort) Persistable() bool {
	return e != "" && e != Max && e.Index() >= 0
}

// Clamp maps want onto the available levels: the highest available level not above
// want, or the lowest available level if all are above it. An empty list yields "".
func Clamp(want Effort, available []Effort) Effort {
	if len(available) == 0 {
		return ""
	}
	wi := want.Index()
	if wi < 0 {
		wi = DefaultEffort.Index()
	}
	best := Effort("")
	for _, l := range available {
		if l.Index() <= wi && (best == "" || l.Index() > best.Index()) {
			best = l
		}
	}
	if best == "" {
		best = available[0]
		for _, l := range available {
			if l.Index() < best.Index() {
				best = l
			}
		}
	}
	return best
}

// Step moves delta positions through the available levels, stopping at either end.
func Step(cur Effort, available []Effort, delta int) Effort {
	if len(available) == 0 {
		return ""
	}
	cur = Clamp(cur, available)
	pos := 0
	for i, l := range available {
		if l == cur {
			pos = i
		}
	}
	pos += delta
	if pos < 0 {
		pos = 0
	}
	if pos >= len(available) {
		pos = len(available) - 1
	}
	return available[pos]
}

// Source says where a resolved effort came from.
type Source int

const (
	SourceNone          Source = iota // the model has no effort levels
	SourceSession                     // chosen this session (flag settings)
	SourceEnv                         // CLAUDE_CODE_EFFORT_LEVEL
	SourceModelSettings               // modelSettings.<model>.effortLevel
	SourceSetting                     // top-level effortLevel
	SourceDefault                     // nothing set; DefaultEffort
)

func (s Source) String() string {
	switch s {
	case SourceSession:
		return "session"
	case SourceEnv:
		return "environment"
	case SourceModelSettings:
		return "model setting"
	case SourceSetting:
		return "setting"
	case SourceDefault:
		return "default"
	}
	return "none"
}

// EffortInputs is everything effort resolution looks at.
type EffortInputs struct {
	// Session is the level applied this session with apply_flag_settings ("" if none).
	Session Effort
	// Env is the raw CLAUDE_CODE_EFFORT_LEVEL value; "auto" and "unset" mean not set.
	Env string
	// Settings is the merged Claude Code settings document (read-only view).
	Settings map[string]any
}

// Resolved is the effort in effect for one model.
type Resolved struct {
	Effort    Effort
	Source    Source
	Requested Effort // the level asked for before clamping ("" when defaulted)
	Capped    bool   // lowered by supportedEffortLevels or maxEffortLevel
}

// ResolveEffort applies the order session flag, environment,
// modelSettings[model].effortLevel, effortLevel, model default, then clamps to the
// model's supported levels and any maxEffortLevel cap.
func ResolveEffort(info Info, in EffortInputs) Resolved {
	avail := capLevels(info.Efforts(), maxEffort(info, in.Settings))
	if len(avail) == 0 {
		return Resolved{Source: SourceNone}
	}
	want, src := Effort(""), SourceDefault
	switch {
	case in.Session != "":
		want, src = in.Session, SourceSession
	case envEffort(in.Env) != "":
		want, src = envEffort(in.Env), SourceEnv
	default:
		if e, ok := modelSetting(info, in.Settings, "effortLevel"); ok {
			want, src = e, SourceModelSettings
		} else if e, ok := settingEffort(in.Settings, "effortLevel"); ok {
			want, src = e, SourceSetting
		}
	}
	r := Resolved{Source: src, Requested: want}
	if want == "" {
		want = DefaultEffort
	}
	r.Effort = Clamp(want, avail)
	r.Capped = r.Requested != "" && r.Effort != r.Requested
	return r
}

func envEffort(raw string) Effort {
	e, _ := ParseEffort(raw)
	return e
}

func settingEffort(doc map[string]any, path ...string) (Effort, bool) {
	v, ok := patch.Get(doc, path...)
	if !ok {
		return "", false
	}
	s, _ := v.(string)
	return ParseEffort(s)
}

// modelSetting looks up modelSettings.<key>.<field>, trying the most specific key first
// and then any key in the same family.
func modelSetting(info Info, doc map[string]any, field string) (Effort, bool) {
	ms, _ := doc["modelSettings"].(map[string]any)
	if len(ms) == 0 {
		return "", false
	}
	for _, k := range info.settingsKeys() {
		for key := range ms {
			if strings.EqualFold(key, k) {
				if e, ok := settingEffort(ms, key, field); ok {
					return e, true
				}
			}
		}
	}
	fam := Family(info.ResolvedModel)
	if fam == "" && !info.IsDefault() {
		fam = Family(info.Value)
	}
	if fam == "" {
		return "", false
	}
	for key := range ms {
		if Family(key) == fam {
			if e, ok := settingEffort(ms, key, field); ok {
				return e, true
			}
		}
	}
	return "", false
}

// maxEffort is the lowest of the global and per-model maxEffortLevel caps ("" = none).
func maxEffort(info Info, doc map[string]any) Effort {
	caps := []Effort{}
	if e, ok := settingEffort(doc, "maxEffortLevel"); ok {
		caps = append(caps, e)
	}
	if e, ok := modelSetting(info, doc, "maxEffortLevel"); ok {
		caps = append(caps, e)
	}
	var low Effort
	for _, c := range caps {
		if low == "" || c.Index() < low.Index() {
			low = c
		}
	}
	return low
}

func capLevels(levels []Effort, max Effort) []Effort {
	if max == "" {
		return levels
	}
	var out []Effort
	for _, l := range levels {
		if l.Index() <= max.Index() {
			out = append(out, l)
		}
	}
	if len(out) == 0 && len(levels) > 0 {
		out = levels[:1]
	}
	return out
}

// Ultracode reports whether the ultracode toggle is on: a session choice wins, then the
// ultracode setting, then an effortLevel of "ultracode".
func Ultracode(session *bool, settings map[string]any) bool {
	if session != nil {
		return *session
	}
	if v, ok := patch.Get(settings, "ultracode"); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	if v, ok := patch.Get(settings, "effortLevel"); ok {
		if s, ok := v.(string); ok && strings.EqualFold(s, "ultracode") {
			return true
		}
	}
	return false
}
