package ext

// Additions in contracts-v1.6 (plan 12's fullscreen switch).

// LayoutRequestMsg asks the host to switch between Inline and Fullscreen at runtime
// (/tui). The host refuses Fullscreen when the alternate screen is disabled
// (CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN) or in screen-reader mode, with a notice.
type LayoutRequestMsg struct{ Mode LayoutMode }

// LayoutChangedMsg is broadcast after the layout changed. Leaving Fullscreen, the host
// clears the screen (ScreenClearedMsg follows) so the commit policy reprints the
// transcript into native scrollback.
type LayoutChangedMsg struct{ Mode LayoutMode }

// SidebarResizeMsg grows (Delta > 0) or shrinks a fullscreen sidebar slot
// (SlotSidebarL or SlotSidebarR) by Delta columns.
type SidebarResizeMsg struct {
	Slot  Slot
	Delta int
}
