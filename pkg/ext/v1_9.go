package ext

// Additions in contracts-v1.9 (request 08-01, plans 08 and 09).

// CommandVisibilityMsg hides slash commands from menus at runtime (commands claude
// hides for this account or provider), without unregistering anything. The host
// keeps one overlay per Source; a new message replaces that source's overlay, and a
// command is hidden while any source hides it. Hidden commands leave Ctx.Commands()
// but keep their names reserved, so an engine command of the same name does not show
// through, and Ctx.Command(name) still resolves them, so typing one still works.
type CommandVisibilityMsg struct {
	Source string          // who decided, e.g. "settings.account"
	Hidden map[string]bool // command name (no slash) → hidden; nil/empty clears the overlay
}
