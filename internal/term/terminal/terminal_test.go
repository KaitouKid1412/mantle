package terminal

import "testing"

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		env  Map
		want Kind
	}{
		{"iterm", Map{"TERM_PROGRAM": "iTerm.app"}, ITerm2},
		{"iterm via ssh", Map{"LC_TERMINAL": "iTerm2"}, ITerm2},
		{"ghostty", Map{"TERM_PROGRAM": "ghostty"}, Ghostty},
		{"ghostty term", Map{"TERM": "xterm-ghostty"}, Ghostty},
		{"kitty in tmux", Map{"TERM_PROGRAM": "tmux", "TMUX": "/tmp/x", "KITTY_WINDOW_ID": "1"}, Kitty},
		{"kitty term", Map{"TERM": "xterm-kitty"}, Kitty},
		{"wezterm", Map{"TERM_PROGRAM": "WezTerm"}, WezTerm},
		{"apple", Map{"TERM_PROGRAM": "Apple_Terminal"}, AppleTerminal},
		{"vscode", Map{"TERM_PROGRAM": "vscode"}, VSCode},
		{"windows terminal", Map{"WT_SESSION": "abc"}, WindowsTerminal},
		{"vte", Map{"VTE_VERSION": "7600"}, VTE},
		{"empty value ignored", Map{"KITTY_WINDOW_ID": ""}, Unknown},
		{"unknown", Map{"TERM": "xterm-256color"}, Unknown},
	}
	for _, tt := range tests {
		if got := Detect(tt.env); got != tt.want {
			t.Errorf("%s: Detect = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestVersion(t *testing.T) {
	if got := Version(Map{"TERM_PROGRAM_VERSION": "3.6.6"}); got != "3.6.6" {
		t.Errorf("Version = %q", got)
	}
	inTmux := Map{"TMUX": "x", "TERM_PROGRAM": "tmux", "TERM_PROGRAM_VERSION": "3.5a",
		"LC_TERMINAL": "iTerm2", "LC_TERMINAL_VERSION": "3.6.7"}
	if got := Version(inTmux); got != "3.6.7" {
		t.Errorf("Version in tmux = %q", got)
	}
}

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		v, min string
		want   bool
	}{
		{"3.6.6", "3.6.6", true},
		{"3.6.10", "3.6.6", true},
		{"3.6", "3.6.6", false},
		{"3.10", "3.6.6", true},
		{"1.2.0-dev", "1.2.0", true},
		{"1.1.9", "1.2", false},
		{"", "1", false},
	}
	for _, tt := range tests {
		if got := VersionAtLeast(tt.v, tt.min); got != tt.want {
			t.Errorf("VersionAtLeast(%q, %q) = %v", tt.v, tt.min, got)
		}
	}
}

func TestInTmuxSSH(t *testing.T) {
	if !InTmux(Map{"TMUX": "/tmp/tmux-501/default,1,0"}) || InTmux(Map{}) {
		t.Error("InTmux")
	}
	if !IsSSH(Map{"SSH_TTY": "/dev/ttys001"}) || IsSSH(Map{}) {
		t.Error("IsSSH")
	}
}

func TestTmuxWrap(t *testing.T) {
	got := string(TmuxWrap([]byte("\x1b]9;hi\a")))
	want := "\x1bPtmux;\x1b\x1b]9;hi\a\x1b\\"
	if got != want {
		t.Errorf("TmuxWrap = %q, want %q", got, want)
	}
	got = string(TmuxWrap([]byte("\x1b]99;i=1;t\x1b\\")))
	want = "\x1bPtmux;\x1b\x1b]99;i=1;t\x1b\x1b\\\x1b\\"
	if got != want {
		t.Errorf("TmuxWrap ST = %q, want %q", got, want)
	}
	if TmuxWrap(nil) != nil {
		t.Error("TmuxWrap(nil) should be nil")
	}
	if got := string(Passthrough([]byte("\a"), false)); got != "\a" {
		t.Errorf("Passthrough(false) = %q", got)
	}
}

func TestStripControls(t *testing.T) {
	if got := StripControls("a\x1b]b\a\nc\u009dd"); got != "a]b cd" {
		t.Errorf("StripControls = %q", got)
	}
	s := "plain"
	if got := StripControls(s); got != s {
		t.Errorf("StripControls(plain) = %q", got)
	}
}
