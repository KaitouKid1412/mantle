package keymap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Severity of a keybinding issue.
type Severity string

const (
	SevError   Severity = "error"   // binding ignored
	SevWarning Severity = "warning" // binding applied, but suspicious
)

// Issue is a problem found while building the keymap. Issues feed `mantle-ui catalog`
// and the /keybindings view.
type Issue struct {
	Source   string // file path, "defaults" or a feature ID
	Context  string
	Keys     string
	Action   ext.ActionID
	Severity Severity
	Message  string
}

func (i Issue) String() string {
	return fmt.Sprintf("%s: %s [%s] %q -> %q: %s", i.Severity, i.Source, i.Context, i.Keys, i.Action, i.Message)
}

// Source is one layer of bindings.
type Source struct {
	Name     string // path or label, for issues
	Bindings []Entry
	// Mantle marks ~/.mantle/keybindings.json: it may bind only mantle:* actions.
	// Claude sources may bind only Claude Code actions.
	Mantle bool
	// User marks user files: reserved keys are rejected there.
	User bool
	// Err is a read or parse error; it is reported as an issue and the source is empty.
	Err error
}

// Entry is a binding in a source. Action "" means unbind (JSON null).
type Entry struct {
	Context, Keys string
	Action        ext.ActionID
}

// fileFormat is Claude Code's keybindings.json format.
type fileFormat struct {
	Bindings []struct {
		Context  string                      `json:"context"`
		Bindings map[string]*json.RawMessage `json:"bindings"`
	} `json:"bindings"`
}

// ParseFile parses a keybindings.json document. Values are action ID strings, or null
// to unbind.
func ParseFile(name string, data []byte, mantle bool) (Source, error) {
	src := Source{Name: name, Mantle: mantle, User: true}
	if len(bytes.TrimSpace(data)) == 0 {
		return src, nil
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return src, fmt.Errorf("%s: %w", name, err)
	}
	for _, block := range f.Bindings {
		keys := make([]string, 0, len(block.Bindings))
		for k := range block.Bindings {
			keys = append(keys, k)
		}
		sort.Strings(keys) // deterministic issue order
		for _, k := range keys {
			raw := block.Bindings[k]
			var action string
			if raw != nil && string(*raw) != "null" {
				if err := json.Unmarshal(*raw, &action); err != nil {
					return src, fmt.Errorf("%s: context %s key %q: action must be a string or null", name, block.Context, k)
				}
				if action == "" {
					return src, fmt.Errorf("%s: context %s key %q: empty action", name, block.Context, k)
				}
			}
			src.Bindings = append(src.Bindings, Entry{Context: block.Context, Keys: k, Action: ext.ActionID(action)})
		}
	}
	return src, nil
}

// LoadFile reads and parses a keybindings file. A missing file is an empty source.
func LoadFile(path string, mantle bool) (Source, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Source{Name: path, Mantle: mantle, User: true}, nil
	}
	if err != nil {
		return Source{Name: path, Mantle: mantle, User: true}, err
	}
	return ParseFile(path, data, mantle)
}

// DefaultsSource wraps a binding table (ext.DefaultBindings, or a feature's
// AddBinding calls) as a source.
func DefaultsSource(name string, bs []ext.Binding) Source {
	src := Source{Name: name}
	for _, b := range bs {
		src.Bindings = append(src.Bindings, Entry{Context: b.Context, Keys: b.Keys, Action: b.Action})
	}
	return src
}

// binding is a resolved binding.
type binding struct {
	action  ext.ActionID
	display string // as written by its source
	source  string
}

// Keymap is an immutable, resolved set of bindings. Build a new one on reload.
type Keymap struct {
	byCtx    map[string]map[string]binding // context → canonical chord → binding
	prefixes map[string]map[string]bool    // context → strict chord prefixes
	Issues   []Issue
}

var reserved = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range ext.ReservedKeys {
		if c, err := CanonicalKey(k); err == nil {
			m[c] = true
		}
	}
	return m
}()

var warn = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range ext.WarnKeys {
		if c, err := CanonicalKey(k); err == nil {
			m[c] = true
		}
	}
	return m
}()

