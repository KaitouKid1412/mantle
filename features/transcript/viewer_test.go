package transcript

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/render"
)

func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func (g *rig) viewer() *viewer {
	g.t.Helper()
	d, err := g.f.newViewer(g.c, nil)
	if err != nil {
		g.t.Fatal(err)
	}
	return d.(*viewer)
}

func viewText(g *rig, v *viewer) []string {
	out := v.View(g.c, ext.Area{Width: g.c.W, MaxHeight: g.c.H, Mode: ext.AltView}).Text
	return strings.Split(render.Strip(out), "\n")
}

func TestViewerNavigation(t *testing.T) {
	g := newRig(t, 80)
	g.c.H = 20
	g.send(sampleSession)
	v := g.viewer()
	if v.Placement() != ext.PlaceAltScreen || v.KeyContext() != ext.ContextTranscript {
		t.Fatal("viewer must be an alt-screen dialog in the Transcript context")
	}
	screen := viewText(g, v)
	if len(screen) != 20 || !strings.HasPrefix(screen[0], "Transcript") {
		t.Fatalf("screen = %q", screen)
	}
	total := len(v.lines)
	if !strings.Contains(screen[19], "of "+itoa(total)) || v.top != total-18 {
		t.Fatalf("viewer should open at the bottom: top %d of %d, status %q", v.top, total, screen[19])
	}
	press := func(keys ...string) {
		for _, k := range keys {
			if ok, _ := v.HandleKey(g.c, keyPress(k)); !ok {
				t.Fatalf("key %q not handled", k)
			}
		}
		viewText(g, v)
	}
	press("g")
	if v.top != 0 {
		t.Fatalf("g: top %d", v.top)
	}
	press("j", "j", "k")
	if v.top != 1 {
		t.Fatalf("j j k: top %d", v.top)
	}
	press("ctrl+d")
	if v.top != 1+9 {
		t.Fatalf("ctrl+d: top %d", v.top)
	}
	press("space")
	if v.top != 10+18 {
		t.Fatalf("space: top %d", v.top)
	}
	press("b", "ctrl+u")
	if v.top != 10-9 {
		t.Fatalf("b ctrl+u: top %d", v.top)
	}
	press("G")
	if v.top != total-18 {
		t.Fatalf("G: top %d", v.top)
	}
}

func TestViewerMetaAndSearch(t *testing.T) {
	g := newRig(t, 100)
	g.c.H = 24
	g.send(sampleSession)
	v := g.viewer()
	viewText(g, v)
	all := strings.Join(v.plain, "\n")
	if !strings.Contains(all, "You · ") || !strings.Contains(all, "Claude · ") {
		t.Fatalf("no per-message headers:\n%s", all)
	}
	// Full detail: Read output that the inline view collapses.
	if !strings.Contains(all, "func (w *Worker) handle(job *Job) {") {
		t.Fatalf("viewer collapsed tool output:\n%s", all)
	}
	for _, k := range []string{"g", "/", "r", "e", "t", "r", "y", "x", "backspace", "enter"} {
		v.HandleKey(g.c, keyPress(k))
	}
	screen := viewText(g, v)
	if len(v.matches) < 3 || v.query != "retry" {
		t.Fatalf("query %q matches %v", v.query, v.matches)
	}
	if !strings.Contains(screen[len(screen)-1], "match 1/") || !strings.Contains(strings.ToLower(screen[1]), "retry") {
		t.Fatalf("not at the first match:\n%s", strings.Join(screen, "\n"))
	}
	first := v.top
	v.HandleKey(g.c, keyPress("n"))
	if v.top <= first || v.cur != 1 {
		t.Fatalf("n: top %d cur %d", v.top, v.cur)
	}
	v.HandleKey(g.c, keyPress("N"))
	if v.cur != 0 {
		t.Fatalf("N: cur %d", v.cur)
	}
	// Esc while typing a query cancels the query, not the viewer.
	v.HandleKey(g.c, keyPress("/"))
	if handled, cmd := v.HandleAction(g.c, ext.ActTranscriptExit); !handled || cmd != nil || v.searching {
		t.Fatal("exit while searching should only end the search")
	}
	v.HandleKey(g.c, keyPress("/"))
	for _, k := range []string{"z", "q", "z", "enter"} {
		v.HandleKey(g.c, keyPress(k))
	}
	if screen := viewText(g, v); !strings.Contains(screen[len(screen)-1], "Pattern not found: zqz") {
		t.Fatalf("status = %q", screen[len(screen)-1])
	}
}

func TestViewerShowAllAndExit(t *testing.T) {
	g := newRig(t, 100)
	g.send(sampleSession)
	v := g.viewer()
	viewText(g, v)
	before := len(v.lines)
	if handled, _ := v.HandleAction(g.c, ext.ActTranscriptToggleShowAll); !handled {
		t.Fatal("toggleShowAll not handled")
	}
	viewText(g, v)
	if len(v.lines) < before {
		t.Fatalf("expand-all shrank the transcript: %d → %d", before, len(v.lines))
	}
	if handled, cmd := v.HandleAction(g.c, ext.ActTranscriptExit); !handled || cmd == nil {
		t.Fatal("exit not handled")
	}
	if len(g.c.Closed) != 1 || g.c.Closed[0] != ViewerDialogID {
		t.Fatalf("closed = %v", g.c.Closed)
	}
	// The ctrl+o action opens the viewer.
	for _, a := range g.r.Actions {
		if a.ID == ext.ActAppToggleTranscript {
			a.Run(g.c)
		}
	}
	if len(g.c.Opened) != 1 || g.c.Opened[0] != ViewerDialogID {
		t.Fatalf("opened = %v", g.c.Opened)
	}
}

// TestViewerAltScreen opens the viewer through the real host: ctrl+o enters
// the alternate screen, q leaves it, and scrollback is untouched.
func TestViewerAltScreen(t *testing.T) {
	if testing.Short() {
		t.Skip("vt")
	}
	hs, _ := startHost(t, 80, 24)
	for _, ev := range decodeLines(t, sampleSession) {
		hs.SendMsg(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: ev})
	}
	hs.WaitFor(func(string) bool { return contains(hs.All(), "Conversation compacted") }, 10*time.Second)
	hs.Settle(80*time.Millisecond, 3*time.Second)
	before := strings.Join(hs.Scrollback(), "\n")
	hs.Send("ctrl+o")
	hs.WaitFor(func(s string) bool { return hs.AltScreen() && strings.Contains(s, "Transcript ·") }, 5*time.Second)
	hs.Send("g")
	hs.WaitForText("You · ", 5*time.Second)
	hs.Send("q")
	hs.WaitFor(func(s string) bool { return !hs.AltScreen() && strings.Contains(s, inputMarker) }, 5*time.Second)
	if after := strings.Join(hs.Scrollback(), "\n"); after != before {
		t.Fatalf("scrollback changed by the viewer")
	}
}
