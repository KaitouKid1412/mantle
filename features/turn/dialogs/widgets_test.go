package dialogs

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"plain":                      "plain",
		"a\tb":                       "a    b",
		"line1\r\nline2":             "line1\nline2",
		"\x1b[31mred\x1b[0m":         "red",
		"title\x1b]0;evil\x07 after": "title after",
		"bell\x07 nul\x00 del\x7f":   "bell nul del",
		"c1\u009bx":                  "c1x",
		"rtl ‮gnp.exe":               "rtl <U+202E>gnp.exe",
		"zero​width":                 "zerowidth",
		"emoji 👩‍💻":                  "emoji 👩‍💻",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q want %q", in, got, want)
		}
	}
	if got := SanitizeLine("a\nb"); got != "a⏎ b" {
		t.Errorf("SanitizeLine = %q", got)
	}
}

func TestTextFieldEditing(t *testing.T) {
	f := &textField{}
	for _, k := range []string{"h", "e", "l", "l", "o", "space", "w", "o", "r", "l", "d"} {
		f.HandleKey(key(k))
	}
	if f.Value() != "hello world" {
		t.Fatalf("typed %q", f.Value())
	}
	f.HandleKey(key("ctrl+w"))
	if f.Value() != "hello " {
		t.Fatalf("ctrl+w → %q", f.Value())
	}
	f.HandleKey(key("ctrl+a"))
	f.HandleKey(key("ctrl+k"))
	if f.Value() != "" {
		t.Fatalf("ctrl+a ctrl+k → %q", f.Value())
	}
	f.insert("one\ntwo")
	if f.Value() != "one two" {
		t.Fatalf("single-line paste kept newline: %q", f.Value())
	}
	f.HandleKey(key("left"))
	f.HandleKey(key("left"))
	f.HandleKey(key("backspace"))
	if f.Value() != "one wo" || f.pos != 4 {
		t.Fatalf("backspace mid-line → %q pos %d", f.Value(), f.pos)
	}
	if f.HandleKey(key("enter")) || f.HandleKey(key("esc")) {
		t.Fatal("enter/esc belong to the dialog")
	}
	if f.HandleKey(key("ctrl+j")) {
		t.Fatal("ctrl+j is not a newline in a single-line field")
	}
	m := &textField{multiline: true}
	m.insert("a")
	m.HandleKey(key("ctrl+j"))
	m.insert("b\x1b[2J")
	if m.Value() != "a\nb" {
		t.Fatalf("multiline → %q", m.Value())
	}
}

func TestTextFieldRenderWidth(t *testing.T) {
	f := &textField{}
	f.SetValue("abcdefghij")
	lines := f.lines(6, testStyles(), true, "> ")
	if len(lines) != 3 {
		t.Fatalf("lines = %q", lines)
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > 6 {
			t.Fatalf("line too wide: %q", l)
		}
	}
	empty := &textField{placeholder: "Type here"}
	if got := ansi.Strip(empty.lines(20, PlainStyles(), false, "> ")[0]); got != "> Type here" {
		t.Fatalf("placeholder line = %q", got)
	}
}

func TestTextAcceptsNonStrings(t *testing.T) {
	var r ToolRequest
	if err := json.Unmarshal([]byte(`{"tool_name":"X","decision_reason":{"type":"rule","rule":"Bash(rm:*)"}}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.DecisionReason != `{"type":"rule","rule":"Bash(rm:*)"}` {
		t.Fatalf("decision_reason = %q", r.DecisionReason)
	}
}

func TestPermissionResultShapes(t *testing.T) {
	deny := PermissionResult{Behavior: "deny"}
	if got := toJSON(t, deny); got != `{"behavior":"deny","message":""}` {
		t.Fatalf("deny always carries message: %s", got)
	}
	allow := PermissionResult{Behavior: "allow", UpdatedPermissions: []PermissionUpdate{SetModeUpdate("plan", "session")}}
	if got := toJSON(t, allow); got != `{"behavior":"allow","updatedPermissions":[{"type":"setMode","mode":"plan","destination":"session"}]}` {
		t.Fatalf("allow = %s", got)
	}
}

func TestWrapKeepsURLsAndIndent(t *testing.T) {
	got := wrap("see https://example.com/docs/en/auto-mode-classifier-billing now", 40)
	want := []string{"see", "https://example.com/docs/en/auto-mode-cl", "assifier-billing now"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("long word: %q", got)
	}
	got = wrap("visit https://x.test/a-b-c please", 30)
	if strings.Join(got, "|") != "visit https://x.test/a-b-c|please" {
		t.Fatalf("hyphenated URL split: %q", got)
	}
	got = wrap("  indented line", 40)
	if got[0] != "  indented line" {
		t.Fatalf("indent lost: %q", got)
	}
	for _, l := range wrap(strings.Repeat("word ", 30), 17) {
		if ansi.StringWidth(l) > 17 {
			t.Fatalf("line too wide: %q", l)
		}
	}
}
