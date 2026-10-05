package app

import (
	"slices"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func menuNames(r *Root) []string {
	var out []string
	for _, c := range r.Ctx().Commands() {
		out = append(out, c.Name)
	}
	return out
}

func TestCommandVisibility(t *testing.T) {
	f := ext.Feature{ID: "test.cmds", Order: 10, Setup: func(r ext.Registrar) error {
		r.AddCommand(ext.Command{Name: "fast", Description: "native"})
		r.AddCommand(ext.Command{Name: "secret", Hidden: true})
		r.AddCommand(ext.Command{Name: "model"})
		return nil
	}}
	r, _ := newRoot(t, f)
	drive(t, r, ext.CommandsMsg{Source: ext.SourceEngine, EngineID: ext.MainEngine, Commands: []ext.Command{
		{Name: "fast", Description: "engine"}, {Name: "secret", Description: "engine"}, {Name: "compact"},
	}})

	names := menuNames(r)
	if slices.Contains(names, "secret") {
		t.Fatalf("a registered-Hidden built-in must reserve its name, not let the engine's show: %v", names)
	}
	if !slices.Contains(names, "fast") || !slices.Contains(names, "compact") {
		t.Fatalf("menu = %v", names)
	}

	drive(t, r, ext.CommandVisibilityMsg{Source: "settings.account", Hidden: map[string]bool{"/fast": true, "compact": true}})
	names = menuNames(r)
	if slices.Contains(names, "fast") || slices.Contains(names, "compact") {
		t.Fatalf("overlay should hide fast and compact: %v", names)
	}
	if c, ok := r.Ctx().Command("fast"); !ok || c.Description != "native" {
		t.Fatalf("hidden commands still resolve when typed: %+v %v", c, ok)
	}

	// Overlays are per source; a command is hidden while any source hides it.
	drive(t, r, ext.CommandVisibilityMsg{Source: "ecosystem", Hidden: map[string]bool{"model": true}})
	drive(t, r, ext.CommandVisibilityMsg{Source: "settings.account"}) // clears that source only
	names = menuNames(r)
	if !slices.Contains(names, "fast") || slices.Contains(names, "model") {
		t.Fatalf("after clearing one source: %v", names)
	}
}
