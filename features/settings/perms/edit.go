package perms

import (
	"errors"
	"fmt"
	"strings"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

// Modes are the values permissions.defaultMode accepts.
var Modes = []string{"default", "acceptEdits", "plan", "auto", "dontAsk", "bypassPermissions"}

// NormalizeMode maps a mode name (including the "manual" alias for "default") to the
// value stored in settings. ok is false for unknown modes.
func NormalizeMode(mode string) (string, bool) {
	if mode == "manual" {
		return "default", true
	}
	for _, m := range Modes {
		if m == mode {
			return m, true
		}
	}
	return "", false
}

func writable(scope patch.Scope) error {
	if !scope.Writable() {
		return fmt.Errorf("%w: %s rules can only be viewed", patch.ErrReadOnly, scope.Label())
	}
	return nil
}

func rulePath(b Behavior) []string { return []string{"permissions", string(b)} }

var dirsPath = []string{"permissions", "additionalDirectories"}

// AddRule adds rule to the behavior list of scope. The same rule is removed from the
// scope's other two lists in the same patch, so adding moves a rule between allow, ask
// and deny. The rule must parse.
func AddRule(scope patch.Scope, b Behavior, rule string) (patch.Patch, error) {
	if err := writable(scope); err != nil {
		return patch.Patch{}, err
	}
	if !b.valid() {
		return patch.Patch{}, fmt.Errorf("unknown rule list %q", b)
	}
	r, err := Parse(rule)
	if err != nil {
		return patch.Patch{}, err
	}
	text := r.String()
	p := patch.Patch{Scope: scope}
	for _, other := range Behaviors {
		if other != b {
			p.Ops = append(p.Ops, patch.Op{Kind: patch.RemoveValue, Path: rulePath(other), Value: text})
		}
	}
	p.Ops = append(p.Ops, patch.Op{Kind: patch.AddUnique, Path: rulePath(b), Value: text})
	return p, nil
}

// RemoveRule removes rule from the behavior list of scope. The rule does not have to
// parse, so broken entries found in a file can still be deleted.
func RemoveRule(scope patch.Scope, b Behavior, rule string) (patch.Patch, error) {
	if err := writable(scope); err != nil {
		return patch.Patch{}, err
	}
	if !b.valid() {
		return patch.Patch{}, fmt.Errorf("unknown rule list %q", b)
	}
	if rule == "" {
		return patch.Patch{}, ErrEmpty
	}
	return patch.Patch{Scope: scope, Ops: []patch.Op{
		{Kind: patch.RemoveValue, Path: rulePath(b), Value: rule},
	}}, nil
}

// ErrEmptyDir is returned for a blank directory.
var ErrEmptyDir = errors.New("empty directory")

// AddDirectory adds dir to permissions.additionalDirectories.
func AddDirectory(scope patch.Scope, dir string) (patch.Patch, error) {
	if err := writable(scope); err != nil {
		return patch.Patch{}, err
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return patch.Patch{}, ErrEmptyDir
	}
	return patch.Patch{Scope: scope, Ops: []patch.Op{
		{Kind: patch.AddUnique, Path: dirsPath, Value: dir},
	}}, nil
}

// RemoveDirectory removes dir (exactly as stored) from permissions.additionalDirectories.
func RemoveDirectory(scope patch.Scope, dir string) (patch.Patch, error) {
	if err := writable(scope); err != nil {
		return patch.Patch{}, err
	}
	if dir == "" {
		return patch.Patch{}, ErrEmptyDir
	}
	return patch.Patch{Scope: scope, Ops: []patch.Op{
		{Kind: patch.RemoveValue, Path: dirsPath, Value: dir},
	}}, nil
}

// SetDefaultMode stores permissions.defaultMode. "manual" is accepted as "default".
func SetDefaultMode(scope patch.Scope, mode string) (patch.Patch, error) {
	if err := writable(scope); err != nil {
		return patch.Patch{}, err
	}
	m, ok := NormalizeMode(mode)
	if !ok {
		return patch.Patch{}, fmt.Errorf("unknown permission mode %q (want one of %s)", mode, strings.Join(Modes, ", "))
	}
	return patch.SetKey(scope, m, "permissions", "defaultMode"), nil
}

// ClearDefaultMode removes permissions.defaultMode from scope.
func ClearDefaultMode(scope patch.Scope) (patch.Patch, error) {
	if err := writable(scope); err != nil {
		return patch.Patch{}, err
	}
	return patch.DeleteKey(scope, "permissions", "defaultMode"), nil
}

// AddAutoModeRule appends text to autoMode.<list>.
func AddAutoModeRule(scope patch.Scope, list AutoList, text string) (patch.Patch, error) {
	if err := writable(scope); err != nil {
		return patch.Patch{}, err
	}
	if !list.valid() {
		return patch.Patch{}, fmt.Errorf("unknown auto-mode list %q", list)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return patch.Patch{}, ErrEmpty
	}
	return patch.Patch{Scope: scope, Ops: []patch.Op{
		{Kind: patch.AddUnique, Path: []string{"autoMode", string(list)}, Value: text},
	}}, nil
}

// RemoveAutoModeRule removes text (exactly as stored) from autoMode.<list>.
func RemoveAutoModeRule(scope patch.Scope, list AutoList, text string) (patch.Patch, error) {
	if err := writable(scope); err != nil {
		return patch.Patch{}, err
	}
	if !list.valid() {
		return patch.Patch{}, fmt.Errorf("unknown auto-mode list %q", list)
	}
	if text == "" {
		return patch.Patch{}, ErrEmpty
	}
	return patch.Patch{Scope: scope, Ops: []patch.Op{
		{Kind: patch.RemoveValue, Path: []string{"autoMode", string(list)}, Value: text},
	}}, nil
}
