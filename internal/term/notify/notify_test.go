package notify

import (
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/term/focus"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
)

func TestParseChannel(t *testing.T) {
	tests := map[string]Channel{
		"":                       Auto,
		"auto":                   Auto,
		"iterm2":                 ITerm2,
		"terminal_bell":          TerminalBell,
		"iterm2_with_bell":       ITerm2WithBell,
		"kitty":                  Kitty,
		"ghostty":                Ghostty,
		"notifications_disabled": Disabled,
		"bell":                   TerminalBell,
		"desktop":                Auto,
		"none":                   Disabled,
		"bogus":                  Auto,
		" kitty ":                Kitty,
	}
	for in, want := range tests {
		if got := ParseChannel(in); got != want {
			t.Errorf("ParseChannel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		ch   Channel
		env  terminal.Map
		want Channel
	}{
		{Auto, terminal.Map{"TERM_PROGRAM": "iTerm.app"}, ITerm2},
		{Auto, terminal.Map{"KITTY_WINDOW_ID": "1"}, Kitty},
		{Auto, terminal.Map{"GHOSTTY_RESOURCES_DIR": "/x"}, Ghostty},
		{Auto, terminal.Map{"TERM_PROGRAM": "tmux", "LC_TERMINAL": "iTerm2"}, ITerm2},
		{Auto, terminal.Map{"TERM_PROGRAM": "Apple_Terminal"}, Disabled},
		{Auto, terminal.Map{"TERM_PROGRAM": "vscode"}, Disabled},
		{"", terminal.Map{"TERM_PROGRAM": "ghostty"}, Ghostty},
		{TerminalBell, terminal.Map{"TERM_PROGRAM": "iTerm.app"}, TerminalBell},
		{Disabled, terminal.Map{"TERM_PROGRAM": "iTerm.app"}, Disabled},
	}
	for _, tt := range tests {
		if got := Resolve(tt.ch, tt.env); got != tt.want {
			t.Errorf("Resolve(%q, %v) = %q, want %q", tt.ch, tt.env, got, tt.want)
		}
	}
}

func TestBuild(t *testing.T) {
	n := Notification{Title: "mantle", Body: "Needs your approval", ID: "n1"}
	tests := []struct {
		name   string
		ch     Channel
		n      Notification
		inTmux bool
		want   string
	}{
		{"bell", TerminalBell, n, false, "\a"},
		{"bell in tmux is not wrapped", TerminalBell, n, true, "\a"},
		{"iterm2", ITerm2, n, false, "\x1b]9;mantle: Needs your approval\a"},
		{"iterm2 tmux", ITerm2, n, true, "\x1bPtmux;\x1b\x1b]9;mantle: Needs your approval\a\x1b\\"},
		{"iterm2 with bell", ITerm2WithBell, n, false, "\x1b]9;mantle: Needs your approval\a\a"},
		{"iterm2 with bell tmux", ITerm2WithBell, n, true,
			"\x1bPtmux;\x1b\x1b]9;mantle: Needs your approval\a\x1b\\\a"},
		{"iterm2 body only", ITerm2, Notification{Body: "done"}, false, "\x1b]9;done\a"},
		{"kitty", Kitty, n, false,
			"\x1b]99;i=n1:d=0;mantle\x1b\\\x1b]99;i=n1:d=1:p=body;Needs your approval\x1b\\"},
		{"kitty tmux", Kitty, n, true,
			"\x1bPtmux;\x1b\x1b]99;i=n1:d=0;mantle\x1b\x1b\\\x1b\x1b]99;i=n1:d=1:p=body;Needs your approval\x1b\x1b\\\x1b\\"},
		{"kitty title only", Kitty, Notification{Title: "t", ID: "x"}, false, "\x1b]99;i=x:d=1;t\x1b\\"},
		{"kitty body only", Kitty, Notification{Body: "b", ID: "x"}, false, "\x1b]99;i=x:d=1;b\x1b\\"},
		{"ghostty", Ghostty, n, false, "\x1b]777;notify;mantle;Needs your approval\a"},
		{"ghostty tmux", Ghostty, n, true, "\x1bPtmux;\x1b\x1b]777;notify;mantle;Needs your approval\a\x1b\\"},
		{"ghostty separator in title", Ghostty, Notification{Title: "a;b", Body: "c;d"}, false,
			"\x1b]777;notify;a,b;c;d\a"},
		{"ghostty no title", Ghostty, Notification{Body: "x"}, false, "\x1b]777;notify;mantle;x\a"},
		{"injection stripped", ITerm2, Notification{Title: "a\x1b]0;pwned\a", Body: "b\nc"}, false,
			"\x1b]9;a]0;pwned: b c\a"},
		{"disabled", Disabled, n, false, ""},
		{"empty", ITerm2, Notification{}, false, ""},
		{"empty kitty", Kitty, Notification{}, false, ""},
		{"empty ghostty", Ghostty, Notification{}, false, ""},
		{"unknown", Channel("nope"), n, false, ""},
	}
	for _, tt := range tests {
		if got := string(Build(tt.ch, tt.n, tt.inTmux)); got != tt.want {
			t.Errorf("%s: Build = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestKittyAutoID(t *testing.T) {
	a := string(Build(Kitty, Notification{Title: "t"}, false))
	b := string(Build(Kitty, Notification{Title: "t"}, false))
	if a == b {
		t.Errorf("auto IDs should differ: %q", a)
	}
	if got := string(Build(Kitty, Notification{Title: "t", ID: "a b:c"}, false)); got != "\x1b]99;i=abc:d=1;t\x1b\\" {
		t.Errorf("sanitized id: %q", got)
	}
}

func TestSequence(t *testing.T) {
	if got := string(Sequence(TerminalBell, "a", "b", false)); got != "\a" {
		t.Errorf("Sequence(bell) = %q", got)
	}
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("TMUX", "")
	if got := string(Sequence(Auto, "a", "b", false)); got != "\x1b]777;notify;a;b\a" {
		t.Errorf("Sequence(auto, ghostty) = %q", got)
	}
}

func TestNeedsPassthrough(t *testing.T) {
	for ch, want := range map[Channel]bool{ITerm2: true, ITerm2WithBell: true, Kitty: true,
		Ghostty: true, TerminalBell: false, Disabled: false} {
		if NeedsPassthrough(ch) != want {
			t.Errorf("NeedsPassthrough(%q) != %v", ch, want)
		}
	}
}

func TestDecide(t *testing.T) {
	on := Prefs{Channel: ITerm2}
	sec := time.Second
	tests := []struct {
		name  string
		s     Situation
		prefs Prefs
		want  bool
	}{
		{"input blurred", Situation{Event: InputNeeded, Focus: focus.Blurred}, on, true},
		{"input focused", Situation{Event: InputNeeded, Focus: focus.Focused, Waiting: time.Hour, Idle: time.Hour}, on, false},
		{"input unknown early", Situation{Event: InputNeeded, Waiting: 2 * sec, Idle: time.Hour}, on, false},
		{"input unknown waited", Situation{Event: InputNeeded, Waiting: 6 * sec, Idle: 6 * sec}, on, true},
		{"input unknown but typing", Situation{Event: InputNeeded, Waiting: 10 * sec, Idle: sec}, on, false},
		{"input disabled by pref", Situation{Event: InputNeeded, Focus: focus.Blurred}, Prefs{Channel: ITerm2, SkipInputNeeded: true}, false},
		{"turn blurred long", Situation{Event: TurnDone, Focus: focus.Blurred, Turn: 4 * sec}, on, true},
		{"turn blurred short", Situation{Event: TurnDone, Focus: focus.Blurred, Turn: sec}, on, false},
		{"turn focused", Situation{Event: TurnDone, Focus: focus.Focused, Turn: time.Hour}, on, false},
		{"turn unknown long away", Situation{Event: TurnDone, Turn: 45 * sec, Idle: 40 * sec}, on, true},
		{"turn unknown long present", Situation{Event: TurnDone, Turn: 45 * sec, Idle: 5 * sec}, on, false},
		{"turn unknown short", Situation{Event: TurnDone, Turn: 10 * sec, Idle: time.Hour}, on, false},
		{"disabled channel", Situation{Event: InputNeeded, Focus: focus.Blurred}, Prefs{Channel: Disabled}, false},
		{"empty channel", Situation{Event: InputNeeded, Focus: focus.Blurred}, Prefs{}, false},
		{"quiet", Situation{Event: InputNeeded, Focus: focus.Blurred}, Prefs{Channel: ITerm2, Quiet: true}, false},
		{"throttled", Situation{Event: InputNeeded, Focus: focus.Blurred, HadLast: true, SinceLast: sec}, on, false},
		{"not throttled", Situation{Event: InputNeeded, Focus: focus.Blurred, HadLast: true, SinceLast: 5 * sec}, on, true},
		{"engine rang bell", Situation{Event: InputNeeded, Focus: focus.Blurred, EngineRang: true}, Prefs{Channel: TerminalBell}, false},
		{"engine rang, desktop still fires", Situation{Event: InputNeeded, Focus: focus.Blurred, EngineRang: true}, on, true},
		{"unknown event", Situation{Focus: focus.Blurred}, on, false},
	}
	for _, tt := range tests {
		if got := Decide(tt.s, tt.prefs, Policy{}); got != tt.want {
			t.Errorf("%s: Decide = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPolicyOverrides(t *testing.T) {
	p := Policy{InputDelay: time.Second}
	s := Situation{Event: InputNeeded, Waiting: time.Second, Idle: time.Second}
	if !Decide(s, Prefs{Channel: Kitty}, p) {
		t.Error("custom InputDelay not applied")
	}
	if got := RecheckAfter(2*time.Second, Policy{}); got != 4*time.Second {
		t.Errorf("RecheckAfter = %v", got)
	}
	if got := RecheckAfter(10*time.Second, Policy{}); got != 0 {
		t.Errorf("RecheckAfter past = %v", got)
	}
}
