package launcher

import (
	"fmt"
	"syscall"
)

// MaxProbationFailures is how many failed launches of a build on probation
// trigger a rollback to last-good.
const MaxProbationFailures = 2

// Outcome classifies how a mantle-ui run ended.
type Outcome int

const (
	// OutcomeClean is exit 0.
	OutcomeClean Outcome = iota
	// OutcomeRestart is exit 75: relaunch current.
	OutcomeRestart
	// OutcomeUsage is exit 64: bad arguments, the UI never started.
	OutcomeUsage
	// OutcomeInterrupted is a death by SIGINT, SIGTERM or SIGHUP, or any
	// exit after the launcher forwarded a signal: not the build's fault.
	OutcomeInterrupted
	// OutcomeCrash is anything else, including a failure to start.
	OutcomeCrash
)

func (o Outcome) String() string {
	switch o {
	case OutcomeClean:
		return "clean"
	case OutcomeRestart:
		return "restart"
	case OutcomeUsage:
		return "usage"
	case OutcomeInterrupted:
		return "interrupted"
	case OutcomeCrash:
		return "crash"
	}
	return fmt.Sprintf("Outcome(%d)", int(o))
}

// ExitStatus is how a child process ended.
type ExitStatus struct {
	Code     int            // exit code; -1 if killed by a signal or never started
	Signaled bool           // killed by Signal
	Signal   syscall.Signal // valid if Signaled
	StartErr error          // the process could not be started
}

// LauncherCode is the exit code the launcher itself should return.
func (e ExitStatus) LauncherCode() int {
	switch {
	case e.StartErr != nil:
		return 1
	case e.Signaled:
		return 128 + int(e.Signal)
	}
	return e.Code
}

func (e ExitStatus) String() string {
	switch {
	case e.StartErr != nil:
		return "failed to start: " + e.StartErr.Error()
	case e.Signaled:
		return "killed by " + e.Signal.String()
	}
	return fmt.Sprintf("exit status %d", e.Code)
}

// Classify maps an exit status to an Outcome. forwarded is true if the
// launcher forwarded a terminating signal to the child during the run.
func Classify(e ExitStatus, forwarded bool) Outcome {
	switch {
	case e.StartErr != nil:
		return OutcomeCrash
	case e.Signaled:
		switch e.Signal {
		case syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP:
			return OutcomeInterrupted
		}
		if forwarded {
			return OutcomeInterrupted
		}
		return OutcomeCrash
	}
	switch e.Code {
	case ExitOK:
		return OutcomeClean
	case ExitRestart:
		return OutcomeRestart
	case ExitUsage:
		return OutcomeUsage
	}
	if forwarded {
		return OutcomeInterrupted
	}
	return OutcomeCrash
}

// ProbationState is a build's probation record.
type ProbationState struct {
	// Healthy is true once the build has written its healthy marker (or
	// exited cleanly): it is no longer on probation.
	Healthy bool
	// Failures counts failed launches while on probation.
	Failures int
}

// Action is what the launcher does after a run.
type Action int

const (
	// ActionExit returns to the shell.
	ActionExit Action = iota
	// ActionRelaunch re-reads current and starts it with the handoff args.
	ActionRelaunch
	// ActionRollback flips current to last-good and relaunches.
	ActionRollback
)

func (a Action) String() string {
	switch a {
	case ActionExit:
		return "exit"
	case ActionRelaunch:
		return "relaunch"
	case ActionRollback:
		return "rollback"
	}
	return fmt.Sprintf("Action(%d)", int(a))
}

// Decision is the result of Decide.
type Decision struct {
	Action Action
	// State is the build's new probation state, to persist.
	State ProbationState
	// PassedProbation is true when the build left probation during this run:
	// last-good should now point at it.
	PassedProbation bool
	// Abnormal is true when the terminal must be restored and a notice shown.
	Abnormal bool
}

// Decide is the probation state machine. prev is the build's state when it
// was launched, markerNow whether the healthy marker exists after the run,
// out how the run ended, and canRollback whether a different last-good build
// exists.
func Decide(prev ProbationState, markerNow bool, out Outcome, canRollback bool) Decision {
	healthy := prev.Healthy || markerNow || out == OutcomeClean
	d := Decision{
		State:           prev,
		PassedProbation: !prev.Healthy && healthy,
		Abnormal:        out == OutcomeCrash || out == OutcomeInterrupted,
	}
	if healthy {
		d.State = ProbationState{Healthy: true}
	}
	switch out {
	case OutcomeRestart:
		d.Action = ActionRelaunch
	case OutcomeCrash:
		if healthy {
			break
		}
		d.State.Failures++
		if d.State.Failures >= MaxProbationFailures && canRollback {
			d.Action = ActionRollback
		}
	}
	return d
}
