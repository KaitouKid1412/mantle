package fullscreen

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// scopedView is a viewport over a two-turn conversation.
func scopedView(t *testing.T, h int) (*transcriptView, *exttest.Ctx) {
	t.Helper()
	v, ctx := unitView(t, "", h)
	ctx.TranscriptV = &store{items: []*ext.Item{
		textItem("p1", ext.KeyUserPrompt, "❯ first question"),
		textItem("r1", ext.KeyAssistantText, "⏺ first answer"),
		textItem("p2", ext.KeyUserPrompt, "❯ second question"),
		textItem("r2", ext.KeyAssistantText, "⏺ second answer"),
	}}
	return v, ctx
}

func viewText(v *transcriptView, ctx *exttest.Ctx, h int) string {
	return v.View(ctx, ext.Area{Width: 60, MaxHeight: h, Mode: ext.Fullscreen}).Text
}

func TestScopeShowsOnlyScopedItems(t *testing.T) {
	v, ctx := scopedView(t, 20)
	if got := viewText(v, ctx, 20); !strings.Contains(got, "first answer") || !strings.Contains(got, "second answer") {
		t.Fatalf("unscoped:\n%s", got)
	}
	node := &store{items: []*ext.Item{
		textItem("p2", ext.KeyUserPrompt, "❯ second question"),
		textItem("r2", ext.KeyAssistantText, "⏺ second answer"),
	}}
	v.Update(ctx, ext.TranscriptScopeMsg{Owner: "research", Source: node})
	got := viewText(v, ctx, 20)
	if strings.Contains(got, "first") || !strings.Contains(got, "second answer") {
		t.Fatalf("scoped to the second node:\n%s", got)
	}
	// Another node with the same item IDs but other content renders afresh.
	other := &store{items: []*ext.Item{
		textItem("p2", ext.KeyUserPrompt, "❯ other question"),
		textItem("r2", ext.KeyAssistantText, "⏺ other answer"),
	}}
	v.Update(ctx, ext.TranscriptScopeMsg{Owner: "research", Source: other})
	if got := viewText(v, ctx, 20); !strings.Contains(got, "other answer") || strings.Contains(got, "second") {
		t.Fatalf("switching nodes reused lines:\n%s", got)
	}
	v.Update(ctx, ext.TranscriptScopeMsg{Owner: "research"})
	if got := viewText(v, ctx, 20); !strings.Contains(got, "first answer") || !strings.Contains(got, "second answer") {
		t.Fatalf("a nil scope restores the whole transcript:\n%s", got)
	}
}

func TestScopeSkipsPrinted(t *testing.T) {
	v, ctx := scopedView(t, 20)
	v.Update(ctx, ext.PrintedMsg{Blocks: []string{"printed banner"}})
	if !strings.Contains(viewText(v, ctx, 20), "printed banner") {
		t.Fatal("printed blocks show unscoped")
	}
	v.Update(ctx, ext.TranscriptScopeMsg{Owner: "research", Source: &store{items: []*ext.Item{
		textItem("r2", ext.KeyAssistantText, "⏺ second answer")}}})
	if got := viewText(v, ctx, 20); strings.Contains(got, "printed banner") {
		t.Fatalf("printed blocks are not part of a scope:\n%s", got)
	}
}

func TestScopeChangeResetsView(t *testing.T) {
	v, ctx := scopedView(t, 5)
	ctx.SettingsV.ClaudeM["copyOnSelect"] = false
	viewText(v, ctx, 5)
	v.sel = Selection{Anchor: Pos{1, 0}, Head: Pos{1, 4}, Active: true}
	v.selSent = "⏺ fi"
	v.search = Search{Query: "answer"}
	v.r.toggle("r1")
	node := &store{items: []*ext.Item{textItem("a9", ext.KeyAssistantText, numbered("line", 20))}}
	msgs := exttest.Exec(v.Update(ctx, ext.TranscriptScopeMsg{Owner: "research", Source: node}))
	got := viewText(v, ctx, 5)
	if !strings.Contains(got, "line 01") || v.vp.Offset() != 0 || v.vp.Following() {
		t.Fatalf("a new scope starts at the top (offset %d):\n%s", v.vp.Offset(), got)
	}
	if !v.sel.Empty() || v.search.Query != "" || len(v.r.expanded) != 0 {
		t.Error("selection, search and expansions reset")
	}
	if !hasSelection(msgs, "") {
		t.Errorf("the cleared selection is broadcast: %v", msgs)
	}
}

