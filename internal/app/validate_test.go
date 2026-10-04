package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/config"
	"github.com/KaitouKid1412/mantle/internal/keymap"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func TestStartupValidationNotices(t *testing.T) {
	home := t.TempDir()
	p := config.PathsFor(home, filepath.Join(home, "proj"), "", "")
	p.Managed = ""
	os.MkdirAll(filepath.Join(home, "proj", ".claude"), 0o755)
	os.MkdirAll(p.ClaudeDir, 0o755)
	os.WriteFile(p.ScopeFile(ext.ScopeProject), []byte(`{"theme":`), 0o644)                   // invalid
	os.WriteFile(p.ScopeFile(ext.ScopeUser), []byte(`{"keybindingFlavor":"classic"}`), 0o644) // deprecated
	store := config.NewStore(p, "")
	bad, _ := keymap.ParseFile("keybindings.json", []byte(`{"bindings":[{"context":"Chat","bindings":{"ctrl+c":"chat:stash"}}]}`), false)

	h := NewHost(nil, HostOptions{Core: CoreFeatures()})
	r := New(Options{Host: h, Settings: store, KeymapSources: []keymap.Source{bad}, NoBackgroundQuery: true, Clock: &manualClock{t: time.Unix(0, 0)}})
	r.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	drive(t, r, cmdMsgsAll(r.Init())...)

	var texts []string
	for _, n := range r.Notices() {
		texts = append(texts, n.Text)
	}
	all := strings.Join(texts, "\n")
	for _, want := range []string{"Ignoring invalid settings in " + p.ScopeFile(ext.ScopeProject), "keybindingFlavor is deprecated", "keybindings: ctrl+c: reserved key"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing notice %q in:\n%s", want, all)
		}
	}
}

// cmdMsgsAll runs a Cmd (without waiting on ticks longer than a few ms) and returns
// its messages.
func cmdMsgsAll(c tea.Cmd) []tea.Msg {
	if c == nil {
		return nil
	}
	m := c()
	if b, ok := m.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, cc := range b {
			out = append(out, cmdMsgsAll(cc)...)
		}
		return out
	}
	if m == nil {
		return nil
	}
	return []tea.Msg{m}
}
