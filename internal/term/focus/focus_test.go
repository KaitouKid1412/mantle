package focus

import (
	"testing"
	"time"
)

func TestTracker(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var tr Tracker
	if tr.State() != Unknown || tr.Since(t0) != 0 {
		t.Fatal("zero Tracker should be Unknown")
	}
	if !tr.Away(t0, time.Minute) {
		t.Error("unknown focus with no input should be away")
	}
	tr.Input(t0)
	if tr.Away(t0.Add(30*time.Second), time.Minute) {
		t.Error("recent input: not away")
	}
	if !tr.Away(t0.Add(61*time.Second), time.Minute) {
		t.Error("idle past threshold: away")
	}
	if tr.State() != Unknown {
		t.Error("input must not invent a focus report")
	}

	tr.Blur(t0.Add(time.Second))
	if tr.State() != Blurred || !tr.Away(t0.Add(time.Second), time.Hour) {
		t.Error("blurred is away")
	}
	if got := tr.Since(t0.Add(3 * time.Second)); got != 2*time.Second {
		t.Errorf("Since = %v", got)
	}
	tr.Blur(t0.Add(5 * time.Second)) // repeated blur keeps the first timestamp
	if got := tr.Since(t0.Add(5 * time.Second)); got != 4*time.Second {
		t.Errorf("Since after repeated blur = %v", got)
	}

	tr.Focus(t0.Add(10 * time.Second))
	if tr.State() != Focused || tr.Away(t0.Add(time.Hour), time.Minute) {
		t.Error("focused is never away")
	}

	tr.Blur(t0.Add(11 * time.Second))
	tr.Input(t0.Add(12 * time.Second))
	if tr.State() != Focused {
		t.Error("input while blurred implies focus")
	}
	if got := tr.Idle(t0.Add(15 * time.Second)); got != 3*time.Second {
		t.Errorf("Idle = %v", got)
	}
}

func TestStateString(t *testing.T) {
	for s, want := range map[State]string{Unknown: "unknown", Focused: "focused", Blurred: "blurred"} {
		if s.String() != want {
			t.Errorf("%d.String() = %q", s, s.String())
		}
	}
}
