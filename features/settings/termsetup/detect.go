package termsetup

import "strings"

// Detected is the result of Detect.
type Detected struct {
	Terminal Terminal
	// InTmux is true when running inside tmux; Terminal is then the outer terminal
	// as far as inherited environment variables reveal it.
	InTmux bool
}

// Detect identifies the terminal from environment variables. goos is runtime.GOOS
// (Terminal.app only exists on darwin).
func Detect(env map[string]string, goos string) Detected {
	d := Detected{InTmux: env["TMUX"] != ""}
	tp := env["TERM_PROGRAM"]
	switch strings.ToLower(tp) {
	case "ghostty":
		d.Terminal = Ghostty
	case "iterm.app":
		d.Terminal = ITerm2
	case "apple_terminal":
		if goos == "darwin" {
			d.Terminal = AppleTerminal
		}
	case "wezterm":
		d.Terminal = WezTerm
	case "vscode":
		d.Terminal = vscodeFlavor(env)
	case "tmux", "screen":
		d.InTmux = d.InTmux || strings.EqualFold(tp, "tmux")
	}
	if d.Terminal != TerminalUnknown {
		return d
	}
	// Markers that survive inside tmux and in terminals that don't set TERM_PROGRAM.
	term := env["TERM"]
	switch {
	case env["GHOSTTY_RESOURCES_DIR"] != "" || term == "xterm-ghostty":
		d.Terminal = Ghostty
	case env["KITTY_WINDOW_ID"] != "" || term == "xterm-kitty":
		d.Terminal = Kitty
	case env["WEZTERM_PANE"] != "" || env["WEZTERM_EXECUTABLE"] != "":
		d.Terminal = WezTerm
	case env["ITERM_SESSION_ID"] != "" || env["LC_TERMINAL"] == "iTerm2":
		d.Terminal = ITerm2
	case env["ALACRITTY_WINDOW_ID"] != "" || env["ALACRITTY_SOCKET"] != "" || term == "alacritty":
		d.Terminal = Alacritty
	case env["VSCODE_GIT_ASKPASS_MAIN"] != "" || env["VSCODE_INJECTION"] != "":
		d.Terminal = vscodeFlavor(env)
	}
	return d
}

// vscodeFlavor tells the VS Code family apart; they all set TERM_PROGRAM=vscode.
func vscodeFlavor(env map[string]string) Terminal {
	switch env["__CFBundleIdentifier"] {
	case "com.microsoft.VSCode":
		return VSCode
	case "com.microsoft.VSCodeInsiders":
		return VSCodeInsiders
	case "com.vscodium", "com.vscodium.codium":
		return VSCodium
	case "com.exafunction.windsurf":
		return Windsurf
	case "com.todesktop.230313mzl4w4u92":
		return Cursor
	}
	if env["CURSOR_TRACE_ID"] != "" {
		return Cursor
	}
	hint := strings.ToLower(env["VSCODE_GIT_ASKPASS_MAIN"] + " " + env["VSCODE_GIT_ASKPASS_NODE"])
	switch {
	case strings.Contains(hint, "cursor"):
		return Cursor
	case strings.Contains(hint, "windsurf"):
		return Windsurf
	case strings.Contains(hint, "insiders"):
		return VSCodeInsiders
	case strings.Contains(hint, "codium"):
		return VSCodium
	}
	return VSCode
}
