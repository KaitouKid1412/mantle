package chrome

import (
	"fmt"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/focus"
	"github.com/KaitouKid1412/mantle/internal/term/notify"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// notifier decides when to send a desktop notification or bell and writes it with
// tea.Raw (in -p mode Claude Code drops hook terminal sequences, so the engine can't).
type notifier struct {
	focus   focus.Tracker
	channel notify.Channel // resolved preferredNotifChannel
	inTmux  bool
	policy  notify.Policy

	waitingSince time.Time // when the session started waiting on the user
	waitGen      int
	waitNotified bool // this wait already produced a notification
	lastSent     time.Time
}

type notifyCheckMsg struct{ gen int }

func (n *notifier) readSettings(s ext.Settings, env terminal.Env) {
	n.channel = notify.Resolve(notify.ParseChannel(ext.ClaudeString(s, "preferredNotifChannel", "")), env)
	n.inTmux = terminal.InTmux(env)
}

// observe handles focus, the session state and turn results. title names the session
// in the notification.
func (n *notifier) observe(ctx ext.Ctx, msg tea.Msg, title string) tea.Cmd {
	now := ctx.Clock().Now()
	switch m := msg.(type) {
	case tea.FocusMsg:
		n.focus.Focus(now)
	case tea.BlurMsg:
		n.focus.Blur(now)
		if !n.waitingSince.IsZero() {
			return n.check(ctx, notify.InputNeeded, title, 0)
		}
	case notifyCheckMsg:
		if m.gen == n.waitGen && !n.waitingSince.IsZero() {
			return n.check(ctx, notify.InputNeeded, title, 0)
		}
	case ext.EngineEventMsg:
		if !isMain(m.EngineID) {
			return nil
		}
		switch e := m.Event.(type) {
		case *proto.SessionStateChanged:
			if e.State != proto.StateRequiresAction {
				n.waitingSince, n.waitNotified = time.Time{}, false
				return nil
			}
			if !n.waitingSince.IsZero() {
				return nil
			}
			n.waitingSince = now
			n.waitGen++
			if cmd := n.check(ctx, notify.InputNeeded, title, 0); cmd != nil {
				return cmd
			}
			gen := n.waitGen
			return ctx.Clock().Tick(notify.RecheckAfter(0, n.policy), func(time.Time) tea.Msg {
				return ext.AddressedMsg{To: TerminalID, Msg: notifyCheckMsg{gen}}
			})
		case *proto.Result:
			if e.Interrupted() {
				return nil
			}
			return n.check(ctx, notify.TurnDone, title, time.Duration(e.DurationMS)*time.Millisecond)
		}
	}
	return nil
}

// activity records a key press or paste (from the chrome interceptor).
func (n *notifier) activity(now time.Time) { n.focus.Input(now) }

func (n *notifier) check(ctx ext.Ctx, ev notify.Event, title string, turn time.Duration) tea.Cmd {
	if ev == notify.InputNeeded && n.waitNotified {
		return nil
	}
	now := ctx.Clock().Now()
	s := notify.Situation{
		Event:     ev,
		Focus:     n.focus.State(),
		Idle:      n.focus.Idle(now),
		Turn:      turn,
		HadLast:   !n.lastSent.IsZero(),
		SinceLast: now.Sub(n.lastSent),
	}
	if !n.waitingSince.IsZero() {
		s.Waiting = now.Sub(n.waitingSince)
	}
	if !notify.Decide(s, notify.Prefs{Channel: n.channel}, n.policy) {
		return nil
	}
	body := "Waiting for your input"
	if ev == notify.TurnDone {
		body = "Finished after " + shortDuration(turn)
	}
	seq := notify.Build(n.channel, notify.Notification{Title: title, Body: body}, n.inTmux)
	if len(seq) == 0 {
		return nil
	}
	n.lastSent = now
	if ev == notify.InputNeeded {
		n.waitNotified = true // one notification per wait
	}
	return tea.Raw(string(seq))
}

// notifyTitle is the session's name, else the project directory, prefixed by mantle.
func notifyTitle(s *sessionState) string {
	name := s.Name()
	if name == "" && s.Cwd != "" {
		name = filepath.Base(s.Cwd)
	}
	if name == "" {
		return notify.DefaultTitle
	}
	return notify.DefaultTitle + " · " + name
}

func shortDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Round(time.Second)/time.Second))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d/time.Minute), int(d%time.Minute/time.Second))
	}
	return fmt.Sprintf("%dh %dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
}
