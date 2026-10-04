package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func fullscreenFeature(side *box) ext.Feature {
	return ext.Feature{ID: "test.fs", Order: 10, Setup: func(r ext.Registrar) error {
		r.AddComponent(ext.SlotHeader, &box{id: "fs.header", text: "HEADER"}, ext.SlotOpts{})
		r.AddComponent(ext.SlotSidebarL, side, ext.SlotOpts{})
		r.AddComponent(ext.SlotLive, &box{id: "fs.live", text: "transcript line 1\ntranscript line 2"}, ext.SlotOpts{})
		r.AddComponent(ext.SlotInput, focusBox{&box{id: "fs.input", text: "> prompt", ctx: ext.ContextChat}}, ext.SlotOpts{})
		r.AddComponent(ext.SlotBelowInput, &box{id: "fs.footer", text: "footer"}, ext.SlotOpts{})
		r.AddDialog("dialog.center", func(c ext.Ctx, args any) (ext.Dialog, error) {
			return &centered{testDialog{focusBox: focusBox{&box{id: "dialog.center#1", text: "CENTERED DIALOG", ctx: ext.ContextConfirmation}}}}, nil
		})
		ext.Subscribe(r, "fs.open", func(c ext.Ctx, m openMsg) tea.Cmd { return c.OpenDialog("dialog.center", nil) })
		return nil
	}}
}

type centered struct{ testDialog }

func (c *centered) Placement() ext.Placement { return ext.PlaceCentered }

func TestFullscreenLayout(t *testing.T) {
	side := &box{id: "fs.side", text: "SIDEBAR\nitem a\nitem b"}
	host := NewHost([]ext.Feature{fullscreenFeature(side)}, HostOptions{Core: CoreFeatures()})
	r := New(Options{Host: host, NoBackgroundQuery: true, Layout: ext.Fullscreen})
	hs := testkit.New(t, r, testkit.WithSize(100, 20))
	hs.WaitForText("> prompt", 3*time.Second)
	if !hs.AltScreen() {
		t.Fatal("fullscreen must use the alternate screen")
	}
	lines := hs.ScreenLines()
	if !strings.HasPrefix(lines[0], "HEADER") {
		t.Fatalf("header not on the first row:\n%s", hs.Screen())
	}
	if !strings.HasPrefix(lines[1], "SIDEBAR") {
		t.Fatalf("left sidebar not below the header:\n%s", hs.Screen())
	}
	sw := SidebarWidth(100)
	row := -1
	for i, l := range lines {
		if strings.Contains(l, "transcript line 2") {
			row = i
			if idx := strings.Index(l, "transcript"); idx != sw+1 {
				t.Fatalf("live area at column %d, want %d:\n%s", idx, sw+1, hs.Screen())
			}
		}
	}
	if row < 0 || !strings.HasPrefix(lines[row+1], "> prompt") || !strings.HasPrefix(lines[row+2], "footer") || row+2 != 19 {
		t.Fatalf("bottom stack wrong (row %d):\n%s", row, hs.Screen())
	}

	// A mouse click on the sidebar reaches the sidebar in its own coordinates.
	hs.Type("\x1b[<0;3;3M\x1b[<0;3;3m") // SGR press+release at column 3, row 3 (1-based)
	hs.WaitFor(func(string) bool {
		for _, m := range side.got {
			if me, ok := m.(MouseEvent); ok && me.X == 2 && me.Y == 1 {
				return true
			}
		}
		return false
	}, 2*time.Second)

	// A centred dialog overlays the frame without replacing it.
	hs.SendMsg(openMsg{})
	hs.WaitForText("CENTERED DIALOG", 2*time.Second)
	s := hs.Screen()
	if !strings.Contains(s, "HEADER") || !strings.Contains(s, "footer") {
		t.Fatalf("overlay hid the frame:\n%s", s)
	}
}

func TestFullscreenDropsPrints(t *testing.T) {
	host := NewHost([]ext.Feature{fullscreenFeature(&box{id: "fs.side"})}, HostOptions{Core: CoreFeatures()})
	r := New(Options{Host: host, NoBackgroundQuery: true, Layout: ext.Fullscreen})
	r.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	if cmd := r.Ctx().Print("x"); cmd != nil {
		t.Fatal("Print in fullscreen must be a no-op")
	}
	if r.Ctx().Layout() != ext.Fullscreen {
		t.Fatal("Layout() should report Fullscreen")
	}
}
