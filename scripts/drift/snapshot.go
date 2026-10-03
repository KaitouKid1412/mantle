package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// List kinds. Every collector produces normalized names only (flag names, action IDs,
// setting keys), never help text or descriptions.
const (
	KindFlag                = "flag"                 // Scope "" is claude's root; otherwise a subcommand
	KindSubcommand          = "subcommand"           // Scope "" is a root subcommand; otherwise its parent
	KindSubcommandCandidate = "subcommand-candidate" // command names in the binary not seen in any help
	KindAction              = "keybinding-action"
	KindContext             = "keybinding-context"
	KindSetting             = "setting" // dotted key, one level deep
	// Engine-reported lists (plan 11, B3).
	KindSlash       = "slash-command"
	KindTool        = "tool"
	KindOutputStyle = "output-style"
	KindModel       = "model"
	KindProtocol    = "protocol"
)

// Item is one normalized entry.
type Item struct {
	Name  string            `json:"name"`
	Scope string            `json:"scope,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Key identifies an item within its list.
func (it Item) Key() string {
	if it.Scope == "" {
		return it.Name
	}
	return it.Scope + " " + it.Name
}

// List is one collector's output.
type List struct {
	Kind   string `json:"kind"`
	Source string `json:"source"`
	// Error is set when the collector couldn't run; Items is then empty.
	Error string `json:"error,omitempty"`
	Items []Item `json:"items,omitempty"`
}

// Available reports whether the collector produced data.
func (l *List) Available() bool { return l != nil && l.Error == "" }

// Snapshot is everything collected from one claude version.
type Snapshot struct {
	ClaudeVersion string `json:"claude_version"`
	Lists         []List `json:"lists"`
}

// List returns the list of the given kind, or nil.
func (s *Snapshot) List(kind string) *List {
	for i := range s.Lists {
		if s.Lists[i].Kind == kind {
			return &s.Lists[i]
		}
	}
	return nil
}

// Put adds or replaces a list, normalizing it.
func (s *Snapshot) Put(l List) {
	normalize(&l)
	if old := s.List(l.Kind); old != nil {
		*old = l
	} else {
		s.Lists = append(s.Lists, l)
	}
	slices.SortFunc(s.Lists, func(a, b List) int { return strings.Compare(a.Kind, b.Kind) })
}

// normalize sorts items by key and merges duplicates.
func normalize(l *List) {
	slices.SortFunc(l.Items, func(a, b Item) int {
		return cmp.Or(strings.Compare(a.Scope, b.Scope), strings.Compare(a.Name, b.Name))
	})
	l.Items = slices.CompactFunc(l.Items, func(a, b Item) bool { return a.Key() == b.Key() })
}

func loadSnapshot(path string) (Snapshot, error) {
	var s Snapshot
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	for i := range s.Lists {
		normalize(&s.Lists[i])
	}
	return s, nil
}

func saveJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
