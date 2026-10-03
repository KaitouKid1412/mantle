// Package terminal detects the host terminal emulator and multiplexer from the
// environment. The other internal/term packages use it to pick notification channels,
// hyperlink support and progress-bar support.
//
// Everything here is a pure function of an Env, so callers and tests can pass a map.
package terminal

import (
	"os"
	"strconv"
	"strings"
)

// Env looks up an environment variable.
type Env interface {
	Lookup(key string) (string, bool)
}

// Map is an Env backed by a map; handy in tests.
type Map map[string]string

// Lookup implements Env.
func (m Map) Lookup(key string) (string, bool) {
	v, ok := m[key]
	return v, ok
}

type osEnv struct{}

func (osEnv) Lookup(key string) (string, bool) { return os.LookupEnv(key) }

// OS returns the process environment.
func OS() Env { return osEnv{} }

// Get returns the value of key, or "" when unset.
func Get(e Env, key string) string {
	v, _ := e.Lookup(key)
	return v
}

// Kind identifies a terminal emulator.
type Kind string

const (
	Unknown         Kind = ""
	ITerm2          Kind = "iterm2"
	Kitty           Kind = "kitty"
	Ghostty         Kind = "ghostty"
	WezTerm         Kind = "wezterm"
	AppleTerminal   Kind = "apple_terminal"
	VSCode          Kind = "vscode"
	WindowsTerminal Kind = "windows_terminal"
	Warp            Kind = "warp"
	Alacritty       Kind = "alacritty"
	VTE             Kind = "vte"
	ConEmu          Kind = "conemu"
)

// Detect returns the outer terminal emulator. Inside tmux, TERM_PROGRAM names tmux, so
// the variables the outer terminal exports (and tmux inherits) are checked as well.
func Detect(e Env) Kind {
	switch strings.ToLower(Get(e, "TERM_PROGRAM")) {
	case "iterm.app":
		return ITerm2
	case "ghostty":
		return Ghostty
	case "wezterm":
		return WezTerm
	case "apple_terminal":
		return AppleTerminal
	case "vscode":
		return VSCode
	case "warpterminal":
		return Warp
	case "kitty":
		return Kitty
	case "alacritty":
		return Alacritty
	}
	term := Get(e, "TERM")
	switch {
	case has(e, "KITTY_WINDOW_ID") || term == "xterm-kitty":
		return Kitty
	case has(e, "GHOSTTY_RESOURCES_DIR") || term == "xterm-ghostty":
		return Ghostty
	case has(e, "ITERM_SESSION_ID") || Get(e, "LC_TERMINAL") == "iTerm2":
		return ITerm2
	case has(e, "WEZTERM_PANE") || has(e, "WEZTERM_EXECUTABLE"):
		return WezTerm
	case has(e, "WT_SESSION"):
		return WindowsTerminal
	case Get(e, "ConEmuANSI") == "ON":
		return ConEmu
	case has(e, "ALACRITTY_WINDOW_ID") || term == "alacritty":
		return Alacritty
	case has(e, "VTE_VERSION"):
		return VTE
	}
	return Unknown
}

// Version returns TERM_PROGRAM_VERSION (or LC_TERMINAL_VERSION for iTerm2 seen through
// ssh or tmux).
func Version(e Env) string {
	if v := Get(e, "TERM_PROGRAM_VERSION"); v != "" && !InTmux(e) {
		return v
	}
	if Get(e, "LC_TERMINAL") == "iTerm2" {
		return Get(e, "LC_TERMINAL_VERSION")
	}
	return Get(e, "TERM_PROGRAM_VERSION")
}

// InTmux reports whether the process runs inside tmux.
func InTmux(e Env) bool {
	return has(e, "TMUX") || strings.EqualFold(Get(e, "TERM_PROGRAM"), "tmux")
}

// IsSSH reports whether the process runs in an ssh session.
func IsSSH(e Env) bool {
	return has(e, "SSH_TTY") || has(e, "SSH_CONNECTION") || has(e, "SSH_CLIENT")
}

// VersionAtLeast compares dotted numeric versions ("3.6.6" >= "3.6"). Non-numeric
// suffixes ("1.2.0-dev") are ignored; an empty version is never at least anything.
func VersionAtLeast(v, min string) bool {
	if v == "" {
		return false
	}
	a, b := parts(v), parts(min)
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x > y
		}
	}
	return true
}

func parts(v string) []int {
	var out []int
	for _, p := range strings.Split(v, ".") {
		end := 0
		for end < len(p) && p[end] >= '0' && p[end] <= '9' {
			end++
		}
		n, _ := strconv.Atoi(p[:end])
		out = append(out, n)
		if end < len(p) {
			break
		}
	}
	return out
}

func has(e Env, key string) bool {
	v, ok := e.Lookup(key)
	return ok && v != ""
}