// New builds a keymap from sources applied in order: later sources override earlier
// ones per (context, chord); an unbind entry removes the chord from its context.
func New(sources ...Source) *Keymap {
	km := &Keymap{byCtx: map[string]map[string]binding{}}
	known := map[ext.ActionID]bool{}
	for _, a := range ext.ClaudeActions {
		known[a] = true
	}
	for _, src := range sources {
		if src.Err != nil {
			km.Issues = append(km.Issues, Issue{Source: src.Name, Severity: SevError, Message: src.Err.Error()})
		}
		for _, e := range src.Bindings {
			issue := func(sev Severity, msg string) {
				km.Issues = append(km.Issues, Issue{Source: src.Name, Context: e.Context, Keys: e.Keys, Action: e.Action, Severity: sev, Message: msg})
			}
			chord, err := CanonicalChord(e.Keys)
			if err != nil {
				issue(SevError, err.Error())
				continue
			}
			if e.Context != ext.ContextMantle && e.Context != ext.ContextResearch && !slices.Contains(ext.Contexts, e.Context) {
				issue(SevWarning, "unknown context")
			}
			action := e.Action
			if to, ok := ext.ActionAliases[action]; ok {
				action = to
			}
			if action != "" {
				switch {
				case src.Mantle && !action.IsMantle():
					issue(SevError, "~/.mantle/keybindings.json binds only mantle:* actions; put Claude Code actions in ~/.claude/keybindings.json")
					continue
				case !src.Mantle && src.User && action.IsMantle():
					issue(SevError, "mantle:* actions belong in ~/.mantle/keybindings.json")
					continue
				case !action.IsMantle() && !known[action]:
					issue(SevWarning, "unknown action (newer Claude Code?)")
				}
			}
			if src.User {
				first := strings.SplitN(chord, " ", 2)[0]
				if reserved[first] || reserved[chord] {
					issue(SevError, "reserved key cannot be rebound")
					continue
				}
				if warn[first] {
					issue(SevWarning, "ctrl+z suspends mantle in most terminals")
				}
			}
			m := km.byCtx[e.Context]
			if m == nil {
				m = map[string]binding{}
				km.byCtx[e.Context] = m
			}
			if action == "" {
				delete(m, chord)
				continue
			}
			if prev, ok := m[chord]; ok && prev.source == src.Name && prev.action != action {
				issue(SevWarning, fmt.Sprintf("also bound to %s in the same file", prev.action))
			}
			m[chord] = binding{action: action, display: e.Keys, source: src.Name}
		}
	}
	km.prefixes = map[string]map[string]bool{}
	for ctx, m := range km.byCtx {
		p := map[string]bool{}
		for chord := range m {
			keys := strings.Split(chord, " ")
			for i := 1; i < len(keys); i++ {
				p[strings.Join(keys[:i], " ")] = true
			}
		}
		km.prefixes[ctx] = p
		for chord, b := range m {
			if p[chord] {
				km.Issues = append(km.Issues, Issue{Source: b.source, Context: ctx, Keys: b.display, Action: b.action, Severity: SevWarning,
					Message: "also a chord prefix in this context; the chord wins and this binding never fires"})
			}
		}
	}
	sort.SliceStable(km.Issues, func(i, j int) bool { return km.Issues[i].Source < km.Issues[j].Source })
	return km
}

// Lookup returns the action bound to a canonical chord in a context.
func (km *Keymap) Lookup(context, chord string) (ext.ActionID, bool) {
	b, ok := km.byCtx[context][chord]
	return b.action, ok
}

// IsPrefix reports whether chord is a strict prefix of a bound chord in context.
func (km *Keymap) IsPrefix(context, chord string) bool { return km.prefixes[context][chord] }

// KeysFor returns the chords bound to an action, as their sources wrote them. Context
// "" searches every context. The order is deterministic (by context, then chord).
func (km *Keymap) KeysFor(context string, a ext.ActionID) []string {
	var ctxs []string
	if context != "" {
		ctxs = []string{context}
	} else {
		for c := range km.byCtx {
			ctxs = append(ctxs, c)
		}
		sort.Strings(ctxs)
	}
	var out []string
	for _, c := range ctxs {
		var chords []string
		for chord, b := range km.byCtx[c] {
			if b.action == a {
				chords = append(chords, chord)
			}
		}
		sort.Slice(chords, func(i, j int) bool {
			// Shorter chords first: "ctrl+g" before "ctrl+x ctrl+e".
			if len(chords[i]) != len(chords[j]) {
				return len(chords[i]) < len(chords[j])
			}
			return chords[i] < chords[j]
		})
		for _, chord := range chords {
			out = append(out, km.byCtx[c][chord].display)
		}
	}
	return out
}

// Binding is one resolved binding, for listings.
type Binding struct {
	Context, Keys, Chord string
	Action               ext.ActionID
	Source               string
}

// All lists every binding, sorted by context then chord.
func (km *Keymap) All() []Binding {
	var out []Binding
	for ctx, m := range km.byCtx {
		for chord, b := range m {
			out = append(out, Binding{Context: ctx, Keys: b.display, Chord: chord, Action: b.action, Source: b.source})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Context != out[j].Context {
			return out[i].Context < out[j].Context
		}
		return out[i].Chord < out[j].Chord
	})
	return out
}
