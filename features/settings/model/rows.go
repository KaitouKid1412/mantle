package model

// DefaultLabel labels the synthesized default row when the engine's list has none.
const DefaultLabel = "Default (recommended)"

// Row is one line of the /model picker.
type Row struct {
	Info        Info
	Value       string // what set_model receives ("default" resets)
	Label       string
	Description string
	IsDefault   bool
	Current     bool // the model this session is using
	Disabled    bool // shown but not selectable (unavailable_models)

	Efforts  []Effort // selectable levels, after any maxEffortLevel cap
	Effort   Resolved // effort that applies if this model is chosen
	Fast     bool     // supports fast mode
	Auto     bool     // supports auto permission mode
	Thinking ThinkingState
}

// ThinkingState says whether extended thinking can be toggled for a model.
type ThinkingState int

const (
	ThinkingToggle ThinkingState = iota // can be switched on or off
	ThinkingLocked                      // adaptive thinking; cannot be turned off
)

// Current describes the session's model as last reported by the engine.
type Current struct {
	// Model is the session model: an alias ("opus"), a full ID, or "" / "default" when
	// the account default is in use.
	Model string
}

// Rows builds picker rows: a default row first, then the engine's models in order, then
// unavailable models (disabled). Exactly one selectable row is marked current.
func Rows(models, unavailable []Info, cur Current, in EffortInputs) []Row {
	var rows []Row
	haveDefault := false
	for _, m := range models {
		if m.IsDefault() {
			haveDefault = true
		}
	}
	if !haveDefault {
		rows = append(rows, newRow(Info{Value: DefaultValue, DisplayName: DefaultLabel}, in))
	}
	for _, m := range models {
		r := newRow(m, in)
		if r.IsDefault {
			// Keep the default row first even if the engine lists it later.
			rows = append([]Row{r}, rows...)
			continue
		}
		rows = append(rows, r)
	}
	for _, m := range unavailable {
		r := newRow(m, in)
		r.Disabled = true
		rows = append(rows, r)
	}
	if i := CurrentIndex(rows, cur); i >= 0 {
		rows[i].Current = true
	}
	return rows
}

func newRow(m Info, in EffortInputs) Row {
	label := m.DisplayName
	if label == "" {
		label = m.Value
	}
	if m.IsDefault() && label == "" {
		label = DefaultLabel
	}
	r := Row{
		Info:        m,
		Value:       m.Value,
		Label:       label,
		Description: m.Description,
		IsDefault:   m.IsDefault(),
		Fast:        m.SupportsFastMode,
		Auto:        m.SupportsAutoMode,
	}
	if r.IsDefault {
		r.Value = DefaultValue
	}
	r.Effort = ResolveEffort(m, in)
	if r.Effort.Source != SourceNone {
		r.Efforts = capLevels(m.Efforts(), maxEffort(m, in.Settings))
	}
	if m.SupportsAdaptiveThinking {
		r.Thinking = ThinkingLocked
	}
	return r
}

// CurrentIndex finds the row for the session model: an exact value match first, then a
// resolved-ID match on a non-default row, then the default row when the session uses the
// default (or nothing matches it). Disabled rows never match. Returns -1 if no row fits.
func CurrentIndex(rows []Row, cur Current) int {
	def := -1
	for i, r := range rows {
		if r.IsDefault && !r.Disabled {
			def = i
		}
	}
	if cur.Model == "" || cur.Model == DefaultValue {
		return def
	}
	for i, r := range rows {
		if !r.Disabled && !r.IsDefault && r.Info.Value == cur.Model {
			return i
		}
	}
	for i, r := range rows {
		if !r.Disabled && !r.IsDefault && r.Info.SameModel(cur.Model) {
			return i
		}
	}
	if def >= 0 && rows[def].Info.SameModel(cur.Model) {
		return def
	}
	// Compare families last, so "claude-opus-5-5[1m]" still finds an "opus" row that
	// resolves to "claude-opus-5-5".
	fam := Family(cur.Model)
	for i, r := range rows {
		if !r.Disabled && !r.IsDefault && (Family(r.Info.ResolvedModel) == fam || Family(r.Info.Value) == fam) {
			return i
		}
	}
	return -1
}
