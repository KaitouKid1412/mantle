package options

import (
	"errors"
	"fmt"
	"strings"

	"github.com/KaitouKid1412/mantle/features/settings/action"
	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

var (
	// ErrNoSession is returned when a session-only change is asked of a setting that
	// can only be saved.
	ErrNoSession = errors.New("this setting cannot be changed for the current session only")
	// ErrPanel is returned for settings that are changed in their own panel.
	ErrPanel = errors.New("this setting is changed in its own panel")
)

// Plan returns the effects of setting o to v. With persist false the change applies to
// this session only (apply_flag_settings); otherwise it is saved through the option's
// write path.
func Plan(o Option, v any, persist bool) ([]action.Effect, error) {
	if err := o.Validate(v); err != nil {
		return nil, err
	}
	if !persist {
		if !o.SessionOK {
			return nil, fmt.Errorf("%w: %s", ErrNoSession, o.Key)
		}
		return []action.Effect{action.ControlEffect("apply_flag_settings",
			map[string]any{"settings": nested(o.Path(), v)})}, nil
	}
	unset := o.UnsetWhen != nil && patch.Equal(v, o.UnsetWhen)
	switch o.Write {
	case WriteConfigWriter:
		scope, err := o.scope()
		if err != nil {
			return nil, err
		}
		if unset {
			return []action.Effect{action.PatchEffect(patch.Patch{Scope: scope,
				Ops: []patch.Op{{Kind: patch.Delete, Path: o.Path()}}})}, nil
		}
		return []action.Effect{action.PatchEffect(patch.Patch{Scope: scope,
			Ops: []patch.Op{{Kind: patch.Set, Path: o.Path(), Value: v}}})}, nil
	case WriteUpdateSettings:
		scope, err := o.scope()
		if err != nil {
			return nil, err
		}
		var val any = v
		if unset {
			val = nil
		}
		return []action.Effect{action.ControlEffect("update_settings", map[string]any{
			"source": string(scope), "settings": nested(o.Path(), val)})}, nil
	case WriteEngineConfig:
		return []action.Effect{{Command: EngineCommand(o, v)}}, nil
	case WriteMantle:
		return []action.Effect{{Mantle: &action.Mantle{Key: o.Key, Value: v}}}, nil
	case WritePanel:
		return nil, fmt.Errorf("%w: %s", ErrPanel, o.Panel)
	}
	return nil, fmt.Errorf("%s: unknown write path %v", o.Key, o.Write)
}

// EngineCommand is the slash command that asks the engine to store v for o.
func EngineCommand(o Option, v any) string {
	return fmt.Sprintf("/config %s=%s", o.Row, Format(v))
}

func (o Option) scope() (patch.Scope, error) {
	switch o.Store {
	case StoreUser:
		return patch.User, nil
	case StoreLocal:
		return patch.Local, nil
	}
	return "", fmt.Errorf("%s: %s is not a settings file", o.Key, o.Store)
}

func nested(path []string, v any) map[string]any {
	inner := map[string]any{path[len(path)-1]: v}
	for i := len(path) - 2; i >= 0; i-- {
		inner = map[string]any{path[i]: inner}
	}
	return inner
}

// Assignment is one "key=value" pair from "/config key=value …".
type Assignment struct {
	Name, Raw string
}

// ParseAssignments splits the argument of "/config" into key=value pairs. A single pair
// keeps spaces in its value ("language=brazilian portuguese"); several pairs are
// separated by whitespace. ok is false when the text holds no assignment (the bare
// command opens the panel).
func ParseAssignments(arg string) (pairs []Assignment, ok bool) {
	arg = strings.TrimSpace(arg)
	if !strings.Contains(arg, "=") {
		return nil, false
	}
	fields := strings.Fields(arg)
	withEq := 0
	for _, f := range fields {
		if strings.Contains(f, "=") {
			withEq++
		}
	}
	if withEq == 1 {
		i := strings.IndexByte(arg, '=')
		name := arg[:i]
		if name == "" || strings.ContainsAny(name, " \t") {
			return nil, false
		}
		return []Assignment{{Name: name, Raw: arg[i+1:]}}, true
	}
	for _, f := range fields {
		i := strings.IndexByte(f, '=')
		if i <= 0 {
			return nil, false
		}
		pairs = append(pairs, Assignment{Name: f[:i], Raw: f[i+1:]})
	}
	return pairs, true
}
