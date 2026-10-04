package fullscreen

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/term/clipboard"
	"github.com/KaitouKid1412/mantle/internal/term/clipcmd"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

func TestStories(t *testing.T) {
	for _, f := range ext.Pending() {
		if f.ID != FeatureID {
			continue
		}
		r, err := exttest.Setup(f)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Stories) < 5 {
			t.Fatalf("stories = %d", len(r.Stories))
		}
		for _, s := range r.Stories {
			t.Run(strings.ReplaceAll(s.ID, "/", "_"), func(t *testing.T) {
				testkit.RunStory(t, s, 80, 120, 200)
			})
		}
		return
	}
	t.Fatal("feature not registered")
}

// ---- an in-test transcript store, attached through ext like features/transcript ----

type store struct {
	mu    sync.Mutex
	items []*ext.Item
}

func (s *store) Items() []*ext.Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.items)
}
func (s *store) Get(id string) *ext.Item {
	for _, it := range s.Items() {
		if it.ID == id {
			return it
		}
	}
	return nil
}
func (s *store) Committed() int { return 0 }

// addMsg appends items to the store on the UI goroutine.
type addMsg struct{ items []*ext.Item }

// textItem renders as one line per "\n"-separated part.
func textItem(id string, key ext.ContentKey, text string) *ext.Item {
	return &ext.Item{ID: id, Key: key, Data: text, State: ext.Done, Rev: 1}
}

func testFeature(st *store) ext.Feature {
	return ext.Feature{ID: "test.transcript", Order: 10, Setup: func(r ext.Registrar) error {
		r.AddRenderer(ext.KeyDefault, func(rc ext.RenderCtx, it *ext.Item) ext.Block {
			s, _ := it.Data.(string)
			lines := strings.Split(s, "\n")
			if rc.Expanded {
				lines = append(lines, "  (expanded details)")
			}
			return ext.Block{Lines: lines, Collapsible: strings.HasPrefix(s, "⏺ Bash")}
		})
		r.AddComponent(ext.SlotInput, &inputBox{}, ext.SlotOpts{})
		r.OnStart("test.attach", func(c ext.Ctx) tea.Cmd { return ext.Msg(ext.TranscriptAttachMsg{Transcript: st}) })
		ext.Subscribe(r, "test.add", func(c ext.Ctx, m addMsg) tea.Cmd {
			st.mu.Lock()
			st.items = append(st.items, m.items...)
			st.mu.Unlock()
			return ext.Msg(ext.EngineEventMsg{EngineID: ext.MainEngine})
		})
		return nil
	}}
}

type inputBox struct{}

func (inputBox) ID() string                                         { return EditorID }
func (inputBox) Init(ext.Ctx) tea.Cmd                               { return nil }
func (inputBox) Update(ext.Ctx, tea.Msg) tea.Cmd                    { return nil }
func (inputBox) View(ext.Ctx, ext.Area) ext.Rendered                { return ext.Rendered{Text: "❯ type here"} }
func (inputBox) KeyContext() string                                 { return ext.ContextChat }
func (inputBox) HandleKey(ext.Ctx, tea.KeyPressMsg) (bool, tea.Cmd) { return true, nil }
func (inputBox) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd)  { return true, nil }

func fullscreenFeature(t *testing.T) ext.Feature {
	for _, f := range ext.Pending() {
		if f.ID == FeatureID {
			return f
		}
	}
	t.Fatal("feature not registered")
	return ext.Feature{}
}

func startFullscreen(t *testing.T, w, h int, settings map[string]any) (*testkit.Harness, *store) {
	t.Helper()
	st := &store{}
	host := app.NewHost([]ext.Feature{testFeature(st), fullscreenFeature(t)}, app.HostOptions{Core: app.CoreFeatures()})
	root := app.New(app.Options{Host: host, NoBackgroundQuery: true, Layout: ext.Fullscreen,
		Settings: exttest.NewSettings(settings)})
	hs := testkit.New(t, root, testkit.WithSize(w, h))
	hs.WaitForText("type here", 5*time.Second)
	return hs, st
}

func numbered(prefix string, n int) string {
	var b []string
	for i := 1; i <= n; i++ {
		b = append(b, fmt.Sprintf("%s %02d", prefix, i))
	}
	return strings.Join(b, "\n")
}

