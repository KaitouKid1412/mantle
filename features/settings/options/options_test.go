package options

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

// The table's integrity rules.
func TestTableIntegrity(t *testing.T) {
	keys := map[string]bool{}
	rows := map[string]bool{}
	for _, o := range Table {
		t.Run(o.Key, func(t *testing.T) {
			if o.Key == "" || o.Label == "" || o.Description == "" || o.Section == "" {
				t.Fatalf("incomplete option %+v", o)
			}
			if keys[strings.ToLower(o.Key)] {
				t.Errorf("duplicate key %s", o.Key)
			}
			keys[strings.ToLower(o.Key)] = true
			if o.Row != "" {
				if rows[strings.ToLower(o.Row)] {
					t.Errorf("duplicate row %s", o.Row)
				}
				rows[strings.ToLower(o.Row)] = true
			}
			// Every option has a write path that fits its store.
			switch o.Write {
			case WriteConfigWriter:
				if o.Store != StoreUser && o.Store != StoreLocal {
					t.Errorf("config writer for %s", o.Store)
				}
			case WriteUpdateSettings:
				if !UpdateSettingsAllowlist[o.Key] {
					t.Errorf("update_settings does not accept %s", o.Key)
				}
			case WriteEngineConfig:
				if o.Row == "" {
					t.Error("engine /config needs a row ID")
				}
			case WriteMantle:
				if o.Store != StoreMantle {
					t.Errorf("mantle write for %s", o.Store)
				}
			case WritePanel:
				if o.Panel == "" {
					t.Error("panel write without a panel")
				}
			default:
				t.Errorf("no write path")
			}
			// The global config is never written by mantle.
			if o.Store == StoreGlobal && o.Write != WriteEngineConfig {
				t.Errorf("global-config key %s written by %s", o.Key, o.Write)
			}
			if o.Type == Enum {
				if len(o.Values) < 2 {
					t.Error("enum with fewer than two values")
				}
				if s, ok := o.Default.(string); !ok || !o.hasValue(s) {
					t.Errorf("default %v not among values", o.Default)
				}
			}
			if o.Type == Bool {
				if _, ok := o.Default.(bool); !ok {
					t.Errorf("bool default %v", o.Default)
				}
			}
			if o.SessionOK && o.Store == StoreGlobal {
				t.Error("global-config keys cannot be session flags")
			}
		})
	}
}

func TestFind(t *testing.T) {
	for name, key := range map[string]string{
		"verbose": "verbose", "VERBOSE": "verbose", "turnDuration": "showTurnDuration",
		"showTurnDuration": "showTurnDuration", "permissionMode": "permissions.defaultMode",
		"permissions.defaultMode": "permissions.defaultMode", "gitignore": "respectGitignore",
	} {
		o, err := Find(name)
		if err != nil || o.Key != key {
			t.Errorf("Find(%q) = %q, %v", name, o.Key, err)
		}
	}
	if _, err := Find("nope"); !errors.Is(err, ErrUnknown) {
		t.Errorf("unknown: %v", err)
	}
}

func TestParseAndNext(t *testing.T) {
	v, _ := Find("verbose")
	for raw, want := range map[string]any{"on": true, "TRUE": true, "0": false, "off": false} {
		got, err := v.Parse(raw)
		if err != nil || got != want {
			t.Errorf("parse %q = %v, %v", raw, got, err)
		}
	}
	if _, err := v.Parse("maybe"); err == nil {
		t.Error("bad bool accepted")
	}
	tf, _ := Find("timeFormat")
	if got, err := tf.Parse("24-HOUR"); err != nil || got != "24-hour" {
		t.Errorf("enum parse %v %v", got, err)
	}
	if tf.Next("24-hour-utc") != "auto" || tf.Next("auto") != "12-hour" || tf.Next("junk") != "auto" {
		t.Error("enum cycle")
	}
	if v.Next(true) != false {
		t.Error("bool toggle")
	}
	lang, _ := Find("language")
	if got, _ := lang.Parse("Default"); got != "" {
		t.Errorf("language default -> %q", got)
	}
}

func TestValue(t *testing.T) {
	theme, _ := Find("theme")
	if got := theme.Value(map[string]any{"theme": "light"}, map[string]any{"theme": "dark-ansi"}); got != "light" {
		t.Errorf("settings first: %v", got)
	}
	if got := theme.Value(nil, map[string]any{"theme": "dark-ansi"}); got != "dark-ansi" {
		t.Errorf("global fallback: %v", got)
	}
	if got := theme.Value(map[string]any{"theme": 42}, nil); got != "dark" {
		t.Errorf("invalid value should fall back to default: %v", got)
	}
	mode, _ := Find("permissions.defaultMode")
	if got := mode.Value(map[string]any{"permissions": map[string]any{"defaultMode": "plan"}}, nil); got != "plan" {
		t.Errorf("nested: %v", got)
	}
	tips, _ := Find("tips")
	if got := tips.Value(nil, map[string]any{"spinnerTipsEnabled": false}); got != true {
		t.Errorf("no global fallback for settings-only key: %v", got)
	}
	gi, _ := Find("gitignore")
	if got := gi.Value(nil, map[string]any{"respectGitignore": false}); got != false {
		t.Errorf("global store: %v", got)
	}
}

