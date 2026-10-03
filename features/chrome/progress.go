package chrome

import "github.com/KaitouKid1412/mantle/internal/term/osc"

// Engine session states (session_state_changed).
const (
	StateIdle           = "idle"
	StateRunning        = "running"
	StateRequiresAction = "requires_action"
)

// ProgressTracker derives the OSC 9;4 state from the session state and turn results:
// indeterminate while running, paused while waiting on the user, error after a failed
// turn (kept until the next turn starts), nothing when idle.
type ProgressTracker struct {
	state  string
	failed bool
}

// OnState records a session_state_changed state.
func (p *ProgressTracker) OnState(state string) {
	if state == StateRunning && p.state != StateRunning && p.state != StateRequiresAction {
		p.failed = false // a new turn clears the previous error
	}
	p.state = state
}

// OnResult records a turn result.
func (p *ProgressTracker) OnResult(isError bool) {
	p.failed = isError
}

// Clear forgets a shown error (the user acknowledged it, e.g. by typing).
func (p *ProgressTracker) Clear() { p.failed = false }

// State is the progress bar to show. enabled is terminalProgressBarEnabled (default
// true) combined with terminal support.
func (p *ProgressTracker) State(enabled bool) osc.ProgressState {
	if !enabled {
		return osc.ProgressNone
	}
	switch p.state {
	case StateRunning:
		return osc.ProgressIndeterminate
	case StateRequiresAction:
		return osc.ProgressPause
	}
	if p.failed {
		return osc.ProgressError
	}
	return osc.ProgressNone
}
