package research

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

var widths = []int{60, 100, 160}

func TestBarCaps(t *testing.T) {
	for _, tc := range []struct {
		h, rows, more int
	}{
		{h: 10, rows: 3, more: 6}, // max(3, 10/5): root, "… 6 more", q8
		{h: 30, rows: 6, more: 3},
		{h: 60, rows: 8, more: 0}, // cap 12 > 8 ancestors
	} {
		r := newRig(t, deepSession, 100, tc.h)
		r.enter("q9")
		rows := r.f.rowsFor(r.ctx, AncestorsID)
		if len(rows) != tc.rows {
			t.Errorf("h=%d: %d ancestor rows, want %d", tc.h, len(rows), tc.rows)
		}
		more := 0
		for _, row := range rows {
			more += row.more
		}
		if more != tc.more {
			t.Errorf("h=%d: more=%d, want %d", tc.h, more, tc.more)
		}
		if rows[0].node == nil || rows[0].node.ID != "q1" || rows[len(rows)-1].node.ID != "q8" {
			t.Errorf("h=%d: rows must start at the root and end at the parent", tc.h)
		}
	}
	// Children over the cap keep the way down and the way to the tip visible.
	r := newRig(t, branchySession, 100, 10)
	r.enter("c5")
	r.exec(r.f.goParent(r.ctx))
	var ids []string
	for _, row := range r.f.rowsFor(r.ctx, ChildrenID) {
		if row.node != nil {
			ids = append(ids, row.node.ID)
		}
	}
	if !slices.Equal(ids, []string{"c5", "c7"}) {
		t.Errorf("children shown at cap 3 = %v, want [c5 c7] (last visited, tip)", ids)
	}
	for _, w := range widths {
		t.Run(fmt.Sprintf("w%d", w), func(t *testing.T) {
			r := newRig(t, deepSession, w, 10)
			r.enter("q9")
			testkit.RequireGolden(t, r.styled())
		})
	}
}

func TestMouseNavigation(t *testing.T) {
	for _, w := range widths {
		t.Run(fmt.Sprintf("w%d", w), func(t *testing.T) {
			r := newRig(t, deepSession, w, 30)
			r.enter("q9")
			// Ancestors: q1, … 3 more, q5, q6, q7, q8.
			r.exec(r.click(AncestorsID, 3))
			if r.f.viewing != "q6" {
				t.Fatalf("click on row 3 viewing %q, want q6", r.f.viewing)
			}
			r.exec(r.click(AncestorsID, 0))
			if r.f.viewing != "q1" {
				t.Fatalf("click on row 0 viewing %q, want q1", r.f.viewing)
			}
			// Children of q1: q2 (way back down, highlighted), q2b.
			r.exec(r.click(ChildrenID, 1))
			if r.f.viewing != "q2b" {
				t.Fatalf("click on child row 1 viewing %q, want q2b", r.f.viewing)
			}
			// The hint row and rows past the end do nothing.
			r.exec(r.click(ChildrenID, 0))
			r.exec(r.click(ChildrenID, 9))
			if r.f.viewing != "q2b" {
				t.Fatalf("hint click moved to %q", r.f.viewing)
			}
			r.exec(r.click(AncestorsID, 0))
			testkit.RequireGolden(t, r.styled())
		})
	}
	// "… N more" opens the tree picker; a right click does nothing.
	r := newRig(t, deepSession, 100, 30)
	r.enter("q9")
	r.exec(r.bar(AncestorsID).Update(r.ctx, ext.MouseEvent{Msg: tea.MouseClickMsg{Y: 0, Button: tea.MouseRight}, Y: 0}))
	if r.f.viewing != "q9" {
		t.Fatalf("right click navigated to %q", r.f.viewing)
	}
	r.exec(r.click(AncestorsID, 1))
	if !slices.Equal(r.ctx.Opened, []string{TreeID}) {
		t.Fatalf("opened %v, want the tree picker", r.ctx.Opened)
	}
	// Hover highlights the row under the pointer.
	r.bar(AncestorsID).Update(r.ctx, ext.MouseEvent{Msg: tea.MouseMotionMsg{Y: 2}, Y: 2})
	if r.f.hover != (hoverRow{bar: AncestorsID, row: 2}) {
		t.Fatalf("hover = %+v", r.f.hover)
	}
}

