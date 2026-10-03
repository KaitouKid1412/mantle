// Package patch describes edits to Claude Code settings files as data.
//
// Panels never write files directly. They build a Patch (a scope plus a list of
// operations) and hand it to the config writer, which re-reads the file under a lock and
// calls Apply on the freshest contents just before writing. Expressing edits as
// operations instead of whole documents keeps concurrent edits by the engine (or by
// the user in an editor) from being lost.
package patch

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Scope is a Claude Code settings source, named as the engine names it.
type Scope string

const (
	User    Scope = "userSettings"    // ~/.claude/settings.json
	Project Scope = "projectSettings" // <project>/.claude/settings.json
	Local   Scope = "localSettings"   // <project>/.claude/settings.local.json
	Flag    Scope = "flagSettings"    // --settings; session only, never written
	Policy  Scope = "policySettings"  // managed; read-only
)

// Scopes lists every scope from lowest to highest precedence.
var Scopes = []Scope{User, Project, Local, Flag, Policy}

// Writable reports whether mantle may write this scope's file.
func (s Scope) Writable() bool {
	return s == User || s == Project || s == Local
}

// Label is a short human name for the scope.
func (s Scope) Label() string {
	switch s {
	case User:
		return "User"
	case Project:
		return "Project"
	case Local:
		return "Local"
	case Flag:
		return "Flag"
	case Policy:
		return "Managed"
	}
	return string(s)
}

// Precedence orders scopes: a higher number wins when the same key is set twice.
func (s Scope) Precedence() int {
	for i, sc := range Scopes {
		if sc == s {
			return i
		}
	}
	return -1
}

// Kind is the type of an operation.
type Kind int

const (
	// Set stores Value at Path, creating intermediate objects.
	Set Kind = iota
	// Delete removes the key at Path. Missing keys are not an error.
	Delete
	// AddUnique appends Value to the array at Path unless an equal element exists.
	AddUnique
	// RemoveValue removes every element equal to Value from the array at Path.
	RemoveValue
)

