// Package focus tracks whether the terminal has focus and when the user last acted, so
// notifications fire only when the user is likely away.
//
// The host enables focus reporting (Bubble Tea v2 View.ReportFocus) and forwards
// tea.FocusMsg / tea.BlurMsg as Focus / Blur calls, and key presses as Input. Terminals
// that never report focus stay in Unknown, and callers fall back to idle time.
package focus

import "time"

// State is the terminal focus state.
type State int

const (
	Unknown State = iota // the terminal has not reported focus
	Focused
	Blurred
)

func (s State) String() string {
	switch s {
	case Focused:
		return "focused"
	case Blurred:
		return "blurred"
	}
	return "unknown"
}

// Tracker records focus changes and user activity. The zero value is ready to use and
// starts in Unknown. It is not safe for concurrent use; keep it on the UI goroutine.
type Tracker struct {
	state     State
	changedAt time.Time
	lastInput time.Time
}

// Focus records a focus-in report.
func (t *Tracker) Focus(now time.Time) {
	if t.state != Focused {
		t.state, t.changedAt = Focused, now
	}
}

// Blur records a focus-out report.
func (t *Tracker) Blur(now time.Time) {
	if t.state != Blurred {
		t.state, t.changedAt = Blurred, now
	}
}

// Input records user activity (a key press, paste or click). Activity implies focus, so
// a terminal that reported Blurred and then delivers a key is treated as focused.
func (t *Tracker) Input(now time.Time) {
	t.lastInput = now
	if t.state == Blurred {
		t.state, t.changedAt = Focused, now
	}
}

// State returns the current focus state.
func (t *Tracker) State() State { return t.state }

// Since returns how long the current state has held, or 0 if no report arrived yet.
func (t *Tracker) Since(now time.Time) time.Duration {
	if t.changedAt.IsZero() {
		return 0
	}
	return now.Sub(t.changedAt)
}

// Idle returns the time since the last Input. Before any input it returns a very large
// duration, so a fresh session with no activity counts as idle.
func (t *Tracker) Idle(now time.Time) time.Duration {
	if t.lastInput.IsZero() {
		return time.Duration(1<<63 - 1)
	}
	return now.Sub(t.lastInput)
}

// Away reports whether the user is probably not looking: the terminal is blurred, or
// focus is unknown and there was no input for at least idle.
func (t *Tracker) Away(now time.Time, idle time.Duration) bool {
	switch t.state {
	case Blurred:
		return true
	case Focused:
		return false
	}
	return t.Idle(now) >= idle
}
