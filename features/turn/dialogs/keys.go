package dialogs

import tea "charm.land/bubbletea/v2"

// Effect tells the host what to do after a key.
type Effect int

const (
	// None: nothing beyond redrawing.
	None Effect = iota
	// Answered: the dialog has its response; reply and close it.
	Answered
	// CycleMode: switch the session's permission mode (shift+tab inside a dialog).
	CycleMode
	// EditExternal: open the dialog's EditText in $EDITOR and pass the result to
	// SetEditedText.
	EditExternal
	// OpenURL: open the dialog's URL in the browser.
	OpenURL
)

// Model is what every dialog view-model implements.
type Model interface {
	// HandleKey processes one key press. handled is false for keys the dialog ignores, so
	// the host can route them elsewhere.
	HandleKey(tea.KeyPressMsg) (handled bool, eff Effect)
	// HandlePaste inserts pasted text into the focused text field, if any.
	HandlePaste(string) (handled bool, eff Effect)
	// Action runs a Claude Code action ID resolved by the keymap (confirm:*, select:*,
	// tabs:*), for users who rebound the defaults.
	Action(id string) (handled bool, eff Effect)
	// View renders the dialog at width cells.
	View(width int, st Styles) string
	// Done reports whether the dialog has been answered.
	Done() bool
}

// Positioner is implemented by view-models that show a queue counter "(1 of 3)".
type Positioner interface {
	SetPosition(index, total int)
}

func counter(index, total int) string {
	if total <= 1 {
		return ""
	}
	return " (" + itoa(index) + " of " + itoa(total) + ")"
}

type act int

const (
	actNone act = iota
	actUp
	actDown
	actYes
	actNo
	actNextField
	actPrevField
	actToggle
	actCycleMode
	actEdit
	actLeft
	actRight
	actPageUp
	actPageDown
	actFirst
	actLast
)

// keyAct maps a key to the Confirmation-context default action. digit is 1–9 for number
// keys, which pick an option directly.
func keyAct(k tea.KeyPressMsg) (a act, digit int) {
	switch k.Keystroke() {
	case "up", "ctrl+p":
		return actUp, 0
	case "down", "ctrl+n":
		return actDown, 0
	case "enter":
		return actYes, 0
	case "esc":
		return actNo, 0
	case "tab":
		return actNextField, 0
	case "space":
		return actToggle, 0
	case "shift+tab":
		return actCycleMode, 0
	case "ctrl+g":
		return actEdit, 0
	case "left":
		return actLeft, 0
	case "right":
		return actRight, 0
	case "pgup":
		return actPageUp, 0
	case "pgdown":
		return actPageDown, 0
	case "home":
		return actFirst, 0
	case "end":
		return actLast, 0
	}
	if k.Mod == 0 && len(k.Text) == 1 && k.Text[0] >= '1' && k.Text[0] <= '9' {
		return actNone, int(k.Text[0] - '0')
	}
	return actNone, 0
}

// actionAct maps a Claude Code action ID to an act.
func actionAct(id string) act {
	switch id {
	case "confirm:yes", "select:accept":
		return actYes
	case "confirm:no", "select:cancel":
		return actNo
	case "confirm:previous", "select:previous":
		return actUp
	case "confirm:next", "select:next":
		return actDown
	case "confirm:nextField":
		return actNextField
	case "confirm:previousField":
		return actPrevField
	case "confirm:toggle":
		return actToggle
	case "confirm:cycleMode":
		return actCycleMode
	case "select:pageUp":
		return actPageUp
	case "select:pageDown":
		return actPageDown
	case "select:first":
		return actFirst
	case "select:last":
		return actLast
	case "tabs:next":
		return actRight
	case "tabs:previous":
		return actLeft
	case "chat:externalEditor":
		return actEdit
	}
	return actNone
}