func TestScopeFollowsStreamingTail(t *testing.T) {
	v, ctx := scopedView(t, 5)
	node := &store{items: []*ext.Item{{ID: "a9", Key: ext.KeyAssistantText, Data: "line 01", State: ext.Streaming, Rev: 1}}}
	v.Update(ctx, ext.TranscriptScopeMsg{Owner: "research", Source: node})
	viewText(v, ctx, 5)
	node.mu.Lock()
	node.items[0] = &ext.Item{ID: "a9", Key: ext.KeyAssistantText, Data: numbered("line", 12), State: ext.Streaming, Rev: 2}
	node.rev++
	node.mu.Unlock()
	if got := viewText(v, ctx, 5); !strings.Contains(got, "line 12") {
		t.Fatalf("the streaming node keeps its tail in view:\n%s", got)
	}
}

func hasSelection(msgs []tea.Msg, text string) bool {
	for _, m := range msgs {
		if s, ok := m.(ext.SelectionMsg); ok && s.Source == ViewportID && s.Text == text {
			return true
		}
	}
	return false
}

func TestSelectionBroadcast(t *testing.T) {
	v, ctx := unitView(t, "⏺ alpha beta gamma\n  delta epsilon", 10)
	ctx.SettingsV.ClaudeM["copyOnSelect"] = false
	v.mouse(ctx, ev(tea.MouseClickMsg{Button: tea.MouseLeft}, 0, 1))
	v.mouse(ctx, ev(tea.MouseMotionMsg{Button: tea.MouseLeft}, 15, 2))
	msgs := exttest.Exec(v.mouse(ctx, ev(tea.MouseReleaseMsg{Button: tea.MouseLeft}, 15, 2)))
	if !hasSelection(msgs, "alpha beta gamma\ndelta epsilon") {
		t.Fatalf("release broadcasts the source text: %#v", msgs)
	}
	// A plain click clears the selection.
	v.mouse(ctx, ev(tea.MouseClickMsg{Button: tea.MouseLeft}, 3, 1))
	msgs = exttest.Exec(v.mouse(ctx, ev(tea.MouseReleaseMsg{Button: tea.MouseLeft}, 3, 1)))
	if !hasSelection(msgs, "") {
		t.Fatalf("a click broadcasts the cleared selection: %#v", msgs)
	}
	ctx.ClockV.Advance(doubleClick * 2)
	v.mouse(ctx, ev(tea.MouseClickMsg{Button: tea.MouseLeft}, 3, 1))
	if msgs := exttest.Exec(v.mouse(ctx, ev(tea.MouseReleaseMsg{Button: tea.MouseLeft}, 3, 1))); len(msgs) != 0 {
		t.Errorf("an unchanged (empty) selection is not sent again: %#v", msgs)
	}
	// Keyboard extension and selection:clear.
	v.sel = Selection{Anchor: Pos{1, 2}, Head: Pos{1, 2}, Active: true}
	_, cmd := v.action(ext.ActSelectionExtendRight)(ctx)
	if msgs := exttest.Exec(cmd); !hasSelection(msgs, "a") {
		t.Errorf("extension broadcasts: %#v", msgs)
	}
	_, cmd = v.action(ext.ActSelectionClear)(ctx)
	if msgs := exttest.Exec(cmd); !hasSelection(msgs, "") {
		t.Errorf("selection:clear broadcasts: %#v", msgs)
	}
}

