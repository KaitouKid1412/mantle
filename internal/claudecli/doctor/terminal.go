package doctor

import (
	"context"
	"strings"
)

// CheckTerminal reports colour depth, the kitty keyboard protocol (needed for
// shift+enter and other modified keys without /terminal-setup), OSC 52
// clipboard and focus reporting. Capabilities detected by querying the terminal
// (Env.Term) win; otherwise the environment is used, and unknown stays unknown.
func CheckTerminal(ctx context.Context, env Env) Check {
	guess := guessTerm(env.getenv)
	pick := func(detected *bool, guessed tri) tri {
		if detected != nil {
			if *detected {
				return yes
			}
			return no
		}
		return guessed
	}
	items := []Check{
		capCheck("Truecolor", pick(env.Term.Truecolor, guess.truecolor),
			"Colours are approximated with 256 colours.", "Use a truecolor terminal or set COLORTERM=truecolor if yours supports it."),
		capCheck("Kitty keyboard protocol", pick(env.Term.KittyKeyboard, guess.kitty),
			"shift+enter and some ctrl/alt keys need /terminal-setup.", "Run /terminal-setup, or use a terminal with the kitty keyboard protocol."),
		capCheck("Clipboard (OSC 52)", pick(env.Term.OSC52, guess.osc52),
			"Copy over SSH may not reach your local clipboard.", guess.osc52Fix),
		capCheck("Focus reporting", pick(env.Term.Focus, guess.focus),
			"Notifications cannot tell whether the terminal is focused.", ""),
	}
	c := Check{Status: Worst(items), Items: items, Detail: guess.name}
	if c.Detail == "" {
		c.Detail = "unknown terminal"
	}
	return c
}

type tri int

const (
	unknown tri = iota
	yes
	no
)

func capCheck(title string, v tri, missing, fix string) Check {
	switch v {
	case yes:
		return Check{Title: title, Status: OK, Detail: "supported"}
	case no:
		return Check{Title: title, Status: Warn, Detail: missing, Fix: fix}
	}
	return Check{Title: title, Status: Skip, Detail: "unknown"}
}

type termGuess struct {
	name                           string
	truecolor, kitty, osc52, focus tri
	osc52Fix                       string
}

// guessTerm infers capabilities from TERM, TERM_PROGRAM, COLORTERM and the
// multiplexer environment.
func guessTerm(getenv func(string) string) termGuess {
	term := getenv("TERM")
	prog := getenv("TERM_PROGRAM")
	g := termGuess{name: prog}
	if g.name == "" {
		g.name = term
	}
	switch ct := strings.ToLower(getenv("COLORTERM")); {
	case ct == "truecolor" || ct == "24bit":
		g.truecolor = yes
	case strings.HasSuffix(term, "-direct"):
		g.truecolor = yes
	}

	kittyLike := getenv("KITTY_WINDOW_ID") != "" || strings.Contains(term, "kitty") ||
		strings.Contains(term, "ghostty") || term == "foot" || strings.HasPrefix(term, "foot-")
	switch {
	case kittyLike:
		g.truecolor, g.kitty, g.osc52, g.focus = yes, yes, yes, yes
	case prog == "ghostty" || prog == "WezTerm":
		g.truecolor, g.kitty, g.osc52, g.focus = yes, yes, yes, yes
	case prog == "iTerm.app":
		g.truecolor, g.osc52, g.focus = yes, yes, yes
		if versionAtLeast(getenv("TERM_PROGRAM_VERSION"), 3, 5) {
			g.kitty = yes
		}
	case prog == "Apple_Terminal":
		g.kitty, g.osc52, g.focus = no, no, yes
		if g.truecolor == unknown {
			g.truecolor = no
		}
	case prog == "vscode":
		g.truecolor, g.focus = yes, yes
	case strings.HasPrefix(term, "alacritty"):
		g.truecolor, g.kitty, g.osc52, g.focus = yes, yes, yes, yes
	}
	if getenv("TMUX") != "" {
		g.name = strings.TrimSpace("tmux " + g.name)
		// tmux forwards OSC 52 only with set-clipboard on, and speaks the kitty
		// protocol to applications only with extended-keys.
		if g.osc52 != no {
			g.osc52 = unknown
		}
		g.osc52Fix = "In tmux, add `set -g set-clipboard on` to ~/.tmux.conf."
		if g.kitty == yes {
			g.kitty = unknown
		}
	}
	return g
}

func versionAtLeast(v string, major, minor int) bool {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return false
	}
	atoi := func(s string) int {
		n := 0
		for _, c := range s {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		return n
	}
	ma, mi := atoi(parts[0]), atoi(parts[1])
	return ma > major || ma == major && mi >= minor
}
