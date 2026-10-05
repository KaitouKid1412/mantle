package fullscreen

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

func TestPrintedMerge(t *testing.T) {
	st := &store{items: []*ext.Item{textItem("u1", ext.KeyUserPrompt, "q"), textItem("a1", ext.KeyAssistantText, "a")}}
	var p printed
	p.add(&store{}, []string{"BANNER"}) // before any item: at the top
	p.add(st, []string{"closed after a1"})
	blocks := []Block{{ID: "u1", Lines: []string{"", "q"}}, {ID: "a1", Lines: []string{"", "a"}}}
	out, coll, items := p.merge(st, blocks, []bool{false, false}, st.Items(), 20)
	var ids []string
	for _, b := range out {
		ids = append(ids, b.ID)
	}
	if !slices.Equal(ids, []string{"printed-1", "u1", "a1", "printed-2"}) || len(coll) != 4 || items[0] != nil || items[3] != nil {
		t.Fatalf("merged = %v", ids)
	}
	if again, _, _ := p.merge(st, blocks, []bool{false, false}, st.Items(), 20); &again[0] != &out[0] {
		t.Error("an unchanged document reuses the merge")
	}
	// Long printed lines wrap to the width.
	if got := wrapPrinted([]string{"one two three four five six"}, 10); len(got) < 3 {
		t.Errorf("wrapped = %q", got)
	}
	// A print onto an emptied transcript (after /clear) starts over.
	p.add(&store{}, []string{"NEW BANNER"})
	if len(p.blocks) != 1 || p.blocks[0].text[0] != "NEW BANNER" {
		t.Errorf("blocks after a cleared conversation = %+v", p.blocks)
	}
}

type printMsg struct{ text string }
type clearStoreMsg struct{}

// printFeature prints the way chrome's banner and the panels' closing lines do.
func printFeature(st *store) ext.Feature {
	return ext.Feature{ID: "test.print", Order: 20, Setup: func(r ext.Registrar) error {
		r.OnStart("test.banner", func(c ext.Ctx) tea.Cmd { return c.Print("BANNER mantle-test\n   /work/app") })
		ext.Subscribe(r, "test.print", func(c ext.Ctx, m printMsg) tea.Cmd { return c.Print(m.text) })
		ext.Subscribe(r, "test.clear", func(c ext.Ctx, m clearStoreMsg) tea.Cmd {
			st.mu.Lock()
			st.items = nil
			st.rev++
			st.mu.Unlock()
			return ext.Msg(ext.EngineEventMsg{EngineID: ext.MainEngine})
		})
		return nil
	}}
}

func TestVTFullscreenShowsPrints(t *testing.T) {
	st := &store{}
	host := app.NewHost([]ext.Feature{testFeature(st), fullscreenFeature(t), printFeature(st)}, app.HostOptions{Core: app.CoreFeatures()})
	root := app.New(app.Options{Host: host, NoBackgroundQuery: true, Layout: ext.Fullscreen, Settings: exttest.NewSettings(nil)})
	hs := testkit.New(t, root, testkit.WithSize(80, 24))
	hs.WaitForText("BANNER mantle-test", 5*time.Second) // the startup banner, in fullscreen

	hs.SendMsg(addMsg{items: []*ext.Item{
		textItem("u1", ext.KeyUserPrompt, "❯ /help"),
		textItem("a1", ext.KeyAssistantText, "⏺ help text"),
	}})
	hs.WaitForText("help text", 3*time.Second)
	hs.SendMsg(printMsg{"  ⎿  Help closed"}) // a panel's closing line
	hs.WaitForText("Help closed", 3*time.Second)
	row := func(s string) int {
		for i, l := range hs.ScreenLines() {
			if strings.Contains(l, s) {
				return i
			}
		}
		return -1
	}
	if b, q, a, c := row("BANNER"), row("❯ /help"), row("help text"), row("Help closed"); !(b < q && q < a && a < c) {
		t.Errorf("order: banner %d, prompt %d, answer %d, closing line %d\n%s", b, q, a, c, hs.Screen())
	}

	// Printed lines scroll with the transcript.
	hs.SendMsg(addMsg{items: []*ext.Item{textItem("a2", ext.KeyAssistantText, numbered("more", 40))}})
	hs.WaitForText("more 40", 3*time.Second)
	if strings.Contains(hs.Screen(), "BANNER") {
		t.Error("the banner scrolled off with the transcript")
	}
	hs.Send("ctrl+home")
	hs.WaitForText("BANNER mantle-test", 3*time.Second)

	// A new conversation (cleared transcript) and its banner: one banner, no stale lines.
	hs.SendMsg(clearStoreMsg{})
	hs.WaitFor(func(s string) bool { return !strings.Contains(s, "help text") }, 3*time.Second)
	hs.SendMsg(printMsg{"BANNER again"})
	hs.WaitForText("BANNER again", 3*time.Second)
	if strings.Contains(hs.Screen(), "Help closed") || strings.Contains(hs.Screen(), "BANNER mantle-test") {
		t.Errorf("prints from the cleared conversation stayed:\n%s", hs.Screen())
	}

	// A Reprint in fullscreen reports ScreenClearedMsg before the prints that follow it
	// (contracts host fix 1baa21a): earlier prints go, later ones stay.
	hs.SendMsg(ext.ScreenClearedMsg{})
	hs.WaitFor(func(s string) bool { return !strings.Contains(s, "BANNER again") }, 3*time.Second)
	hs.SendMsg(printMsg{"BANNER after the clear"})
	hs.WaitForText("BANNER after the clear", 3*time.Second)
}
