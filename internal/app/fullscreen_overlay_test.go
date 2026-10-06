package app

import (
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// overlayBox is a SlotAboveInput component drawn over the rows above the input.
type overlayBox struct{ text string }

type setOverlayMsg struct{ text string }

func (o *overlayBox) ID() string              { return "fs.overlay" }
func (o *overlayBox) Init(ext.Ctx) tea.Cmd    { return nil }
func (o *overlayBox) OverlayAboveInput() bool { return true }
func (o *overlayBox) Update(c ext.Ctx, m tea.Msg) tea.Cmd {
	if s, ok := m.(setOverlayMsg); ok {
		o.text = s.text
		c.Invalidate(o.ID())
	}
	return nil
}
func (o *overlayBox) View(ext.Ctx, ext.Area) ext.Rendered { return ext.Rendered{Text: o.text} }

func TestFullscreenOverlayAboveInput(t *testing.T) {
	var live []string
	for i := 1; i <= 40; i++ {
		live = append(live, "transcript line "+strconv.Itoa(i)+" with a long tail of text")
	}
	f := ext.Feature{ID: "test.fsov", Order: 10, Setup: func(r ext.Registrar) error {
		r.AddComponent(ext.SlotLive, &box{id: "fs.live", text: strings.Join(live, "\n")}, ext.SlotOpts{})
		r.AddComponent(ext.SlotAboveInput, &box{id: "fs.effort", text: "effort line"}, ext.SlotOpts{Weight: 1000})
		r.AddComponent(ext.SlotAboveInput, &overlayBox{}, ext.SlotOpts{Weight: 2000, MaxHeight: 5})
		r.AddComponent(ext.SlotInput, focusBox{&box{id: "fs.input", text: "> prompt", ctx: ext.ContextChat}}, ext.SlotOpts{})
		r.AddComponent(ext.SlotBelowInput, &box{id: "fs.footer", text: "footer"}, ext.SlotOpts{})
		return nil
	}}
	host := NewHost([]ext.Feature{f}, HostOptions{Core: CoreFeatures()})
	r := New(Options{Host: host, NoBackgroundQuery: true, Layout: ext.Fullscreen, NoMouse: true})
	hs := testkit.New(t, r, testkit.WithSize(80, 20))
	hs.WaitForText("> prompt", 3*time.Second)
	row := func(text string) int {
		for i, l := range hs.ScreenLines() {
			if strings.HasPrefix(l, text) {
				return i
			}
		}
		return -1
	}
	closed := hs.ScreenLines()
	promptRow := row("> prompt")
	if promptRow != 18 || row("effort line") != 17 {
		t.Fatalf("closed layout:\n%s", hs.Screen())
	}

	hs.SendMsg(setOverlayMsg{text: "  menu a\n  menu b\n  menu c"})
	hs.WaitForText("menu c", 2*time.Second)
	open := hs.ScreenLines()
	if got := row("> prompt"); got != promptRow {
		t.Fatalf("the input moved from row %d to %d:\n%s", promptRow, got, hs.Screen())
	}
	if row("  menu a") != 15 || row("  menu b") != 16 || row("  menu c") != 17 {
		t.Fatalf("overlay not directly above the input:\n%s", hs.Screen())
	}
	if strings.Contains(open[17], "effort") || strings.TrimRight(open[17], " ") != "  menu c" {
		t.Fatalf("overlay must cover the row underneath completely: %q", open[17])
	}
	// The transcript above the overlay did not move.
	for i := 0; i < 15; i++ {
		if open[i] != closed[i] {
			t.Fatalf("row %d changed under the overlay:\n%q\n%q", i, closed[i], open[i])
		}
	}

	hs.SendMsg(setOverlayMsg{text: ""})
	hs.WaitForText("effort line", 2*time.Second)
	if got := strings.Join(hs.ScreenLines(), "\n"); got != strings.Join(closed, "\n") {
		t.Fatalf("closing the overlay must restore the frame:\n%s", hs.Screen())
	}
}
