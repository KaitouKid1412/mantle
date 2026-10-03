package chrome

import (
	"path/filepath"
	"strings"

	"github.com/KaitouKid1412/mantle/internal/term/osc"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
)

// TitleInput is everything the window title depends on.
type TitleInput struct {
	// SessionName is a name set with /rename or -n.
	SessionName string
	// AITitle is the engine's generated session title.
	AITitle string
	Cwd     string
	// FromRename is the terminalTitleFromRename setting (default true): when false a
	// /rename or -n name does not reach the title.
	FromRename bool
}

// TitleDisabled reports whether CLAUDE_CODE_DISABLE_TERMINAL_TITLE turns titles off.
func TitleDisabled(env terminal.Env) bool {
	switch strings.ToLower(strings.TrimSpace(terminal.Get(env, "CLAUDE_CODE_DISABLE_TERMINAL_TITLE"))) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// WindowTitle picks the title: the session name (if allowed), else the generated title,
// else the working directory's base name. The result is sanitized and capped.
func WindowTitle(in TitleInput) string {
	t := ""
	switch {
	case in.FromRename && strings.TrimSpace(in.SessionName) != "":
		t = in.SessionName
	case strings.TrimSpace(in.AITitle) != "":
		t = in.AITitle
	case in.Cwd != "":
		t = filepath.Base(filepath.Clean(in.Cwd))
		if t == "/" || t == "." {
			t = in.Cwd
		}
	}
	return osc.SanitizeTitle(t, 0)
}
