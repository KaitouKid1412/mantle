package model

import (
	"strings"

	"github.com/KaitouKid1412/mantle/features/settings/action"
	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

// Choice is what the user picked in the /model picker or typed as /model <name>.
type Choice struct {
	Row         Row
	Effort      Effort // "" leaves effort alone
	SessionOnly bool   // the picker's "this session only" key
}

// Plan lists the effects of a choice, in order:
//  1. set_model for the session ("default" for the default row);
//  2. apply_flag_settings {effortLevel} when an effort was chosen;
//  3. unless SessionOnly, a user-settings patch saving model (and per-model effort).
//
// The headless engine never persists /model itself, so step 3 is how the choice
// survives into the next mantle or claude session.
func Plan(c Choice) []action.Effect {
	var out []action.Effect
	model := c.Row.Value
	if c.Row.IsDefault {
		model = DefaultValue
	}
	out = append(out, action.ControlEffect("set_model", map[string]any{"model": model}))

	effort := c.Effort
	if effort != "" && len(c.Row.Efforts) > 0 {
		effort = Clamp(effort, c.Row.Efforts)
		out = append(out, action.ControlEffect("apply_flag_settings",
			map[string]any{"settings": map[string]any{"effortLevel": string(effort)}}))
	} else {
		effort = ""
	}
	if c.SessionOnly {
		return out
	}

	p := patch.Patch{Scope: patch.User}
	if c.Row.IsDefault {
		p.Ops = append(p.Ops, patch.Op{Kind: patch.Delete, Path: []string{"model"}})
	} else {
		p.Ops = append(p.Ops, patch.Op{Kind: patch.Set, Path: []string{"model"}, Value: c.Row.Value})
	}
	if effort != "" {
		key := c.Row.Info.SettingsKey()
		switch {
		case !effort.Persistable():
			out = append(out, action.Effect{Note: "Max effort applies to this session only; it is not saved."})
		case key == "":
			out = append(out, action.Effect{Note: "Effort applies to this session only: the default model's ID is unknown."})
		default:
			p.Ops = append(p.Ops, patch.Op{Kind: patch.Set, Path: []string{"modelSettings", key, "effortLevel"}, Value: string(effort)})
		}
	}
	return append(out, action.PatchEffect(p))
}

// FindRow resolves a typed model name (/model <name>) against the rows: an exact value,
// display-name or resolved-ID match. ok is false when nothing matches; the caller may
// still pass the name to the engine, which knows models the list omits.
func FindRow(rows []Row, name string) (Row, bool) {
	if name == "" {
		return Row{}, false
	}
	for _, r := range rows {
		if r.Disabled {
			continue
		}
		if r.IsDefault && name == DefaultValue {
			return r, true
		}
		if !r.IsDefault && (r.Value == name || strings.EqualFold(r.Label, name)) {
			return r, true
		}
	}
	for _, r := range rows {
		if !r.Disabled && !r.IsDefault && r.Info.SameModel(name) {
			return r, true
		}
	}
	return Row{}, false
}