func TestSourceText(t *testing.T) {
	doc := []string{
		"",
		"⏺ The quick brown fox jumps over",
		"  the lazy dog.",
		"",
		"  - a list item",
		"    nested",
		"",
		"⏺ Bash(ls)",
		"  ⎿  one",
		"     two",
	}
	starts := []int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	lines := func(n int) string {
		if n < 0 || n >= len(doc) {
			return ""
		}
		return doc[n]
	}
	bs := func(n int) int {
		if n < 0 || n >= len(doc) {
			return -1
		}
		return starts[n]
	}
	const w = 35 // "the" would not fit after line 1
	cases := []struct {
		sel  Selection
		want string
	}{
		{Selection{Anchor: Pos{1, 0}, Head: Pos{2, 15}, Active: true}, "The quick brown fox jumps over the lazy dog."},
		{Selection{Anchor: Pos{2, 6}, Head: Pos{5, 10}, Active: true}, "lazy dog.\n\n- a list item\n  nested"},
		{Selection{Anchor: Pos{7, 0}, Head: Pos{9, 10}, Active: true}, "Bash(ls)\none\ntwo"},
		{Selection{Anchor: Pos{1, 6}, Head: Pos{1, 11}, Active: true}, "quick"},
	}
	for _, c := range cases {
		if got := SourceText(lines, c.sel, w, bs); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.sel, got, c.want)
		}
	}
	// A short line followed by a word that would have fit is a hard break.
	if got := SourceText(lines, Selection{Anchor: Pos{1, 0}, Head: Pos{2, 15}, Active: true}, 60, bs); got != "The quick brown fox jumps over\nthe lazy dog." {
		t.Errorf("hard break = %q", got)
	}
}

// TestVTResearchQuoteKey: an ambient Research binding for ">" wins over the focused
// viewport's own key handling (which would hand the key to the prompt).
func TestVTResearchQuoteKey(t *testing.T) {
	quoted := make(chan string, 1)
	var sel string
	research := ext.Feature{ID: "test.research", Order: 20, Setup: func(r ext.Registrar) error {
		r.AddAction(ext.Action{ID: "mantle:research.quote", Context: ext.ContextResearch,
			Run: func(c ext.Ctx) (bool, tea.Cmd) {
				if sel == "" {
					return false, nil
				}
				quoted <- sel
				return true, nil
			}})
		r.AddBinding(ext.Binding{Context: ext.ContextResearch, Keys: ">", Action: "mantle:research.quote"})
		r.OnStart("test.research.on", func(c ext.Ctx) tea.Cmd {
			c.SetContextActive(ext.ContextResearch, true)
			return nil
		})
		ext.Subscribe(r, "test.research.sel", func(c ext.Ctx, m ext.SelectionMsg) tea.Cmd {
			sel = m.Text
			return nil
		})
		return nil
	}}
	st := &store{}
	host := app.NewHost([]ext.Feature{testFeature(st), research, fullscreenFeature(t)}, app.HostOptions{Core: app.CoreFeatures()})
	root := app.New(app.Options{Host: host, NoBackgroundQuery: true, Layout: ext.Fullscreen,
		Settings: exttest.NewSettings(map[string]any{"copyOnSelect": false})})
	hs := testkit.New(t, root, testkit.WithSize(60, 20))
	hs.WaitForText("type here", 5*time.Second)
	hs.SendMsg(addMsg{items: []*ext.Item{textItem("a1", ext.KeyAssistantText, "⏺ alpha beta gamma")}})
	hs.WaitForText("alpha beta", 3*time.Second)
	row := -1
	for i, l := range hs.ScreenLines() {
		if strings.Contains(l, "alpha beta") {
			row = i
		}
	}
	mouse(hs, tea.MouseClickMsg{X: 2, Y: row, Button: tea.MouseLeft})
	mouse(hs, tea.MouseMotionMsg{X: 12, Y: row, Button: tea.MouseLeft})
	mouse(hs, tea.MouseReleaseMsg{X: 12, Y: row, Button: tea.MouseLeft})
	hs.Type(">")
	select {
	case got := <-quoted:
		if got != "alpha beta" {
			t.Errorf("quoted %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("> did not reach the Research binding")
	}
}
