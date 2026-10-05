package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

type initFixture struct {
	Models      []Info
	Unavailable []Info
}

func loadFixture(t *testing.T) initFixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "08", "model", "init_models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var init proto.InitializeResponse
	if err := json.Unmarshal(b, &init); err != nil {
		t.Fatal(err)
	}
	return initFixture{Models: FromProto(init.Models), Unavailable: ParseUnavailable(init.UnavailableModels)}
}

func TestParseUnavailable(t *testing.T) {
	if ParseUnavailable(nil) != nil || ParseUnavailable(json.RawMessage(`{"not":"a list"}`)) != nil {
		t.Error("bad input should yield nothing")
	}
	got := ParseUnavailable(json.RawMessage(`[{"value":"x","displayName":"X","disabled":true}]`))
	if len(got) != 1 || got[0].Value != "x" {
		t.Errorf("got %+v", got)
	}
}

func settings(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestFamily(t *testing.T) {
	cases := map[string]string{
		"claude-opus-5-5":           "claude-opus-5-5",
		"claude-opus-5-5[1m]":       "claude-opus-5-5",
		"Claude-Opus-5-5":           "claude-opus-5-5",
		"claude-haiku-4-5-20251001": "claude-haiku-4-5",
		"opus":                      "opus",
		"":                          "",
	}
	for in, want := range cases {
		if got := Family(in); got != want {
			t.Errorf("Family(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRowsFromFixture(t *testing.T) {
	f := loadFixture(t)
	rows := Rows(f.Models, f.Unavailable, Current{Model: "opus"}, EffortInputs{})
	var labels []string
	for _, r := range rows {
		labels = append(labels, r.Label)
	}
	want := []string{"Default (recommended)", "Fable", "Opus", "Opus (1M context)", "Sonnet", "Haiku", "Legacy X"}
	if !reflect.DeepEqual(labels, want) {
		t.Fatalf("labels %q", labels)
	}
	if !rows[0].IsDefault || rows[0].Value != DefaultValue {
		t.Errorf("first row not default: %+v", rows[0])
	}
	cur := 0
	for i, r := range rows {
		if r.Current {
			cur++
			if r.Label != "Opus" {
				t.Errorf("current is %q (row %d)", r.Label, i)
			}
		}
	}
	if cur != 1 {
		t.Errorf("%d current rows", cur)
	}
	last := rows[len(rows)-1]
	if !last.Disabled {
		t.Error("unavailable model is selectable")
	}
	haiku := rows[5]
	if haiku.Effort.Source != SourceNone || len(haiku.Efforts) != 0 {
		t.Errorf("haiku efforts: %+v", haiku)
	}
	if haiku.Thinking != ThinkingToggle {
		t.Error("haiku thinking should be toggleable")
	}
	if rows[2].Thinking != ThinkingLocked || !rows[2].Fast || !rows[2].Auto {
		t.Errorf("opus flags: %+v", rows[2])
	}
	if rows[1].Fast {
		t.Error("fable has no fast mode in fixture")
	}
}

func TestSynthesizedDefaultRow(t *testing.T) {
	rows := Rows([]Info{{Value: "sonnet", DisplayName: "Sonnet"}}, nil, Current{}, EffortInputs{})
	if len(rows) != 2 || !rows[0].IsDefault || rows[0].Label != DefaultLabel || !rows[0].Current {
		t.Fatalf("rows %+v", rows)
	}
}

func TestCurrentIndex(t *testing.T) {
	f := loadFixture(t)
	cases := map[string]string{
		"":                          "Default (recommended)",
		"default":                   "Default (recommended)",
		"opus":                      "Opus",
		"opus[1m]":                  "Opus (1M context)",
		"claude-opus-5-5[1m]":       "Opus (1M context)",
		"claude-opus-5-5":           "Opus",
		"claude-sonnet-5-5":         "Sonnet",
		"claude-haiku-4-5-20251001": "Haiku",
		"claude-haiku-4-5":          "Haiku",
		"claude-legacy-x":           "",
		"something-else":            "",
	}
	for model, want := range cases {
		rows := Rows(f.Models, f.Unavailable, Current{Model: model}, EffortInputs{})
		i := CurrentIndex(rows, Current{Model: model})
		got := ""
		if i >= 0 {
			got = rows[i].Label
		}
		if got != want {
			t.Errorf("current %q -> %q, want %q", model, got, want)
		}
	}
}

func TestResolveEffort(t *testing.T) {
	f := loadFixture(t)
	opus, sonnet, haiku := f.Models[2], f.Models[4], f.Models[5]
	userSettings := settings(t, `{"effortLevel":"medium","modelSettings":{"claude-opus-5-5":{"effortLevel":"xhigh"}}}`)
	cases := []struct {
		name string
		info Info
		in   EffortInputs
		want Resolved
	}{
		{"model settings win over effortLevel", opus, EffortInputs{Settings: userSettings},
			Resolved{Effort: XHigh, Source: SourceModelSettings, Requested: XHigh}},
		{"1m variant shares family settings", f.Models[3], EffortInputs{Settings: userSettings},
			Resolved{Effort: XHigh, Source: SourceModelSettings, Requested: XHigh}},
		{"default row resolves via resolvedModel", f.Models[0], EffortInputs{Settings: userSettings},
			Resolved{Effort: XHigh, Source: SourceModelSettings, Requested: XHigh}},
		{"top-level effortLevel", sonnet, EffortInputs{Settings: userSettings},
			Resolved{Effort: Medium, Source: SourceSetting, Requested: Medium}},
		{"session wins", opus, EffortInputs{Session: Low, Env: "max", Settings: userSettings},
			Resolved{Effort: Low, Source: SourceSession, Requested: Low}},
		{"env beats settings", opus, EffortInputs{Env: "HIGH", Settings: userSettings},
			Resolved{Effort: High, Source: SourceEnv, Requested: High}},
		{"env auto is unset", sonnet, EffortInputs{Env: "auto", Settings: userSettings},
			Resolved{Effort: Medium, Source: SourceSetting, Requested: Medium}},
		{"clamped to supported levels", sonnet, EffortInputs{Session: Max},
			Resolved{Effort: High, Source: SourceSession, Requested: Max, Capped: true}},
		{"default effort", opus, EffortInputs{},
			Resolved{Effort: Medium, Source: SourceDefault}},
		{"no effort support", haiku, EffortInputs{Session: High}, Resolved{Source: SourceNone}},
		{"global cap", opus, EffortInputs{Session: Max, Settings: settings(t, `{"maxEffortLevel":"high"}`)},
			Resolved{Effort: High, Source: SourceSession, Requested: Max, Capped: true}},
		{"per-model cap", opus, EffortInputs{Settings: settings(t,
			`{"effortLevel":"xhigh","modelSettings":{"claude-opus-5-5":{"maxEffortLevel":"medium"}}}`)},
			Resolved{Effort: Medium, Source: SourceSetting, Requested: XHigh, Capped: true}},
		{"garbage setting ignored", opus, EffortInputs{Settings: settings(t, `{"effortLevel":"turbo"}`)},
			Resolved{Effort: Medium, Source: SourceDefault}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveEffort(c.info, c.in); got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestClampAndStep(t *testing.T) {
	lmh := []Effort{Low, Medium, High}
	if Clamp(Max, lmh) != High || Clamp(Low, []Effort{High, XHigh}) != High || Clamp(Medium, nil) != "" {
		t.Error("clamp")
	}
	if Step(Medium, lmh, 1) != High || Step(High, lmh, 1) != High || Step(Low, lmh, -1) != Low || Step(XHigh, lmh, -1) != Medium {
		t.Error("step")
	}
	if Max.Persistable() || !XHigh.Persistable() || Effort("").Persistable() {
		t.Error("persistable")
	}
}

func TestUltracode(t *testing.T) {
	on, off := true, false
	if Ultracode(nil, nil) || !Ultracode(nil, settings(t, `{"ultracode":true}`)) ||
		!Ultracode(nil, settings(t, `{"effortLevel":"ultracode"}`)) || Ultracode(&off, settings(t, `{"ultracode":true}`)) ||
		!Ultracode(&on, nil) {
		t.Error("ultracode resolution")
	}
}

func TestPlan(t *testing.T) {
	f := loadFixture(t)
	rows := Rows(f.Models, nil, Current{}, EffortInputs{})
	opus, _ := FindRow(rows, "opus")
	def, _ := FindRow(rows, "default")

	steps := Plan(Choice{Row: opus, Effort: XHigh})
	if len(steps) != 3 {
		t.Fatalf("steps %+v", steps)
	}
	if c := steps[0].Control; c == nil || c.Subtype != "set_model" || c.Payload["model"] != "opus" {
		t.Errorf("set_model %+v", c)
	}
	if c := steps[1].Control; c == nil || c.Subtype != "apply_flag_settings" ||
		!reflect.DeepEqual(c.Payload, map[string]any{"settings": map[string]any{"effortLevel": "xhigh"}}) {
		t.Errorf("flag %+v", c)
	}
	got, err := steps[2].Patch.Apply(settings(t, `{"theme":"dark"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := settings(t, `{"theme":"dark","model":"opus","modelSettings":{"claude-opus-5-5":{"effortLevel":"xhigh"}}}`)
	if !patch.Equal(got, want) {
		t.Errorf("persisted %v", got)
	}

	// Session only: no patch.
	for _, s := range Plan(Choice{Row: opus, Effort: Low, SessionOnly: true}) {
		if s.Patch != nil {
			t.Error("session-only choice wrote settings")
		}
	}

	// Default row: set_model("default") and model removed from settings.
	steps = Plan(Choice{Row: def})
	if steps[0].Control.Payload["model"] != DefaultValue {
		t.Error("default should send \"default\"")
	}
	got, _ = steps[len(steps)-1].Patch.Apply(settings(t, `{"model":"sonnet"}`))
	if _, ok := got["model"]; ok {
		t.Error("default did not unset model")
	}

	// Max is applied but not saved.
	steps = Plan(Choice{Row: opus, Effort: Max})
	var note bool
	for _, s := range steps {
		if s.Note != "" {
			note = true
		}
		if s.Patch != nil {
			if _, ok := patch.Get(must(s.Patch.Apply(nil)), "modelSettings"); ok {
				t.Error("max effort persisted")
			}
		}
	}
	if !note {
		t.Error("no note for max")
	}

	// Effort for a model without levels is dropped.
	haiku, _ := FindRow(rows, "Haiku")
	for _, s := range Plan(Choice{Row: haiku, Effort: High}) {
		if s.Control != nil && s.Control.Subtype == "apply_flag_settings" {
			t.Error("effort sent for haiku")
		}
	}
}

func must(m map[string]any, err error) map[string]any {
	if err != nil {
		panic(err)
	}
	return m
}

func TestFindRow(t *testing.T) {
	f := loadFixture(t)
	rows := Rows(f.Models, f.Unavailable, Current{}, EffortInputs{})
	for name, want := range map[string]string{
		"opus": "Opus", "OPUS": "Opus", "Opus (1M context)": "Opus (1M context)",
		"claude-sonnet-5-5": "Sonnet", "default": "Default (recommended)",
	} {
		r, ok := FindRow(rows, name)
		if !ok || r.Label != want {
			t.Errorf("FindRow(%q) = %q, %v", name, r.Label, ok)
		}
	}
	if _, ok := FindRow(rows, "claude-legacy-x"); ok {
		t.Error("found disabled model")
	}
	if _, ok := FindRow(rows, "nope"); ok {
		t.Error("found unknown model")
	}
}
