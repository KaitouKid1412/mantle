package app

import (
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// TestNoticeExpiresWithRealClock runs the host's real clock inside a synctest bubble:
// time.Sleep and tea.Tick advance the fake clock instantly and deterministically.
func TestNoticeExpiresWithRealClock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := NewHost(nil, HostOptions{Core: CoreFeatures()})
		r := New(Options{Host: h, NoBackgroundQuery: true}) // realClock
		r.Update(tea.WindowSizeMsg{Width: 60, Height: 10})

		start := time.Now()
		cmd := r.Ctx().Notify(ext.Notice{Key: "k", Text: "hello", Timeout: 3 * time.Second})
		if len(r.Notices()) != 1 {
			t.Fatal("notice not shown")
		}
		// Replacing the notice restarts its timer: the first expiry must be ignored.
		cmd2 := r.Ctx().Notify(ext.Notice{Key: "k", Text: "hello again", Timeout: 5 * time.Second})
		for _, m := range exttest.Exec(cmd) { // fires at +3s
			r.Update(m)
		}
		if len(r.Notices()) != 1 || r.Notices()[0].Text != "hello again" {
			t.Fatalf("stale expiry removed the replacement: %v", r.Notices())
		}
		for _, m := range exttest.Exec(cmd2) { // fires at +5s
			r.Update(m)
		}
		if len(r.Notices()) != 0 {
			t.Fatalf("notice did not expire: %v", r.Notices())
		}
		if got := time.Since(start); got != 5*time.Second {
			t.Fatalf("virtual time elapsed %v, want 5s", got)
		}
	})
}

func TestStickyNoticeAndDedupe(t *testing.T) {
	r, _ := newRoot(t)
	r.Ctx().Notify(ext.Notice{Key: "a", Text: "one", Timeout: -1})
	r.Ctx().Notify(ext.Notice{Key: "a", Text: "two", Timeout: -1})
	r.Ctx().Notify(ext.Notice{Key: "b", Text: "three", Level: ext.NoticeError})
	ns := r.Notices()
	if len(ns) != 2 || ns[0].Text != "two" || ns[1].Text != "three" {
		t.Fatalf("notices = %v", ns)
	}
}
