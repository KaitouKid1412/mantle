package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// FrozenClock is a clock stopped at T whose ticks never fire: stories, selftest and
// goldens use it so time-dependent output is deterministic.
type FrozenClock struct{ T time.Time }

func (c FrozenClock) Now() time.Time                                      { return c.T }
func (c FrozenClock) Tick(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }

// StoryEpoch is the time stories render at.
var StoryEpoch = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

// StoryRoot builds a root for rendering stories outside a running program: the host's
// features, a frozen clock, the default theme, no engine, a terminal of width×height.
func StoryRoot(h *Host, width, height int) *Root {
	t := theme.Default()
	r := New(Options{Host: h, Clock: FrozenClock{T: StoryEpoch}, Theme: &t, NoBackgroundQuery: true})
	r.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return r
}

// Story finds a registered story by ID.
func (h *Host) Story(id string) (ext.Story, bool) {
	for _, s := range h.Stories() {
		if s.ID == id {
			return s, true
		}
	}
	return ext.Story{}, false
}
