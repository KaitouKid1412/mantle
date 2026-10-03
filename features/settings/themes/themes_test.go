package themes

import (
	"testing"
	"testing/fstest"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

func fixtureFS() fstest.MapFS {
	return fstest.MapFS{
		"ocean.json":  {Data: []byte(`{"base":"dark","overrides":{"claude":"#3a7bd5"}}`)},
		"broken.json": {Data: []byte(`{"base":`)},
		"notes.txt":   {Data: []byte("ignored")},
		"amber.json":  {Data: []byte(`{}`)},
	}
}

func names(es []Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Name)
	}
	return out
}

func TestList(t *testing.T) {
	es := ListFS(fixtureFS(), "/t/themes")
	got := names(es)
	want := append(append([]string(nil), BuiltIn...), "custom:amber", "custom:broken", "custom:ocean")
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	broken := es[len(BuiltIn)+1]
	if broken.Selectable() || broken.Err == "" {
		t.Error("broken theme selectable")
	}
	if es[len(BuiltIn)].Path != "/t/themes/amber.json" || es[len(BuiltIn)].Label != "Custom: amber" {
		t.Errorf("custom entry %+v", es[len(BuiltIn)])
	}
	if len(List("/does/not/exist")) != len(BuiltIn) {
		t.Error("missing dir")
	}
}

func TestPickerPreviewConfirm(t *testing.T) {
	p := NewPicker(ListFS(fixtureFS(), "/t"), "dark", false)
	if p.Highlighted().Name != "dark" {
		t.Fatalf("opened on %s", p.Highlighted().Name)
	}
	if prev, ok := p.Move(1); !ok || prev != "light" {
		t.Errorf("move: %s %v", prev, ok)
	}
	if _, ok := p.Move(-100); !ok || p.Highlighted().Name != "auto" {
		t.Errorf("clamp top: %s", p.Highlighted().Name)
	}
	if _, ok := p.Move(-1); ok {
		t.Error("moved past top")
	}
	// Moving onto the custom block skips the broken theme.
	p.Cursor = len(BuiltIn) // custom:amber
	if prev, _ := p.Move(1); prev != "custom:ocean" {
		t.Errorf("skip broken: %s", prev)
	}
	if _, ok := p.Move(1); ok {
		t.Error("moved past bottom")
	}
	chosen, eff := p.Confirm()
	if chosen != "custom:ocean" || len(eff) != 1 {
		t.Fatalf("confirm %s %v", chosen, eff)
	}
	doc, err := eff[0].Patch.Apply(map[string]any{"theme": "dark", "model": "opus"})
	if err != nil || doc["theme"] != "custom:ocean" || doc["model"] != "opus" || eff[0].Patch.Scope != patch.User {
		t.Errorf("patch %v %v", doc, err)
	}
	if !p.Done() {
		t.Error("not done")
	}
	if _, ok := p.Move(1); ok {
		t.Error("moved after confirm")
	}
}

func TestPickerCancelRestores(t *testing.T) {
	p := NewPicker(ListFS(fixtureFS(), "/t"), "light", false)
	p.Move(2)
	p.ToggleSyntax()
	restore, syntaxOff := p.Cancel()
	if restore != "light" || syntaxOff || p.Highlighted().Name != "light" {
		t.Errorf("cancel: %s %v %s", restore, syntaxOff, p.Highlighted().Name)
	}
}

func TestPickerNoChangeNoWrite(t *testing.T) {
	p := NewPicker(ListFS(fixtureFS(), "/t"), "dark", true)
	p.Move(1)
	p.Move(-1)
	if _, eff := p.Confirm(); len(eff) != 0 {
		t.Errorf("wrote %v", eff)
	}
}

func TestPickerSyntaxToggle(t *testing.T) {
	p := NewPicker(ListFS(fixtureFS(), "/t"), "dark", true)
	p.ToggleSyntax()
	_, eff := p.Confirm()
	doc, _ := eff[0].Patch.Apply(map[string]any{"syntaxHighlightingDisabled": true})
	if _, ok := doc["syntaxHighlightingDisabled"]; ok {
		t.Error("re-enabling highlighting should unset the key")
	}
	p = NewPicker(ListFS(fixtureFS(), "/t"), "dark", false)
	p.ToggleSyntax()
	_, eff = p.Confirm()
	doc, _ = eff[0].Patch.Apply(nil)
	if doc["syntaxHighlightingDisabled"] != true {
		t.Error("disabling highlighting not saved")
	}
}

func TestPickerUnknownCurrent(t *testing.T) {
	p := NewPicker(ListFS(fixtureFS(), "/t"), "custom:deleted", false)
	if p.Highlighted().Name != "custom:deleted" {
		t.Fatalf("unknown current not kept: %s", p.Highlighted().Name)
	}
	p.Move(-1)
	if restore, _ := p.Cancel(); restore != "custom:deleted" {
		t.Error("restore")
	}
	if p := NewPicker(nil, "", false); p.Original != "dark" {
		t.Errorf("empty current: %s", p.Original)
	}
}

func TestCustomPath(t *testing.T) {
	if p, ok := CustomPath("/h/.claude/themes", "custom:ocean"); !ok || p != "/h/.claude/themes/ocean.json" {
		t.Errorf("%s %v", p, ok)
	}
	for _, bad := range []string{"dark", "custom:", "custom:../x", "custom:a/b"} {
		if _, ok := CustomPath("/h", bad); ok {
			t.Errorf("%s accepted", bad)
		}
	}
}
