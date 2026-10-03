package keymap

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func TestCanonicalKey(t *testing.T) {
	cases := map[string]string{
		"ctrl+x":       "ctrl+x",
		"Ctrl+Shift+B": "ctrl+shift+b",
		"shift+ctrl+b": "ctrl+shift+b",
		"meta+p":       "alt+p",
		"opt+p":        "alt+p",
		"cmd+k":        "super+k",
		"esc":          "escape",
		"return":       "enter",
		"G":            "g",
		"shift+g":      "shift+g",
		"?":            "?",
		"ctrl+_":       "ctrl+_",
		"ctrl+-":       "ctrl+-",
		"space":        "space",
		" ":            "space",
		"pageup":       "pageup",
		"PgUp":         "pageup",
		"ctrl++":       "ctrl++",
	}
	for in, want := range cases {
		got, err := CanonicalKey(in)
		if err != nil || got != want {
			t.Errorf("CanonicalKey(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "hyper+x", "ctrl+nope"} {
		if _, err := CanonicalKey(bad); err == nil {
			t.Errorf("CanonicalKey(%q) should fail", bad)
		}
	}
	if c, _ := CanonicalChord("ctrl+x  Ctrl+K"); c != "ctrl+x ctrl+k" {
		t.Errorf("chord = %q", c)
	}
}

func TestEventKeys(t *testing.T) {
	cases := []struct {
		k    tea.KeyPressMsg
		want string
	}{
		{tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, "ctrl+c"},
		{tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, "shift+tab"},
		{tea.KeyPressMsg{Code: 'g', Text: "G", Mod: tea.ModShift, ShiftedCode: 'G'}, "shift+g"},
		{tea.KeyPressMsg{Code: tea.KeyEscape}, "escape"},
		{tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}, "ctrl+enter"},
		{tea.KeyPressMsg{Code: 'p', Mod: tea.ModAlt}, "alt+p"},
		{tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, "space"},
		{tea.KeyPressMsg{Code: tea.KeyPgDown}, "pagedown"},
		{tea.KeyPressMsg{Code: '_', Mod: tea.ModCtrl}, "ctrl+_"},
	}
	for _, c := range cases {
		got := EventKeys(c.k)
		if len(got) == 0 || got[0] != c.want {
			t.Errorf("EventKeys(%+v) = %v, want first %q", c.k, got, c.want)
		}
	}
	// kitty-style shifted symbol matches the plain symbol too.
	got := EventKeys(tea.KeyPressMsg{Code: '/', Text: "?", Mod: tea.ModShift})
	if strings.Join(got, ",") != "shift+/,?" {
		t.Errorf("shifted symbol = %v", got)
	}
}

func defaults(t *testing.T) *Keymap {
	t.Helper()
	km := New(DefaultsSource("defaults", ext.DefaultBindings))
	for _, i := range km.Issues {
		t.Errorf("default bindings issue: %v", i)
	}
	return km
}

func TestDefaultsResolve(t *testing.T) {
	km := defaults(t)
	if a, ok := km.Lookup(ext.ContextChat, "enter"); !ok || a != ext.ActChatSubmit {
		t.Fatalf("enter in Chat = %q", a)
	}
	if !km.IsPrefix(ext.ContextChat, "ctrl+x") {
		t.Fatal("ctrl+x should be a chord prefix in Chat")
	}
	got := km.KeysFor(ext.ContextChat, ext.ActChatExternalEditor)
	if strings.Join(got, ",") != "ctrl+g,ctrl+x ctrl+e" {
		t.Fatalf("KeysFor externalEditor = %v", got)
	}
}

func TestUserOverridesAndUnbind(t *testing.T) {
	user, err := ParseFile("user.json", []byte(`{
	  "$schema": "x",
	  "bindings": [
	    {"context": "Chat", "bindings": {"ctrl+e": "chat:externalEditor", "ctrl+s": null, "ctrl+c": "chat:stash", "mantle:x": null}},
	    {"context": "Chat", "bindings": {"ctrl+k": "mantle:selfmod"}},
	    {"context": "Nowhere", "bindings": {"f5": "chat:submit"}},
	    {"context": "Chat", "bindings": {"ctrl+q": "chat:messageSelector"}}
	  ]}`), false)
	if err == nil {
		// "mantle:x" is not a valid key; ParseFile only checks JSON shape.
	}
	if err != nil {
		t.Fatal(err)
	}
	km := New(DefaultsSource("defaults", ext.DefaultBindings), user)
	if a, _ := km.Lookup(ext.ContextChat, "ctrl+e"); a != ext.ActChatExternalEditor {
		t.Errorf("ctrl+e = %q", a)
	}
	if _, ok := km.Lookup(ext.ContextChat, "ctrl+s"); ok {
		t.Error("ctrl+s should be unbound")
	}
	if a, _ := km.Lookup(ext.ContextGlobal, "ctrl+c"); a != ext.ActAppInterrupt {
		t.Error("reserved default must survive")
	}
	if _, ok := km.Lookup(ext.ContextChat, "ctrl+c"); ok {
		t.Error("reserved key must not be user-bindable")
	}
	if _, ok := km.Lookup(ext.ContextChat, "ctrl+k"); ok {
		t.Error("mantle action in Claude file must be rejected")
	}
	if a, _ := km.Lookup("Nowhere", "f5"); a != ext.ActChatSubmit {
		t.Error("unknown context binding should still apply (with a warning)")
	}
	var msgs []string
	for _, i := range km.Issues {
		msgs = append(msgs, string(i.Severity)+":"+i.Keys+":"+i.Message)
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{"error:ctrl+c:reserved", "error:ctrl+k:mantle:* actions belong", "warning:f5:unknown context", "warning:ctrl+q:unknown action", "error:mantle:x:"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing issue %q in:\n%s", want, all)
		}
	}
}

func TestMantleFile(t *testing.T) {
	m, err := ParseFile("mantle.json", []byte(`{"bindings":[{"context":"Chat","bindings":{"ctrl+k":"mantle:selfmod","ctrl+e":"chat:externalEditor"}}]}`), true)
	if err != nil {
		t.Fatal(err)
	}
	km := New(DefaultsSource("defaults", ext.DefaultBindings), m)
	if a, _ := km.Lookup(ext.ContextChat, "ctrl+k"); a != "mantle:selfmod" {
		t.Errorf("ctrl+k = %q", a)
	}
	if _, ok := km.Lookup(ext.ContextChat, "ctrl+e"); ok {
		t.Error("Claude action in mantle file must be rejected")
	}
}

func TestLegacyAlias(t *testing.T) {
	u, _ := ParseFile("u", []byte(`{"bindings":[{"context":"MessageSelector","bindings":{"k":"messageSelector:up"}}]}`), false)
	km := New(u)
	if a, _ := km.Lookup(ext.ContextMessageSelector, "k"); a != ext.ActSelectPrevious {
		t.Errorf("alias not applied: %q", a)
	}
}

func TestChords(t *testing.T) {
	r := NewResolver(defaults(t))
	now := time.Unix(0, 0)
	chat := []string{ext.ContextChat}

	res := r.Resolve(chat, []string{"ctrl+x"}, now)
	if res.Outcome != Pending || r.Pending() != "ctrl+x" {
		t.Fatalf("ctrl+x: %+v pending=%q", res, r.Pending())
	}
	res = r.Resolve(chat, []string{"ctrl+k"}, now.Add(time.Second))
	if res.Outcome != Matched || res.Matches[0].Action != ext.ActChatKillAgents {
		t.Fatalf("ctrl+x ctrl+k: %+v", res)
	}

	// Timeout: the prefix expires, the next key resolves alone.
	r.Resolve(chat, []string{"ctrl+x"}, now)
	res = r.Resolve(chat, []string{"enter"}, now.Add(4*time.Second))
	if res.Outcome != Matched || res.Matches[0].Action != ext.ActChatSubmit {
		t.Fatalf("after timeout: %+v", res)
	}

	// Abandoned chord: ctrl+x then a key that completes nothing.
	r.Resolve(chat, []string{"ctrl+x"}, now)
	res = r.Resolve(chat, []string{"enter"}, now)
	if res.Outcome != Matched || res.Matches[0].Action != ext.ActChatQueueSubmit {
		t.Fatalf("ctrl+x enter: %+v", res)
	}
	r.Resolve(chat, []string{"ctrl+x"}, now)
	res = r.Resolve(chat, []string{"q"}, now)
	if res.Outcome != Abandoned || res.Retry != NoMatch {
		t.Fatalf("ctrl+x q: %+v", res)
	}
}

func TestContextStacking(t *testing.T) {
	r := NewResolver(defaults(t))
	now := time.Unix(0, 0)
	// Autocomplete over Chat: up goes to the menu first, then history.
	res := r.Resolve([]string{ext.ContextAutocomplete, ext.ContextChat}, []string{"up"}, now)
	if res.Outcome != Matched || len(res.Matches) != 2 ||
		res.Matches[0].Action != ext.ActAutocompletePrevious || res.Matches[1].Action != ext.ActHistoryPrevious {
		t.Fatalf("stacked up: %+v", res)
	}
	// Global is always consulted.
	res = r.Resolve([]string{ext.ContextChat}, []string{"ctrl+o"}, now)
	if res.Outcome != Matched || res.Matches[0].Action != ext.ActAppToggleTranscript {
		t.Fatalf("ctrl+o: %+v", res)
	}
	// Unbound printable key passes through.
	if res := r.Resolve([]string{ext.ContextChat}, []string{"a"}, now); res.Outcome != NoMatch {
		t.Fatalf("a: %+v", res)
	}
}

func TestUserOverDefaultPrecedence(t *testing.T) {
	u, _ := ParseFile("u", []byte(`{"bindings":[{"context":"Global","bindings":{"ctrl+o":"app:toggleTodos"}}]}`), false)
	r := NewResolver(New(DefaultsSource("defaults", ext.DefaultBindings), u))
	res := r.Resolve(nil, []string{"ctrl+o"}, time.Unix(0, 0))
	if res.Outcome != Matched || res.Matches[0].Action != ext.ActAppToggleTodos {
		t.Fatalf("user override: %+v", res)
	}
}