func (k Kind) String() string {
	switch k {
	case Set:
		return "set"
	case Delete:
		return "delete"
	case AddUnique:
		return "add"
	case RemoveValue:
		return "remove"
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// Op is one edit. Path addresses nested objects, e.g. ["permissions", "allow"].
// Values must be JSON-compatible (bool, float64/int, string, []any, map[string]any, nil).
type Op struct {
	Kind  Kind
	Path  []string
	Value any
}

// Patch is a list of operations for one settings scope.
type Patch struct {
	Scope Scope
	Ops   []Op
}

// SetKey is shorthand for a Patch that sets one top-level or nested key.
func SetKey(scope Scope, value any, path ...string) Patch {
	return Patch{Scope: scope, Ops: []Op{{Kind: Set, Path: path, Value: value}}}
}

// DeleteKey is shorthand for a Patch that removes one key.
func DeleteKey(scope Scope, path ...string) Patch {
	return Patch{Scope: scope, Ops: []Op{{Kind: Delete, Path: path}}}
}

// ErrReadOnly is returned when a patch targets a scope mantle must not write.
var ErrReadOnly = errors.New("settings scope is read-only")

// Validate checks that the patch is well formed and targets a writable scope.
func (p Patch) Validate() error {
	if !p.Scope.Writable() {
		return fmt.Errorf("%w: %s", ErrReadOnly, p.Scope)
	}
	for i, op := range p.Ops {
		if len(op.Path) == 0 {
			return fmt.Errorf("op %d (%s): empty path", i, op.Kind)
		}
		for _, seg := range op.Path {
			if seg == "" {
				return fmt.Errorf("op %d (%s): empty path segment in %q", i, op.Kind, strings.Join(op.Path, "."))
			}
		}
		if (op.Kind == AddUnique || op.Kind == RemoveValue) && op.Value == nil {
			return fmt.Errorf("op %d (%s): nil value", i, op.Kind)
		}
	}
	return nil
}

// Apply runs the operations against doc and returns the result. doc may be nil (missing
// file). The input is not modified. Apply fails when an intermediate path element exists
// but is not an object, or when an array op finds a non-array; it never discards data it
// does not understand.
func (p Patch) Apply(doc map[string]any) (map[string]any, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	out := cloneMap(doc)
	if out == nil {
		out = map[string]any{}
	}
	for i, op := range p.Ops {
		if err := applyOp(out, op); err != nil {
			return nil, fmt.Errorf("op %d (%s %s): %w", i, op.Kind, strings.Join(op.Path, "."), err)
		}
	}
	return out, nil
}

func applyOp(doc map[string]any, op Op) error {
	parentPath, key := op.Path[:len(op.Path)-1], op.Path[len(op.Path)-1]
	create := op.Kind == Set || op.Kind == AddUnique
	parent, err := walk(doc, parentPath, create)
	if err != nil || parent == nil {
		return err
	}
	switch op.Kind {
	case Set:
		parent[key] = normalize(op.Value)
	case Delete:
		delete(parent, key)
	case AddUnique:
		v := normalize(op.Value)
		arr, err := arrayAt(parent, key)
		if err != nil {
			return err
		}
		for _, e := range arr {
			if Equal(e, v) {
				return nil
			}
		}
		parent[key] = append(arr, v)
	case RemoveValue:
		if _, ok := parent[key]; !ok {
			return nil
		}
		arr, err := arrayAt(parent, key)
		if err != nil {
			return err
		}
		v := normalize(op.Value)
		kept := make([]any, 0, len(arr))
		for _, e := range arr {
			if !Equal(e, v) {
				kept = append(kept, e)
			}
		}
		parent[key] = kept
	default:
		return fmt.Errorf("unknown op kind %d", int(op.Kind))
	}
	return nil
}

// walk returns the object at path. With create, missing objects are made; without it a
// missing object yields (nil, nil).
func walk(doc map[string]any, path []string, create bool) (map[string]any, error) {
	cur := doc
	for _, seg := range path {
		next, ok := cur[seg]
		if !ok || next == nil {
			if !create {
				return nil, nil
			}
			m := map[string]any{}
			cur[seg] = m
			cur = m
			continue
		}
		m, ok := next.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%q is %T, not an object", seg, next)
		}
		cur = m
	}
	return cur, nil
}

func arrayAt(parent map[string]any, key string) ([]any, error) {
	v, ok := parent[key]
	if !ok || v == nil {
		return nil, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%q is %T, not an array", key, v)
	}
	return arr, nil
}

// Get reads the value at path from doc.
func Get(doc map[string]any, path ...string) (any, bool) {
	var cur any = doc
	for _, seg := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// Equal compares two JSON-compatible values, treating all numeric types as float64.
func Equal(a, b any) bool {
	return reflect.DeepEqual(normalize(a), normalize(b))
}

// normalize converts Go values into the shapes encoding/json produces when decoding
// into any, so values built in code compare equal to values read from files.
func normalize(v any) any {
	switch t := v.(type) {
	case int:
		return float64(t)
	case int32:
		return float64(t)
	case int64:
		return float64(t)
	case float32:
		return float64(t)
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = normalize(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = normalize(e)
		}
		return out
	}
	return v
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	return normalize(m).(map[string]any)
}

// Describe renders the patch as short human-readable lines, e.g.
// "User: add permissions.allow ← \"Bash(git *)\"".
func (p Patch) Describe() []string {
	lines := make([]string, 0, len(p.Ops))
	for _, op := range p.Ops {
		path := strings.Join(op.Path, ".")
		switch op.Kind {
		case Delete:
			lines = append(lines, fmt.Sprintf("%s: unset %s", p.Scope.Label(), path))
		case Set:
			lines = append(lines, fmt.Sprintf("%s: %s = %s", p.Scope.Label(), path, formatValue(op.Value)))
		case AddUnique:
			lines = append(lines, fmt.Sprintf("%s: add %s to %s", p.Scope.Label(), formatValue(op.Value), path))
		case RemoveValue:
			lines = append(lines, fmt.Sprintf("%s: remove %s from %s", p.Scope.Label(), formatValue(op.Value), path))
		}
	}
	return lines
}

func formatValue(v any) string {
	switch t := v.(type) {
	case string:
		return fmt.Sprintf("%q", t)
	case nil:
		return "null"
	}
	return fmt.Sprint(v)
}
