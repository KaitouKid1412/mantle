package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func TestLayoutSwitch(t *testing.T) {
	var changed []ext.LayoutMode
	watch := ext.Feature{ID: "test.layoutwatch", Order: 11, Setup: func(r ext.Registrar) error {
		ext.Subscribe(r, "test.layoutwatch", func(c ext.Ctx, m ext.LayoutChangedMsg) tea.Cmd {
			changed = append(changed, m.Mode)
			return nil
		})
		return nil
	}}
	host := NewHost([]ext.Feature{fullscreenFeature(&box{id: "fs.side", text: "SIDE"}), watch}, HostOptions{Core: CoreFeatures()})
	r := New(Options{Host: host, NoBackgroundQuery: true, Clock: &manualClock{}})
	r.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if r.View().AltScreen {
		t.Fatal("starts inline")
	}
	drive(t, r, ext.LayoutRequestMsg{Mode: ext.Fullscreen})
	v := r.View()
	if !v.AltScreen || r.Ctx().Layout() != ext.Fullscreen || v.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("not fullscreen after request (alt=%v mouse=%v)", v.AltScreen, v.MouseMode)
	}
	// Back to inline: LayoutChangedMsg, and a clear so the transcript is reprinted.
	out := drive(t, r, ext.LayoutRequestMsg{Mode: ext.Inline})
	if r.View().AltScreen || r.Ctx().Layout() != ext.Inline {
		t.Fatal("still fullscreen")
	}
	cleared := false
	for _, m := range out {
		if rm, ok := m.(tea.RawMsg); ok && strings.Contains(rm.Msg.(string), "\x1b[3J") {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("leaving fullscreen should clear for a reprint; got %v", out)
	}
	if len(changed) != 2 || changed[0] != ext.Fullscreen || changed[1] != ext.Inline {
		t.Fatalf("LayoutChangedMsg = %v", changed)
	}
}

func TestFullscreenRefusedAndMouseOff(t *testing.T) {
	host := NewHost([]ext.Feature{fullscreenFeature(&box{id: "fs.side", text: "SIDE"})}, HostOptions{Core: CoreFeatures()})
	r := New(Options{Host: host, NoBackgroundQuery: true, Clock: &manualClock{}, NoAltScreen: true})
	r.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	drive(t, r, ext.LayoutRequestMsg{Mode: ext.Fullscreen})
	if r.Ctx().Layout() != ext.Inline || len(r.Notices()) == 0 {
		t.Fatal("fullscreen must be refused when the alternate screen is disabled")
	}
	r2 := New(Options{Host: host, NoBackgroundQuery: true, Layout: ext.Fullscreen, NoMouse: true})
	r2.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if v := r2.View(); v.MouseMode != tea.MouseModeNone || v.OnMouse != nil {
		t.Fatal("NoMouse must keep the mouse off")
	}
}

func TestFullscreenInlineDialogAtBottom(t *testing.T) {
	in := &box{id: "test.input", text: "> prompt", ctx: ext.ContextChat}
	var got []string
	host := NewHost([]ext.Feature{inputFeature(in, &got)}, HostOptions{Core: CoreFeatures()})
	r := New(Options{Host: host, NoBackgroundQuery: true, Layout: ext.Fullscreen, Clock: &manualClock{}})
	r.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	drive(t, r, cmdMsgs(r.Ctx().OpenDialog("dialog.test", "rm -rf build"))...)
	lines := strings.Split(r.View().Content, "\n")
	last := ""
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			last = l
		}
	}
	if !strings.Contains(last, "Allow? rm -rf build") {
		t.Fatalf("inline dialog should sit at the bottom in place of the input:\n%s", r.View().Content)
	}
	if strings.Contains(r.View().Content, "> prompt") {
		t.Fatal("the input should be replaced while the inline dialog is open")
	}
}

func TestEmptySidebarTakesNoColumns(t *testing.T) {
	side := &box{id: "fs.side", text: ""} // a closed pane
	host := NewHost([]ext.Feature{fullscreenFeature(side)}, HostOptions{Core: CoreFeatures()})
	r := New(Options{Host: host, NoBackgroundQuery: true, Layout: ext.Fullscreen})
	r.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	for _, l := range strings.Split(r.View().Content, "\n") {
		if i := strings.Index(l, "transcript"); i >= 0 && i != 0 {
			t.Fatalf("empty sidebar shifted the transcript to column %d", i)
		}
	}
	side.text = "OPEN PANE"
	r.invalidate("fs.side")
	shifted := false
	for _, l := range strings.Split(r.View().Content, "\n") {
		if strings.Index(l, "transcript") == r.sidebarWidth(ext.SlotSidebarL)+1 {
			shifted = true
		}
	}
	if !shifted {
		t.Fatalf("open sidebar should take its width:\n%s", r.View().Content)
	}
}

func TestSidebarResize(t *testing.T) {
	side := &box{id: "fs.side", text: "SIDE"}
	host := NewHost([]ext.Feature{fullscreenFeature(side)}, HostOptions{Core: CoreFeatures()})
	r := New(Options{Host: host, NoBackgroundQuery: true, Layout: ext.Fullscreen})
	r.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	base := r.sidebarWidth(ext.SlotSidebarL)
	drive(t, r, ext.SidebarResizeMsg{Slot: ext.SlotSidebarL, Delta: 6})
	if got := r.sidebarWidth(ext.SlotSidebarL); got != base+6 {
		t.Fatalf("width %d, want %d", got, base+6)
	}
	drive(t, r, ext.SidebarResizeMsg{Slot: ext.SlotSidebarL, Delta: 500})
	if got := r.sidebarWidth(ext.SlotSidebarL); got != 50 {
		t.Fatalf("width clamps to half the terminal: %d", got)
	}
	drive(t, r, ext.SidebarResizeMsg{Slot: ext.SlotSidebarL, Delta: -500})
	if got := r.sidebarWidth(ext.SlotSidebarL); got != 12 {
		t.Fatalf("width clamps to 12: %d", got)
	}
	found := false
	for _, l := range strings.Split(r.View().Content, "\n") {
		if i := strings.Index(l, "transcript"); i >= 0 {
			found = true
			if i != 13 {
				t.Fatalf("live area should start after a 12-wide sidebar: col %d\n%s", i, r.View().Content)
			}
		}
	}
	if !found {
		t.Fatal("live area not rendered")
	}
}
