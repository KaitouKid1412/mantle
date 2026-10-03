package mode

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Mode
		ok   bool
	}{
		{"default", Default, true},
		{"manual", Default, true},
		{"Manual", Default, true},
		{"acceptEdits", AcceptEdits, true},
		{"acceptedits", AcceptEdits, true},
		{"plan", Plan, true},
		{"auto", Auto, true},
		{"bypassPermissions", BypassPermissions, true},
		{"dontAsk", DontAsk, true},
		{" plan ", Plan, true},
		{"", "", false},
		{"bubble", "", false},
		{"yolo", "", false},
	}
	for _, c := range cases {
		got, ok := Parse(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("Parse(%q) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
	if Mode("manual").Valid() {
		t.Error("alias should not be a canonical mode")
	}
	if !Plan.Valid() {
		t.Error("plan should be valid")
	}
}

// The cycle order matches Claude Code 2.1.288: default → acceptEdits → plan →
// (bypassPermissions if enabled, else auto if available, else default); bypass → auto |
// default; auto and dontAsk → default.
func TestNext(t *testing.T) {
	none := Availability{}
	auto := Availability{Auto: true}
	bypass := Availability{Bypass: true}
	both := Availability{Bypass: true, Auto: true}
	cases := []struct {
		from Mode
		a    Availability
		want Mode
	}{
		{Default, none, AcceptEdits},
		{Default, both, AcceptEdits},
		{"manual", none, AcceptEdits},
		{AcceptEdits, none, Plan},
		{AcceptEdits, both, Plan},
		{Plan, none, Default},
		{Plan, auto, Auto},
		{Plan, bypass, BypassPermissions},
		{Plan, both, BypassPermissions},
		{BypassPermissions, none, Default},
		{BypassPermissions, bypass, Default},
		{BypassPermissions, both, Auto},
		{BypassPermissions, auto, Auto},
		{Auto, none, Default},
		{Auto, both, Default},
		{DontAsk, both, Default},
		{"garbage", both, AcceptEdits}, // unknown normalizes to default first
	}
	for _, c := range cases {
		if got := Next(c.from, c.a); got != c.want {
			t.Errorf("Next(%q, %+v) = %q want %q", c.from, c.a, got, c.want)
		}
	}
}

// Full loops: starting at default, repeated shift+tab visits exactly these modes.
func TestCycleLoops(t *testing.T) {
	cases := []struct {
		a    Availability
		want []Mode
	}{
		{Availability{}, []Mode{Default, AcceptEdits, Plan}},
		{Availability{Auto: true}, []Mode{Default, AcceptEdits, Plan, Auto}},
		{Availability{Bypass: true}, []Mode{Default, AcceptEdits, Plan, BypassPermissions}},
		{Availability{Bypass: true, Auto: true}, []Mode{Default, AcceptEdits, Plan, BypassPermissions, Auto}},
	}
	for _, c := range cases {
		m := Default
		var got []Mode
		for range 10 {
			got = append(got, m)
			m = Next(m, c.a)
			if m == Default {
				break
			}
		}
		if len(got) != len(c.want) {
			t.Fatalf("%+v: loop %v want %v", c.a, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%+v: loop %v want %v", c.a, got, c.want)
			}
		}
	}
}

func TestIndicator(t *testing.T) {
	cases := map[Mode]string{
		Default:           "⏸ manual mode on",
		AcceptEdits:       "⏵⏵ accept edits on",
		Plan:              "⏸ plan mode on",
		Auto:              "⏵⏵ auto mode on",
		BypassPermissions: "⏵⏵ bypass permissions on",
		"manual":          "⏸ manual mode on",
	}
	for m, want := range cases {
		if got := IndicatorFor(m).String(); got != want {
			t.Errorf("IndicatorFor(%q) = %q want %q", m, got, want)
		}
	}
	for _, m := range All {
		if IndicatorFor(m).Token == "" || Label(m) == "" {
			t.Errorf("mode %q lacks token or label", m)
		}
	}
}

func TestStartup(t *testing.T) {
	auto := Availability{Auto: true}
	both := Availability{Auto: true, Bypass: true}
	cases := []struct {
		name string
		in   StartupInput
		want Mode
	}{
		{"headless default", StartupInput{}, Default},
		{"interactive prefers auto", StartupInput{PreferAuto: true, Availability: auto}, Auto},
		{"auto unavailable", StartupInput{PreferAuto: true}, Default},
		{"settings default wins over auto", StartupInput{SettingsDefault: "acceptEdits", PreferAuto: true, Availability: auto}, AcceptEdits},
		{"flag wins", StartupInput{Flag: "plan", SettingsDefault: "acceptEdits", Availability: both}, Plan},
		{"manual alias flag", StartupInput{Flag: "manual", PreferAuto: true, Availability: auto}, Default},
		{"dangerously skip", StartupInput{DangerouslySkip: true, Availability: both}, BypassPermissions},
		{"bypass without availability", StartupInput{Flag: "bypassPermissions"}, Default},
		{"settings bypass unavailable", StartupInput{SettingsDefault: "bypassPermissions", Availability: auto}, Default},
		{"bad flag falls through", StartupInput{Flag: "nope", SettingsDefault: "plan"}, Plan},
	}
	for _, c := range cases {
		if got := Startup(c.in); got != c.want {
			t.Errorf("%s: Startup = %q want %q", c.name, got, c.want)
		}
	}
}
