package config

import (
	"strconv"
	"strings"
)

// Env holds the Claude Code environment variables that change the UI (CF-17). Read it
// once at startup with ReadEnv(os.Getenv).
type Env struct {
	DisableTerminalTitle bool  // CLAUDE_CODE_DISABLE_TERMINAL_TITLE
	DisableAltScreen     bool  // CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN: never fullscreen
	DisableMouse         bool  // CLAUDE_CODE_DISABLE_MOUSE: no mouse in fullscreen
	NoFlicker            bool  // CLAUDE_CODE_NO_FLICKER: prefer the fullscreen renderer
	SyntaxHighlight      *bool // CLAUDE_CODE_SYNTAX_HIGHLIGHT (unset = nil)
	ScrollSpeed          int   // CLAUDE_CODE_SCROLL_SPEED (0 = default)
	Accessibility        bool  // CLAUDE_CODE_ACCESSIBILITY: keep the real cursor visible
	ScreenReader         bool  // CLAUDE_AX_SCREEN_READER
	DisableColors        bool  // NO_COLOR (any value)
	Raw                  map[string]string
}

var envNames = []string{
	"CLAUDE_CODE_DISABLE_TERMINAL_TITLE", "CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN",
	"CLAUDE_CODE_DISABLE_MOUSE", "CLAUDE_CODE_NO_FLICKER", "CLAUDE_CODE_SYNTAX_HIGHLIGHT",
	"CLAUDE_CODE_SCROLL_SPEED", "CLAUDE_CODE_ACCESSIBILITY", "CLAUDE_AX_SCREEN_READER",
	"NO_COLOR",
}

// ReadEnv reads the UI environment variables through getenv.
func ReadEnv(getenv func(string) string) Env {
	e := Env{Raw: map[string]string{}}
	for _, n := range envNames {
		if v := getenv(n); v != "" {
			e.Raw[n] = v
		}
	}
	e.DisableTerminalTitle = truthy(e.Raw["CLAUDE_CODE_DISABLE_TERMINAL_TITLE"])
	e.DisableAltScreen = truthy(e.Raw["CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN"])
	e.DisableMouse = truthy(e.Raw["CLAUDE_CODE_DISABLE_MOUSE"])
	e.NoFlicker = truthy(e.Raw["CLAUDE_CODE_NO_FLICKER"])
	e.Accessibility = truthy(e.Raw["CLAUDE_CODE_ACCESSIBILITY"])
	e.ScreenReader = truthy(e.Raw["CLAUDE_AX_SCREEN_READER"])
	_, e.DisableColors = e.Raw["NO_COLOR"]
	if v, ok := e.Raw["CLAUDE_CODE_SYNTAX_HIGHLIGHT"]; ok {
		on := !falsy(v)
		e.SyntaxHighlight = &on
	}
	if n, err := strconv.Atoi(e.Raw["CLAUDE_CODE_SCROLL_SPEED"]); err == nil && n > 0 {
		e.ScrollSpeed = n
	}
	return e
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func falsy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "0", "false", "no", "off":
		return true
	}
	return false
}
