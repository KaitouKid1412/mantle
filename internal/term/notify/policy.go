package notify

import (
	"time"

	"github.com/KaitouKid1412/mantle/internal/term/focus"
)

// Event is what may deserve a notification.
type Event int

const (
	// InputNeeded: the session waits on the user (session_state_changed=requires_action:
	// a permission prompt, a dialog or a question).
	InputNeeded Event = iota + 1
	// TurnDone: a turn finished (result).
	TurnDone
)

// Situation is the state a decision is made from. Every field is a plain value, so
// Decide is a pure function and can be table-tested.
type Situation struct {
	Event Event
	Focus focus.State
	// Idle is the time since the user's last key press.
	Idle time.Duration
	// Waiting is how long an InputNeeded request has been pending.
	Waiting time.Duration
	// Turn is the duration of the turn that just ended (TurnDone).
	Turn time.Duration
	// SinceLast is the time since the previous notification; ignored when !HadLast.
	SinceLast time.Duration
	HadLast   bool
	// EngineRang is set when the engine's own Notification hook already rang a bell
	// for this event, so mantle does not ring twice.
	EngineRang bool
}

// Prefs are the user's notification settings.
type Prefs struct {
	Channel Channel // resolved; Disabled turns everything off
	// SkipInputNeeded turns off InputNeeded notifications.
	SkipInputNeeded bool
	// Quiet suppresses all notifications (quiet hours, do-not-disturb).
	Quiet bool
}

// Policy holds the timing thresholds. The zero value uses DefaultPolicy's values.
type Policy struct {
	// InputDelay: with unknown focus, notify about a pending request only after it has
	// waited this long with no key press.
	InputDelay time.Duration
	// BlurredTurn: with a blurred terminal, notify about turns at least this long.
	BlurredTurn time.Duration
	// LongTurn: with unknown focus, notify about turns at least this long...
	LongTurn time.Duration
	// ...and only if the user has been idle at least this long.
	AwayIdle time.Duration
	// MinGap throttles bursts of notifications.
	MinGap time.Duration
}

// DefaultPolicy is mantle's default timing.
var DefaultPolicy = Policy{
	InputDelay:  6 * time.Second,
	BlurredTurn: 3 * time.Second,
	LongTurn:    30 * time.Second,
	AwayIdle:    20 * time.Second,
	MinGap:      2 * time.Second,
}

func (p Policy) withDefaults() Policy {
	d := DefaultPolicy
	if p.InputDelay > 0 {
		d.InputDelay = p.InputDelay
	}
	if p.BlurredTurn > 0 {
		d.BlurredTurn = p.BlurredTurn
	}
	if p.LongTurn > 0 {
		d.LongTurn = p.LongTurn
	}
	if p.AwayIdle > 0 {
		d.AwayIdle = p.AwayIdle
	}
	if p.MinGap > 0 {
		d.MinGap = p.MinGap
	}
	return d
}

// Decide reports whether to notify. A focused terminal never notifies: the user is
// looking at it.
func Decide(s Situation, prefs Prefs, p Policy) bool {
	p = p.withDefaults()
	switch {
	case prefs.Channel == Disabled || prefs.Channel == "" || prefs.Quiet:
		return false
	case s.EngineRang && prefs.Channel == TerminalBell:
		return false
	case s.HadLast && s.SinceLast < p.MinGap:
		return false
	case s.Focus == focus.Focused:
		return false
	}
	switch s.Event {
	case InputNeeded:
		if prefs.SkipInputNeeded {
			return false
		}
		if s.Focus == focus.Blurred {
			return true
		}
		return s.Waiting >= p.InputDelay && s.Idle >= p.InputDelay
	case TurnDone:
		if s.Focus == focus.Blurred {
			return s.Turn >= p.BlurredTurn
		}
		return s.Turn >= p.LongTurn && s.Idle >= p.AwayIdle
	}
	return false
}

// RecheckAfter returns when a caller should re-evaluate an InputNeeded event that was
// not yet notifiable (unknown focus): after the request has waited InputDelay.
func RecheckAfter(waiting time.Duration, p Policy) time.Duration {
	p = p.withDefaults()
	if waiting >= p.InputDelay {
		return 0
	}
	return p.InputDelay - waiting
}
