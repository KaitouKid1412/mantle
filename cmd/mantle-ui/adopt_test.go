package main

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/engine"
)

func TestAdoptHook(t *testing.T) {
	mgr := engine.NewManager(func(tea.Msg) {})
	if adoptHook(mgr, "") != nil {
		t.Fatal("no hand-off file: the host must spawn normally")
	}
	hook := adoptHook(mgr, filepath.Join(t.TempDir(), "missing.handoff.json"))
	if hook == nil {
		t.Fatal("hand-off file given: expected an Adopt hook")
	}
	// A missing or unreadable hand-off file is an error, so the host falls back to
	// MainSpawn (--resume) instead of running without an engine.
	if err := hook(); err == nil {
		t.Fatal("adopting a missing hand-off file must fail")
	}
}
