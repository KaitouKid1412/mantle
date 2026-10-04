package fullscreen

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// TestVTFullscreenGoldens renders the whole alt screen at the plan's three sizes: the
// transcript scrolled up (sticky header and jump-to-bottom pill), a selection, and the
// left sidebar layout. go test ./features/fullscreen -run Goldens -update rewrites them.
func TestVTFullscreenGoldens(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {200, 60}} {
		w, h := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			st := &store{}
			host := app.NewHost([]ext.Feature{testFeature(st), fullscreenFeature(t), notesPaneFeature()},
				app.HostOptions{Core: app.CoreFeatures()})
			root := app.New(app.Options{Host: host, NoBackgroundQuery: true, Layout: ext.Fullscreen,
				Settings: exttest.NewSettings(nil)})
			hs := testkit.New(t, root, testkit.WithSize(w, h))
			hs.WaitForText("type here", 5*time.Second)
			hs.SendMsg(addMsg{items: []*ext.Item{
				textItem("u1", ext.KeyUserPrompt, "❯ summarise the build log"),
				textItem("a1", ext.KeyAssistantText, numbered("log line", h*2)),
			}})
			hs.WaitForText(fmt.Sprintf("log line %02d", h*2), 3*time.Second)

			// Scroll up one page, then let new output arrive: header + pill.
			last := fmt.Sprintf("log line %02d", h*2)
			hs.Send("pageup")
			hs.WaitFor(func(s string) bool { return !strings.Contains(s, last) }, 3*time.Second)
			hs.SendMsg(addMsg{items: []*ext.Item{textItem("a2", ext.KeyAssistantText, "done.")}})
			hs.WaitForText("new lines", 3*time.Second)

			// Select two words on the first visible log line.
			row := -1
			for i, l := range hs.ScreenLines() {
				if strings.Contains(l, "log line") {
					row = i
					break
				}
			}
			x := app.SidebarWidth(w) + 1
			mouse(hs, tea.MouseClickMsg{X: x, Y: row, Button: tea.MouseLeft})
			mouse(hs, tea.MouseMotionMsg{X: x + 7, Y: row, Button: tea.MouseLeft})
			hs.Settle(100*time.Millisecond, time.Second)
			testkit.RequireGolden(t, hs.Styled())
		})
	}
}
