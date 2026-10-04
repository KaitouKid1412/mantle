package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

type openMsg struct{ arg string }
type closeMsg struct{}

// TestFrameNeverEmpty: with nothing but a dialog mounted, closing the dialog leaves an
// empty frame. Bubble Tea treats empty content as a zero-height frame and does not
// erase the old one, so the host must keep at least one row.
func TestFrameNeverEmpty(t *testing.T) {
	f := ext.Feature{ID: "test.dialogs", Order: 10, Setup: func(r ext.Registrar) error {
		r.AddDialog("dialog.test", func(c ext.Ctx, args any) (ext.Dialog, error) {
			return &testDialog{focusBox: focusBox{&box{id: "dialog.test#1", text: "TRUST THIS FOLDER?\nline two\nline three", ctx: ext.ContextConfirmation}}}, nil
		})
		ext.Subscribe(r, "test.open", func(c ext.Ctx, m openMsg) tea.Cmd { return c.OpenDialog("dialog.test", m.arg) })
		ext.Subscribe(r, "test.close", func(c ext.Ctx, m closeMsg) tea.Cmd { return c.CloseDialog("dialog.test") })
		return nil
	}}
	host := NewHost([]ext.Feature{f}, HostOptions{Core: CoreFeatures()})
	hs := testkit.New(t, New(Options{Host: host, NoBackgroundQuery: true}), testkit.WithSize(60, 12))
	hs.SendMsg(openMsg{})
	hs.WaitForText("TRUST THIS FOLDER?", 3*time.Second)
	hs.SendMsg(closeMsg{})
	gone := func(s string) bool {
		return !strings.Contains(s, "TRUST") && !strings.Contains(s, "line two") && !strings.Contains(s, "line three")
	}
	hs.WaitFor(gone, 3*time.Second)
	// After the held height is released (a later Update schedules it), nothing stale
	// comes back.
	hs.SendMsg(struct{}{})
	time.Sleep(100 * time.Millisecond)
	hs.SendMsg(struct{}{})
	hs.Settle(100*time.Millisecond, 2*time.Second)
	if s := hs.Screen(); !gone(s) {
		t.Fatalf("stale dialog rows after shrink:\n%s", s)
	}
}
