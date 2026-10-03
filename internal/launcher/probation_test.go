package launcher

import (
	"errors"
	"syscall"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name      string
		st        ExitStatus
		forwarded bool
		want      Outcome
	}{
		{"exit 0", ExitStatus{Code: 0}, false, OutcomeClean},
		{"exit 0 after forward", ExitStatus{Code: 0}, true, OutcomeClean},
		{"exit 75", ExitStatus{Code: 75}, false, OutcomeRestart},
		{"exit 64", ExitStatus{Code: 64}, false, OutcomeUsage},
		{"exit 1", ExitStatus{Code: 1}, false, OutcomeCrash},
		{"exit 2 panic", ExitStatus{Code: 2}, false, OutcomeCrash},
		{"exit 1 after forward", ExitStatus{Code: 1}, true, OutcomeInterrupted},
		{"SIGINT", ExitStatus{Code: -1, Signaled: true, Signal: syscall.SIGINT}, false, OutcomeInterrupted},
		{"SIGHUP", ExitStatus{Code: -1, Signaled: true, Signal: syscall.SIGHUP}, false, OutcomeInterrupted},
		{"SIGTERM", ExitStatus{Code: -1, Signaled: true, Signal: syscall.SIGTERM}, false, OutcomeInterrupted},
		{"SIGSEGV", ExitStatus{Code: -1, Signaled: true, Signal: syscall.SIGSEGV}, false, OutcomeCrash},
		{"SIGKILL", ExitStatus{Code: -1, Signaled: true, Signal: syscall.SIGKILL}, false, OutcomeCrash},
		{"SIGKILL after forward", ExitStatus{Code: -1, Signaled: true, Signal: syscall.SIGKILL}, true, OutcomeInterrupted},
		{"start error", ExitStatus{Code: -1, StartErr: errors.New("x")}, false, OutcomeCrash},
	}
	for _, c := range cases {
		if got := Classify(c.st, c.forwarded); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLauncherCode(t *testing.T) {
	if c := (ExitStatus{Code: 3}).LauncherCode(); c != 3 {
		t.Errorf("got %d", c)
	}
	if c := (ExitStatus{Code: -1, Signaled: true, Signal: syscall.SIGSEGV}).LauncherCode(); c != 139 {
		t.Errorf("got %d", c)
	}
	if c := (ExitStatus{Code: -1, StartErr: errors.New("x")}).LauncherCode(); c != 1 {
		t.Errorf("got %d", c)
	}
}

func TestDecide(t *testing.T) {
	probation := ProbationState{}
	oneFail := ProbationState{Failures: 1}
	healthy := ProbationState{Healthy: true}
	cases := []struct {
		name        string
		prev        ProbationState
		marker      bool
		out         Outcome
		canRollback bool
		want        Decision
	}{
		{"clean exit passes probation", probation, false, OutcomeClean, true,
			Decision{Action: ActionExit, State: healthy, PassedProbation: true}},
		{"clean exit of healthy build", healthy, true, OutcomeClean, true,
			Decision{Action: ActionExit, State: healthy}},
		{"first crash on probation", probation, false, OutcomeCrash, true,
			Decision{Action: ActionExit, State: oneFail, Abnormal: true}},
		{"second crash rolls back", oneFail, false, OutcomeCrash, true,
			Decision{Action: ActionRollback, State: ProbationState{Failures: 2}, Abnormal: true}},
		{"second crash without target", oneFail, false, OutcomeCrash, false,
			Decision{Action: ActionExit, State: ProbationState{Failures: 2}, Abnormal: true}},
		{"crash after marker written", oneFail, true, OutcomeCrash, true,
			Decision{Action: ActionExit, State: healthy, PassedProbation: true, Abnormal: true}},
		{"crash of healthy build", healthy, true, OutcomeCrash, true,
			Decision{Action: ActionExit, State: healthy, Abnormal: true}},
		{"restart on probation", probation, false, OutcomeRestart, true,
			Decision{Action: ActionRelaunch, State: probation}},
		{"restart after marker", probation, true, OutcomeRestart, true,
			Decision{Action: ActionRelaunch, State: healthy, PassedProbation: true}},
		{"interrupt does not count", oneFail, false, OutcomeInterrupted, true,
			Decision{Action: ActionExit, State: oneFail, Abnormal: true}},
		{"usage does not count", oneFail, false, OutcomeUsage, true,
			Decision{Action: ActionExit, State: oneFail}},
	}
	for _, c := range cases {
		if got := Decide(c.prev, c.marker, c.out, c.canRollback); got != c.want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}
