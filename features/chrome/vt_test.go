package chrome

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// startChrome runs every chrome feature in the real host inside the vt emulator.
func startChrome(t *testing.T, settings map[string]any) *testkit.Harness {
	t.Helper()
	var feats []ext.Feature
	for _, f := range ext.Pending() {
		if strings.HasPrefix(f.ID, "chrome.") {
			feats = append(feats, f)
		}
	}
	host := app.NewHost(feats, app.HostOptions{Core: app.CoreFeatures()})
	root := app.New(app.Options{Host: host, NoBackgroundQuery: true,
		Settings: exttest.NewSettings(settings)})
	hs := testkit.New(t, root, testkit.WithSize(100, 30))
	hs.WaitForText("manual approval", 5*time.Second) // the footer is up
	return hs
}

// waitOutput waits until the raw terminal output contains seq.
func waitOutput(t *testing.T, hs *testkit.Harness, seq string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(hs.Output(), []byte(seq)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("output never contained %q\n%q", seq, tail(hs.Output(), 600))
}

func tail(b []byte, n int) []byte {
	if len(b) > n {
		return b[len(b)-n:]
	}
	return b
}

func mainEv(e proto.Event) ext.EngineEventMsg {
	return ext.EngineEventMsg{EngineID: ext.MainEngine, Event: e}
}

func TestVTTitleAndProgress(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("TERM_PROGRAM_VERSION", "1.2.0")
	t.Setenv("CLAUDE_CODE_DISABLE_TERMINAL_TITLE", "")
	t.Setenv("TMUX", "")
	hs := startChrome(t, nil)

	hs.SendMsg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{
		SessionID: "s1", Cwd: "/work/widgets", PermissionMode: "plan"}})
	waitOutput(t, hs, "\x1b]2;widgets\a")
	hs.WaitForText("planning only", 3*time.Second)

	hs.SendMsg(mainEv(&proto.SessionStateChanged{State: proto.StateRunning}))
	waitOutput(t, hs, "\x1b]9;4;3")
	before := len(hs.Output())
	hs.SendMsg(mainEv(&proto.SessionStateChanged{State: proto.StateIdle}))
	deadline := time.Now().Add(5 * time.Second)
	for !bytes.Contains(hs.Output()[before:], []byte("\x1b]9;4;0")) {
		if time.Now().After(deadline) {
			t.Fatalf("progress not cleared on idle: %q", tail(hs.Output(), 300))
		}
		time.Sleep(10 * time.Millisecond)
	}

	hs.SendMsg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{Title: "parser work"}})
	waitOutput(t, hs, "\x1b]2;parser work\a")
}

func TestVTStatusLineAndFooter(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "Apple_Terminal") // no progress bar, no desktop notifications
	hs := startChrome(t, map[string]any{
		"statusLine": map[string]any{"type": "command", "command": `printf 'SL %s cols' "$COLUMNS"`},
	})
	hs.SendMsg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{
		SessionID: "s1", Cwd: t.TempDir(), PermissionMode: "acceptEdits"}})
	hs.WaitForText("SL 100 cols", 10*time.Second)
	hs.WaitForText("edits auto-approved", 3*time.Second)
	if strings.Contains(hs.Screen(), "? for shortcuts") {
		t.Error("a statusLine command hides the shortcuts hint")
	}
	slRow, modeRow := -1, -1
	for i, l := range strings.Split(hs.Screen(), "\n") {
		if strings.Contains(l, "SL 100 cols") {
			slRow = i
		}
		if strings.Contains(l, "edits auto-approved") {
			modeRow = i
		}
	}
	if slRow < 0 || slRow+1 != modeRow {
		t.Errorf("the status line sits right above the mode line (rows %d, %d):\n%s", slRow, modeRow, hs.Screen())
	}
	// Effort from the engine: the hint goes on the status line's row.
	hs.SendMsg(mainEv(&proto.SystemInit{SessionID: "s1", Effort: "medium"}))
	hs.WaitFor(func(s string) bool {
		for _, l := range strings.Split(s, "\n") {
			if strings.Contains(l, "SL 100 cols") && strings.Contains(l, "medium · /effort") {
				return true
			}
		}
		return false
	}, 3*time.Second)
	if bytes.Contains(hs.Output(), []byte("\x1b]9;4;")) {
		t.Error("no progress bar for a terminal that doesn't render it")
	}
	for _, l := range hs.Scrollback() {
		if strings.Contains(l, "SL 100 cols") || strings.Contains(l, "auto-approved") {
			t.Errorf("live chrome leaked into scrollback: %q", l)
		}
	}
	// The welcome banner was committed above the live area, once.
	banners := 0
	for _, l := range hs.All() {
		if strings.Contains(l, "/help for commands") {
			banners++
		}
	}
	if banners != 1 {
		t.Errorf("banner printed %d times:\n%s", banners, strings.Join(hs.All(), "\n"))
	}
}