func TestVTFullscreenLayoutAndScroll(t *testing.T) {
	hs, _ := startFullscreen(t, 80, 24, nil)
	if !hs.AltScreen() {
		t.Fatal("fullscreen uses the alternate screen")
	}
	hs.SendMsg(addMsg{items: []*ext.Item{
		textItem("u1", ext.KeyUserPrompt, "❯ first question"),
		textItem("a1", ext.KeyAssistantText, numbered("answer", 40)),
	}})
	hs.WaitForText("answer 40", 3*time.Second)
	screen := hs.ScreenLines()
	if !strings.Contains(screen[len(screen)-1], "type here") {
		t.Errorf("input should stay at the bottom:\n%s", hs.Screen())
	}
	if strings.Contains(hs.Screen(), "answer 01") {
		t.Error("only the tail fits while following")
	}

	hs.Send("pageup")
	hs.WaitFor(func(s string) bool { return !strings.Contains(s, "answer 40") }, 3*time.Second)
	hs.Send("ctrl+home")
	hs.WaitForText("first question", 3*time.Second)
	// The sticky header is empty at the very top (the prompt itself is visible).
	if strings.Count(hs.Screen(), "first question") != 1 {
		t.Errorf("prompt shown twice at the top:\n%s", hs.Screen())
	}

	// New output while scrolled up raises the jump-to-bottom pill.
	hs.SendMsg(addMsg{items: []*ext.Item{textItem("a2", ext.KeyAssistantText, numbered("more", 3))}})
	hs.WaitForText("new lines · ctrl+end", 3*time.Second)
	hs.Send("ctrl+end")
	hs.WaitForText("more 03", 3*time.Second)
	if strings.Contains(hs.Screen(), "new lines") {
		t.Error("the pill goes away at the bottom")
	}

	// Scroll into the answer: the sticky header names the question.
	hs.Send("pageup")
	hs.WaitFor(func(s string) bool { return strings.Contains(strings.Split(s, "\n")[0], "first question") }, 3*time.Second)
}

func TestVTFullscreenWheel(t *testing.T) {
	hs, _ := startFullscreen(t, 80, 24, map[string]any{"wheelScrollAccelerationEnabled": false})
	hs.SendMsg(addMsg{items: []*ext.Item{textItem("a1", ext.KeyAssistantText, numbered("line", 60))}})
	hs.WaitForText("line 60", 3*time.Second)
	for range 4 {
		hs.SendMsg(tea.MouseWheelMsg{X: 10, Y: 5, Button: tea.MouseWheelUp})
	}
	// 4 notches × 3 lines: "line 60" scrolls off the bottom.
	hs.WaitFor(func(s string) bool { return !strings.Contains(s, "line 60") }, 3*time.Second)
	hs.WaitFor(func(s string) bool { return strings.Contains(s, "line 48") }, 3*time.Second)
}

// mouse sends one mouse event and gives the host time to deliver it: Bubble Tea runs
// each View.OnMouse result in its own goroutine, so back-to-back events can arrive out
// of order (see docs/plans/requests/12-01-fullscreen-switch.md).
func mouse(hs *testkit.Harness, m tea.MouseMsg) {
	hs.SendMsg(m)
	time.Sleep(60 * time.Millisecond)
}

type fakeClip struct {
	mu   sync.Mutex
	text string
}

func (f *fakeClip) copier() clipboard.Copier {
	return clipboard.Copier{Env: terminal.Map{}, GOOS: "darwin",
		LookPath: func(string) (string, error) { return "/bin/pbcopy", nil },
		Run: func(_ context.Context, _ string, _ []string, stdin []byte) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.text = string(stdin)
			return nil
		}}
}

func (f *fakeClip) get() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.text
}

