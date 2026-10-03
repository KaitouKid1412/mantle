package ext

import tea "charm.land/bubbletea/v2"

// Additions in contracts-v1.4.

// MouseEvent is a mouse message delivered to the component under the pointer (in
// the fullscreen layout, through AddressedMsg), with coordinates relative to the
// component's top-left cell. Inline mode has no mouse.
type MouseEvent struct {
	Msg  tea.MouseMsg
	X, Y int
}
