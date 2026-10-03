package osc

import (
	"testing"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
)

func TestProgress(t *testing.T) {
	tests := []struct {
		state   ProgressState
		percent int
		want    string
	}{
		{ProgressNone, 50, "\x1b]9;4;0\a"},
		{ProgressNormal, 42, "\x1b]9;4;1;42\a"},
		{ProgressNormal, 150, "\x1b]9;4;1;100\a"},
		{ProgressNormal, -3, "\x1b]9;4;1;0\a"},
		{ProgressError, 100, "\x1b]9;4;2;100\a"},
		{ProgressIndeterminate, 0, "\x1b]9;4;3\a"},
		{ProgressPause, 7, "\x1b]9;4;4;7\a"},
		{ProgressState(9), 7, "\x1b]9;4;0\a"},
	}
	for _, tt := range tests {
		if got := string(Progress(tt.state, tt.percent)); got != tt.want {
			t.Errorf("Progress(%v, %d) = %q, want %q", tt.state, tt.percent, got, tt.want)
		}
	}
	if got := string(ClearProgress()); got != "\x1b]9;4;0\a" {
		t.Errorf("ClearProgress = %q", got)
	}
}

func TestProgressSupported(t *testing.T) {
	tests := []struct {
		name string
		env  terminal.Map
		want bool
	}{
		{"ghostty new", terminal.Map{"TERM_PROGRAM": "ghostty", "TERM_PROGRAM_VERSION": "1.2.3"}, true},
		{"ghostty old", terminal.Map{"TERM_PROGRAM": "ghostty", "TERM_PROGRAM_VERSION": "1.1.0"}, false},
		{"iterm new", terminal.Map{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.6.6"}, true},
		{"iterm old", terminal.Map{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.5.0"}, false},
		{"windows terminal", terminal.Map{"WT_SESSION": "x"}, true},
		{"apple terminal", terminal.Map{"TERM_PROGRAM": "Apple_Terminal"}, false},
		{"unknown", terminal.Map{}, false},
	}
	for _, tt := range tests {
		if got := ProgressSupported(tt.env); got != tt.want {
			t.Errorf("%s: ProgressSupported = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestHyperlink(t *testing.T) {
	got := Hyperlink("https://example.com/a b", "text", "")
	want := "\x1b]8;;https://example.com/ab\x1b\\text\x1b]8;;\x1b\\"
	if got != want {
		t.Errorf("Hyperlink = %q, want %q", got, want)
	}
	got = Hyperlink("https://x.test", "t", "id:1;")
	want = "\x1b]8;id=id1;https://x.test\x1b\\t\x1b]8;;\x1b\\"
	if got != want {
		t.Errorf("Hyperlink with id = %q, want %q", got, want)
	}
	if got := Hyperlink("", "plain", ""); got != "plain" {
		t.Errorf("empty url = %q", got)
	}
	if got := Hyperlink("https://x.test/\x1b]8;;evil\a", "t", ""); got != "\x1b]8;;https://x.test/]8;;evil\x1b\\t\x1b]8;;\x1b\\" {
		t.Errorf("controls not stripped: %q", got)
	}
	if got := Link(false, "https://x.test", "t"); got != "t" {
		t.Errorf("Link(false) = %q", got)
	}
}

func TestHyperlinksSupported(t *testing.T) {
	tests := []struct {
		name string
		env  terminal.Map
		want bool
	}{
		{"force on", terminal.Map{"FORCE_HYPERLINK": "1", "TERM_PROGRAM": "Apple_Terminal"}, true},
		{"force off", terminal.Map{"FORCE_HYPERLINK": "0", "TERM_PROGRAM": "iTerm.app"}, false},
		{"force false", terminal.Map{"FORCE_HYPERLINK": "false", "TERM_PROGRAM": "iTerm.app"}, false},
		{"iterm", terminal.Map{"TERM_PROGRAM": "iTerm.app"}, true},
		{"kitty in tmux", terminal.Map{"TERM_PROGRAM": "tmux", "KITTY_WINDOW_ID": "3"}, true},
		{"apple terminal", terminal.Map{"TERM_PROGRAM": "Apple_Terminal"}, false},
		{"vscode old", terminal.Map{"TERM_PROGRAM": "vscode", "TERM_PROGRAM_VERSION": "1.60.0"}, false},
		{"vscode new", terminal.Map{"TERM_PROGRAM": "vscode", "TERM_PROGRAM_VERSION": "1.95.1"}, true},
		{"vte", terminal.Map{"VTE_VERSION": "7600"}, true},
		{"dumb", terminal.Map{"TERM": "dumb", "KITTY_WINDOW_ID": "1"}, false},
		{"none", terminal.Map{}, false},
	}
	for _, tt := range tests {
		if got := HyperlinksSupported(tt.env); got != tt.want {
			t.Errorf("%s: HyperlinksSupported = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestSanitizeTitle(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 0, "hello"},
		{"  a\tb\n\nc\x1b]0;x\a ", 0, "a b c]0;x"},
		{"abcdefghij", 5, "abcd…"},
		{"ab   cdefg", 4, "ab…"},
		{"日本語のタイトル", 4, "日本語…"},
		{"abc", 1, "…"},
		{"bad\xffutf8", 0, "badutf8"},
		{"\u009b31m", 0, "31m"},
	}
	for _, tt := range tests {
		if got := SanitizeTitle(tt.in, tt.max); got != tt.want {
			t.Errorf("SanitizeTitle(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
	}
}

func TestTitle(t *testing.T) {
	if got := string(Title("proj\a")); got != "\x1b]2;proj\a" {
		t.Errorf("Title = %q", got)
	}
}