func TestVTFullscreenSelectAndCopy(t *testing.T) {
	clip := &fakeClip{}
	old := clipcmd.Copier
	clipcmd.Copier = clip.copier()
	t.Cleanup(func() { clipcmd.Copier = old })

	hs, _ := startFullscreen(t, 60, 20, nil)
	hs.SendMsg(addMsg{items: []*ext.Item{textItem("a1", ext.KeyAssistantText, "alpha beta gamma\ndelta epsilon")}})
	hs.WaitForText("delta epsilon", 3*time.Second)
	row := -1
	for i, l := range hs.ScreenLines() {
		if strings.Contains(l, "alpha beta") {
			row = i
		}
	}
	if row < 0 {
		t.Fatalf("no row:\n%s", hs.Screen())
	}
	// Drag from "beta" to the end of the next line: copy-on-select copies it.
	mouse(hs, tea.MouseClickMsg{X: 6, Y: row, Button: tea.MouseLeft})
	mouse(hs, tea.MouseMotionMsg{X: 13, Y: row + 1, Button: tea.MouseLeft})
	mouse(hs, tea.MouseReleaseMsg{X: 13, Y: row + 1, Button: tea.MouseLeft})
	deadline := time.Now().Add(3 * time.Second)
	for clip.get() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := clip.get(); got != "beta gamma\ndelta epsilon" {
		t.Errorf("copied %q", got)
	}
	hs.WaitForText("Copied to clipboard", 3*time.Second)
}

func TestVTFullscreenClickExpands(t *testing.T) {
	hs, _ := startFullscreen(t, 60, 20, nil)
	hs.SendMsg(addMsg{items: []*ext.Item{textItem("t1", ext.ToolKey("Bash"), "⏺ Bash(ls)\n  ⎿ 3 files")}})
	hs.WaitForText("3 files", 3*time.Second)
	row := -1
	for i, l := range hs.ScreenLines() {
		if strings.Contains(l, "Bash(ls)") {
			row = i
		}
	}
	mouse(hs, tea.MouseMotionMsg{X: 3, Y: row})
	hs.WaitForText("click to expand", 3*time.Second)
	mouse(hs, tea.MouseClickMsg{X: 3, Y: row, Button: tea.MouseLeft})
	mouse(hs, tea.MouseReleaseMsg{X: 3, Y: row, Button: tea.MouseLeft})
	hs.WaitForText("(expanded details)", 3*time.Second)
}

func TestVTFullscreenSearch(t *testing.T) {
	hs, _ := startFullscreen(t, 60, 12, nil)
	hs.SendMsg(addMsg{items: []*ext.Item{textItem("a1", ext.KeyAssistantText, numbered("row", 30)+"\nneedle here")}})
	hs.WaitForText("needle here", 3*time.Second)
	// Click into the transcript to focus it, then search for an early row.
	mouse(hs, tea.MouseClickMsg{X: 1, Y: 2, Button: tea.MouseLeft})
	mouse(hs, tea.MouseReleaseMsg{X: 1, Y: 2, Button: tea.MouseLeft})
	hs.Type("/row 03")
	hs.WaitForText("/row 03", 3*time.Second)
	hs.Send("enter")
	hs.WaitForText("1 of 1", 3*time.Second)
	if !strings.Contains(hs.Screen(), "row 03") {
		t.Errorf("search should scroll to the match:\n%s", hs.Screen())
	}
	hs.Send("esc") // clear the search
	hs.Send("esc") // back to the prompt
	hs.WaitFor(func(s string) bool { return !strings.Contains(s, "1 of 1") }, 3*time.Second)
}

func TestInlineLayoutDrawsNothing(t *testing.T) {
	ctx := exttest.NewCtx()
	v := newTranscriptView(terminal.Map{})
	v.Init(ctx)
	if v.View(ctx, ext.Area{Width: 80, MaxHeight: 10}).Text != "" {
		t.Error("the viewport is fullscreen-only")
	}
	if handled, _ := v.action(ext.ActScrollPageUp)(ctx); handled {
		t.Error("scroll actions decline in the inline layout")
	}
}

func TestScrollSpeed(t *testing.T) {
	ctx := exttest.NewCtx()
	env := terminal.Map{}
	if scrollSpeed(ctx.Settings(), env) != 3 {
		t.Error("default")
	}
	exttest.Exec(scrollSpeedCommand(env)(ctx, "7"))
	if scrollSpeed(ctx.Settings(), env) != 7 {
		t.Errorf("set = %v", ctx.SettingsV.MantleM)
	}
	scrollSpeedCommand(env)(ctx, "99")
	if scrollSpeed(ctx.Settings(), env) != 7 || !strings.Contains(ctx.Notices[len(ctx.Notices)-1].Text, "1 to 20") {
		t.Error("out of range rejected")
	}
	if scrollSpeed(ctx.Settings(), terminal.Map{"CLAUDE_CODE_SCROLL_SPEED": "5"}) != 5 {
		t.Error("env wins")
	}
}
