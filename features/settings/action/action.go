// Package action describes the side effects of a settings change as data, so the pure
// panel models can be tested without an engine or a disk. The settings feature runs
// them in order: control requests through the engine, patches through the config
// writer, commands as engine slash commands, mantle keys through ext.Settings.
package action

import (
	"fmt"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

// Control is a control request for the engine.
type Control struct {
	Subtype string
	Payload map[string]any
}

// Mantle sets a key in ~/.mantle/settings.json.
type Mantle struct {
	Key   string
	Value any
}

// Effect is one side effect. Exactly one field is set.
type Effect struct {
	Control *Control     // send a control request
	Patch   *patch.Patch // edit a settings file through the config writer
	Command string       // send to the engine as a slash command, e.g. "/config copyOnSelect=false"
	Mantle  *Mantle      // set a mantle setting
	Note    string       // tell the user something (no side effect)
}

// Kind names the populated field, for logs and tests.
func (e Effect) Kind() string {
	switch {
	case e.Control != nil:
		return "control:" + e.Control.Subtype
	case e.Patch != nil:
		return "patch:" + string(e.Patch.Scope)
	case e.Command != "":
		return "command"
	case e.Mantle != nil:
		return "mantle"
	case e.Note != "":
		return "note"
	}
	return "empty"
}

// String is a one-line description, for confirmation dialogs and logs.
func (e Effect) String() string {
	switch {
	case e.Control != nil:
		return fmt.Sprintf("%s %v", e.Control.Subtype, e.Control.Payload)
	case e.Patch != nil:
		return fmt.Sprint(e.Patch.Describe())
	case e.Command != "":
		return e.Command
	case e.Mantle != nil:
		return fmt.Sprintf("mantle %s = %v", e.Mantle.Key, e.Mantle.Value)
	}
	return e.Note
}

// ControlEffect is shorthand for a control-request effect.
func ControlEffect(subtype string, payload map[string]any) Effect {
	return Effect{Control: &Control{Subtype: subtype, Payload: payload}}
}

// PatchEffect is shorthand for a settings-file effect.
func PatchEffect(p patch.Patch) Effect { return Effect{Patch: &p} }
