package input

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// fsBox is a plain component for the fullscreen host tests (a transcript or the
// chrome lines around the prompt).
type fsBox struct{ id, text string }

func (b *fsBox) ID() string                          { return b.id }
func (b *fsBox) Init(ext.Ctx) tea.Cmd                { return nil }
func (b *fsBox) Update(ext.Ctx, tea.Msg) tea.Cmd     { return nil }
func (b *fsBox) View(ext.Ctx, ext.Area) ext.Rendered { return ext.Rendered{Text: b.text} }

// startFullscreen runs the real host in the fullscreen layout with this feature, a
// transcript that fills the screen, an effort line above the prompt and a footer.
func startFullscreen(t *testing.T, w, h int) (*testkit.Harness, *state) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	s := newState()
	s.histPath = filepath.Join(dir, "history.jsonl")
	s.cwd = "/work/demo"
	var live []string
	for i := 1; i <= 80; i++ {
		live = append(live, fmt.Sprintf("  transcript line %d", i))
	}
	others := ext.Feature{ID: "test.chrome", Order: 10, Setup: func(r ext.Registrar) error {
		r.AddComponent(ext.SlotLive, &fsBox{id: "test.live", text: strings.Join(live, "\n")}, ext.SlotOpts{})
		r.AddComponent(ext.SlotAboveInput, &fsBox{id: "test.effort", text: strings.Repeat(" ", 60) + "effort"}, ext.SlotOpts{Weight: 1000, MaxHeight: 1})
		r.AddComponent(ext.SlotBelowInput, &fsBox{id: "test.footer", text: "  footer"}, ext.SlotOpts{})
		return nil
	}}
	f := ext.Feature{ID: FeatureID, Order: 400, Setup: func(r ext.Registrar) error { register(r, s); return nil }}
	host := app.NewHost([]ext.Feature{others, f}, app.HostOptions{Core: app.CoreFeatures()})
	root := app.New(app.Options{Host: host, NoBackgroundQuery: true, StateDir: t.TempDir(), Layout: ext.Fullscreen, NoMouse: true})
	hs := testkit.New(t, root, testkit.WithSize(w, h))
	hs.SendMsg(ext.EngineAttachMsg{EngineID: ext.MainEngine, Engine: &fakeEngine{}})
	var cmds []ext.Command
	for _, n := range []string{"add-dir", "autocompact", "background", "branch", "btw", "bug", "cd", "clear", "color", "compact", "config", "context", "copy", "cost", "diff"} {
		desc := "Run /" + n
		if n == "btw" {
			desc = "Ask a quick side question without interrupting the main conversation, which wraps onto a second row"
		}
		cmds = append(cmds, ext.Command{Name: n, Description: desc, Source: ext.SourceEngine})
	}
	hs.SendMsg(ext.CommandsMsg{Source: ext.SourceEngine, EngineID: ext.MainEngine, Commands: cmds})
	hs.WaitForText("❯", vtWait)
	return hs, s
}

// promptRow is the screen row of the prompt.
func promptRow(hs *testkit.Harness) int {
	for i, l := range hs.ScreenLines() {
		if strings.HasPrefix(l, "❯ ") || l == "❯" {
			return i
		}
	}
	return -1
}

// TestFullscreenSlashMenuOverlay: in fullscreen the / menu is drawn over the rows
// directly above the prompt (claude's fullscreen overlay): at most five items in five
// rows, scrolling with the selection, and the prompt never moves.
func TestFullscreenSlashMenuOverlay(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			hs, _ := startFullscreen(t, size[0], size[1])
			closed := hs.ScreenLines()
			row := promptRow(hs)
			if row != size[1]-2 {
				t.Fatalf("prompt at row %d, want %d:\n%s", row, size[1]-2, hs.Screen())
			}

			hs.Type("/")
			hs.WaitForText("/autocompact", vtWait)
			open := hs.ScreenLines()
			if got := promptRow(hs); got != row {
				t.Fatalf("the prompt moved from row %d to %d:\n%s", row, got, hs.Screen())
			}
			menu := open[row-4 : row]
			if !strings.HasPrefix(menu[0], "  ❯ /add-dir") || !strings.HasPrefix(menu[3], "    /branch") {
				t.Fatalf("menu not directly above the prompt:\n%s", hs.Screen())
			}
			if strings.Contains(open[row-5], "/btw") || !strings.Contains(open[row-5], "transcript line") {
				t.Fatalf("the menu must stay within five rows (btw wraps to two):\n%s", hs.Screen())
			}
			for i := 0; i < row-4; i++ {
				if open[i] != closed[i] {
					t.Fatalf("row %d above the menu changed:\n%q\n%q", i, closed[i], open[i])
				}
			}
			if open[row+1] != closed[row+1] || !strings.Contains(open[row+1], "footer") {
				t.Fatalf("the footer row must stay: %q", open[row+1])
			}
			testkit.RequireGoldenLines(t, open)

			// Scrolling keeps the selection two items from the top.
			hs.Send("down", "down", "down", "down", "down", "down")
			hs.WaitFor(func(s string) bool { return strings.Contains(s, "❯ /cd") }, vtWait)
			if got := promptRow(hs); got != row {
				t.Fatalf("the prompt moved while scrolling:\n%s", hs.Screen())
			}
			lines := hs.ScreenLines()
			if !strings.HasPrefix(lines[row-4], "    /btw") && !strings.HasPrefix(lines[row-5], "    /btw") {
				t.Fatalf("window around /cd:\n%s", hs.Screen())
			}

			hs.Send("escape")
			hs.WaitFor(func(s string) bool { return !strings.Contains(s, "/autocompact") }, vtWait)
			after := hs.ScreenLines()
			if promptRow(hs) != row {
				t.Fatalf("the prompt moved on close:\n%s", hs.Screen())
			}
			for i := 0; i < row; i++ {
				if after[i] != closed[i] {
					t.Fatalf("row %d not restored after esc:\n%q\n%q", i, closed[i], after[i])
				}
			}
		})
	}
}

// TestFullscreenNoMatchAndFiles: "No commands match" and @ suggestions use the same
// overlay.
func TestFullscreenNoMatchAndFiles(t *testing.T) {
	hs, _ := startFullscreen(t, 80, 24)
	row := promptRow(hs)
	hs.Type("/zzz")
	hs.WaitForText(`No commands match "/zzz"`, vtWait)
	lines := hs.ScreenLines()
	if promptRow(hs) != row || !strings.HasPrefix(lines[row-1], `  No commands match "/zzz"`) {
		t.Fatalf("no-match line above the prompt:\n%s", hs.Screen())
	}
}
