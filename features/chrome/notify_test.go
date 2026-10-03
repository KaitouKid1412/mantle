package chrome

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// raws runs cmd and returns the tea.Raw payloads and addressed messages it produced.
func raws(cmd tea.Cmd) (out []string, addressed []tea.Msg) {
	for _, m := range exttest.Exec(cmd) {
		switch v := m.(type) {
		case tea.RawMsg:
			out = append(out, v.Msg.(string))
		case ext.AddressedMsg:
			addressed = append(addressed, v.Msg)
		}
	}
	return out, addressed
}

func newNotifyTerm(t *testing.T, env terminal.Map) (*terminalComp, *exttest.Ctx) {
	t.Helper()
	ctx := exttest.NewCtx()
	c := newTerminal(env)
	c.Init(ctx)
	c.Update(ctx, ext.SessionChangedMsg{Info: ext.SessionInfo{Cwd: "/w/app"}})
	return c, ctx
}

func TestNotifyInputNeededBlurred(t *testing.T) {
	c, ctx := newNotifyTerm(t, terminal.Map{"TERM_PROGRAM": "ghostty"})
	if !c.TerminalState(ctx).ReportFocus {
		t.Fatal("focus reporting must be on")
	}
	c.Update(ctx, tea.BlurMsg{})
	out, _ := raws(c.Update(ctx, ev(&proto.SessionStateChanged{State: proto.StateRequiresAction})))
	if len(out) != 1 || out[0] != "\x1b]777;notify;mantle · app;Waiting for your input\a" {
		t.Fatalf("notification = %q", out)
	}
	// The same wait never notifies twice.
	ctx.ClockV.Advance(time.Minute)
	if out, _ := raws(c.Update(ctx, tea.BlurMsg{})); len(out) != 0 {
		t.Errorf("second notification for one wait: %q", out)
	}
}

func TestNotifyInputNeededFocused(t *testing.T) {
	c, ctx := newNotifyTerm(t, terminal.Map{"TERM_PROGRAM": "ghostty"})
	c.Update(ctx, tea.FocusMsg{})
	out, addressed := raws(c.Update(ctx, ev(&proto.SessionStateChanged{State: proto.StateRequiresAction})))
	if len(out) != 0 {
		t.Errorf("focused terminal notified: %q", out)
	}
	// The recheck finds the user still looking.
	ctx.ClockV.Advance(10 * time.Second)
	for _, m := range addressed {
		if out, _ := raws(c.Update(ctx, m)); len(out) != 0 {
			t.Errorf("recheck notified while focused: %q", out)
		}
	}
	// Blurring while still waiting notifies.
	if out, _ := raws(c.Update(ctx, tea.BlurMsg{})); len(out) != 1 {
		t.Errorf("blur while waiting should notify, got %q", out)
	}
}

func TestNotifyInputNeededUnknownFocus(t *testing.T) {
	c, ctx := newNotifyTerm(t, terminal.Map{"TERM_PROGRAM": "iTerm.app"})
	out, addressed := raws(c.Update(ctx, ev(&proto.SessionStateChanged{State: proto.StateRequiresAction})))
	if len(out) != 0 || len(addressed) != 1 {
		t.Fatalf("immediate = %q, rechecks = %v", out, addressed)
	}
	ctx.ClockV.Advance(6 * time.Second)
	out, _ = raws(c.Update(ctx, addressed[0]))
	if len(out) != 1 || !strings.HasPrefix(out[0], "\x1b]9;mantle · app: Waiting") {
		t.Errorf("recheck notification = %q", out)
	}
	// A stale recheck (the wait ended) does nothing.
	c.Update(ctx, ev(&proto.SessionStateChanged{State: proto.StateRunning}))
	if out, _ := raws(c.Update(ctx, addressed[0])); len(out) != 0 {
		t.Errorf("stale recheck = %q", out)
	}
}

func TestNotifyTurnDone(t *testing.T) {
	c, ctx := newNotifyTerm(t, terminal.Map{"KITTY_WINDOW_ID": "1", "TMUX": "/tmp/t"})
	c.Update(ctx, tea.BlurMsg{})
	out, _ := raws(c.Update(ctx, ev(&proto.Result{DurationMS: 65_000})))
	if len(out) != 1 || !strings.HasPrefix(out[0], "\x1bPtmux;") || !strings.Contains(out[0], "Finished after 1m 5s") {
		t.Fatalf("turn notification = %q", out)
	}
	ctx.ClockV.Advance(time.Minute)
	if out, _ := raws(c.Update(ctx, ev(&proto.Result{DurationMS: 65_000, TerminalReason: proto.TerminalAbortedTools}))); len(out) != 0 {
		t.Errorf("interrupted turn notified: %q", out)
	}
	c.Update(ctx, tea.FocusMsg{})
	if out, _ := raws(c.Update(ctx, ev(&proto.Result{DurationMS: 65_000}))); len(out) != 0 {
		t.Errorf("focused turn notified: %q", out)
	}
}

func TestNotifyKeyActivity(t *testing.T) {
	c, ctx := newNotifyTerm(t, terminal.Map{"TERM_PROGRAM": "iTerm.app"})
	msg, cmd := c.intercept(ctx, tea.KeyPressMsg{Code: 'x'})
	if cmd != nil || msg == nil {
		t.Fatal("the interceptor passes messages through")
	}
	// Unknown focus and recent typing: a long turn doesn't notify.
	ctx.ClockV.Advance(5 * time.Second)
	if out, _ := raws(c.Update(ctx, ev(&proto.Result{DurationMS: 60_000}))); len(out) != 0 {
		t.Errorf("present user notified: %q", out)
	}
	ctx.ClockV.Advance(time.Minute)
	if out, _ := raws(c.Update(ctx, ev(&proto.Result{DurationMS: 60_000}))); len(out) != 1 {
		t.Errorf("away user not notified: %q", out)
	}
}

func TestNotifyChannelSetting(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.SettingsV.ClaudeM["preferredNotifChannel"] = "terminal_bell"
	c := newTerminal(terminal.Map{"TERM_PROGRAM": "Apple_Terminal"})
	c.Init(ctx)
	c.Update(ctx, tea.BlurMsg{})
	if out, _ := raws(c.Update(ctx, ev(&proto.SessionStateChanged{State: proto.StateRequiresAction}))); len(out) != 1 || out[0] != "\a" {
		t.Errorf("bell = %q", out)
	}
	ctx.SettingsV.ClaudeM["preferredNotifChannel"] = "notifications_disabled"
	c.Update(ctx, ext.SettingsMsg{})
	c.Update(ctx, ev(&proto.SessionStateChanged{State: proto.StateIdle}))
	ctx.ClockV.Advance(time.Minute)
	if out, _ := raws(c.Update(ctx, ev(&proto.SessionStateChanged{State: proto.StateRequiresAction}))); len(out) != 0 {
		t.Errorf("disabled = %q", out)
	}
	// auto in a terminal without native notifications: nothing.
	c2, ctx2 := newNotifyTerm(t, terminal.Map{"TERM_PROGRAM": "vscode"})
	c2.Update(ctx2, tea.BlurMsg{})
	if out, _ := raws(c2.Update(ctx2, ev(&proto.SessionStateChanged{State: proto.StateRequiresAction}))); len(out) != 0 {
		t.Errorf("auto/vscode = %q", out)
	}
}

func TestShortDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		4 * time.Second: "4s", 65 * time.Second: "1m 5s", 2*time.Hour + 3*time.Minute: "2h 3m",
	} {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%v) = %q", d, got)
		}
	}
}
