// Package notify encodes desktop notifications and the terminal bell, and decides when
// one should fire.
//
// In -p mode Claude Code drops a hook's terminalSequence output, so mantle renders
// notifications itself. The host writes the bytes from Sequence with tea.Raw.
package notify

import (
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
)

// Channel is a preferredNotifChannel value.
type Channel string

const (
	Auto           Channel = "auto"
	ITerm2         Channel = "iterm2"
	TerminalBell   Channel = "terminal_bell"
	ITerm2WithBell Channel = "iterm2_with_bell"
	Kitty          Channel = "kitty"
	Ghostty        Channel = "ghostty"
	Disabled       Channel = "notifications_disabled"
)

// ParseChannel reads a preferredNotifChannel setting. Empty and unknown values mean
// Auto; the short forms "bell", "desktop" and "none" are accepted too.
func ParseChannel(s string) Channel {
	switch c := Channel(strings.TrimSpace(s)); c {
	case Auto, ITerm2, TerminalBell, ITerm2WithBell, Kitty, Ghostty, Disabled:
		return c
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "bell":
		return TerminalBell
	case "none", "off", "disabled":
		return Disabled
	}
	return Auto
}

// Resolve turns Auto into a concrete channel for the detected terminal. Only terminals
// with native desktop notifications get one; everything else resolves to Disabled (set
// terminal_bell explicitly for a bell).
func Resolve(c Channel, env terminal.Env) Channel {
	if c != Auto && c != "" {
		return c
	}
	switch terminal.Detect(env) {
	case terminal.ITerm2:
		return ITerm2
	case terminal.Kitty:
		return Kitty
	case terminal.Ghostty:
		return Ghostty
	}
	return Disabled
}

// NeedsPassthrough reports whether the channel's sequence is an OSC that tmux would
// swallow. The bell is handled by tmux itself and is never wrapped.
func NeedsPassthrough(c Channel) bool {
	switch c {
	case ITerm2, ITerm2WithBell, Kitty, Ghostty:
		return true
	}
	return false
}

// DefaultTitle is used when a notification has no title.
const DefaultTitle = "mantle"

var kittyID atomic.Uint64

// Sequence encodes a notification for channel. Auto is resolved against the process
// environment. Inside tmux the OSC is wrapped in DCS passthrough. Disabled (or an empty
// title and body on a desktop channel) returns nil.
func Sequence(channel Channel, title, body string, inTmux bool) []byte {
	if channel == Auto || channel == "" {
		channel = Resolve(channel, terminal.OS())
	}
	return Build(channel, Notification{Title: title, Body: body}, inTmux)
}

// Notification is one notification's content.
type Notification struct {
	Title, Body string
	// ID groups kitty notification chunks; empty picks a fresh one.
	ID string
}

// Build is Sequence with a resolved channel and an explicit Notification.
func Build(channel Channel, n Notification, inTmux bool) []byte {
	title := terminal.StripControls(strings.TrimSpace(n.Title))
	body := terminal.StripControls(strings.TrimSpace(n.Body))
	var seq string
	switch channel {
	case TerminalBell:
		return []byte("\a")
	case ITerm2, ITerm2WithBell:
		msg := joinTitleBody(title, body)
		if msg == "" {
			return nil
		}
		seq = "\x1b]9;" + msg + "\a"
		if channel == ITerm2WithBell {
			return append(terminal.Passthrough([]byte(seq), inTmux), '\a')
		}
	case Kitty:
		if title == "" && body == "" {
			return nil
		}
		id := sanitizeKittyID(n.ID)
		if id == "" {
			id = "mantle-" + strconv.FormatUint(kittyID.Add(1), 10)
		}
		if title == "" {
			title, body = body, ""
		}
		if body == "" {
			seq = "\x1b]99;i=" + id + ":d=1;" + title + "\x1b\\"
		} else {
			seq = "\x1b]99;i=" + id + ":d=0;" + title + "\x1b\\" +
				"\x1b]99;i=" + id + ":d=1:p=body;" + body + "\x1b\\"
		}
	case Ghostty:
		if title == "" && body == "" {
			return nil
		}
		if title == "" {
			title = DefaultTitle
		}
		// The title is a ';'-separated field; the body runs to the terminator.
		seq = "\x1b]777;notify;" + strings.ReplaceAll(title, ";", ",") + ";" + body + "\a"
	default:
		return nil
	}
	return terminal.Passthrough([]byte(seq), inTmux)
}

func joinTitleBody(title, body string) string {
	switch {
	case title == "":
		return body
	case body == "":
		return title
	}
	return title + ": " + body
}

func sanitizeKittyID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '+' || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
