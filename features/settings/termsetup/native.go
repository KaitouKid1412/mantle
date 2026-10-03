package termsetup

// planKittyProtocol covers terminals that always speak the kitty keyboard protocol,
// which Bubble Tea turns on at startup: Shift+Enter already arrives as its own key.
func planKittyProtocol(in Input) Proposal {
	return Proposal{
		Status: Native,
		Summary: in.Terminal.Name() + " reports Shift+Enter on its own (kitty keyboard protocol), " +
			"so there is nothing to install.",
	}
}

// planWezTerm: WezTerm implements the kitty protocol but only answers programs that ask
// for it once enable_kitty_keyboard is set. Its config is Lua, which mantle won't edit.
func planWezTerm(in Input) Proposal {
	if in.KittyKeyboard {
		return nativeProposal(in.Terminal)
	}
	return Proposal{
		Status:  Manual,
		Summary: "WezTerm reports Shift+Enter once its kitty keyboard support is switched on.",
		Notes: []string{
			"Add `config.enable_kitty_keyboard = true` to ~/.wezterm.lua (or ~/.config/wezterm/wezterm.lua), " +
				"then restart mantle. mantle doesn't edit Lua configs itself.",
			fallbackNote,
		},
	}
}

// nativeProposal is used when the running terminal confirmed the kitty keyboard
// protocol, whatever its config says.
func nativeProposal(t Terminal) Proposal {
	return Proposal{
		Status: Native,
		Summary: t.Name() + " confirmed kitty keyboard protocol support, so Shift+Enter already " +
			"inserts a newline. Nothing to install.",
	}
}
