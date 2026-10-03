// Package osc encodes the operating-system-command sequences mantle writes outside the
// normal frame: the OSC 9;4 progress bar, OSC 8 hyperlinks and the window title.
//
// Bubble Tea v2 already emits the title and progress bar from View fields; these
// encoders exist for the paths that bypass the renderer (tea.Raw, the launcher's crash
// restore, screen-reader mode) and for tests that assert the raw bytes.
package osc

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
)

const (
	bel = "\a"
	st  = "\x1b\\"
)

// ProgressState is an OSC 9;4 state.
type ProgressState int

const (
	ProgressNone          ProgressState = 0 // remove the bar
	ProgressNormal        ProgressState = 1 // determinate, with a percentage
	ProgressError         ProgressState = 2
	ProgressIndeterminate ProgressState = 3
	ProgressPause         ProgressState = 4 // paused / warning
)

func (s ProgressState) String() string {
	switch s {
	case ProgressNone:
		return "none"
	case ProgressNormal:
		return "normal"
	case ProgressError:
		return "error"
	case ProgressIndeterminate:
		return "indeterminate"
	case ProgressPause:
		return "pause"
	}
	return "unknown"
}

// Progress encodes OSC 9;4. percent is clamped to 0–100 and only sent for states that
// carry a value (normal, error, pause).
func Progress(state ProgressState, percent int) []byte {
	if state < ProgressNone || state > ProgressPause {
		state = ProgressNone
	}
	percent = max(0, min(100, percent))
	var b strings.Builder
	b.WriteString("\x1b]9;4;")
	b.WriteString(strconv.Itoa(int(state)))
	switch state {
	case ProgressNormal, ProgressError, ProgressPause:
		b.WriteByte(';')
		b.WriteString(strconv.Itoa(percent))
	}
	b.WriteString(bel)
	return []byte(b.String())
}

// ClearProgress removes the progress bar.
func ClearProgress() []byte { return Progress(ProgressNone, 0) }

// ProgressSupported reports whether the terminal is known to render OSC 9;4. Older
// iTerm2 builds read OSC 9;4 as an OSC 9 notification and would pop a spurious
// "4;3;" alert, so detection is conservative.
func ProgressSupported(e terminal.Env) bool {
	switch terminal.Detect(e) {
	case terminal.WindowsTerminal, terminal.ConEmu:
		return true
	case terminal.Ghostty:
		v := terminal.Version(e)
		return v == "" || terminal.VersionAtLeast(v, "1.2.0")
	case terminal.ITerm2:
		return terminal.VersionAtLeast(terminal.Version(e), "3.6.6")
	}
	return false
}

// Hyperlink wraps text in an OSC 8 link. An empty url returns text unchanged. id, when
// set, groups separate runs of the same link (for wrapped lines).
func Hyperlink(url, text, id string) string {
	url = sanitizeURL(url)
	if url == "" {
		return text
	}
	params := ""
	if id != "" {
		params = "id=" + sanitizeParam(id)
	}
	return "\x1b]8;" + params + ";" + url + st + text + "\x1b]8;;" + st
}

// HyperlinksSupported reports whether OSC 8 links should be emitted. FORCE_HYPERLINK
// wins: "0" or "false" disables, any other non-empty value enables.
func HyperlinksSupported(e terminal.Env) bool {
	if v, ok := e.Lookup("FORCE_HYPERLINK"); ok && v != "" {
		switch strings.ToLower(v) {
		case "0", "false", "no", "off":
			return false
		}
		return true
	}
	if terminal.Get(e, "TERM") == "dumb" {
		return false
	}
	switch terminal.Detect(e) {
	case terminal.ITerm2, terminal.Kitty, terminal.Ghostty, terminal.WezTerm,
		terminal.WindowsTerminal, terminal.Alacritty, terminal.Warp:
		return true
	case terminal.VSCode:
		v := terminal.Version(e)
		return v == "" || terminal.VersionAtLeast(v, "1.72")
	case terminal.VTE:
		n, _ := strconv.Atoi(terminal.Get(e, "VTE_VERSION"))
		return n >= 5000
	}
	return false
}

// Link returns an OSC 8 link when enabled, else text.
func Link(enabled bool, url, text string) string {
	if !enabled {
		return text
	}
	return Hyperlink(url, text, "")
}

// DefaultTitleMax is the title length cap, in runes.
const DefaultTitleMax = 120

// SanitizeTitle prepares text for a window title: control characters become spaces,
// runs of whitespace collapse, and the result is capped at maxRunes (an ellipsis marks
// the cut). maxRunes <= 0 uses DefaultTitleMax.
func SanitizeTitle(s string, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = DefaultTitleMax
	}
	s = strings.Join(strings.Fields(terminal.StripControls(strings.ToValidUTF8(s, ""))), " ")
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	r := []rune(s)
	if maxRunes == 1 {
		return "…"
	}
	return strings.TrimRight(string(r[:maxRunes-1]), " ") + "…"
}

// Title encodes OSC 2 (window title) for already-sanitized text.
func Title(title string) []byte {
	return []byte("\x1b]2;" + terminal.StripControls(title) + bel)
}

func sanitizeURL(u string) string {
	u = strings.TrimSpace(u)
	var b strings.Builder
	for _, r := range u {
		// URLs inside OSC 8 must be printable ASCII-ish; drop controls and spaces.
		if r < 0x21 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func sanitizeParam(p string) string {
	var b strings.Builder
	for _, r := range p {
		if r > 0x20 && r < 0x7f && r != ':' && r != ';' && r != '=' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