func TestPlan(t *testing.T) {
	get := func(name string) Option {
		o, err := Find(name)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}

	// Settings file write.
	eff, err := Plan(get("verbose"), true, true)
	if err != nil || len(eff) != 1 || eff[0].Patch == nil || eff[0].Patch.Scope != patch.User {
		t.Fatalf("verbose: %v %v", eff, err)
	}
	doc, _ := eff[0].Patch.Apply(nil)
	if doc["verbose"] != true {
		t.Errorf("verbose doc %v", doc)
	}

	// Local settings and nested keys.
	eff, _ = Plan(get("tips"), false, true)
	if eff[0].Patch.Scope != patch.Local {
		t.Errorf("tips scope %s", eff[0].Patch.Scope)
	}
	eff, _ = Plan(get("permissionMode"), "acceptEdits", true)
	doc, _ = eff[0].Patch.Apply(map[string]any{"permissions": map[string]any{"allow": []any{"Read"}}})
	want := map[string]any{"permissions": map[string]any{"allow": []any{"Read"}, "defaultMode": "acceptEdits"}}
	if !reflect.DeepEqual(doc, want) {
		t.Errorf("nested doc %v", doc)
	}

	// UnsetWhen removes the key.
	eff, _ = Plan(get("thinking"), true, true)
	doc, _ = eff[0].Patch.Apply(map[string]any{"alwaysThinkingEnabled": false})
	if _, ok := doc["alwaysThinkingEnabled"]; ok {
		t.Error("thinking on should unset the key")
	}
	eff, _ = Plan(get("thinking"), false, true)
	doc, _ = eff[0].Patch.Apply(nil)
	if doc["alwaysThinkingEnabled"] != false {
		t.Error("thinking off should write false")
	}

	// Global config goes through the engine.
	eff, _ = Plan(get("copyOnSelect"), false, true)
	if eff[0].Command != "/config copyOnSelect=false" {
		t.Errorf("engine command %q", eff[0].Command)
	}
	eff, _ = Plan(get("prStatusFooterEnabled"), true, true)
	if eff[0].Command != "/config prStatus=true" {
		t.Errorf("engine command %q", eff[0].Command)
	}

	// Session only.
	eff, err = Plan(get("thinking"), false, false)
	if err != nil || eff[0].Control == nil || eff[0].Control.Subtype != "apply_flag_settings" ||
		!reflect.DeepEqual(eff[0].Control.Payload, map[string]any{"settings": map[string]any{"alwaysThinkingEnabled": false}}) {
		t.Errorf("session thinking %v %v", eff, err)
	}
	if _, err := Plan(get("verbose"), true, false); !errors.Is(err, ErrNoSession) {
		t.Errorf("session verbose: %v", err)
	}

	// Panels own their writes; validation errors surface.
	if _, err := Plan(get("theme"), "dark", true); !errors.Is(err, ErrPanel) {
		t.Errorf("theme: %v", err)
	}
	if _, err := Plan(get("timeFormat"), "13-hour", true); err == nil {
		t.Error("invalid enum accepted")
	}
	if _, err := Plan(get("verbose"), "yes", true); err == nil {
		t.Error("string for bool accepted")
	}
}

// Every persistent write path either targets a settings file through a patch or goes
// through the engine; none can name ~/.claude.json.
func TestNoGlobalConfigWrites(t *testing.T) {
	env := patch.Env{Home: t.TempDir(), ProjectRoot: t.TempDir()}
	for _, o := range Table {
		var v any = o.Default
		if o.Type == Bool {
			v = !o.Default.(bool)
		}
		eff, err := Plan(o, v, true)
		if errors.Is(err, ErrPanel) {
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", o.Key, err)
			continue
		}
		for _, e := range eff {
			if e.Patch == nil {
				continue
			}
			p, err := env.Path(e.Patch.Scope)
			if err != nil {
				t.Errorf("%s: %v", o.Key, err)
			}
			if strings.HasSuffix(p, ".claude.json") {
				t.Errorf("%s writes %s", o.Key, p)
			}
		}
	}
}

func TestParseAssignments(t *testing.T) {
	cases := []struct {
		in   string
		want []Assignment
		ok   bool
	}{
		{"", nil, false},
		{"verbose", nil, false},
		{"verbose=true", []Assignment{{"verbose", "true"}}, true},
		{"language=brazilian portuguese", []Assignment{{"language", "brazilian portuguese"}}, true},
		{"verbose=true tips=off", []Assignment{{"verbose", "true"}, {"tips", "off"}}, true},
		// One "=" keeps the rest as the value, as the engine does; Parse then rejects it.
		{"verbose=true junk", []Assignment{{"verbose", "true junk"}}, true},
		{"a=1 b=2 c", nil, false},
		{"=true", nil, false},
	}
	for _, c := range cases {
		got, ok := ParseAssignments(c.in)
		if ok != c.ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %v %v", c.in, got, ok)
		}
	}
}

func TestSections(t *testing.T) {
	s := Sections()
	if len(s) != 7 || s[0] != SecModel {
		t.Errorf("sections %v", s)
	}
}
