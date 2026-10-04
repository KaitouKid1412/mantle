package app

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func TestAdoptInsteadOfSpawn(t *testing.T) {
	for _, tc := range []struct {
		name      string
		adoptErr  error
		wantSpawn bool
	}{
		{"adopted", nil, false},
		{"adopt fails, spawn instead", errors.New("fds gone"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spawned, adopted := 0, 0
			h := NewHost(nil, HostOptions{Core: CoreFeatures()})
			r := New(Options{
				Host: h, NoBackgroundQuery: true, Clock: &manualClock{t: time.Unix(0, 0)}, FrameInterval: time.Millisecond,
				MainSpawn: &ext.SpawnOpts{Resume: "s1"},
				Spawn:     func(string, ext.SpawnOpts) error { spawned++; return nil },
				Adopt:     func() error { adopted++; return tc.adoptErr },
			})
			r.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
			drive(t, r, cmdMsgsAll(r.Init())...)
			if adopted != 1 {
				t.Fatalf("adopt called %d times", adopted)
			}
			if (spawned == 1) != tc.wantSpawn {
				t.Fatalf("spawned %d times, want spawn=%v", spawned, tc.wantSpawn)
			}
		})
	}
}

func TestSpawnWithoutGates(t *testing.T) {
	var got ext.SpawnOpts
	h := NewHost(nil, HostOptions{Core: CoreFeatures()})
	r := New(Options{
		Host: h, NoBackgroundQuery: true, Clock: &manualClock{t: time.Unix(0, 0)}, FrameInterval: time.Millisecond,
		MainSpawn: &ext.SpawnOpts{Model: "m"},
		Spawn:     func(id string, o ext.SpawnOpts) error { got = o; return nil },
	})
	r.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	drive(t, r, cmdMsgsAll(r.Init())...)
	if got.Model != "m" {
		t.Fatalf("main engine not spawned directly: %+v", got)
	}
}

func TestSpawnThroughGate(t *testing.T) {
	var gate *ext.SpawnGateMsg
	f := ext.Feature{ID: "test.gate", Order: 10, Setup: func(r ext.Registrar) error {
		ext.Subscribe(r, "test.gate", func(c ext.Ctx, m ext.SpawnGateMsg) tea.Cmd { gate = &m; return nil })
		return nil
	}}
	spawned := ""
	h := NewHost([]ext.Feature{f}, HostOptions{Core: CoreFeatures()})
	r := New(Options{
		Host: h, NoBackgroundQuery: true, Clock: &manualClock{t: time.Unix(0, 0)}, FrameInterval: time.Millisecond,
		MainSpawn: &ext.SpawnOpts{Model: "m"},
		Spawn:     func(id string, o ext.SpawnOpts) error { spawned = o.Settings; return nil },
	})
	r.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	drive(t, r, cmdMsgsAll(r.Init())...)
	if gate == nil || spawned != "" {
		t.Fatalf("gate=%v spawned=%q: must wait for the gate", gate, spawned)
	}
	o := gate.Opts
	o.Settings = `{"disabledMcpjsonServers":["x"]}`
	drive(t, r, cmdMsgsAll(gate.Proceed(o))...)
	if spawned != o.Settings {
		t.Fatalf("Proceed did not spawn with amended options: %q", spawned)
	}
	out := drive(t, r, cmdMsgsAll(gate.Abort("declined"))...)
	quit := false
	for _, m := range out {
		_, quit = m.(tea.QuitMsg)
	}
	if !quit || r.ExitReason() != "declined" {
		t.Fatalf("Abort must exit with the reason: quit=%v reason=%q", quit, r.ExitReason())
	}
}