func TestKeyboardNavigation(t *testing.T) {
	r := newRig(t, deepSession, 100, 30)
	want := map[ext.ActionID]string{
		ActParent: "alt+up", ActChild: "alt+down", ActPrevSibling: "alt+left", ActNextSibling: "alt+right",
		ActRoot: "alt+home", ActTip: "alt+end", ActTree: "ctrl+t", ActQuote: ">",
	}
	for _, b := range r.reg.Bindings {
		if want[b.Action] == b.Keys && b.Context == ext.ContextResearch {
			delete(want, b.Action)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing Research bindings: %v", want)
	}
	// Inactive: every move declines, so the keys keep their usual meaning.
	if ok, _ := r.action(ActParent); ok {
		t.Fatal("alt+up handled outside research mode")
	}

	r.enter("q9")
	if !r.ctx.ActiveCtxs[ext.ContextResearch] {
		t.Fatal("Research context not active")
	}
	steps := []struct {
		act  ext.ActionID
		want string
	}{
		{ActParent, "q8"},
		{ActRoot, "q1"},
		{ActChild, "q2"}, // retraces the path we came down
		{ActNextSibling, "q2b"},
		{ActNextSibling, "q2b"}, // no next sibling: stays
		{ActPrevSibling, "q2"},
		{ActParent, "q1"},
		{ActParent, "q1"}, // root has no parent
		{ActTip, "q9"},
		{ActChild, "q9"}, // no children
	}
	for i, s := range steps {
		ok, cmd := r.action(s.act)
		if !ok {
			t.Fatalf("step %d %s declined", i, s.act)
		}
		r.exec(cmd)
		if r.f.viewing != s.want {
			t.Fatalf("step %d %s: viewing %q, want %q", i, s.act, r.f.viewing, s.want)
		}
	}
	// Down from q1 now leads back to q2b once it was visited last.
	r.exec(r.f.navigate(r.ctx, "q2b"))
	r.exec(r.f.goParent(r.ctx))
	r.exec(r.f.goChild(r.ctx))
	if r.f.viewing != "q2b" {
		t.Fatalf("child after visiting q2b = %q", r.f.viewing)
	}
	if _, cmd := r.action(ActTree); !slices.Equal(r.ctx.Opened, []string{TreeID}) {
		t.Fatalf("ctrl+t opened %v", r.ctx.Opened)
	} else {
		r.exec(cmd)
	}
	for _, w := range widths {
		t.Run(fmt.Sprintf("w%d", w), func(t *testing.T) {
			r := newRig(t, branchySession, w, 30)
			r.enter("c7")
			r.exec(r.f.goParent(r.ctx))
			r.exec(r.f.goChild(r.ctx))       // back to c7
			r.exec(r.f.goSibling(r.ctx, -1)) // c6
			r.exec(r.f.goParent(r.ctx))      // r1: c6 highlighted as the way down
			testkit.RequireGolden(t, r.styled())
		})
	}
}

func TestPicker(t *testing.T) {
	r := newRig(t, deepSession, 100, 30)
	r.enter("q9")
	d, err := r.f.openPicker(r.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Placement() != ext.PlaceCentered {
		t.Fatal("picker is not centered")
	}
	p := d.(*picker)
	if it, _ := p.sel.Current(); it.Value != "q9" {
		t.Fatalf("picker starts at %v, want the viewed node", it.Value)
	}
	for _, k := range "poller" {
		p.HandleKey(r.ctx, tea.KeyPressMsg{Code: k, Text: string(k)})
	}
	_, cmd := p.HandleAction(r.ctx, ext.ActSelectAccept)
	r.exec(cmd)
	if r.f.viewing != "q2b" {
		t.Fatalf("picker accept viewing %q, want q2b", r.f.viewing)
	}
	if !slices.Equal(r.ctx.Closed, []string{TreeID}) {
		t.Fatalf("closed %v", r.ctx.Closed)
	}
	// Indentation follows depth.
	d, _ = r.f.openPicker(r.ctx, nil)
	view := ansi.Strip(d.View(r.ctx, ext.Area{Width: 100, MaxHeight: 20}).Text)
	if !strings.Contains(view, "    What is the run queue?") {
		t.Fatalf("picker view not indented:\n%s", view)
	}
}

func TestQuote(t *testing.T) {
	r := newRig(t, deepSession, 100, 30)
	r.enter("q9")
	if ok, _ := r.action(ActQuote); ok {
		t.Fatal("quote without a selection must decline (so > types)")
	}
	r.deliver(ext.SelectionMsg{Source: "fullscreen.viewport", Text: "line one\nline two"})
	ok, cmd := r.action(ActQuote)
	msgs := exttest.Exec(cmd)
	if !ok || len(msgs) != 1 || msgs[0] != (ext.EditorQuoteMsg{Text: "> line one\n> line two\n\n"}) {
		t.Fatalf("quote = %v %#v", ok, msgs)
	}
	r.deliver(ext.SelectionMsg{Source: "fullscreen.viewport"})
	if ok, _ := r.action(ActQuote); ok {
		t.Fatal("quote after the selection cleared")
	}
	// Toggle asks for the mode switch; the mode wiring answers.
	_, cmd = r.action(ActToggle)
	if m := exttest.Exec(cmd); len(m) != 1 || m[0] != (ext.UIModeRequestMsg{Mode: ""}) {
		t.Fatalf("toggle while active = %#v", m)
	}
}

// store is a fake live transcript store.
type store struct{ items []*ext.Item }

func (s *store) Items() []*ext.Item { return s.items }
func (s *store) Get(id string) *ext.Item {
	for _, it := range s.items {
		if it.ID == id {
			return it
		}
	}
	return nil
}
func (s *store) Committed() int { return 0 }

// turnItems are the items of one node: the prompt, a tool call with a nested child,
// and the answer.
func turnItems(id string) []*ext.Item {
	return []*ext.Item{
		{ID: "user:" + id, Key: ext.KeyUserPrompt, Data: "Question " + id, State: ext.Done},
		{ID: "tool-" + id, Key: ext.ToolKey("Read"), Data: "Read " + id + ".go", State: ext.Done},
		{ID: "sub-" + id, ParentID: "tool-" + id, Key: ext.KeyAssistantText, Data: "nested", State: ext.Done},
		{ID: "text-" + id, Key: ext.KeyAssistantText, Data: "Answer to " + id, State: ext.Done},
	}
}

func branchItems(ids ...string) []*ext.Item {
	var out []*ext.Item
	for _, id := range ids {
		out = append(out, turnItems(id)...)
	}
	return out
}

// renderScope draws a scope's items with a plain renderer at width w.
func renderScope(ctx *exttest.Ctx, s ext.Transcript, w int) string {
	var lines []string
	for _, it := range s.Items() {
		indent := ""
		if it.ParentID != "" {
			indent = "  ⎿ "
		}
		lines = append(lines, ansi.Truncate(fmt.Sprintf("%s%-18s %v", indent, it.Key, it.Data), w, "…"))
	}
	return ctx.Theme().Paint("text", strings.Join(lines, "\n"))
}

func TestScope(t *testing.T) {
	for _, w := range widths {
		t.Run(fmt.Sprintf("w%d", w), func(t *testing.T) {
			r := newRig(t, deepSession, w, 30)
			// The live store holds the engine's branch: q1 … q9 (not q2b).
			live := &store{items: branchItems("q1", "q2", "q3", "q4", "q5", "q6", "q7", "q8", "q9")}
			r.ctx.TranscriptV = live
			var loads []string
			r.f.load = func(leaf string) ([]*ext.Item, error) {
				loads = append(loads, leaf)
				return branchItems("q1", "q2b"), nil
			}

			// On branch: sliced from the live store, nested items included.
			on := r.enter("q2")
			if on.Owner != FeatureID {
				t.Fatalf("owner %q", on.Owner)
			}
			if _, ok := on.Source.(liveScope); !ok {
				t.Fatalf("q2 scope is %T, want the live store", on.Source)
			}
			onText := renderScope(r.ctx, on.Source, w)

			// Streaming shows: the live scope reads the store on every call.
			r.exec(r.f.goTip(r.ctx))
			tip := r.scope(r.f.rescope(r.ctx))
			live.items = append(live.items, &ext.Item{ID: "stream", Key: ext.KeyAssistantText, Data: "still typing", State: ext.Streaming})
			if got := tip.Source.Items(); got[len(got)-1].ID != "stream" {
				t.Fatal("live scope does not follow the store")
			}

			// Off branch: loaded from the JSONL at the node's leaf, once, then cached.
			off := r.scope(r.f.navigate(r.ctx, "q2b"))
			if _, ok := off.Source.(*staticScope); !ok {
				t.Fatalf("q2b scope is %T, want a loaded scope", off.Source)
			}
			offText := renderScope(r.ctx, off.Source, w)
			r.scope(r.f.navigate(r.ctx, "q1"))
			r.scope(r.f.navigate(r.ctx, "q2b"))
			if !slices.Equal(loads, []string{"q2b-a"}) {
				t.Fatalf("loads = %v, want one load at q2b's leaf", loads)
			}
			testkit.RequireGolden(t, "on branch (q2):\n"+onText+"\n\noff branch (q2b):\n"+offText)
		})
	}
}

func TestScopeLifecycle(t *testing.T) {
	r := newRig(t, deepSession, 100, 30)
	r.ctx.TranscriptV = &store{items: branchItems("q1", "q2", "q3", "q4", "q5", "q6", "q7", "q8", "q9")}

	// Off branch while loading shows the node empty, never another node's items.
	r.f.load = func(string) ([]*ext.Item, error) { return nil, errors.New("gone") }
	r.enter("q9")
	msgs := exttest.Exec(r.f.navigate(r.ctx, "q2b"))
	var loaded *scopeLoadedMsg
	for _, m := range msgs {
		switch m := m.(type) {
		case ext.TranscriptScopeMsg:
			if n := len(m.Source.Items()); n != 0 {
				t.Fatalf("loading scope shows %d items", n)
			}
		case scopeLoadedMsg:
			loaded = &m
		}
	}
	if loaded == nil {
		t.Fatal("no load started")
	}
	r.exec(r.f.scopeLoaded(r.ctx, *loaded))
	if len(r.ctx.Notices) != 1 {
		t.Fatalf("failed load notices = %v", r.ctx.Notices)
	}
	// A failed load is retried on the next visit.
	if _, cmd := r.f.scopes.get(scopeKey{"q2b", "q2b-a"}, r.f.load); cmd == nil {
		t.Fatal("failed load was cached")
	}

	// A store reset or a screen clear re-sends the scope; other history does not.
	for _, tc := range []struct {
		msg  tea.Msg
		want bool
	}{
		{ext.TranscriptHistoryMsg{Reset: true}, true},
		{ext.TranscriptHistoryMsg{}, false},
		{ext.ScreenClearedMsg{}, true},
	} {
		got := false
		for _, m := range r.deliver(tc.msg) {
			_, got = m.(ext.TranscriptScopeMsg)
		}
		if got != tc.want {
			t.Errorf("%T%+v re-scoped = %v, want %v", tc.msg, tc.msg, got, tc.want)
		}
	}

	// A rebuilt tree keeps the viewed node by ID and falls back to the tip without it.
	r.exec(r.f.navigate(r.ctx, "q3"))
	r.exec(r.f.setTree(r.ctx, fakeTree(deepSession)))
	if r.f.viewing != "q3" {
		t.Fatalf("after rebuild viewing %q", r.f.viewing)
	}
	r.exec(r.f.setTree(r.ctx, fakeTree(deepSession[:2])))
	if n := r.f.node(); n == nil || n.ID != "q2" {
		t.Fatalf("after losing q3 the view is %v, want the tip q2", n)
	}

	// Leaving clears the scope and the context.
	msgs = exttest.Exec(r.f.deactivate(r.ctx))
	if len(msgs) != 1 || msgs[0].(ext.TranscriptScopeMsg).Source != nil {
		t.Fatalf("leave = %#v", msgs)
	}
	if r.ctx.ActiveCtxs[ext.ContextResearch] {
		t.Fatal("Research context still active")
	}
	if got := r.bar(AncestorsID).View(r.ctx, r.ctx.Area()).Text; got != "" {
		t.Fatalf("bar drawn while inactive: %q", got)
	}
	if r.deliver(ext.ScreenClearedMsg{}) != nil {
		t.Fatal("re-scoped while inactive")
	}
}
