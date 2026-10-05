package ext

// Additions in contracts-v1.10 (request 12-01, fullscreen print).

// PrintedMsg carries the blocks a feature printed (Ctx.Print) while the fullscreen
// layout is active. Inline, printed blocks go to the terminal's scrollback and no
// PrintedMsg is sent; in fullscreen there is no scrollback, so the host broadcasts
// them instead and the fullscreen transcript shows them in place (the startup banner,
// panel closing lines, mod output). Switching layouts reprints, so nothing needs to
// cross a switch.
type PrintedMsg struct {
	Blocks []string
}
